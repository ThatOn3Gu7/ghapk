package device

import "testing"

func TestProbeDebugX(t *testing.T) {
	in := Probe()
	t.Logf("%+v", in)
}
