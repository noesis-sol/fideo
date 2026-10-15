package fideo

import (
	"path/filepath"
	"testing"
)

func TestGetOutputPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		format string
		want   string
	}{
		{"format swaps extension", "clip.mov", "mp4", "out_clip.mp4"},
		{"format same as source", "clip.mp4", "mp4", "out_clip.mp4"},
		{"no format keeps extension", "clip.mov", "", "out_clip.mov"},
		{"no format keeps upper-case extension", "CLIP.MOV", "", "out_CLIP.MOV"},
		{"keeps relative directory", filepath.Join("dir", "sub", "clip.mkv"), "webm", filepath.Join("dir", "sub", "out_clip.webm")},
		{"keeps absolute directory", filepath.Join("/v", "clip.mov"), "", filepath.Join("/v", "out_clip.mov")},
		{"only the last extension is swapped", "a.b.mov", "mp4", "out_a.b.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getOutputPath(tt.input, tt.format); got != tt.want {
				t.Errorf("getOutputPath(%q, %q) = %q, want %q", tt.input, tt.format, got, tt.want)
			}
		})
	}
}

// TestResolveOutputPathDisambiguates resolves each file in order, recording its
// claim the way startFile does, so every later file sees the earlier claims.
func TestResolveOutputPathDisambiguates(t *testing.T) {
	tests := []struct {
		name   string
		format string
		paths  []string
		want   []string
	}{
		{"first claimant keeps the natural name", "mp4", []string{"a.mp4"}, []string{"out_a.mp4"}},
		{"distinct stems do not interfere", "mp4", []string{"a.mp4", "b.mov"}, []string{"out_a.mp4", "out_b.mp4"}},
		{
			"second source folds in its extension, third gets a counter", "mp4",
			[]string{"a.mp4", "a.mov", "a.MOV"},
			[]string{"out_a.mp4", "out_a_mov.mp4", "out_a-1.mp4"},
		},
		{
			"counter increments past taken numbers", "mp4",
			[]string{"a.mp4", "a.mov", "a.mov", "a.mov"},
			[]string{"out_a.mp4", "out_a_mov.mp4", "out_a-1.mp4", "out_a-2.mp4"},
		},
		{"source extension is lowercased", "mp4", []string{"a.mp4", "a.MOV"}, []string{"out_a.mp4", "out_a_mov.mp4"}},
		{"claims differing only in case are taken", "mp4", []string{"A.mp4", "a.mov"}, []string{"out_A.mp4", "out_a_mov.mp4"}},
		{"no format keeps each container apart", "", []string{"a.mp4", "a.mov"}, []string{"out_a.mp4", "out_a.mov"}},
		{"other directories do not collide", "mp4", []string{"a.mp4", filepath.Join("d", "a.mov")}, []string{"out_a.mp4", filepath.Join("d", "out_a.mp4")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{config: compressionConfig{outputFormat: tt.format}}
			for _, p := range tt.paths {
				m.files = append(m.files, videoFile{path: p})
			}
			for i := range m.files {
				m.files[i].outPath = m.resolveOutputPath(i)
			}
			for i, want := range tt.want {
				if got := m.files[i].outPath; got != want {
					t.Errorf("file %d (%q): outPath = %q, want %q", i, tt.paths[i], got, want)
				}
			}
		})
	}
}

// TestResolveOutputPathIgnoresOwnClaim re-resolves a file that already holds a
// claim: its own outPath must not count as taken, or a retry would drift to a
// disambiguated name.
func TestResolveOutputPathIgnoresOwnClaim(t *testing.T) {
	m := model{
		config: compressionConfig{outputFormat: "mp4"},
		files: []videoFile{
			{path: "a.mp4", outPath: "out_a.mp4"},
			{path: "a.mov", outPath: "out_a_mov.mp4"},
		},
	}
	for i, want := range []string{"out_a.mp4", "out_a_mov.mp4"} {
		if got := m.resolveOutputPath(i); got != want {
			t.Errorf("resolveOutputPath(%d) = %q, want its own claim %q", i, got, want)
		}
	}
}
