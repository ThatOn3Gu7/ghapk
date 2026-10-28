package apk

import (
	"fmt"
	"strconv"
	"strings"
)

// WarningKind classifies pre-install safety warnings.
type WarningKind string

const (
	WarnPackageMismatch   WarningKind = "package_mismatch"
	WarnSignatureMismatch WarningKind = "signature_mismatch"
	WarnDowngrade         WarningKind = "downgrade"
	WarnMinSDK            WarningKind = "min_sdk"
)

// SafetyWarning describes one risk that requires blocking or explicit user
// confirmation before handing an APK to an installer.
type SafetyWarning struct {
	Kind    WarningKind
	Title   string
	Message string
	Blocked bool // true when policy forbids proceeding (e.g. downgrade=never/skip)
}

// SafetyInput supplies device and policy context to EvaluateSafety.
type SafetyInput struct {
	ExpectedPackage string // configured or previously installed package name
	InstalledFound  bool
	InstalledVCode  int64
	InstalledVName  string
	InstalledCert   string // active signing cert SHA-256 on device
	HistoryCert     string // signing cert SHA-256 from last install of same package/repo
	DeviceSDK       int    // 0 if unknown (desktop without connected device)
	DowngradeMode   string // "auto" (confirm), "allow" (permit with note), "never"/"skip" (block)
	CheckPackage    bool   // enforce package identity warning (default true)
	CheckSignature  bool   // enforce signing key mismatch warning (default true)
}

// SafetyAssessment summarizes version relation, notes, and warnings for an APK.
type SafetyAssessment struct {
	VersionRelation  string // "fresh", "upgrade", "reinstall", "downgrade", or "unknown"
	IsDowngrade      bool
	AllowDowngrade   bool // true ONLY when DowngradeMode == "allow" (or after explicit user confirmation)
	Notes            []string
	Warnings         []SafetyWarning
	BlockedReason    string
	NeedsConfirm     bool
}

