package versionconstraint

import "testing"

func TestMatch(t *testing.T) {
	for _, tt := range []struct {
		version, constraint string
		want                bool
	}{
		{"17.0.0", ">=17", true},
		{"16.9.9", ">=17", false},
		{"25.0.4", ">=17 <26", true},
		{"26.0.0", ">=17 <26", false},
		{"25.0.4", ">=17, <26", true},
		{"3.9.16", "3.9.16", true},
		{"3.9.17", "3.9.16", false},
		{"3.9.16", "=3.9.16", true},
		{"3.9.16", "<=3.9.16", true},
		{"3.9.16", "<3.9.16", false},
		{"3.10.0", "^3.9.0", true},
		{"4.0.0", "^3.9.0", false},
		{"3.9.16", "~3.9.0", true},
		{"3.10.0", "~3.9.0", false},
		{"0.3.0", "^0.2.3", false},
		{"17.0.1", "17.x", true},
		{"18.0.0", "17", false},
		{"21.0.3", "17.x || 21.x", true},
		{"20.0.0", "17.x || 21.x", false},
		{"21.0.0", "17.0.0 - 21.0.0", true},
		{"21.0.0-rc.1", ">=17", false},
		{"21.0.0-rc.1", ">=21.0.0-rc.0 <22", true},
		{"3.9.16+build.2", "3.9.16+build.1", true},
	} {
		t.Run(tt.version+"/"+tt.constraint, func(t *testing.T) {
			got, err := Match(tt.version, tt.constraint)
			if err != nil || got != tt.want {
				t.Fatalf("Match = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestRejectInvalidInput(t *testing.T) {
	for _, constraint := range []string{"", " ", "=>17", "banana", ">=25.0.4.1+1"} {
		if _, err := Match("25.0.4", constraint); err == nil {
			t.Errorf("accepted invalid constraint %q", constraint)
		}
	}
	for _, version := range []string{"25.0.4.1+1", "17", "01.0.0", "unknown"} {
		if _, err := Match(version, ">=17"); err == nil {
			t.Errorf("coerced invalid version %q", version)
		}
	}
}
