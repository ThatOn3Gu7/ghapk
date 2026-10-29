// Package activity records local, bounded, privacy-conscious activity history.
package activity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const DefaultMaxBytes int64 = 1 << 20

type Entry struct {
	At      time.Time `json:"at"`
	Session string    `json:"session"`
	Kind    string    `json:"kind"`
	Text    string    `json:"text"`
}
type Store struct {
	mu            sync.Mutex
	path, session string
	max           int64
	entries       []Entry
	size          int64
	modified      time.Time
	version       uint64
	err           error
}

// Open loads the previous sessions and starts a new one. Failures are returned
// to the caller so the interface can warn rather than silently losing logs.
func Open(dir string, max int64) (*Store, error) {
	if max < 4096 {
		max = DefaultMaxBytes
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "activity.log"), session: fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405.000000000"), os.Getpid()), max: max}
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return nil, err
	}
	err = s.reload()
	if err == nil {
		err = s.repairTail()
	}
	unlockFile(lock)
	if err != nil {
		return nil, err
	}
	if err := s.Write("session", "Session started"); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}
func (s *Store) Error() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
func (s *Store) Close() error { return s.Write("session", "Session ended") }

func (s *Store) reload() error {
	fi, err := os.Lstat(s.path)
	if os.IsNotExist(err) {
		if s.size != 0 || len(s.entries) != 0 {
			s.version++
		}
		s.entries = nil
		s.size = 0
		s.modified = time.Time{}
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("activity log is not a regular file")
	}
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Existing oversized files are read from the tail only.
	if fi.Size() > s.max {
		_, err = f.Seek(fi.Size()-s.max, io.SeekStart)
		if err != nil {
			return err
		}
		br := bufio.NewReader(f)
		_, _ = br.ReadString('\n')
		return s.readEntries(br, fi)
	}
	return s.readEntries(f, fi)
}
func (s *Store) readEntries(r io.Reader, fi os.FileInfo) error {
	var entries []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), int(s.max)+4096)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && !e.At.IsZero() {
			entries = append(entries, e)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	s.entries = entries
	s.version++
	s.size = fi.Size()
	s.modified = fi.ModTime()
	return nil
}

// refresh checks for other sessions, taking their lock before reading or
// repairing a potentially incomplete append. The caller holds s.mu.
func (s *Store) refresh() {
	fi, err := os.Lstat(s.path)
	if err == nil && fi.Size() == s.size && fi.ModTime().Equal(s.modified) && fi.Mode().IsRegular() {
		return
	}
	if os.IsNotExist(err) && s.size == 0 {
		return
	}
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		s.err = err
		return
	}
	defer unlockFile(lock)
	if err = s.reload(); err == nil {
		err = s.repairTail()
	}
	if err != nil {
		s.err = err
	}
}
func (s *Store) Entries() []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return append([]Entry(nil), s.entries...)
}

// Version changes whenever entries are appended or reloaded from another session.
func (s *Store) Version() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	return s.version
}

func (s *Store) Write(kind, text string) error {
	if s == nil {
		return nil
	}
	e := Entry{At: time.Now(), Session: s.session, Kind: clean(kind, 48), Text: clean(Redact(text), 3072)}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// JSON escaping can expand a record beyond a small configured cap.
	for int64(len(b)) > s.max {
		r := []rune(e.Text)
		if len(r) == 0 {
			return fmt.Errorf("activity record exceeds limit")
		}
		e.Text = string(r[:len(r)/2])
		b, err = json.Marshal(e)
		if err != nil {
			return err
		}
		b = append(b, '\n')
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		s.err = err
		return err
	}
	defer unlockFile(lock)
	if fi, e := os.Lstat(s.path); e == nil && !fi.Mode().IsRegular() {
		s.err = fmt.Errorf("activity log is not a regular file")
		return s.err
	} else if e != nil && !os.IsNotExist(e) {
		s.err = e
		return e
	}
	fi, err := os.Stat(s.path)
	if err != nil && !os.IsNotExist(err) {
		s.err = err
		return err
	}
	if os.IsNotExist(err) && s.size != 0 {
		s.entries = nil
		s.size = 0
		s.modified = time.Time{}
		s.version++
	} else if err == nil && (fi.Size() != s.size || !fi.ModTime().Equal(s.modified)) {
		if err = s.reload(); err == nil {
			err = s.repairTail()
		}
		if err != nil {
			s.err = err
			return err
		}
	}
	if s.size+int64(len(b)) > s.max {
		if err = s.compact(int(s.max*3/4) - len(b)); err != nil {
			s.err = err
			return err
		}
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		s.err = err
		return err
	}
	_ = f.Chmod(0600)
	_, err = f.Write(b)
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		s.err = err
		return err
	}
	s.entries = append(s.entries, e)
	s.version++
	if fi, err = os.Stat(s.path); err == nil {
		s.size = fi.Size()
		s.modified = fi.ModTime()
	}
	s.err = nil
	return nil
}

// Recover a partial final record left by a crash before appending more data.
// Called while holding the cross-process lock, after loading valid records.
func (s *Store) repairTail() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	if fi.Size() == 0 {
		return f.Close()
	}
	var tail [1]byte
	_, err = f.ReadAt(tail[:], fi.Size()-1)
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if tail[0] != '\n' {
		return s.compact(int(s.max * 3 / 4))
	}
	return nil
}

func (s *Store) compact(budget int) error {
	var kept [][]byte
	size := 0
	for i := len(s.entries) - 1; i >= 0; i-- {
		b, e := json.Marshal(s.entries[i])
		if e != nil {
			continue
		}
		b = append(b, '\n')
		if size+len(b) > budget {
			break
		}
		kept = append(kept, b)
		size += len(b)
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".activity-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	for i := len(kept) - 1; i >= 0; i-- {
		if _, err = f.Write(kept[i]); err != nil {
			f.Close()
			return err
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, s.path); err != nil {
		return err
	}
	if len(kept) == 0 {
		s.entries = nil
	} else {
		s.entries = s.entries[len(s.entries)-len(kept):]
	}
	s.size = int64(size)
	return nil
}
func clean(v string, limit int) string {
	v = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	v = strings.Join(strings.Fields(v), " ")
	if len(v) <= limit {
		return v
	}
	b := []byte(v[:limit])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b) + "..."
}

var active struct {
	sync.RWMutex
	store *Store
}

func SetActive(s *Store) { active.Lock(); active.store = s; active.Unlock() }
func Active() *Store     { active.RLock(); defer active.RUnlock(); return active.store }
func Log(kind, text string) {
	if s := Active(); s != nil {
		_ = s.Write(kind, text)
	}
}
