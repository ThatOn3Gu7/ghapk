package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"

	"github.com/ghapk/ghapk/internal/activity"
)

type deviceChecks struct {
	setup      device.Setup
	setupOK    bool
	rate       ghapi.RateLimit
	rateOK     bool
	cacheBytes int64
	cacheOK    bool
	freeBytes  int64
	freeOK     bool
}
type deviceChecksMsg struct {
	seq    int
	checks deviceChecks
}

// Probes run off the UI thread; slow/offline GitHub and inaccessible files are
// shown as unknown, never as a successful check.
func deviceChecksCmd(seq int, in device.Info) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		type result struct {
			kind       int
			setup      device.Setup
			setupOK    bool
			rate       ghapi.RateLimit
			size, free int64
			ok, freeOK bool
		}
		ch := make(chan result, 3)
		go func() { ch <- result{kind: 0, setup: device.ProbeSetupContext(ctx, in)} }()
		go func() { r, e := ghapi.CoreRateLimit(ctx); ch <- result{kind: 1, rate: r, ok: e == nil} }()
		go func() {
			size, ok := cacheSize(ctx, config.CacheDir())
			free, fok := cacheFree(ctx, config.CacheDir())
			ch <- result{kind: 2, size: size, ok: ok, free: free, freeOK: fok}
		}()
		d := deviceChecks{}
		for i := 0; i < 3; i++ {
			select {
			case r := <-ch:
				switch r.kind {
				case 0:
					d.setup = r.setup
					d.setupOK = true
				case 1:
					d.rate, d.rateOK = r.rate, r.ok
				case 2:
					d.cacheBytes, d.cacheOK, d.freeBytes, d.freeOK = r.size, r.ok, r.free, r.freeOK
				}
			case <-ctx.Done():
				return deviceChecksMsg{seq, d}
			}
		}
		return deviceChecksMsg{seq, d}
	}
}

func cacheSize(ctx context.Context, root string) (int64, bool) {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return 0, true
	} else if err != nil {
		return 0, false
	}
	// Follow an explicitly configured cache-root symlink, but never recurse
	// through symlinks inside the cache.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return 0, false
	}
	var size int64
	count := 0
	err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		count++
		if count > 100000 {
			return errors.New("cache too large to scan")
		}
		if entry.Type().IsRegular() {
			info, e := entry.Info()
			if e != nil {
				return e
			}
			size += info.Size()
		}
		return nil
	})
	return size, err == nil
}

func cacheFree(ctx context.Context, path string) (int64, bool) {
	// The cache may not exist yet; use its nearest existing parent filesystem.
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	for {
		st, err := os.Stat(path)
		if err == nil && st.IsDir() {
			break
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, false
		}
		next := filepath.Dir(path)
		if next == path {
			return 0, false
		}
		path = next
	}
	cmd := activity.CommandContext(ctx, "df", "-Pk", path)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * 1024, true
}

type deviceHistoryMsg struct {
	seq   int
	repo  string
	entry config.HistoryEntry
	ok    bool
}

func deviceHistoryCmd(repo string, seq int) tea.Cmd {
	return func() tea.Msg { h, ok := config.History(repo); return deviceHistoryMsg{seq, repo, h, ok} }
}

func (m *Model) ensureDeviceHistory() tea.Cmd {
	if m.screen != ScreenDashboard || m.focus != PanelDevice || m.settings.Repo == "" {
		return nil
	}
	if m.deviceHistoryLoading && m.deviceHistoryRepo == m.settings.Repo {
		return nil
	}
	if m.deviceHistoryLoaded && m.deviceHistoryRepo == m.settings.Repo {
		return nil
	}
	m.deviceHistoryRepo = m.settings.Repo
	m.deviceHistoryLoading = true
	m.deviceHistoryLoaded = false
	m.deviceHistorySeq++
	return deviceHistoryCmd(m.settings.Repo, m.deviceHistorySeq)
}
