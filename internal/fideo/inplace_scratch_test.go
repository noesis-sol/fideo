package fideo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCreateScratch pins fideo-nr2: the in-place scratch file exists, empty
// and owner-only, before ffmpeg writes a byte to it, so a private source's
// re-encoded contents are never readable by others mid-encode. A stale file
// or symlink at the scratch path is replaced, never written through.
func TestCreateScratch(t *testing.T) {
	tests := []struct {
		name  string
		stale func(t *testing.T, tmp, dir string) // what sits at tmp beforehand
	}{
		{"fresh", func(*testing.T, string, string) {}},
		{"stale world-readable file", func(t *testing.T, tmp, _ string) {
			writeSized(t, tmp, 50, 'x')
			if err := os.Chmod(tmp, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"stale symlink", func(t *testing.T, tmp, dir string) {
			if runtime.GOOS == "windows" {
				t.Skip("symlinks need privileges on Windows")
			}
			target := filepath.Join(dir, "victim")
			writeSized(t, target, 50, 'v')
			if err := os.Symlink(target, tmp); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "clip.mp4")
			tmp := tempOutputPath(src, src)
			tt.stale(t, tmp, dir)

			if err := createScratch(tmp); err != nil {
				t.Fatalf("createScratch: %v", err)
			}
			fi, err := os.Lstat(tmp)
			if err != nil {
				t.Fatalf("scratch file missing: %v", err)
			}
			if !fi.Mode().IsRegular() {
				t.Fatalf("scratch is %v, want a regular file", fi.Mode())
			}
			if fi.Size() != 0 {
				t.Errorf("scratch is %d bytes, want empty", fi.Size())
			}
			// Windows has no Unix permission bits.
			if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
				t.Errorf("scratch mode = %v, want no group/other access", fi.Mode().Perm())
			}
			if victim := filepath.Join(dir, "victim"); pathExists(victim) {
				if vi, _ := os.Stat(victim); vi.Size() != 50 {
					t.Errorf("symlink target was written through: %d bytes", vi.Size())
				}
			}
		})
	}
}
