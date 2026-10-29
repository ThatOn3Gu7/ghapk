package tui

import (
	"strings"
	"unicode"

	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
)

type artifactKind int

const (
	artifactUnknown artifactKind = iota
	artifactAndroid
	artifactIOS
	artifactMac
	artifactWindows
	artifactLinux
	artifactWeb
	artifactDocument
	artifactArchive
)

// GitHub's artifact API exposes an artifact name, not the files inside it or
// their app icons. Infer only from recognizable suffixes or whole name tokens;
// ambiguous names stay generic. This is cosmetic, not a compatibility check.
func artifactKindFromName(name string) artifactKind {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, rule := range []struct {
		suffixes []string
		kind     artifactKind
	}{
		{[]string{".apk", ".apks", ".aab"}, artifactAndroid},
		{[]string{".ipa"}, artifactIOS},
		{[]string{".dmg"}, artifactMac},
		{[]string{".exe", ".msi", ".msix", ".appx"}, artifactWindows},
		{[]string{".deb", ".rpm", ".appimage", ".flatpak", ".snap"}, artifactLinux},
		{[]string{".html", ".htm"}, artifactWeb},
		{[]string{".txt", ".md", ".log", ".json", ".xml", ".yml", ".yaml", ".pdf"}, artifactDocument},
	} {
		for _, suffix := range rule.suffixes {
			if strings.HasSuffix(n, suffix) {
				return rule.kind
			}
		}
	}
	tokens := strings.FieldsFunc(n, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	kinds := map[artifactKind]bool{}
	for _, token := range tokens {
		switch token {
		case "apk", "apks", "aab", "android":
			kinds[artifactAndroid] = true
		case "ipa", "ios", "iphone", "ipad":
			kinds[artifactIOS] = true
		case "mac", "macos", "osx", "darwin":
			kinds[artifactMac] = true
		case "windows", "win", "win32", "win64":
			kinds[artifactWindows] = true
		case "linux", "ubuntu", "debian", "fedora":
			kinds[artifactLinux] = true
		case "web", "browser":
			kinds[artifactWeb] = true
		}
	}
	if len(kinds) == 1 {
		for kind := range kinds {
			return kind
		}
	}
	if len(kinds) > 1 {
		return artifactUnknown
	}
	for _, suffix := range []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.xz", ".7z", ".rar"} {
		if strings.HasSuffix(n, suffix) {
			return artifactArchive
		}
	}
	return artifactUnknown
}

func artifactIcon(g theme.Glyphs, kind artifactKind) string {
	switch kind {
	case artifactAndroid:
		return g.ArtifactAndroid
	case artifactIOS:
		return g.ArtifactIOS
	case artifactMac:
		return g.ArtifactMac
	case artifactWindows:
		return g.ArtifactWindows
	case artifactLinux:
		return g.ArtifactLinux
	case artifactWeb:
		return g.ArtifactWeb
	case artifactDocument:
		return g.ArtifactDocument
	case artifactArchive:
		return g.ArtifactArchive
	default:
		return g.ArtifactUnknown
	}
}

// Keep every icon in a fixed two-cell column. The ! retains the old rejected
// marker without implying that a detected platform is installable here.
func (m Model) artifactRow(name string, rejected bool) string {
	icon := components.Pad(artifactIcon(m.st.G, artifactKindFromName(name)), 2)
	flag := " "
	if rejected {
		flag = "!"
	}
	return icon + flag + " " + name
}
