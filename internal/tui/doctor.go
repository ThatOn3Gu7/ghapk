package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
)

// DoctorState holds the health-check report.
type DoctorState struct {
	Rows   []doctorRow
	offset int
	ready  bool
}

type doctorRow struct {
	kind string // ok warn err skip
	name string
	info string
}

type DoctorDoneMsg struct{ Rows []doctorRow }

func (m *Model) openDoctor() tea.Cmd {
	m.doctor = &DoctorState{}
	m.modal = ModalDoctor
	repo := m.settings.Repo
	return func() tea.Msg {
		var rows []doctorRow
		add := func(kind, name, info string) {
			rows = append(rows, doctorRow{kind, name, info})
		}

		if ghapi.Available() {
			add("ok", "gh", ghapi.Version())
		} else {
			add("err", "gh", "not installed — required for GitHub API access")
		}
		if u, err := ghapi.AuthStatus(); err == nil {
			add("ok", "auth", "@"+u)
		} else {
			add("err", "auth", "not logged in — run: gh auth login")
		}

		info := device.Probe()
		if info.OS == "android" {
			add("ok", "device", fmt.Sprintf("%s · %s · sdk %d", info.Model, strings.Join(info.ABIs, ","), info.SDK))
		} else {
			add("warn", "device", info.Word()+" — downloads work, installing needs adb or manual transfer")
		}
		switch info.InstallVia {
		case "termux-open":
			add("ok", "installer", "termux-open (Android package installer prompt)")
		case "am":
			add("ok", "installer", "am start (Android VIEW intent prompt)")
		case "adb":
			if info.ADBConnected {
				add("ok", "installer", "adb (device connected)")
			} else {
				add("warn", "installer", "adb (no device attached — enable USB/wireless debugging)")
			}
		default:
			add("err", "installer", "no Android installer found (install adb on desktop or termux-tools on Termux)")
		}
		if info.HasRish {
			add("ok", "shizuku", "rish connected")
		}

		if exec := os.Getenv("PREFIX"); exec != "" && strings.Contains(exec, "termux") {
			add("ok", "termux", exec)
		}

		// cache dir writable?
		dir := config.CacheDir()
		if err := os.MkdirAll(filepath.Join(dir, "apk"), 0o755); err == nil {
			if tf, err := os.CreateTemp(dir, ".probe"); err == nil {
				_ = tf.Close()
				_ = os.Remove(tf.Name())
				add("ok", "cache", dir)
			} else {
				add("err", "cache", dir+": not writable")
			}
		}

		if repo != "" {
			if r, err := ghapi.GetRepo(repo); err == nil {
				add("ok", "repo", r.FullName)
			} else {
				add("err", "repo "+repo, "not reachable — private? typo? token scope?")
			}
		} else {
			add("skip", "repo", "none selected yet (press R)")
		}

		add("ok", "runtime", fmt.Sprintf("go %s · %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH))
		return DoctorDoneMsg{Rows: rows}
	}
}

func (d *DoctorState) scroll(n int) {
	d.offset += n
	if d.offset < 0 {
		d.offset = 0
	}
	max := len(d.Rows) - 12
	if max < 0 {
		max = 0
	}
	if d.offset > max {
		d.offset = max
	}
}

func (d DoctorState) view(m *Model) string {
	st := m.st
	if !d.ready {
		return m.spin.View() + " " + st.Text.Render("running checks…")
	}
	var b strings.Builder
	for _, r := range d.Rows {
		var icon string
		switch r.kind {
		case "ok":
			icon = st.OK.Render(st.G.OK)
		case "warn":
			icon = st.Warn.Render("!")
		case "err":
			icon = st.Err.Render(st.G.Fail)
		case "skip":
			icon = st.Dim.Render(st.G.Skipped)
		}
		b.WriteString(fmt.Sprintf(" %s  %-14s %s\n", icon, r.name, st.Muted.Render(r.info)))
	}
	b.WriteString("\n" + st.Dim.Render("r re-run · esc close") + "\n")
	return b.String()
}

var _ = time.Now
