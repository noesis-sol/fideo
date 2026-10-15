package fideo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestFinishInPlaceKeepsAttrs pins fideo-3mk: replacing a source in place must
// keep its permissions and timestamps, not take on those of ffmpeg's new file —
// a private 0600 clip must not come back world-readable, and an old clip's
// mtime must not jump to now.
func TestFinishInPlaceKeepsAttrs(t *testing.T) {
	tests := []struct {
		name     string
		srcName  string
		destName string
		mode     os.FileMode
	}{
		{"same container, private", "clip.mp4", "clip.mp4", 0o600},
		{"same container, group-readable", "clip.mp4", "clip.mp4", 0o640},
		{"format conversion, private", "clip.mov", "clip.mp4", 0o600},
	}
	atime := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tt.srcName)
			dest := filepath.Join(dir, tt.destName)
			tmp := tempOutputPath(dest, src)
			writeSized(t, src, 200, 's')
			writeSized(t, tmp, 100, 't') // smaller, so it replaces the source
			if err := os.Chmod(src, tt.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(src, atime, mtime); err != nil {
				t.Fatal(err)
			}

			if _, err := finishInPlace(tmp, dest, src); err != nil {
				t.Fatalf("finishInPlace: %v", err)
			}
			fi, err := os.Stat(dest)
			if err != nil {
				t.Fatalf("destination missing: %v", err)
			}
			if fi.Size() != 100 {
				t.Fatalf("destination is %d bytes, want the 100-byte re-encode", fi.Size())
			}
			// Windows has no Unix permission bits; Chmod only toggles read-only.
			if runtime.GOOS != "windows" && fi.Mode().Perm() != tt.mode {
				t.Errorf("mode = %v, want %v", fi.Mode().Perm(), tt.mode)
			}
			if !fi.ModTime().Equal(mtime) {
				t.Errorf("mtime = %v, want %v", fi.ModTime(), mtime)
			}
			if got := accessTime(fi); !got.IsZero() && !got.Equal(atime) {
				t.Errorf("atime = %v, want %v", got, atime)
			}
		})
	}
}

// TestFinishInPlaceAttrsFailure keeps the source and removes the scratch file
// when the source's attributes can't be read, rather than publishing a file
// whose permissions may be wider than the original's.
func TestFinishInPlaceAttrsFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mov") // never created
	dest := filepath.Join(dir, "clip.mp4")
	tmp := tempOutputPath(dest, src)
	writeSized(t, tmp, 100, 't')

	if _, err := finishInPlace(tmp, dest, src); err == nil {
		t.Fatal("expected an error when the source can't be stat'ed")
	}
	if pathExists(tmp) {
		t.Error("scratch file must be removed after a failed attribute copy")
	}
	if pathExists(dest) {
		t.Error("destination must not be written when the attribute copy fails")
	}
}
