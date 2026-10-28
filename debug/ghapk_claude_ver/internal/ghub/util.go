package ghub

import (
	"io"
	"strings"
	"sync"
)

// limitedBuf keeps only the first 8 KiB written to it: enough to classify an
// error without letting a runaway stderr eat memory.
type limitedBuf struct {
	mu sync.Mutex
	b  []byte
}

func (l *limitedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if room := 8192 - len(l.b); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.b = append(l.b, p[:room]...)
	}
	return len(p), nil
}

func (l *limitedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.b)
}

func firstLine(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func stringsReader(s string) io.Reader { return strings.NewReader(s) }
