package device

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ───────────────── general probes (no external tools needed) ─────────────────

func unameLine() string {
	if runtime.GOOS == "windows" {
		return "windows " + runtime.GOARCH
	}
	out, err := run(3*time.Second, "uname", "-sr")
	if err != nil || out == "" {
		return ""
	}
	return strings.TrimSpace(out)
}

func uptimeLine() string {
	data, err := os.ReadFile("/proc/uptime")
	if err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 1 {
			secs, err := strconv.ParseFloat(f[0], 64)
			if err == nil {
				return humanUptime(time.Duration(secs) * time.Second)
			}
		}
	}
	out, err := run(3*time.Second, "uptime")
	if err != nil {
		return ""
	}
	if i := strings.Index(out, " up "); i >= 0 {
		s := out[i+4:]
		if j := strings.Index(s, ","); j > 0 {
			return strings.TrimSpace(s[:j])
		}
	}
	return ""
}

func humanUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func memInfoLine() (totalMB, freeMB int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalMB = kb / 1024
		case "MemAvailable:":
			freeMB = kb / 1024
		}
		if totalMB > 0 && freeMB > 0 {
			break
		}
	}
	return
}

func cpuModelLine() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		for _, key := range []string{"model name", "Hardware", "Processor"} {
			if strings.HasPrefix(line, key) {
				if i := strings.Index(line, ":"); i >= 0 {
					return strings.TrimSpace(line[i+1:])
				}
			}
		}
	}
	return ""
}

func shellLine() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return ""
	}
	return filepath.Base(sh)
}

func termLine() string {
	if tp := os.Getenv("TERM_PROGRAM"); tp != "" {
		return tp
	}
	if strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		return "Termux"
	}
	if t := os.Getenv("TERM"); t != "" {
		return t
	}
	return ""
}

func osReleaseLine() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		out, err := run(3*time.Second, "sw_vers", "-productVersion")
		if err == nil && out != "" {
			return "macOS " + out
		}
		return "macOS"
	}
	// linux-ish: try /etc/os-release
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				v := strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				if strings.Contains(os.Getenv("PREFIX"), "com.termux") && !strings.Contains(v, "Termux") {
					return "Termux / " + v
				}
				return v
			}
		}
	}
	return runtime.GOOS
}

// diskLine reads total/free bytes of the volume holding $HOME.
// Returns GiB; zeros when unavailable (windows, odd mounts).
func diskLine() (totalGB, freeGB float64) {
	t, f := diskBytes()
	if t <= 0 {
		return 0, 0
	}
	const gi = 1024.0 * 1024.0 * 1024.0
	return float64(t) / gi, float64(f) / gi
}

// ───────────────── fastfetch upgrade ─────────────────

type ffEntry struct {
	Type   string          `json:"type"`
	Result json.RawMessage `json:"result"`
}

func fastfetchJSON() ([]ffEntry, bool) {
	if !have("fastfetch") {
		return nil, false
	}
	out, err := run(4*time.Second, "fastfetch", "--format", "json")
	if err != nil || !strings.HasPrefix(out, "[") {
		return nil, false
	}
	var entries []ffEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, false
	}
	return entries, true
}

// applyFastfetch overlays nicer values fastfetch knows (CPU model, disk…).
func applyFastfetch(in *Info, entries []ffEntry) {
	in.Fastfetch = true
	for _, e := range entries {
		switch e.Type {
		case "Host":
			var r struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(e.Result, &r) == nil && r.Name != "" && in.Model == "" {
				in.Model = r.Name
			}
		case "CPU":
			var r struct {
				CPU   string `json:"cpu"`
				Cores struct {
					Online int `json:"online"`
				} `json:"cores"`
				Temperature any `json:"temperature"`
			}
			if json.Unmarshal(e.Result, &r) == nil {
				if r.CPU != "" {
					in.CPUModel = r.CPU
				}
				if r.Cores.Online > 0 {
					in.Cores = r.Cores.Online
				}
			}
		case "Kernel":
			var r struct {
				Release string `json:"release"`
			}
			if json.Unmarshal(e.Result, &r) == nil && r.Release != "" {
				in.Kernel = r.Release
			}
		case "Uptime":
			// fastfetch json reports an object; format loosely
			var r struct {
				Days    int `json:"days"`
				Hours   int `json:"hours"`
				Minutes int `json:"minutes"`
			}
			if json.Unmarshal(e.Result, &r) == nil && (r.Days|r.Hours|r.Minutes) != 0 {
				switch {
				case r.Days > 0:
					in.Uptime = fmt.Sprintf("%dd %dh", r.Days, r.Hours)
				case r.Hours > 0:
					in.Uptime = fmt.Sprintf("%dh %dm", r.Hours, r.Minutes)
				default:
					in.Uptime = fmt.Sprintf("%dm", r.Minutes)
				}
			}
		case "Memory":
			var r struct {
				Total int64 `json:"total"`
				Used  int64 `json:"used"`
			}
			if json.Unmarshal(e.Result, &r) == nil && r.Total > 0 {
				in.MemTotalMB = r.Total / 1024 / 1024
				in.MemFreeMB = r.Total/1024/1024 - r.Used/1024/1024
			}
		case "Disk":
			// result is an array of mounts; take the root one
			var rs []struct {
				Bytes struct {
					Total int64 `json:"total"`
					Free  int64 `json:"free"`
				} `json:"bytes"`
			}
			if json.Unmarshal(e.Result, &rs) == nil {
				for _, d := range rs {
					if d.Bytes.Total > 0 {
						in.DiskTotalGB = float64(d.Bytes.Total) / 1024 / 1024 / 1024
						in.DiskFreeGB = float64(d.Bytes.Free) / 1024 / 1024 / 1024
						break
					}
				}
			}
		case "Shell":
			var r struct {
				PrettyName string `json:"prettyName"`
			}
			if json.Unmarshal(e.Result, &r) == nil && r.PrettyName != "" {
				in.Shell = r.PrettyName
			}
		case "Terminal":
			var r struct {
				PrettyName string `json:"prettyName"`
			}
			if json.Unmarshal(e.Result, &r) == nil && r.PrettyName != "" {
				in.Terminal = r.PrettyName
			}
		}
	}
}
