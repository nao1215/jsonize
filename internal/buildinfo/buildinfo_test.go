package buildinfo

import "testing"

func TestParseSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    Semver
		wantErr bool
	}{
		{"v1.2.3", Semver{1, 2, 3}, false},
		{"0.1.0", Semver{0, 1, 0}, false},
		{"1.2.3-rc1", Semver{1, 2, 3}, false},
		{"1.2.3+build", Semver{1, 2, 3}, false},
		{"1.2", Semver{}, true},
		{"a.b.c", Semver{}, true},
		{"1.-1.0", Semver{}, true},
		{"(devel)", Semver{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSemver(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSemver(%q) err = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseSemver(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestLessAndString(t *testing.T) {
	t.Parallel()
	a, b := Semver{1, 2, 3}, Semver{1, 3, 0}
	if !a.Less(b) || b.Less(a) {
		t.Error("minor comparison")
	}
	if !(Semver{0, 9, 9}).Less(Semver{1, 0, 0}) {
		t.Error("major comparison")
	}
	if !(Semver{1, 0, 0}).Less(Semver{1, 0, 1}) || (Semver{1, 0, 1}).Less(Semver{1, 0, 1}) {
		t.Error("patch comparison")
	}
	if a.String() != "1.2.3" {
		t.Errorf("String = %s", a.String())
	}
}

func TestGetAndCurrent(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = ""
	if Get() == "" {
		t.Error("Get returned empty")
	}
	if _, ok := Current(); ok {
		// A test binary has no module version; Current must report false.
		t.Error("Current should report a development build")
	}
	Version = "v1.4.0"
	if Get() != "v1.4.0" {
		t.Errorf("Get = %s", Get())
	}
	v, ok := Current()
	if !ok || v != (Semver{1, 4, 0}) {
		t.Errorf("Current = %v, %v", v, ok)
	}
}
