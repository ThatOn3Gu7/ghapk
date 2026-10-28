// Package pick decides which artifact of a run is "the one for this device".
// It is a straight port of the shell script's scoring: variant (debug/release),
// architecture, Android API level, and a few name hints each add or remove
// points, and artifacts that cannot run here are filtered out first.
package pick

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ghapk/internal/ghub"
	"ghapk/internal/glob"
)

// Policy is everything the ranking depends on.
type Policy struct {
	Arch    string   // auto any arm64 armv7 x86_64 x86 universal
	Variant string   // auto debug release any
	Cats    []string // device ABI categories, best first (arm64 armv7 x86_64 x86)
	SDK     int      // device API level, 0 = unknown
	AllArch bool     // let every architecture through the filter
	Name    string   // exact artifact name, bypasses everything
	Include []*glob.Pattern
	Exclude []*glob.Pattern
}

// ABICategory maps an ABI name to one of arm64 armv7 x86_64 x86 unknown.
func ABICategory(a string) string {
	switch strings.ToLower(a) {
	case "arm64-v8a", "arm64", "aarch64":
		return "arm64"
	case "armeabi-v7a", "armeabi", "arm":
		return "armv7"
	case "x86_64", "x64", "amd64":
		return "x86_64"
	case "x86", "i686", "i386":
		return "x86"
	}
	if strings.HasPrefix(strings.ToLower(a), "armv7") {
		return "armv7"
	}
	return "unknown"
}

// Cats turns a device ABI list into de-duplicated categories, best first.
func Cats(abis []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range abis {
		c := ABICategory(a)
		if c != "unknown" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		out = []string{"unknown"}
	}
	return out
}

// ArchTags reads architecture hints out of a file or artifact name. It returns
// nil when the name says nothing about architecture.
func ArchTags(name string) []string {
	n := strings.ToLower(name)
	var tags []string
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(n, s) {
				return true
			}
		}
		return false
	}
	if has("arm64-v8a", "arm64", "aarch64") {
		tags = append(tags, "arm64")
	}
	if has("armeabi-v7a", "armeabi", "armv7") {
		tags = append(tags, "armv7")
	}
	if has("x86_64", "x64", "amd64") {
		tags = append(tags, "x86_64")
	}
	if has("x86") && !has("x86_64", "x64", "amd64") {
		tags = append(tags, "x86")
	}
	if has("universal", "noarch", "-any", "_any", "-all", "_all", "-fat", "_fat") {
		tags = append(tags, "universal")
	}
	return tags
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}

