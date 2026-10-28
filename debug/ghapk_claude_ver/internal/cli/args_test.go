package cli

import "testing"

func TestParseArgs(t *testing.T) {
	c, err := parseArgs([]string{"-r", "a/b", "--arch", "arm64-v8a", "-y", "--no-releases", "--run=#42", "--set", "theme=nord", "--unset", "branch"})
	if err != nil {
		t.Fatal(err)
	}
	if c.vals["repo"] != "a/b" || c.vals["arch"] != "arm64" || c.vals["source"] != "actions" || !c.yes || c.runID != 42 {
		t.Errorf("%+v", c)
	}
	if len(c.setOps) != 2 || c.setOps[0].key != "theme" || c.setOps[0].val != "nord" || c.setOps[1].key != "branch" {
		t.Errorf("%+v", c.setOps)
	}
	if !c.wantHeadless() {
		t.Error("-y should run without the interface")
	}
	if c2, _ := parseArgs([]string{"owner/name"}); c2.vals["repo"] != "owner/name" || c2.wantHeadless() {
		t.Error("a bare repo should open the interface on it")
	}
	if _, err := parseArgs([]string{"--arch", "nope"}); err == nil {
		t.Error("bad enum accepted")
	}
	if _, err := parseArgs([]string{"--bogus"}); err == nil {
		t.Error("unknown flag accepted")
	}
	if c3, err := parseArgs([]string{"--width", "80", "--spinner", "dots", "--no-color"}); err != nil || len(c3.vals) != 0 {
		t.Errorf("legacy flags should be ignored: %v %v", err, c3)
	}
}
