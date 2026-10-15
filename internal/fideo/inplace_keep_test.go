package fideo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSized writes a file of n bytes, all set to fill, so a test can tell which
// file ended up where by its size and contents.
func writeSized(t *testing.T, path string, n int, fill byte) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Repeat(string(fill), n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFinishInPlace pins fideo-wkh: --overwrite must not replace a source with a
// re-encode that came out no smaller, as a low-bitrate clip can.
func TestFinishInPlace(t *testing.T) {
	tests := []struct {
		name     string
		srcName  string
		destName string
		srcSize  int
		tmpSize  int
		wantKept bool // the source is left untouched and a note is returned
	}{
		{"smaller re-encode replaces source", "clip.mp4", "clip.mp4", 200, 100, false},
		{"larger re-encode keeps source", "clip.mp4", "clip.mp4", 148, 264, true},
		{"equal size keeps source", "clip.mp4", "clip.mp4", 150, 150, true},
		{"larger format conversion still replaces", "clip.mov", "clip.mp4", 148, 264, false},
		{"smaller format conversion replaces", "clip.mov", "clip.mp4", 200, 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tt.srcName)
			dest := filepath.Join(dir, tt.destName)
			tmp := tempOutputPath(dest, src)
			writeSized(t, src, tt.srcSize, 's')
			writeSized(t, tmp, tt.tmpSize, 't')

			note, err := finishInPlace(tmp, dest, src)
			if err != nil {
				t.Fatalf("finishInPlace: %v", err)
			}
			if pathExists(tmp) {
				t.Errorf("scratch file %s left behind", tmp)
			}

			if tt.wantKept {
				if note == "" {
					t.Error("kept source must be reported with a note")
				}
				got, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("source gone: %v", err)
				}
				if len(got) != tt.srcSize || got[0] != 's' {
					t.Errorf("source was modified: %d bytes starting %q", len(got), got[0])
				}
				return
			}

			if note != "" {
				t.Errorf("replaced source must not carry a note; got %q", note)
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("destination missing: %v", err)
			}
			if len(got) != tt.tmpSize || got[0] != 't' {
				t.Errorf("destination is not the re-encode: %d bytes starting %q", len(got), got[0])
			}
			if src != dest && pathExists(src) {
				t.Errorf("source %s must be removed after a format change", src)
			}
		})
	}
}

// TestFinishInPlaceNoteSizes checks the note names both sizes, so the user can
// see why the source was kept.
func TestFinishInPlaceNoteSizes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	tmp := tempOutputPath(src, src)
	writeSized(t, src, 148<<10, 's')
	writeSized(t, tmp, 264<<10, 't')

	note, err := finishInPlace(tmp, src, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kept original", "264.0 KB", "148.0 KB"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
}

// TestFinishInPlaceRenameFailure keeps the source and cleans up when the scratch
// file can't be moved over the destination.
func TestFinishInPlaceRenameFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mov")
	dest := filepath.Join(dir, "missing", "clip.mp4") // parent doesn't exist
	tmp := filepath.Join(dir, ".clip.mov"+tempOutputMarker+".mp4")
	writeSized(t, src, 200, 's')
	writeSized(t, tmp, 100, 't')

	if _, err := finishInPlace(tmp, dest, src); err == nil {
		t.Fatal("expected an error when the rename fails")
	}
	if pathExists(tmp) {
		t.Error("scratch file must be removed after a failed rename")
	}
	if !pathExists(src) {
		t.Error("source must survive a failed rename")
	}
}

// TestHandleDoneRecordsNote shows a kept source on its row: handleDone stores
// the note, the row renders it, and rowLines counts the extra line.
func TestHandleDoneRecordsNote(t *testing.T) {
	m := newModel([]videoFile{{name: "clip.mp4", path: "clip.mp4", status: statusProcessing, info: "in"}})
	m.width = 80
	m.processing = true
	m.processingCount = 1
	m.totalToProcess = 1

	m, _ = m.handleDone(doneMsg{idx: 0, note: "kept original: re-encode was larger"})
	f := m.files[0]
	if f.status != statusDone || f.note == "" {
		t.Fatalf("status %v note %q; want done with a note", f.status, f.note)
	}

	var s strings.Builder
	m.renderFileRow(&s, 0, f)
	out := s.String()
	if !strings.Contains(out, "kept original") {
		t.Errorf("row does not show the note:\n%s", out)
	}
	if got, want := strings.Count(out, "\n"), rowLines(f); got != want {
		t.Errorf("renderFileRow emitted %d lines, rowLines says %d:\n%s", got, want, out)
	}
}
