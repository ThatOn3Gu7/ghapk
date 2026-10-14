// Package score is a faithful port of the original script's artifact ranking
// (variant_score + arch_score + api_score, exactly as written in bash).
// Higher is better for this device and the user's preferences.
package score

import (
	"regexp"
	"strings"
)

// Device describes what the phone can run.
type Device struct {
	ABIs []string // e.g. arm64-v8a, armeabi-v7a
	SDK  int      // ro.build.version.sdk, 0 when unknown
}

// Cats returns the ABI categories the device accepts, most-preferred first
// (mirrors DEVICE_CATS in the script).
func (d Device) Cats() []string {
	seen := map[string]bool{}
	var out []string
	for _, abi := range d.ABIs {
		c := abiCategory(abi)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		out = []string{"unknown"}
	}
	return out
}

func abiCategory(abi string) string {
	switch {
	case strings.Contains(abi, "arm64") || strings.Contains(abi, "aarch64"):
		return "arm64"
	case strings.HasPrefix(abi, "armeabi") || strings.Contains(abi, "armv7"):
		return "armv7"
	case strings.Contains(abi, "x86_64"):
		return "x86_64"
	case strings.Contains(abi, "x86"):
		return "x86"
	default:
		return "unknown"
	}
}

// ArchTags classifies a file/artifact name ("", "arm64 armv7" or "none").
func ArchTags(name string) string {
	n := strings.ToLower(name)
	var tags []string
	add := func(t string) {
		for _, x := range tags {
			if x == t {
				return
			}
		}
		tags = append(tags, t)
	}
	if strings.Contains(n, "arm64-v8a") || strings.Contains(n, "arm64") || strings.Contains(n, "aarch64") {
		add("arm64")
	}
	if strings.Contains(n, "armeabi-v7a") || strings.Contains(n, "armeabi") || strings.Contains(n, "armv7") {
		add("armv7")
	}
	if strings.Contains(n, "x86_64") || strings.Contains(n, "x64") || strings.Contains(n, "amd64") {
		add("x86_64")
	}
	if strings.Contains(n, "x86") && !strings.Contains(n, "x86_64") && !strings.Contains(n, "x64") && !strings.Contains(n, "amd64") {
		add("x86")
	}
	if strings.Contains(n, "universal") || strings.Contains(n, "noarch") ||
		strings.Contains(n, "-any") || strings.Contains(n, "_any") ||
		strings.Contains(n, "-all") || strings.Contains(n, "_all") ||
		strings.Contains(n, "-fat") || strings.Contains(n, "_fat") {
		add("universal")
	}
	if len(tags) == 0 {
		return "none"
	}
	return strings.Join(tags, " ")
}

func tagsContain(tags, t string) bool {
	if tags == "none" {
		return false
	}
	for _, x := range strings.Fields(tags) {
		if x == t {
			return true
		}
	}
	return false
}

// Prefs are the user-tunable matching preferences (ARCH_PREF / VARIANT_PREF).
type Prefs struct {
	Arch    string // auto any arm64 armv7 x86_64 x86 universal
	Variant string // auto debug release any
}

func deviceAllowsTag(cats []string, tag string) bool {
	for _, cat := range cats {
		switch cat {
		case "arm64":
			if tag == "arm64" || tag == "armv7" || tag == "universal" {
				return true
			}
		case "armv7":
			if tag == "armv7" || tag == "universal" {
				return true
			}
		case "x86_64":
			if tag == "x86_64" || tag == "x86" || tag == "universal" {
				return true
			}
		case "x86":
			if tag == "x86" || tag == "universal" {
				return true
			}
		case "unknown":
			return true
		}
	}
	return false
}

// PolicyAllowsArch mirrors policy_allows_arch.
func PolicyAllowsArch(name, archPref string, d Device) bool {
	tags := ArchTags(name)
	if tags == "none" || tagsContain(tags, "universal") {
		return true
	}
	switch archPref {
	case "any":
		return true
	case "auto", "":
		for _, t := range strings.Fields(tags) {
			if deviceAllowsTag(d.Cats(), t) {
				return true
			}
		}
		return false
	case "arm64":
		return tagsContain(tags, "arm64")
	case "armv7":
		return tagsContain(tags, "armv7")
	case "x86_64":
		return tagsContain(tags, "x86_64")
	case "x86":
		return tagsContain(tags, "x86")
	case "universal":
		return false
	default:
		return true
	}
}

