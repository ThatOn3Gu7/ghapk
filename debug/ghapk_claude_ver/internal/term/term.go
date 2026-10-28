package term

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Options controls what Start turns on.
type Options struct {
	Mouse bool
}

// Terminal owns the controlling terminal for the lifetime of the TUI: raw
// mode, the alternate screen, mouse reporting and bracketed paste are switched
// on by Start and every one of them is undone by Close, so the shell is handed
// back exactly as it was found (scrollback included, nothing cleared).
type Terminal struct {
	in      *os.File
	out     *os.File
	w       *bufio.Writer
	saved   *termState
	started bool
	opts    Options
	Events  chan Event
	stop    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
	Render  *Renderer
}

// Open attaches to the controlling terminal. It prefers /dev/tty so the TUI
// works even when stdin or stdout is redirected.
func Open() (*Terminal, error) {
	var in, out *os.File
	if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		in, out = f, f
	} else if isTTY(os.Stdin) && isTTY(os.Stdout) {
		in, out = os.Stdin, os.Stdout
	} else {
		return nil, errors.New("no terminal available: ghapk's interface needs one (try the --plain flags for scripts)")
	}
	t := &Terminal{in: in, out: out, w: bufio.NewWriterSize(out, 64*1024),
		Events: make(chan Event, 256), stop: make(chan struct{})}
	t.Render = NewRenderer(t.w, DetectDepth())
	return t, nil
}

// DetectDepth works out the colour depth from the environment.
func DetectDepth() Depth {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return DepthNone
	}
	ct := os.Getenv("COLORTERM")
	if ct == "truecolor" || ct == "24bit" || os.Getenv("TERMUX_VERSION") != "" {
		return DepthTrue
	}
	switch term := os.Getenv("TERM"); {
	case contains(term, "256color"):
		if contains(os.Getenv("TERM_PROGRAM"), "iTerm") || os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("WT_SESSION") != "" {
			return DepthTrue
		}
		return Depth256
	case term == "linux" || term == "vt100" || term == "ansi":
		return Depth16
	}
	return Depth256
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Size is the terminal's current width and height in cells.
func (t *Terminal) Size() (w, h int) {
	w, h, err := termSize(t.out)
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// Start enters the TUI: raw mode, alternate screen, hidden cursor, mouse,
// bracketed paste, and starts delivering Events.
func (t *Terminal) Start(o Options) error {
	st, err := makeRaw(t.in)
	if err != nil {
		return err
	}
	t.saved, t.opts, t.started = st, o, true
	seq := "\x1b[?1049h" + // alternate screen: the shell's screen is kept untouched underneath
		"\x1b[?25l" + // hide cursor
		"\x1b[?7l" + // no autowrap: drawing the last cell must not scroll
		"\x1b[?2004h" // bracketed paste
	if o.Mouse {
		seq += "\x1b[?1000h\x1b[?1002h\x1b[?1006h" // press/release, drag, SGR coordinates
	}
	seq += "\x1b[2J\x1b[H"
	if _, err := t.w.WriteString(seq); err != nil {
		return err
	}
	if err := t.w.Flush(); err != nil {
		return err
	}
	t.wg.Add(2)
	go t.readLoop()
	go t.resizeLoop()
	return nil
}

// Flush draws a buffer.
func (t *Terminal) Flush(b *Buffer) error {
	if err := t.Render.Flush(b); err != nil {
		return err
	}
	return t.w.Flush()
}

// Raw writes escape sequences straight to the terminal (e.g. OSC 52 copy).
func (t *Terminal) Raw(s string) {
	t.w.WriteString(s)
	t.w.Flush()
}

// SetMouse turns mouse reporting on or off while running.
func (t *Terminal) SetMouse(on bool) {
	if !t.started || on == t.opts.Mouse {
		return
	}
	t.opts.Mouse = on
	if on {
		t.Raw("\x1b[?1000h\x1b[?1002h\x1b[?1006h")
	} else {
		t.Raw("\x1b[?1006l\x1b[?1002l\x1b[?1000l")
	}
}

// Invalidate makes the next Flush repaint everything (after a resize or ^L).
func (t *Terminal) Invalidate() { t.Render.Invalidate() }

// SetTitle sets the window title (OSC 2).
func (t *Terminal) SetTitle(s string) { t.Raw("\x1b]2;" + sanitize(s) + "\x07") }

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	return string(out)
}

// Close undoes everything Start did. It is safe to call more than once and
// from a panic handler.
func (t *Terminal) Close() {
	t.once.Do(func() {
		close(t.stop)
		if !t.started {
			return
		}
		seq := "\x1b[0m"
		if t.opts.Mouse {
			seq += "\x1b[?1006l\x1b[?1002l\x1b[?1000l"
		}
		seq += "\x1b[?2004l\x1b[?7h\x1b[?25h\x1b[?1049l"
		t.w.WriteString(seq)
		t.w.Flush()
		if t.saved != nil {
			restore(t.in, t.saved)
		}
		if t.in != os.Stdin {
			// unblock the reader goroutine; it exits on the stop channel
			t.in.SetReadDeadline(time.Now())
		}
		done := make(chan struct{})
		go func() { t.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(300 * time.Millisecond):
		}
		if t.in != os.Stdin {
			t.in.Close()
		}
	})
}

func (t *Terminal) readLoop() {
	defer t.wg.Done()
	var p Parser
	chunks := make(chan []byte, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := t.in.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				select {
				case chunks <- c:
				case <-t.stop:
					return
				}
			}
			if err != nil {
				if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
					close(chunks)
					return
				}
				select {
				case <-t.stop:
					close(chunks)
					return
				default:
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
	emit := func(evs []Event) {
		for _, e := range evs {
			select {
			case t.Events <- e:
			case <-t.stop:
				return
			}
		}
	}
	for {
		var timeout <-chan time.Time
		if p.Pending() {
			timeout = time.After(30 * time.Millisecond) // a lone ESC is the Esc key
		}
		select {
		case c, ok := <-chunks:
			if !ok {
				return
			}
			emit(p.Feed(c))
		case <-timeout:
			emit(p.Flush())
		case <-t.stop:
			return
		}
	}
}

func (t *Terminal) resizeLoop() {
	defer t.wg.Done()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	for {
		select {
		case <-ch:
			w, h := t.Size()
			select {
			case t.Events <- Event{Kind: EvResize, W: w, H: h}:
			case <-t.stop:
				return
			}
		case <-t.stop:
			return
		}
	}
}
