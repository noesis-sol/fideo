package fideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFiles creates each named file in dir with distinct contents and returns
// their full paths.
func writeFiles(t *testing.T, dir string, names ...string) []string {
	t.Helper()
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.Join(dir, name)
		if err := os.WriteFile(paths[i], []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// caseSensitiveDir reports whether dir's filesystem tells apart names that
// differ only in case (Linux by default; not macOS or Windows by default).
func caseSensitiveDir(t *testing.T, dir string) bool {
	t.Helper()
	probe := writeFiles(t, dir, ".case-probe")[0]
	defer os.Remove(probe)
	return !pathExists(filepath.Join(dir, ".CASE-PROBE"))
}

// TestInPlaceDestKeepsSourceExtensionCase pins that a source whose extension
// already names --format in another case keeps its own spelling, so the
// destination is the source itself on every filesystem.
func TestInPlaceDestKeepsSourceExtensionCase(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		format string
		want   string
	}{
		{"upper ext same format", "dir/CLIP.MP4", "mp4", "dir/CLIP.MP4"},
		{"mixed case ext same format", "dir/clip.Mov", "mov", "dir/clip.Mov"},
		{"upper ext other format", "dir/CLIP.MOV", "mp4", "dir/CLIP.mp4"},
		{"no ext gains format", "dir/clip", "mp4", "dir/clip.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inPlaceDest(tt.input, tt.format); got != tt.want {
				t.Errorf("inPlaceDest(%q, %q) = %q, want %q", tt.input, tt.format, got, tt.want)
			}
		})
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, "a.mp4", "b.mp4")
	a, b := paths[0], paths[1]
	link := filepath.Join(dir, "link.mp4")
	if err := os.Symlink(a, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	missing := filepath.Join(dir, "missing.mp4")

	tests := []struct {
		name string
		x, y string
		want bool
	}{
		{"identical path", a, a, true},
		{"different spelling of one file", a, filepath.Join(dir, ".", "a.mp4"), true},
		{"symlink to the file", link, a, true},
		{"distinct files", a, b, false},
		{"missing destination", missing, a, false},
		{"missing on both sides", missing, missing, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameFile(tt.x, tt.y); got != tt.want {
				t.Errorf("sameFile(%q, %q) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

// inPlaceModel returns a model with one selected, pending in-place file.
func inPlaceModel(path, format string) model {
	return model{
		files:   []videoFile{{path: path, name: filepath.Base(path), selected: true, status: statusPending}},
		cancels: map[int]context.CancelFunc{},
		config:  compressionConfig{maxConcurrent: 2, inPlace: true, outputFormat: format},
	}
}

// TestClobbersOtherFile pins the overwrite-prompt gate: only an existing file
// that is not the source itself counts as a clobber, whatever its spelling.
func TestClobbersOtherFile(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, "clip.MOV", "clip.mp4")
	src, other := paths[0], paths[1]
	link := filepath.Join(dir, "alias.MOV")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	tests := []struct {
		name string
		file videoFile
		want bool
	}{
		{"destination is the source", videoFile{path: src, outPath: inPlaceDest(src, "mov")}, false},
		{"destination is the source via another path", videoFile{path: link, outPath: src}, false},
		{"destination does not exist", videoFile{path: src, outPath: filepath.Join(dir, "clip.mkv")}, false},
		{"destination is a distinct existing file", videoFile{path: src, outPath: other}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clobbersOtherFile(tt.file); got != tt.want {
				t.Errorf("clobbersOtherFile(%+v) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}

// TestFillSlotsInPlaceCaseDistinctFilePrompts reproduces the data-loss bug on a
// case-sensitive filesystem: clip.MOV under --overwrite --format mov beside an
// unrelated clip.mov must replace clip.MOV itself, and a destination that
// differs from the source only in case is a distinct file that must prompt.
func TestFillSlotsInPlaceCaseDistinctFilePrompts(t *testing.T) {
	dir := t.TempDir()
	if !caseSensitiveDir(t, dir) {
		t.Skip("filesystem is case-insensitive; case-only names are one file here")
	}
	paths := writeFiles(t, dir, "clip.MOV", "clip.mov")
	src, other := paths[0], paths[1]

	m := inPlaceModel(src, "mov")
	if got := m.resolveOutputPath(0); got != src {
		t.Errorf("resolveOutputPath = %q, want the source %q (not the unrelated %q)", got, src, other)
	}

	m.files[0].outPath = other
	m, cmds := m.fillSlots(-1, nil)
	if !m.showOverwritePrompt {
		t.Fatal("expected a prompt before clobbering a case-distinct, unrelated file")
	}
	if m.pendingOutputFile != other {
		t.Errorf("pendingOutputFile = %q, want %q", m.pendingOutputFile, other)
	}
	if len(cmds) != 0 {
		t.Errorf("no encode should start while the prompt is pending; got %d cmds", len(cmds))
	}
}
