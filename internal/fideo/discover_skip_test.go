package fideo

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestFindVideosSkipsHiddenAndOwnOutputs pins that a directory scan skips
// dotfiles (macOS AppleDouble "._" sidecars) and every name fideo itself
// writes beside a source — including the ones resolveOutputPath disambiguates
// within a batch (out_<stem>_<srcext>, out_<stem>-<n>) — while an out_ file
// whose source is gone, or that only looks like a disambiguated name, is kept.
func TestFindVideosSkipsHiddenAndOwnOutputs(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"AppleDouble sidecars", []string{"._a.mov", "a.mov", "._b.MP4"}, []string{"a.mov"}},
		{"other dotfiles", []string{".hidden.mp4", "clip.mp4"}, []string{"clip.mp4"}},
		{"source-extension disambiguation", []string{"a.mp4", "a.mov", "out_a.mp4", "out_a_mov.mp4"}, []string{"a.mov", "a.mp4"}},
		{"source-extension disambiguation of an upper-case source", []string{"a.MOV", "out_a_mov.mp4"}, []string{"a.MOV"}},
		{"numeric disambiguation", []string{"a.mp4", "out_a-1.mp4", "out_a-12.mkv"}, []string{"a.mp4"}},
		{"output beside an upper-case source", []string{"clip.MOV", "out_clip.mp4"}, []string{"clip.MOV"}},
		{"disambiguated name with no source", []string{"out_a_mov.mp4", "out_a-1.mp4"}, []string{"out_a-1.mp4", "out_a_mov.mp4"}},
		{"source extension does not match", []string{"a.mp4", "out_a_mkv.mp4"}, []string{"a.mp4", "out_a_mkv.mp4"}},
		{"suffix that is not a counter", []string{"a.mp4", "out_a-x.mp4", "out_a-0.mp4", "out_a-.mp4"}, []string{"a.mp4", "out_a-.mp4", "out_a-0.mp4", "out_a-x.mp4"}},
		{"underscore that is part of the stem", []string{"my_clip.mp4", "out_my_clip.mp4"}, []string{"my_clip.mp4"}},
		{"dash that is part of the stem", []string{"day-2.mov", "out_day-2.mp4"}, []string{"day-2.mov"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeScanTree(t, dir, tt.files...)
			if got := sortedNames(findVideos(dir)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findVideos found %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCollectVideosFromPatternSkipsDotfiles pins that, like a shell, a pattern
// whose last element doesn't start with "." doesn't match dotfiles, while one
// that does (e.g. "._*") still reaches them.
func TestCollectVideosFromPatternSkipsDotfiles(t *testing.T) {
	tests := []struct {
		pattern string
		want    []string
	}{
		{"*.mov", []string{"a.mov"}},
		{"*a.mov", []string{"a.mov"}},
		{"._*", []string{"._a.mov"}},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			dir := t.TempDir()
			makeScanTree(t, dir, "a.mov", "._a.mov")
			files, err := collectVideosFromPattern(filepath.Join(dir, tt.pattern))
			if err != nil {
				t.Fatal(err)
			}
			if got := sortedNames(files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("names = %q, want %q", got, tt.want)
			}
		})
	}
}
