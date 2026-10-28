package term

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Key identifies a non-text key.
type Key uint16

// Keys. KeyRune means "a printable character; see Event.Rune".
const (
	KeyNone Key = iota
	KeyRune
	KeyEnter
	KeyTab
	KeyBacktab
	KeyBackspace
	KeyDelete
	KeyInsert
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Mod is a set of modifier keys.
type Mod uint8

// Modifiers.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
)

// EventKind says which fields of an Event are meaningful.
type EventKind uint8

// Event kinds.
const (
	EvKey EventKind = iota + 1
	EvMouse
	EvPaste
	EvResize
)

// MouseButton identifies a mouse button or wheel direction.
type MouseButton uint8

// Mouse buttons.
const (
	BtnNone MouseButton = iota
	BtnLeft
	BtnMiddle
	BtnRight
	BtnWheelUp
	BtnWheelDown
)

// MouseAction is what the mouse did.
type MouseAction uint8

// Mouse actions.
const (
	MousePress MouseAction = iota + 1
	MouseRelease
	MouseDrag
	MouseWheel
)

// Event is one thing the user did.
type Event struct {
	Kind   EventKind
	Key    Key
	Rune   rune
	Mod    Mod
	X, Y   int // mouse position, 0-based
	Button MouseButton
	Action MouseAction
	Text   string // pasted text
	W, H   int    // new size, for EvResize
}

// Is reports whether the event is the given rune key with no Ctrl/Alt.
func (e Event) Is(r rune) bool {
	return e.Kind == EvKey && e.Key == KeyRune && e.Rune == r && e.Mod&(ModCtrl|ModAlt) == 0
}

// IsCtrl reports whether the event is Ctrl+letter.
func (e Event) IsCtrl(r rune) bool {
	return e.Kind == EvKey && e.Key == KeyRune && e.Rune == r && e.Mod&ModCtrl != 0
}

// Parser turns the raw byte stream into events. It is a pure state machine:
// feed it bytes, it returns the events it could complete and keeps any partial
// escape sequence until more bytes arrive (or Flush is called on a timeout).
type Parser struct {
	buf     []byte
	inPaste bool
	paste   []byte
}

// Feed adds bytes and returns the completed events.
func (p *Parser) Feed(b []byte) []Event {
	p.buf = append(p.buf, b...)
	return p.drain(false)
}

// Pending reports whether an incomplete sequence is buffered.
func (p *Parser) Pending() bool { return len(p.buf) > 0 }

// Flush resolves any buffered bytes as final: a lone ESC becomes the Esc key.
func (p *Parser) Flush() []Event { return p.drain(true) }

func (p *Parser) drain(final bool) []Event {
	var out []Event
	for len(p.buf) > 0 {
		if p.inPaste {
			i := indexOf(p.buf, "\x1b[201~")
			if i < 0 {
				// keep a possible partial terminator at the tail
				keep := partialSuffix(p.buf, "\x1b[201~")
				p.paste = append(p.paste, p.buf[:len(p.buf)-keep]...)
				p.buf = p.buf[len(p.buf)-keep:]
				if final {
					p.buf = nil
				}
				break
			}
			p.paste = append(p.paste, p.buf[:i]...)
			p.buf = p.buf[i+len("\x1b[201~"):]
			out = append(out, Event{Kind: EvPaste, Text: string(p.paste)})
			p.paste, p.inPaste = nil, false
			continue
		}
		ev, n, ok := p.parseOne(final)
		if !ok {
			break // need more bytes
		}
		p.buf = p.buf[n:]
		if ev.Kind != 0 {
			out = append(out, ev)
		}
	}
	return out
}

func indexOf(b []byte, s string) int { return strings.Index(string(b), s) }

func partialSuffix(b []byte, s string) int {
	for n := len(s) - 1; n > 0; n-- {
		if len(b) >= n && string(b[len(b)-n:]) == s[:n] {
			return n
		}
	}
	return 0
}

func key(k Key, m Mod) Event { return Event{Kind: EvKey, Key: k, Mod: m} }

func runeKey(r rune, m Mod) Event { return Event{Kind: EvKey, Key: KeyRune, Rune: r, Mod: m} }

