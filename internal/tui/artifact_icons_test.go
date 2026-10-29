package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
)

func TestArtifactKindFromName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want artifactKind
	}{
		{"nazo-debug-apk", artifactAndroid}, {"APP-RELEASE.APK", artifactAndroid}, {"app.aab", artifactAndroid}, {"android-arm64.zip", artifactAndroid},
		{"app.ipa", artifactIOS}, {"ios-simulator", artifactIOS}, {"client-macos.dmg", artifactMac}, {"app-darwin-arm64", artifactMac},
		{"app.exe", artifactWindows}, {"client-win64.zip", artifactWindows}, {"app.appimage", artifactLinux}, {"app-linux.tar.gz", artifactLinux},
		{"web-client", artifactWeb}, {"index.html", artifactWeb}, {"android-readme.txt", artifactDocument}, {"mapping.json", artifactDocument},
		{"output.zip", artifactArchive}, {"backup.tar.gz", artifactArchive}, {"outputs", artifactUnknown}, {"mapkeeper", artifactUnknown},
		{"windowshade", artifactUnknown}, {"android-ios-bundle", artifactUnknown}, {"android-windows.zip", artifactUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := artifactKindFromName(tc.name); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestArtifactIconsHaveASCIIFallbackAndFixedWidths(t *testing.T) {
	for _, tc := range []struct {
		env         string
		wantAndroid string
	}{{"0", "🤖"}, {"1", "A"}} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("GHAPK_ASCII", tc.env)
			t.Setenv("TERM", "xterm-256color")
			m := newTestModel()
			if icon := artifactIcon(m.st.G, artifactAndroid); icon != tc.wantAndroid {
				t.Fatalf("icon=%q", icon)
			}
			for _, kind := range []artifactKind{artifactAndroid, artifactIOS, artifactMac, artifactWindows, artifactLinux, artifactWeb, artifactDocument, artifactArchive, artifactUnknown} {
				icon := artifactIcon(m.st.G, kind)
				if icon == "" || components.VisibleWidth(icon) > 2 {
					t.Fatalf("bad icon %q", icon)
				}
				row := components.Pad(icon, 2) + "  name"
				if components.VisibleWidth(row) != 8 {
					t.Fatalf("misaligned row %q", row)
				}
			}
			if row := components.StripANSI(m.artifactRow("nazo-debug-apk", true)); !strings.Contains(row, "! nazo-debug-apk") {
				t.Fatalf("rejected marker missing: %q", row)
			}
		})
	}
}

func TestBuildArtifactIconsAndFiltering(t *testing.T) {
	t.Setenv("GHAPK_ASCII", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := buildModel(t, sz[0], sz[1])
			m.focus = PanelArtifacts
			m.buildDetail = true
			m.settings.Source = "actions"
			arts := []ghapi.Artifact{{ID: 1, Name: "nazo-debug-apk", SizeInBytes: 1024}, {ID: 2, Name: "client-windows.exe", SizeInBytes: 2048, Rejected: true}, {ID: 3, Name: "mystery", SizeInBytes: 4096}}
			m.runCache[m.selectedRunID] = arts
			m.setArtifactItems(arts)
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) != sz[1] {
				t.Fatalf("wrong height %d", len(lines))
			}
			lineWidths(t, sz[0], sz[1], lines)
			panel := components.StripANSI(m.buildArtifacts(m.buildsLayout().arts))
			if !strings.Contains(panel, "🤖  nazo-debug-apk") || strings.Contains(panel, "✓  nazo-debug") {
				t.Fatalf("Android artifact row: %s", panel)
			}
			if !strings.Contains(panel, "⊞ ! client-windows") {
				t.Fatalf("rejected platform row: %s", panel)
			}
			m.arts.Filter = "nazo"
			m.arts.Cursor = 0
			m.syncArtifactSelection()
			panel = components.StripANSI(m.buildArtifacts(m.buildsLayout().arts))
			if !strings.Contains(panel, "🤖  nazo-debug-apk") || strings.Contains(panel, "client-windows") {
				t.Fatalf("filter lost icon or showed wrong row: %s", panel)
			}
			if a, ok := m.cursorArtifact(); !ok || a.ID != 1 {
				t.Fatal("filtered selection changed")
			}
			mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = mm.(Model)
			if m.modal != ModalPlan || m.plan == nil || m.plan.choices[m.plan.selected].name() != "nazo-debug-apk" {
				t.Fatal("icon affected artifact action")
			}
		})
	}
}

func TestReleaseAssetIconsAndASCIIMode(t *testing.T) {
	t.Setenv("GHAPK_ASCII", "1")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 120, 40)
	m.settings.Source = "releases"
	m.releases = []ghapi.Release{{TagName: "v1", Assets: []ghapi.ReleaseAsset{{ID: 42, Name: "release.apk", Size: 1024}, {ID: 43, Name: "output.zip", Size: 2048}}}}
	m.syncReleaseArtifactItems()
	view := components.StripANSI(m.buildArtifacts(m.buildsLayout().arts))
	if !strings.Contains(view, "A   release.apk") || !strings.Contains(view, "Z   output.zip") || strings.Contains(view, "✓") {
		t.Fatalf("wrong release rows: %s", view)
	}
	if _, ok := m.selectedReleaseAsset(); !ok {
		t.Fatal("release selection changed")
	}
}

func TestArtifactIconGlyphSetsAreDistinct(t *testing.T) {
	for _, g := range []theme.Glyphs{theme.UnicodeGlyphs, theme.ASCIIGlyphs} {
		seen := map[string]bool{}
		for _, kind := range []artifactKind{artifactAndroid, artifactIOS, artifactMac, artifactWindows, artifactLinux, artifactWeb, artifactDocument, artifactArchive, artifactUnknown} {
			icon := artifactIcon(g, kind)
			if seen[icon] {
				t.Fatalf("duplicate icon %q", icon)
			}
			seen[icon] = true
		}
	}
}
