package fideo

import (
	"context"
	"path/filepath"
	"testing"
)

// collisionModel returns an in-place model with every path selected and pending.
func collisionModel(format string, paths ...string) model {
	files := make([]videoFile, len(paths))
	for i, p := range paths {
		files[i] = videoFile{path: p, name: filepath.Base(p), selected: true, status: statusPending}
	}
	return model{
		files:   files,
		cancels: map[int]context.CancelFunc{},
		config:  compressionConfig{inPlace: true, outputFormat: format},
	}
}

// TestMarkInPlaceCollisionsCaseOnly pins that in-place destinations differing
// only in case collide exactly when the filesystem makes them one file. On a
// case-sensitive filesystem the case-distinct sources are created side by side
// and must all proceed; on a case-insensitive one the same names reach one file,
// so they must be refused.
func TestMarkInPlaceCollisionsCaseOnly(t *testing.T) {
	tests := []struct {
		name   string
		format string
		files  []string // created on disk, where the filesystem can hold them
		picked []string // selected, by these spellings
	}{
		{"plain overwrite, case-only source names", "", []string{"clip.MOV", "clip.mov"}, []string{"clip.MOV", "clip.mov"}},
		{"format keeps both spellings", "mov", []string{"clip.MOV", "clip.mov"}, []string{"clip.MOV", "clip.mov"}},
		{"format converts case-only stems", "mp4", []string{"CLIP.mov", "clip.mov"}, []string{"CLIP.mov", "clip.mov"}},
		{"existing destination in another case", "mp4", []string{"CLIP.MP4", "clip.mov"}, []string{"CLIP.MP4", "clip.mov"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sensitive := caseSensitiveDir(t, dir)
			for _, name := range tt.files {
				if !pathExists(filepath.Join(dir, name)) {
					writeFiles(t, dir, name)
				}
			}
			paths := make([]string, len(tt.picked))
			for i, name := range tt.picked {
				paths[i] = filepath.Join(dir, name)
			}

			m := collisionModel(tt.format, paths...)
			m.markInPlaceCollisions()

			want := statusError
			if sensitive {
				want = statusPending
			}
			for i, f := range m.files {
				if f.status != want {
					t.Errorf("file %d (%s): status %v, want %v (case-sensitive filesystem: %v; err: %v)",
						i, filepath.Base(f.path), f.status, want, sensitive, f.err)
				}
			}
		})
	}
}

// TestSameDestination pins the filesystem-independent answers: identical
// spellings are one destination, and destinations that differ only in case are
// one exactly when the filesystem says so.
func TestSameDestination(t *testing.T) {
	dir := t.TempDir()
	sensitive := caseSensitiveDir(t, dir)
	src := writeFiles(t, dir, "clip.mov")[0]
	upper := filepath.Join(dir, "CLIP.mp4")
	lower := filepath.Join(dir, "clip.mp4")

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical spelling, not on disk", lower, lower, true},
		{"case-only difference, not on disk", lower, upper, !sensitive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameDestination(tt.a, tt.b, src); got != tt.want {
				t.Errorf("sameDestination(%q, %q, %q) = %v, want %v", tt.a, tt.b, src, got, tt.want)
			}
		})
	}
}