// DeviceAllowsTag reports whether a device with these ABI categories can run
// an APK carrying this tag (arm64 devices run armv7, x86_64 runs x86).
func DeviceAllowsTag(cats []string, tag string) bool {
	for _, c := range cats {
		switch c {
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

// AllowsArch is the architecture filter.
func (p Policy) AllowsArch(name string) bool {
	tags := ArchTags(name)
	if len(tags) == 0 || hasTag(tags, "universal") {
		return true
	}
	switch p.Arch {
	case "any":
		return true
	case "", "auto":
		for _, t := range tags {
			if DeviceAllowsTag(p.Cats, t) {
				return true
			}
		}
		return false
	case "arm64", "armv7", "x86_64", "x86":
		return hasTag(tags, p.Arch)
	case "universal":
		return false
	}
	return true
}

// AllowsVariant is the debug/release filter.
func (p Policy) AllowsVariant(name string) bool {
	n := strings.ToLower(name)
	switch p.Variant {
	case "debug":
		return !strings.Contains(n, "release")
	case "release":
		return !strings.Contains(n, "debug")
	}
	return true
}

// Part is one line of a score breakdown.
type Part struct {
	Label string
	Pts   int
}

var reAPI = regexp.MustCompile(`android-([0-9]+)`)

func (p Policy) variantPart(n string) Part {
	s := 0
	switch p.Variant {
	case "debug", "", "auto":
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
	label := "variant"
	switch {
	case strings.Contains(n, "debug"):
		label = "debug"
	case strings.Contains(n, "release"):
		label = "release"
	}
	return Part{label, s}
}

func (p Policy) apiPart(n string) Part {
	m := reAPI.FindStringSubmatch(n)
	if m == nil {
		return Part{"api", 0}
	}
	v, _ := strconv.Atoi(m[1])
	if p.SDK > 0 && v > p.SDK {
		return Part{"needs newer android", -1000}
	}
	return Part{"android-" + m[1], v}
}

func (p Policy) archPart(name string) Part {
	tags := ArchTags(name)
	arch := p.Arch
	if arch == "" {
		arch = "auto"
	}
	if len(tags) == 0 {
		if arch == "universal" || arch == "any" || arch == "auto" {
			return Part{"no abi in name", 15}
		}
		return Part{"no abi in name", 5}
	}
	if hasTag(tags, "universal") {
		switch arch {
		case "universal":
			return Part{"universal", 35}
		case "any", "auto":
			return Part{"universal", 20}
		}
		return Part{"universal", 10}
	}
	label := strings.Join(tags, "+")
	switch arch {
	case "any":
		return Part{label, 0}
	case "auto":
		best := -100
		for _, c := range p.Cats {
			add := -100
			switch c {
			case "arm64":
				if hasTag(tags, "arm64") {
					add = 70
				} else if hasTag(tags, "armv7") {
					add = 35
				}
			case "armv7":
				if hasTag(tags, "armv7") {
					add = 65
				}
			case "x86_64":
				if hasTag(tags, "x86_64") {
					add = 70
				} else if hasTag(tags, "x86") {
					add = 35
				}
			case "x86":
				if hasTag(tags, "x86") {
					add = 65
				}
			case "unknown":
				add = 0
			}
			if add > best {
				best = add
			}
		}
		return Part{label, best}
	case "arm64", "armv7", "x86_64", "x86":
		if hasTag(tags, arch) {
			return Part{label, 80}
		}
		return Part{label, -50}
	case "universal":
		return Part{label, -70}
	}
	return Part{label, 0}
}

// Breakdown explains an artifact's score.
func (p Policy) Breakdown(name string) []Part {
	n := strings.ToLower(name)
	parts := []Part{p.variantPart(n), p.archPart(name), p.apiPart(n)}
	if strings.Contains(n, "apk") {
		parts = append(parts, Part{"apk", 40})
	}
	if strings.Contains(n, "symbol") || strings.Contains(n, "mapping") || strings.Contains(n, "report") {
		parts = append(parts, Part{"not an app", -80})
	}
	var out []Part
	for _, x := range parts {
		if x.Pts != 0 {
			out = append(out, x)
		}
	}
	return out
}

// ArtifactRank is the score of an artifact name; higher is better.
func (p Policy) ArtifactRank(name string) int {
	s := 0
	for _, x := range p.Breakdown(name) {
		s += x.Pts
	}
	return s
}

// APKRank is the score of an .apk file name inside an artifact.
func (p Policy) APKRank(name string) int {
	s := p.variantPart(strings.ToLower(name)).Pts + p.archPart(name).Pts + p.apiPart(strings.ToLower(name)).Pts
	if strings.Contains(strings.ToLower(name), "signed") {
		s += 10
	}
	return s
}

// ---- candidates ------------------------------------------------------------

// Candidate is one artifact with the verdict on it.
type Candidate struct {
	Art    ghub.Artifact
	Score  int
	Tags   []string
	Match  bool   // eligible for automatic selection
	Skip   string // why it is not eligible
	Parts  []Part
	Choice bool // the first choice
}

var reChecksum = regexp.MustCompile(`sha256|sha512|checksum|md5|sums`)

// Rank scores every artifact of a run. Eligible ones come first, best first;
// the rest follow with the reason they were passed over, so a pattern miss is
// visible and fixable instead of silent.
func Rank(arts []ghub.Artifact, p Policy) []Candidate {
	var ok, no []Candidate
	for _, a := range arts {
		ln := strings.ToLower(a.Name)
		c := Candidate{Art: a, Tags: ArchTags(a.Name)}
		c.Parts = p.Breakdown(a.Name)
		c.Score = p.ArtifactRank(a.Name)
		switch {
		case reChecksum.MatchString(ln):
			c.Skip = "checksum file"
		case p.Name != "":
			if a.Name == p.Name {
				c.Match, c.Score = true, 999
				c.Parts = []Part{{"named exactly", 999}}
			} else {
				c.Skip = "not the named artifact"
			}
		case glob.MatchAny(a.Name, p.Exclude):
			c.Skip = "excluded"
		case p.Arch != "any" && !p.AllArch && !p.AllowsArch(a.Name):
			c.Skip = "other architecture"
		case !glob.MatchAny(a.Name, p.Include):
			c.Skip = "no pattern match"
		default:
			c.Match = true
		}
		if c.Match {
			ok = append(ok, c)
		} else {
			no = append(no, c)
		}
	}
	sort.SliceStable(ok, func(i, j int) bool { return ok[i].Score > ok[j].Score })
	sort.SliceStable(no, func(i, j int) bool { return no[i].Score > no[j].Score })
	if len(ok) > 0 {
		ok[0].Choice = true
	}
	return append(ok, no...)
}
