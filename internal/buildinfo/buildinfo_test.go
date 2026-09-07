package buildinfo

import "testing"

func TestGet(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	// A test binary carries no module version, so the fallback is what a
	// message built from Get says.
	Version = ""
	if Get() != "(devel)" {
		t.Errorf("Get = %s, want (devel)", Get())
	}
	Version = "v1.4.0"
	if Get() != "v1.4.0" {
		t.Errorf("Get = %s", Get())
	}
}
