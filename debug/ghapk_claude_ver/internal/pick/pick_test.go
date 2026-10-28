package pick

import (
	"testing"

	"ghapk/internal/ghub"
	"ghapk/internal/glob"
)

func arts(names ...string) []ghub.Artifact {
	var out []ghub.Artifact
	for i, n := range names {
		out = append(out, ghub.Artifact{ID: int64(i + 1), Name: n})
	}
	return out
}

func pol(cats ...string) Policy {
	return Policy{Arch: "auto", Variant: "auto", Cats: cats,
		Include: glob.SplitList("*debug*,*apk*")}
}

func TestArm64DevicePrefersArm64Debug(t *testing.T) {
	c := Rank(arts("nazo-debug-armeabi-v7a", "nazo-debug-x86_64", "nazo-release-arm64-v8a",
		"nazo-debug-arm64-v8a", "nazo-debug-universal", "nazo-debug-sha256sums", "nazo-mapping"), pol("arm64", "armv7"))
	if c[0].Art.Name != "nazo-debug-arm64-v8a" || !c[0].Choice {
		t.Fatalf("first choice = %s", c[0].Art.Name)
	}
	skip := map[string]string{}
	for _, x := range c {
		if !x.Match {
			skip[x.Art.Name] = x.Skip
		}
	}
	if skip["nazo-debug-x86_64"] != "other architecture" || skip["nazo-debug-sha256sums"] != "checksum file" ||
		skip["nazo-mapping"] != "no pattern match" {
		t.Fatalf("skips: %v", skip)
	}
}

func TestX86Emulator(t *testing.T) {
	c := Rank(arts("app-debug-arm64-v8a", "app-debug-x86_64"), pol("x86_64", "x86"))
	if c[0].Art.Name != "app-debug-x86_64" {
		t.Fatal(c[0].Art.Name)
	}
}

func TestExplicitNameAndAllArch(t *testing.T) {
	p := pol("arm64")
	p.Name = "weird"
	c := Rank(arts("app-debug", "weird"), p)
	if c[0].Art.Name != "weird" || c[0].Score != 999 {
		t.Fatal("explicit name must win")
	}
	p = pol("arm64")
	p.AllArch = true
	c = Rank(arts("app-debug-x86_64", "app-debug-arm64"), p)
	if !c[0].Match || !c[1].Match || c[0].Art.Name != "app-debug-arm64" {
		t.Fatal("all-arch lets everything through but keeps this device first")
	}
}

func TestAPILevel(t *testing.T) {
	p := pol("arm64")
	p.SDK = 28
	if p.ArtifactRank("app-debug-android-34") > p.ArtifactRank("app-debug-android-26") {
		t.Fatal("an artifact needing a newer Android must lose")
	}
}

func TestCats(t *testing.T) {
	got := Cats([]string{"arm64-v8a", "armeabi-v7a", "armeabi"})
	if len(got) != 2 || got[0] != "arm64" || got[1] != "armv7" {
		t.Fatalf("%v", got)
	}
}
