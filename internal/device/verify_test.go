package device

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryPackageExtractsCertAndVerifyInstallPolls(t *testing.T) {
	origPoll := verifyPollInterval
	verifyPollInterval = 15 * time.Millisecond
	t.Cleanup(func() { verifyPollInterval = origPoll })

	wantCert := strings.Repeat("c9", 32)
	var calls atomic.Int32

	withDeviceStubs(t, true, map[string]bool{"rish": true}, nil,
		func(ctx context.Context, name string, args ...string) (string, error) {
			n := calls.Add(1)
			if n < 3 {
				return "Packages:\n  Package [com.example.rocket]\n    versionCode=100 minSdk=26\n    versionName=1.0.0\n    cert sha256: " + wantCert + "\n", nil
			}
			return "Packages:\n  Package [com.example.rocket]\n    versionCode=200 minSdk=26\n    versionName=2.0.0\n    cert sha256: " + wantCert + "\n", nil
		},
	)

	res := VerifyInstall(context.Background(), "com.example.rocket", 200, 300*time.Millisecond)
	if res.Outcome != VerifyConfirmed || res.App.VersionCode != 200 {
		t.Fatalf("expected VerifyConfirmed at versionCode 200, got %+v", res)
	}
	if res.App.CertSHA256 != wantCert {
		t.Fatalf("expected CertSHA256 %s, got %q", wantCert, res.App.CertSHA256)
	}
}

func TestVerifyInstallUnavailableWithoutQueryableDoor(t *testing.T) {
	withDeviceStubs(t, true, map[string]bool{"termux-open": true}, nil, nil)
	res := VerifyInstall(context.Background(), "com.example.rocket", 200, 50*time.Millisecond)
	if res.Outcome != VerifyUnavailable {
		t.Fatalf("expected VerifyUnavailable when no dumpsys/rish/adb door exists, got %+v", res)
	}
}

func TestVerifyInstallUnchangedOnTimeout(t *testing.T) {
	origPoll := verifyPollInterval
	verifyPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { verifyPollInterval = origPoll })

	withDeviceStubs(t, false, map[string]bool{"adb": true}, nil,
		func(ctx context.Context, name string, args ...string) (string, error) {
			return "Packages:\n  Package [com.example.rocket]\n    versionCode=100\n    versionName=1.0.0\n", nil
		},
	)
	res := VerifyInstall(context.Background(), "com.example.rocket", 200, 35*time.Millisecond)
	if res.Outcome != VerifyUnchanged {
		t.Fatalf("expected VerifyUnchanged when user never accepts prompt, got %+v", res)
	}
}

func TestQueryPackageUnknownOutputDoesNotMeanNotInstalled(t *testing.T) {
	withDeviceStubs(t, false, map[string]bool{"adb": true}, nil, func(ctx context.Context, name string, args ...string) (string, error) {
		return "service temporarily unavailable", nil
	})
	app, found := QueryPackageContext(context.Background(), "com.example.rocket")
	if found || app.Queryable {
		t.Fatalf("ambiguous output must remain unknown: %+v", app)
	}
}

func TestQueryPackageExplicitMissingIsQueryable(t *testing.T) {
	withDeviceStubs(t, false, map[string]bool{"adb": true}, nil, func(ctx context.Context, name string, args ...string) (string, error) {
		return "Unable to find package: com.example.rocket", nil
	})
	app, found := QueryPackageContext(context.Background(), "com.example.rocket")
	if found || !app.Queryable {
		t.Fatalf("explicit missing package should be queryable: %+v", app)
	}
}