// parseOne decodes one event from the front of the buffer. ok is false when the
// buffer holds only the start of a longer sequence.
func (p *Parser) parseOne(final bool) (Event, int, bool) {
	b := p.buf
	c := b[0]
	switch {
	case c == 0x1b:
		return p.parseEsc(final)
	case c == '\r' || c == '\n':
		return key(KeyEnter, 0), 1, true
	case c == '\t':
		return key(KeyTab, 0), 1, true
	case c == 0x7f || c == 0x08:
		return key(KeyBackspace, 0), 1, true
	case c == 0:
		return runeKey(' ', ModCtrl), 1, true
	case c < 0x20:
		return runeKey(rune('a'+c-1), ModCtrl), 1, true
	case c < 0x80:
		return runeKey(rune(c), 0), 1, true
	}
	if !utf8.FullRune(b) {
		if final {
			return Event{}, 1, true
		}
		return Event{}, 0, false
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n == 1 {
		return Event{}, 1, true
	}
	return runeKey(r, 0), n, true
}

func (p *Parser) parseEsc(final bool) (Event, int, bool) {
	b := p.buf
	if len(b) == 1 {
		if final {
			return key(KeyEsc, 0), 1, true
		}
		return Event{}, 0, false
	}
	switch b[1] {
	case '[':
		return p.parseCSI()
	case 'O': // SS3: application-mode arrows and F1-F4
		if len(b) < 3 {
			if final {
				return runeKey('o', ModAlt), 2, true
			}
			return Event{}, 0, false
		}
		m := map[byte]Key{'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd,
			'P': KeyF1, 'Q': KeyF2, 'R': KeyF3, 'S': KeyF4}
		if k, ok := m[b[2]]; ok {
			return key(k, 0), 3, true
		}
		return Event{}, 3, true
	case 0x1b: // ESC ESC: an Esc, then whatever follows
		return key(KeyEsc, 0), 1, true
	}
	// Alt + key
	if b[1] == 0x7f {
		return key(KeyBackspace, ModAlt), 2, true
	}
	if b[1] < 0x20 {
		if b[1] == '\r' {
			return key(KeyEnter, ModAlt), 2, true
		}
		return runeKey(rune('a'+b[1]-1), ModAlt|ModCtrl), 2, true
	}
	if !utf8.FullRune(b[1:]) {
		if final {
			return key(KeyEsc, 0), 1, true
		}
		return Event{}, 0, false
	}
	r, n := utf8.DecodeRune(b[1:])
	return runeKey(r, ModAlt), 1 + n, true
}

func (p *Parser) parseCSI() (Event, int, bool) {
	b := p.buf
	// find the final byte (0x40..0x7e) after parameter bytes
	i := 2
	for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
		i++
		if i > 40 {
			return Event{}, 2, true // garbage: drop "ESC ["
		}
	}
	if i >= len(b) {
		return Event{}, 0, false
	}
	params, fin := string(b[2:i]), b[i]
	n := i + 1

	if strings.HasPrefix(params, "<") && (fin == 'M' || fin == 'm') {
		return parseSGRMouse(params[1:], fin), n, true
	}
	if fin == '~' && params == "200" {
		p.inPaste = true
		return Event{}, n, true
	}
	nums := splitInts(params)
	mod := Mod(0)
	if len(nums) >= 2 && nums[1] > 1 {
		mod = modFromParam(nums[1])
	}
	switch fin {
	case 'A':
		return key(KeyUp, mod), n, true
	case 'B':
		return key(KeyDown, mod), n, true
	case 'C':
		return key(KeyRight, mod), n, true
	case 'D':
		return key(KeyLeft, mod), n, true
	case 'H':
		return key(KeyHome, mod), n, true
	case 'F':
		return key(KeyEnd, mod), n, true
	case 'Z':
		return key(KeyBacktab, ModShift), n, true
	case 'P', 'Q', 'R', 'S':
		return key(KeyF1+Key(fin-'P'), mod), n, true
	case '~':
		if len(nums) == 0 {
			return Event{}, n, true
		}
		m := map[int]Key{1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPgUp, 6: KeyPgDn, 7: KeyHome, 8: KeyEnd,
			11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5, 17: KeyF6, 18: KeyF7, 19: KeyF8, 20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12}
		if k, ok := m[nums[0]]; ok {
			return key(k, mod), n, true
		}
	case 'u': // kitty/fixterms: CSI code ; mods u
		if len(nums) >= 1 && nums[0] > 0 {
			if nums[0] == 27 {
				return key(KeyEsc, mod), n, true
			}
			if nums[0] == 13 {
				return key(KeyEnter, mod), n, true
			}
			if nums[0] == 9 {
				return key(KeyTab, mod), n, true
			}
			if nums[0] == 127 {
				return key(KeyBackspace, mod), n, true
			}
			return runeKey(rune(nums[0]), mod), n, true
		}
	}
	return Event{}, n, true // unknown sequence: swallow it
}

func modFromParam(v int) Mod {
	v--
	var m Mod
	if v&1 != 0 {
		m |= ModShift
	}
	if v&2 != 0 {
		m |= ModAlt
	}
	if v&4 != 0 {
		m |= ModCtrl
	}
	return m
}

func splitInts(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Split(s, ";") {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

func parseSGRMouse(params string, fin byte) Event {
	nums := splitInts(params)
	if len(nums) < 3 {
		return Event{}
	}
	b, x, y := nums[0], nums[1]-1, nums[2]-1
	ev := Event{Kind: EvMouse, X: x, Y: y}
	if b&4 != 0 {
		ev.Mod |= ModShift
	}
	if b&8 != 0 {
		ev.Mod |= ModAlt
	}
	if b&16 != 0 {
		ev.Mod |= ModCtrl
	}
	switch {
	case b&64 != 0:
		ev.Action = MouseWheel
		if b&3 == 0 {
			ev.Button = BtnWheelUp
		} else {
			ev.Button = BtnWheelDown
		}
	default:
		switch b & 3 {
		case 0:
			ev.Button = BtnLeft
		case 1:
			ev.Button = BtnMiddle
		case 2:
			ev.Button = BtnRight
		}
		switch {
		case fin == 'm':
			ev.Action = MouseRelease
		case b&32 != 0:
			ev.Action = MouseDrag
		default:
			ev.Action = MousePress
		}
	}
	return ev
}
