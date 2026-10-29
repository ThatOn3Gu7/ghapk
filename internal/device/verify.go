package device

import (
	"context"
	"fmt"
	"time"
)

// VerifyOutcome describes the result of post-install device verification.
type VerifyOutcome string

const (
	// VerifyConfirmed means QueryPackage positively observed pkg on the device
	// at the expected versionCode. Callers must also establish a different baseline.
	VerifyConfirmed VerifyOutcome = "confirmed"

	// VerifyUnchanged means the device could be queried, and after waiting the
	// package was still missing or still at its old versionCode.
	VerifyUnchanged VerifyOutcome = "unchanged"

	// VerifyUnavailable means the device package table cannot be queried from
	// this environment (e.g. plain Termux without Shizuku/ADB), so installation
	// cannot be verified automatically.
	VerifyUnavailable VerifyOutcome = "unavailable"
)

// VerifyResult reports post-install verification details.
type VerifyResult struct {
	Outcome VerifyOutcome
	App     InstalledApp
	Found   bool
	Detail  string
}

var verifyPollInterval = 1500 * time.Millisecond

// VerifyInstall checks (and optionally polls up to wait) whether pkg is now
// installed on the device at wantVersionCode. When wait <= 0 it performs a
// single immediate check.
func VerifyInstall(ctx context.Context, pkg string, wantVersionCode int64, wait time.Duration) VerifyResult {
	if !ValidPackage(pkg) {
		return VerifyResult{
			Outcome: VerifyUnavailable,
			Detail:  "package name unknown — cannot verify on device",
		}
	}

	if wantVersionCode <= 0 {
		return VerifyResult{Outcome: VerifyUnavailable, Detail: "APK version code unknown — cannot verify installation"}
	}

	checkOnce := func() (VerifyResult, bool) {
		app, found := QueryPackageContext(ctx, pkg)
		if !app.Queryable && !found {
			return VerifyResult{
				Outcome: VerifyUnavailable,
				Detail:  "device package manager not queryable without Shizuku/ADB",
			}, true
		}
		if found && app.VersionCode == wantVersionCode {
			return VerifyResult{
				Outcome: VerifyConfirmed,
				App:     app,
				Found:   true,
				Detail:  fmt.Sprintf("verified %s versionCode %d on device", pkg, app.VersionCode),
			}, true
		}
		var detail string
		if found {
			detail = fmt.Sprintf("device still has %s versionCode %d (expected %d)", pkg, app.VersionCode, wantVersionCode)
		} else {
			detail = fmt.Sprintf("%s is not yet installed on device", pkg)
		}
		return VerifyResult{
			Outcome: VerifyUnchanged,
			App:     app,
			Found:   found,
			Detail:  detail,
		}, false
	}

	res, done := checkOnce()
	if done || wait <= 0 {
		return res
	}

	deadline := time.Now().Add(wait)
	ticker := time.NewTicker(verifyPollInterval)
	defer ticker.Stop()

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return res
		}
		select {
		case <-ctx.Done():
			return res
		case <-ticker.C:
			res, done = checkOnce()
			if done {
				return res
			}
		}
	}
}
