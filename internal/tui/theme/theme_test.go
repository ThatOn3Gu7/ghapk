package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestReadableOn(t *testing.T) {
	cases := map[string]string{
		"#F85149": "#101010", // bright red: dark text
		"#CF222E": "#FFFFFF", // deep red: white text
		"#000000": "#FFFFFF",
		"#FFFFFF": "#101010",
		"red":     "#FFFFFF",
	}
	for bg, want := range cases {
		if got := string(readableOn(lipglossColor(bg))); got != want {
			t.Errorf("%s: got %s want %s", bg, got, want)
		}
	}
}

func TestEveryThemeHasReadableErrorToast(t *testing.T) {
	for name, p := range Themes {
		st := New(p)
		if st.ToastErr.GetForeground() == st.ToastErr.GetBackground() {
			t.Errorf("%s: error toast text matches its background", name)
		}
	}
}

func lipglossColor(s string) lipgloss.Color { return lipgloss.Color(s) }
