package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger keeps the last few hundred log lines in memory (for the Log tab) and
// appends them to a file under the cache directory.
type Logger struct {
	mu    sync.Mutex
	lines []LogLine
	file  *os.File
	onAdd func()
}

// LogLine is one entry.
type LogLine struct {
	At   time.Time
	Text string
}

// NewLogger opens (and trims) the log file. Failure to open it is not an error:
// the in-memory log still works.
func NewLogger(cacheDir string) *Logger {
	l := &Logger{}
	dir := filepath.Join(cacheDir, "logs")
	if os.MkdirAll(dir, 0o700) == nil {
		p := filepath.Join(dir, "ghapk.log")
		if st, err := os.Stat(p); err == nil && st.Size() > 512*1024 {
			_ = os.Rename(p, p+".1")
		}
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			l.file = f
		}
	}
	return l
}

// Path is where the log file lives ("" if it could not be opened).
func (l *Logger) Path() string {
	if l == nil || l.file == nil {
		return ""
	}
	return l.file.Name()
}

// OnAdd registers a callback fired after each line (used to redraw the Log tab).
func (l *Logger) OnAdd(f func()) { l.mu.Lock(); l.onAdd = f; l.mu.Unlock() }

// Logf records one line.
func (l *Logger) Logf(format string, a ...any) {
	if l == nil {
		return
	}
	txt := fmt.Sprintf(format, a...)
	now := time.Now()
	l.mu.Lock()
	l.lines = append(l.lines, LogLine{now, txt})
	if len(l.lines) > 500 {
		l.lines = l.lines[len(l.lines)-400:]
	}
	if l.file != nil {
		fmt.Fprintf(l.file, "[%s] %s\n", now.Format("15:04:05"), txt)
	}
	cb := l.onAdd
	l.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// Lines returns a copy of the in-memory log.
func (l *Logger) Lines() []LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogLine(nil), l.lines...)
}

// Close flushes and closes the file.
func (l *Logger) Close() {
	if l != nil && l.file != nil {
		l.file.Close()
	}
}
