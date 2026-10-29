package apk

import (
	"fmt"
	"sort"
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
	WarnSameVersion       WarningKind = "same_version"
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
	ExpectedPackage    string // configured or previously installed package name
	InstalledFound     bool
	InstalledQueryable bool
	InstalledVCode     int64
	InstalledVName     string
	InstalledCert      string // active signing cert SHA-256 on device
	HistoryCert        string // signing cert SHA-256 from last confirmed install
	HistoryPackage     string // package that history refers to
	DeviceSDK          int    // 0 if unknown (desktop without connected device)
	DowngradeMode      string // "auto" (confirm), "allow" (permit with note), "never"/"skip" (block)
	CheckPackage       bool   // enforce package identity warning (default true)
	CheckSignature     bool   // enforce signing key mismatch warning (default true)
}

// SafetyAssessment summarizes version relation, notes, and warnings for an APK.
type SafetyAssessment struct {
	VersionRelation string // "fresh", "upgrade", "reinstall", "downgrade", or "unknown"
	IsDowngrade     bool
	AllowDowngrade  bool // true ONLY when DowngradeMode == "allow" (or after explicit user confirmation)
	Notes           []string
	Warnings        []SafetyWarning
	BlockedReason   string
	NeedsConfirm    bool
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
		if refCert == "" && in.HistoryCert != "" && in.HistoryPackage == info.Package {
			refCert, refSource = in.HistoryCert, "the last build recorded for this app"
		}
		if refCert == "" {
			out.Notes = append(out.Notes, "installed signing key unavailable; could not compare")
		} else {
			if strings.EqualFold(refCert, ins.Cert.SHA256) {
				out.Notes = append(out.Notes, "signing key matches")
			} else {
				out.Warnings = append(out.Warnings, SafetyWarning{
					Kind:  WarnSignatureMismatch,
					Title: "Signing key mismatch",
					Message: fmt.Sprintf(
						"APK signing key (%.12s…) differs from %s (%.12s…). Android may refuse this update; uninstalling the old app erases its data. Key rotation can also cause a mismatch.",
						ins.Cert.SHA256, refSource, refCert,
					),
				})
			}
		}
	} else if in.CheckSignature {
		out.Notes = append(out.Notes, "APK signing key unavailable; could not compare")
	}

	// 3. Installed-vs-download version comparison & downgrade handling
	if in.InstalledFound && info.VersionCode > 0 && in.InstalledVCode > 0 {
		cmp := compareAPKVersion(info.VersionCode, info.VersionName, in.InstalledVCode, in.InstalledVName)
		switch {
		case cmp > 0:
			out.VersionRelation = "upgrade"
			out.Notes = append(out.Notes, fmt.Sprintf("upgrade %s → %s",
				fmtVer(in.InstalledVName, in.InstalledVCode),
				fmtVer(info.VersionName, info.VersionCode)))
		case cmp == 0:
			out.VersionRelation = "reinstall"
			out.Warnings = append(out.Warnings, SafetyWarning{
				Kind: WarnSameVersion, Title: "Already installed",
				Message: fmt.Sprintf("The installed %s and APK have the same version code (%d). They may still contain different builds. Keep the APK or reinstall?", info.Package, info.VersionCode),
			})
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
					Message: msg + " Android may refuse downgrades and app data may be incompatible.",
				})
			}
		}
	} else if !in.InstalledFound && in.InstalledQueryable {
		out.VersionRelation = "fresh"
	} else if !in.InstalledFound {
		out.Notes = append(out.Notes, "installed app status unavailable; version and signing key cannot be verified")
	} else {
		out.Notes = append(out.Notes, "version codes unavailable; cannot determine upgrade or downgrade")
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

	order := map[WarningKind]int{WarnPackageMismatch: 0, WarnDowngrade: 1, WarnMinSDK: 2, WarnSignatureMismatch: 3, WarnSameVersion: 4}
	sort.SliceStable(out.Warnings, func(i, j int) bool { return order[out.Warnings[i].Kind] < order[out.Warnings[j].Kind] })
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