// EvaluateSafety checks package identity, signing certificate continuity,
// installed-vs-download version relation, and device SDK compatibility.
//
// Importantly, `downgrade=auto` NEVER silently sets AllowDowngrade=true; it
// emits a WarnDowngrade item with NeedsConfirm=true so interactive UIs ask the
// user explicitly and non-interactive runs refuse unless `downgrade=allow`.
func EvaluateSafety(ins Inspection, in SafetyInput) SafetyAssessment {
	var out SafetyAssessment
	out.VersionRelation = "unknown"

	if !ins.HaveInfo {
		if ins.InfoErr != "" {
			out.Notes = append(out.Notes, "manifest unreadable ("+ins.InfoErr+")")
		}
		return out
	}

	info := ins.Info

	// 1. Package identity check
	if in.CheckPackage && in.ExpectedPackage != "" && info.Package != "" && info.Package != in.ExpectedPackage {
		out.Warnings = append(out.Warnings, SafetyWarning{
			Kind:  WarnPackageMismatch,
			Title: "Package name mismatch",
			Message: fmt.Sprintf(
				"APK declares package %q, which differs from tracked package %q. Installing it will add or touch a different app.",
				info.Package, in.ExpectedPackage,
			),
		})
	}

	// 2. Signing certificate continuity check
	if in.CheckSignature && ins.Cert.SHA256 != "" {
		refCert, refSource := in.InstalledCert, "the app currently on the device"
		if refCert == "" && in.HistoryCert != "" && (in.ExpectedPackage == "" || in.ExpectedPackage == info.Package) {
			refCert, refSource = in.HistoryCert, "the last build recorded for this app"
		}
		if refCert != "" {
			if strings.EqualFold(refCert, ins.Cert.SHA256) {
				out.Notes = append(out.Notes, "signing key matches")
			} else {
				out.Warnings = append(out.Warnings, SafetyWarning{
					Kind:  WarnSignatureMismatch,
					Title: "Signing key mismatch",
					Message: fmt.Sprintf(
						"APK signing key (%.12s…) differs from %s (%.12s…). Android refuses cross-key updates (INSTALL_FAILED_UPDATE_INCOMPATIBLE) unless the old app is uninstalled first.",
						ins.Cert.SHA256, refSource, refCert,
					),
				})
			}
		}
	}

	// 3. Installed-vs-download version comparison & downgrade handling
	if in.InstalledFound {
		cmp := compareAPKVersion(info.VersionCode, info.VersionName, in.InstalledVCode, in.InstalledVName)
		switch {
		case cmp > 0:
			out.VersionRelation = "upgrade"
			out.Notes = append(out.Notes, fmt.Sprintf("upgrade %s → %s",
				fmtVer(in.InstalledVName, in.InstalledVCode),
				fmtVer(info.VersionName, info.VersionCode)))
		case cmp == 0:
			out.VersionRelation = "reinstall"
			out.Notes = append(out.Notes, fmt.Sprintf("same version %s",
				fmtVer(info.VersionName, info.VersionCode)))
		case cmp < 0:
			out.VersionRelation = "downgrade"
			out.IsDowngrade = true
			msg := fmt.Sprintf(
				"APK version %s is older than installed %s on the device.",
				fmtVer(info.VersionName, info.VersionCode),
				fmtVer(in.InstalledVName, in.InstalledVCode),
			)
			mode := strings.ToLower(strings.TrimSpace(in.DowngradeMode))
			switch mode {
			case "allow":
				out.AllowDowngrade = true
				out.Notes = append(out.Notes, "downgrade allowed by setting")
			case "never", "skip":
				w := SafetyWarning{
					Kind:    WarnDowngrade,
					Title:   "Downgrade blocked",
					Message: msg + " Refusing because downgrade policy is set to " + mode + ".",
					Blocked: true,
				}
				out.Warnings = append(out.Warnings, w)
				out.BlockedReason = w.Message
			default: // "auto" or unset -> explicit confirmation required, NEVER silent!
				out.Warnings = append(out.Warnings, SafetyWarning{
					Kind:    WarnDowngrade,
					Title:   "Downgrade warning",
					Message: msg + " Android blocks downgrades on release builds and may break app data.",
				})
			}
		}
	} else {
		out.VersionRelation = "fresh"
	}

	// 4. Device SDK compatibility check
	if info.MinSDK > 0 && in.DeviceSDK > 0 && info.MinSDK > in.DeviceSDK {
		out.Warnings = append(out.Warnings, SafetyWarning{
			Kind:  WarnMinSDK,
			Title: "Android SDK too old",
			Message: fmt.Sprintf(
				"APK requires Android API %d+, but target device runs API %d (INSTALL_FAILED_OLDER_SDK).",
				info.MinSDK, in.DeviceSDK,
			),
		})
	}

	for _, w := range out.Warnings {
		if !w.Blocked {
			out.NeedsConfirm = true
		}
	}
	return out
}

func fmtVer(name string, code int64) string {
	if name == "" {
		name = "?"
	}
	if code > 0 {
		return fmt.Sprintf("%s (%d)", name, code)
	}
	return name
}

func compareAPKVersion(dlCode int64, dlName string, instCode int64, instName string) int {
	if dlCode > 0 && instCode > 0 {
		switch {
		case dlCode > instCode:
			return 1
		case dlCode < instCode:
			return -1
		default:
			return 0
		}
	}
	if dlName != "" && instName != "" {
		return CompareVersionStrings(dlName, instName)
	}
	return 0
}

// CompareVersionStrings compares dotted version strings (e.g. "1.4.2" vs
// "1.4.2-rc1"). Missing numeric components count as 0, and pre-release tags
// sort below clean numeric releases.
func CompareVersionStrings(a, b string) int {
	a = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(a), "v"), "V")
	b = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(b), "v"), "V")
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		xn, xt := splitVerPart(x)
		yn, yt := splitVerPart(y)
		if xn != yn {
			if xn > yn {
				return 1
			}
			return -1
		}
		if xt == yt {
			continue
		}
		switch {
		case xt == "":
			return 1
		case yt == "":
			return -1
		case xt > yt:
			return 1
		default:
			return -1
		}
	}
	return 0
}

func splitVerPart(c string) (int64, string) {
	i := 0
	for i < len(c) && c[i] >= '0' && c[i] <= '9' {
		i++
	}
	var n int64
	if i > 0 {
		n, _ = strconv.ParseInt(c[:i], 10, 64)
	}
	return n, c[i:]
}
