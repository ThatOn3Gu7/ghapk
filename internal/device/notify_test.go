package device

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationPolicyAndOptionalHelpers(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	t.Setenv("PATH", dir)
	t.Setenv("NOTIFY_LOG", log)
	for _, name := range []string{"termux-notification", "termux-tts-speak", "termux-toast"} {
		script := "#!/bin/sh\nprintf '%s\\n' " + name + " >> \"$NOTIFY_LOG\"\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	Notify("never", true, true, true, "title", "body")
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("never sent notification")
	}
	Notify("success", true, true, false, "title", "body")
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("success policy notified failure")
	}
	Notify("failure", true, true, false, "title", "body")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"termux-notification", "termux-tts-speak", "termux-toast"} {
		if !strings.Contains(string(b), s) {
			t.Fatalf("missing %s in %s", s, b)
		}
	}
}
