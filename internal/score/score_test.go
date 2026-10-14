package score

import "testing"

var arm64Phone = Device{ABIs: []string{"arm64-v8a", "armeabi-v7a", "armeabi"}, SDK: 34}

func TestArchTags(t *testing.T) {
	cases := map[string]string{
		"app-arm64-v8a-debug.apk":   "arm64",
		"APK-armeabi-v7a-release":   "armv7",
		"build-x86_64.zip":          "x86_64",
		"thing-x64.apk":             "x86_64",
		"plain-name":                "none",
		"app-universal-debug.apk":   "universal",
		"app-arm64-v8a+armeabi.apk": "arm64 armv7",
	}
	for in, want := range cases {
		if got := ArchTags(in); got != want {
			t.Errorf("ArchTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArm64PhonePrefersArm64(t *testing.T) {
	p := Prefs{Arch: "auto", Variant: "auto"}
	arm := ArtifactRank("app-arm64-v8a-debug.apk", p, arm64Phone)
	uni := ArtifactRank("app-universal-debug.apk", p, arm64Phone)
	x86 := ArtifactRank("app-x86_64-debug.apk", p, arm64Phone)
	if !(arm > uni && uni > x86) {
		t.Errorf("ranking wrong on arm64 phone: arm=%d uni=%d x86=%d", arm, uni, x86)
	}
	// numbers should match the bash: debug(+90)+arch(+70)+apk(+40) = 200
	if arm != 200 {
		t.Errorf("arm64 apk rank = %d, want 200 (90 var + 70 arch + 40 apk)", arm)
	}
}

func TestVariant(t *testing.T) {
	p := Prefs{Arch: "auto", Variant: "auto"}
	if !VariantAllowed("app-release.apk", "auto") || VariantAllowed("app-release.apk", "debug") {
		t.Error("variant_allowed mismatch")
	}
	release := ArtifactRank("app-arm64-v8a-release.apk", p, arm64Phone)
	debug := ArtifactRank("app-arm64-v8a-debug.apk", p, arm64Phone)
	if debug <= release {
		t.Errorf("debug should outrank release with VARIANT_PREF=auto: %d vs %d", debug, release)
	}
}

func TestPolicy(t *testing.T) {
	p := Prefs{Arch: "auto", Variant: "auto"}
	if !AllowedByPolicy("app-arm64-v8a-debug.apk", p, arm64Phone) {
		t.Error("arm64 apk should be allowed on an arm64 phone")
	}
	if AllowedByPolicy("app-x86_64-debug.apk", p, arm64Phone) {
		t.Error("x86_64 apk must be rejected on a pure arm64 phone")
	}
	if !AllowedByPolicy("app-universal-debug.apk", p, arm64Phone) {
		t.Error("universal always allowed")
	}
	// armv7 is compatible with arm64 devices
	if !AllowedByPolicy("app-armeabi-v7a-debug.apk", p, arm64Phone) {
		t.Error("armv7 apk should be allowed on an arm64 phone")
	}
}

func TestAPIScore(t *testing.T) {
	if got := apiScore("app-android-30.apk", 34); got != 30 {
		t.Errorf("apiScore = %d want 30", got)
	}
	if got := apiScore("app-android-35.apk", 34); got != -1000 {
		t.Errorf("apiScore too-new = %d want -1000", got)
	}
}
