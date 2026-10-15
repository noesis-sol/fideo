package fideo

import (
	"path/filepath"
	"testing"
)

// TestAutoStartOnlyForNamedFile pins that a lone video starts on launch only
// when the user named it: one found by scanning a directory or matching a
// pattern is pre-selected but waits for enter, so `fideo --overwrite` (or
// `fideo --overwrite dir/`) never replaces a file without a keypress.
func TestAutoStartOnlyForNamedFile(t *testing.T) {
	dir := t.TempDir()
	clip := writeFiles(t, dir, "clip.mp4")[0]

	tests := []struct {
		name      string
		build     func() (model, error)
		wantStart bool
	}{
		{"named file", func() (model, error) { return createModelFromPaths([]string{clip}) }, true},
		{"directory argument", func() (model, error) { return createModelFromPaths([]string{dir}) }, false},
		{"pattern argument", func() (model, error) {
			return createModelFromPaths([]string{filepath.Join(dir, "*.mp4")})
		}, false},
		{"current-directory scan", func() (model, error) { return initialModel(dir), nil }, false},
		// The same file reached both by name and through its directory is kept
		// once, and stays named whichever argument comes first.
		{"named then scanned", func() (model, error) { return createModelFromPaths([]string{clip, dir}) }, true},
		{"scanned then named", func() (model, error) { return createModelFromPaths([]string{dir, clip}) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := tt.build()
			if err != nil {
				t.Fatal(err)
			}
			if len(m.files) != 1 {
				t.Fatalf("got %d files, want 1", len(m.files))
			}
			if !m.files[0].selected {
				t.Error("lone file not pre-selected")
			}
			cmd := m.Init()
			if started := cmd != nil; started != tt.wantStart {
				t.Fatalf("auto-start = %v, want %v", started, tt.wantStart)
			}
			if cmd != nil {
				if _, ok := cmd().(autoStartMsg); !ok {
					t.Errorf("Init sent %T, want autoStartMsg", cmd())
				}
			}
		})
	}
}
