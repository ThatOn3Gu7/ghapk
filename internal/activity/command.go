package activity

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	tokenPattern    = regexp.MustCompile(`(?i)\b(?:gh[opusr]_[a-z0-9_]{8,}|github_pat_[a-z0-9_]{8,})\b`)
	secretPattern   = regexp.MustCompile(`(?i)\b(authorization|cookie|session|credential|password|passwd|access_token|refresh_token|token|secret|client_secret|x-api-key|api[_-]?key)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	bearerPattern   = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/-]+=*`)
	userInfoPattern = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	queryPattern    = regexp.MustCompile(`\?[^\s"']+`)
)

func Redact(s string) string {
	s = tokenPattern.ReplaceAllString(s, "[redacted]")
	s = secretPattern.ReplaceAllString(s, "$1=[redacted]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [redacted]")
	s = userInfoPattern.ReplaceAllString(s, "${1}[redacted]@")
	return queryPattern.ReplaceAllString(s, "?[query redacted]")
}
func commandLabel(name string, args []string) string {
	name = filepath.Base(name)
	parts := []string{name}
	hideNext := false
	for _, arg := range args {
		if hideNext {
			parts = append(parts, "[redacted]")
			hideNext = false
			continue
		}
		low := strings.ToLower(arg)
		if name == "termux-tts-speak" || name == "termux-toast" || name == "termux-notification" {
			parts = append(parts, "[content redacted]")
			continue
		}
		if low == "--token" || low == "--password" || low == "--secret" || low == "--access-token" || low == "--refresh-token" || low == "--client-secret" || low == "--api-key" || low == "--authorization" || low == "--cookie" || low == "--credential" || low == "--field" || low == "-f" || low == "--raw-field" || low == "--header" || arg == "-H" {
			parts = append(parts, arg)
			hideNext = true
			continue
		}
		arg = Redact(arg)
		if strings.ContainsAny(arg, " \t\n\r\"") {
			arg = fmt.Sprintf("%q", arg)
		}
		parts = append(parts, arg)
	}
	return clean(strings.Join(parts, " "), 2048)
}

// Cmd embeds exec.Cmd so callers can still configure pipes and deadlines.
type Cmd struct {
	*exec.Cmd
	label string
	start time.Time
	once  sync.Once
}

func Command(name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.Command(name, args...), label: commandLabel(name, args)}
}
func CommandContext(ctx context.Context, name string, args ...string) *Cmd {
	return &Cmd{Cmd: exec.CommandContext(ctx, name, args...), label: commandLabel(name, args)}
}
func (c *Cmd) begin() { c.start = time.Now(); Log("command", c.label) }
func (c *Cmd) end(err error) {
	c.once.Do(func() {
		outcome := "ok"
		if err != nil {
			outcome = "failed: " + err.Error()
		}
		Log("command", fmt.Sprintf("%s (%s, %s)", c.label, outcome, time.Since(c.start).Round(time.Millisecond)))
	})
}
func (c *Cmd) Run() error { c.begin(); err := c.Cmd.Run(); c.end(err); return err }
func (c *Cmd) Output() ([]byte, error) {
	c.begin()
	out, err := c.Cmd.Output()
	c.end(err)
	return out, err
}
func (c *Cmd) CombinedOutput() ([]byte, error) {
	c.begin()
	out, err := c.Cmd.CombinedOutput()
	c.end(err)
	return out, err
}
func (c *Cmd) Start() error {
	c.begin()
	err := c.Cmd.Start()
	if err != nil {
		c.end(err)
	}
	return err
}
func (c *Cmd) Wait() error { err := c.Cmd.Wait(); c.end(err); return err }
