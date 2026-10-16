package fideo

import (
	"runtime/debug"
	"testing"
)

// TestVersionString pins what --version prints for each kind of build: a tagged
// release, a pseudo-version past a tag, a dirty tree, and builds that carry no
// version (no VCS info, or no build info at all).
func TestVersionString(t *testing.T) {
	withVersion := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "fideo", Version: v}}
	}
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"tagged release", withVersion("v1.1.0"), true, "v1.1.0"},
		{"past a tag", withVersion("v1.1.1-0.20261016072447-1aa996c442cc"), true, "v1.1.1-0.20261016072447-1aa996c442cc"},
		{"dirty tree", withVersion("v1.1.0+dirty"), true, "v1.1.0+dirty"},
		{"no VCS info", withVersion("(devel)"), true, "(devel)"},
		{"empty version", withVersion(""), true, "(devel)"},
		{"no build info", nil, false, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionString(tt.info, tt.ok); got != tt.want {
				t.Errorf("versionString() = %q, want %q", got, tt.want)
			}
		})
	}
}
