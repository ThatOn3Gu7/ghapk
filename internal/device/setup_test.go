package device

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProbeSetupDistinguishesToolsAndReachability(t *testing.T) {
	oldLook, oldHook := lookPathFn, probeCmdHook
	t.Cleanup(func() { lookPathFn, probeCmdHook = oldLook, oldHook })
	tools := map[string]bool{"rish": true, "termux-open": true, "termux-notification": true, "pm": true}
	lookPathFn = func(name string) (string, error) {
		if tools[name] {
			return name, nil
		}
		return "", errors.New("missing")
	}
	probeCmdHook = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "rish" {
			return "SecurityException", nil
		}
		if name == "pm" {
			return "package:android\n", nil
		}
		return "", errors.New("missing")
	}
	got := ProbeSetupContext(context.Background(), Info{OS: "android"})
	if !got.RishPresent || !got.Notifications || got.PackageManager != "pm" || strings.Join(got.Installers, ",") != "termux-open" {
		t.Fatalf("%+v", got)
	}
	probeCmdHook = func(ctx context.Context, name string, args ...string) (string, error) {
		return "package:androidx\n", nil
	}
	got = ProbeSetupContext(context.Background(), Info{OS: "android"})
	if got.PackageManager != "" {
		t.Fatalf("false positive: %+v", got)
	}
}
