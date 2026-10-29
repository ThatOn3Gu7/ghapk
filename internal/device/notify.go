package device

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/ghapk/ghapk/internal/activity"
)

// Notify sends a completion notification through Termux:API when installed.
// No shell is involved, and each optional helper has a bounded runtime.
func Notify(policy string, speak, toast, success bool, title, message string) {
	switch policy {
	case "never":
		return
	case "success":
		if !success {
			return
		}
	case "failure":
		if success {
			return
		}
	case "always":
	default:
		return
	}
	run := func(name string, args ...string) {
		p, err := exec.LookPath(name)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = activity.CommandContext(ctx, p, args...).Run()
	}
	title = strings.ReplaceAll(title, "\x00", "")
	message = strings.ReplaceAll(message, "\x00", "")
	if speak {
		run("termux-tts-speak", title+". "+message)
	}
	if toast {
		run("termux-toast", "-g", "bottom", title+": "+message)
	}
	run("termux-notification", "--title", title, "--content", message)
}