// VariantAllowed mirrors variant_allowed.
func VariantAllowed(name, variantPref string) bool {
	n := strings.ToLower(name)
	switch variantPref {
	case "debug":
		return !strings.Contains(n, "release")
	case "release":
		return !strings.Contains(n, "debug")
	default:
		return true
	}
}

// AllowedByPolicy mirrors apk_allowed_by_policy.
func AllowedByPolicy(name string, p Prefs, d Device) bool {
	return VariantAllowed(name, p.Variant) && PolicyAllowsArch(name, p.Arch, d)
}

func variantScore(name, pref string) int {
	n := strings.ToLower(name)
	s := 0
	switch pref {
	case "debug", "auto", "":
		if strings.Contains(n, "debug") {
			s += 90
		}
		if strings.Contains(n, "release") {
			s -= 70
		}
	case "release":
		if strings.Contains(n, "release") {
			s += 90
		}
		if strings.Contains(n, "debug") {
			s -= 70
		}
	case "any":
		if strings.Contains(n, "debug") || strings.Contains(n, "release") {
			s += 5
		}
	}
	return s
}

var androidAPIRe = regexp.MustCompile(`android-([0-9]+)`)

func apiScore(name string, sdk int) int {
	n := strings.ToLower(name)
	m := androidAPIRe.FindStringSubmatch(n)
	if m == nil {
		return 0
	}
	var v int
	for _, c := range m[1] {
		v = v*10 + int(c-'0')
	}
	if sdk > 0 && v > sdk {
		return -1000
	}
	return v
}

func archScore(name, pref string, d Device) int {
	tags := ArchTags(name)
	if tags == "none" {
		switch pref {
		case "universal", "any", "auto", "":
			return 15
		default:
			return 5
		}
	}
	if tagsContain(tags, "universal") {
		switch pref {
		case "universal":
			return 35
		case "any", "auto", "":
			return 20
		default:
			return 10
		}
	}
	switch pref {
	case "any":
		return 0
	case "auto", "":
		best := -100
		for _, cat := range d.Cats() {
			add := -100
			switch cat {
			case "arm64":
				if tagsContain(tags, "arm64") {
					add = 70
				} else if tagsContain(tags, "armv7") {
					add = 35
				}
			case "armv7":
				if tagsContain(tags, "armv7") {
					add = 65
				}
			case "x86_64":
				if tagsContain(tags, "x86_64") {
					add = 70
				} else if tagsContain(tags, "x86") {
					add = 35
				}
			case "x86":
				if tagsContain(tags, "x86") {
					add = 65
				}
			case "unknown":
				add = 0
			}
			if add > best {
				best = add
			}
		}
		return best
	case "arm64":
		if tagsContain(tags, "arm64") {
			return 80
		}
		return -50
	case "armv7":
		if tagsContain(tags, "armv7") {
			return 80
		}
		return -50
	case "x86_64":
		if tagsContain(tags, "x86_64") {
			return 80
		}
		return -50
	case "x86":
		if tagsContain(tags, "x86") {
			return 80
		}
		return -50
	case "universal":
		return -70
	default:
		return 0
	}
}

// ArtifactRank mirrors artifact_rank — used to order a run's artifacts.
func ArtifactRank(name string, p Prefs, d Device) int {
	n := strings.ToLower(name)
	s := variantScore(name, p.Variant) + archScore(name, p.Arch, d) + apiScore(name, d.SDK)
	if strings.Contains(n, "apk") {
		s += 40
	}
	if strings.Contains(n, "symbol") || strings.Contains(n, "mapping") || strings.Contains(n, "report") {
		s -= 80
	}
	return s
}

// APKRank mirrors apk_rank — used to order APK files inside an artifact zip.
func APKRank(name string, p Prefs, d Device) int {
	n := strings.ToLower(name)
	s := variantScore(name, p.Variant) + archScore(name, p.Arch, d) + apiScore(name, d.SDK)
	if strings.Contains(n, "signed") {
		s += 10
	}
	return s
}

// LooksLikeAPK mirrors the loose filter the script applies before scoring.
func LooksLikeAPK(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "apk")
}
