package fideo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// sortedNames returns the files' names in sorted order, so assertions don't
// depend on glob or directory-listing order.
func sortedNames(files []videoFile) []string {
	names := foundNames(files)
	sort.Strings(names)
	return names
}

// TestCollectVideosFromPatternErrors pins the errors a pattern argument
// reports: no match at all, matches that hold no video, and a malformed
// pattern each fail instead of yielding an empty list.
func TestCollectVideosFromPatternErrors(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "notes.txt", "cover.jpg")

	tests := []struct {
		name    string
		pattern string
		wantErr string
	}{
		{"matches nothing", filepath.Join(dir, "*.mp4"), "no files or directories match pattern: "},
		{"matches only non-videos", filepath.Join(dir, "*"), "no video files found matching pattern: "},
		{"malformed", filepath.Join(dir, "["), "error expanding pattern: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := collectVideosFromPattern(tt.pattern)
			if err == nil {
				t.Fatalf("got %v, want an error", foundNames(files))
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestCollectVideosFromPatternMalformedIsBadPattern pins that a malformed
// pattern wraps filepath.ErrBadPattern, both called directly and when an
// argument that names no real file falls through to pattern expansion.
func TestCollectVideosFromPatternMalformedIsBadPattern(t *testing.T) {
	pattern := filepath.Join(t.TempDir(), "[")
	if _, err := collectVideosFromPattern(pattern); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("collectVideosFromPattern(%q) = %v, want ErrBadPattern", pattern, err)
	}
	if _, err := collectVideosFromArg(pattern); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("collectVideosFromArg(%q) = %v, want ErrBadPattern", pattern, err)
	}
}

// TestCollectVideosFromPatternScansDirectories pins that a pattern matching a
// directory lists the videos inside it, alongside the video files it matches
// directly, and skips matched non-videos. Pattern results are not pre-selected.
func TestCollectVideosFromPatternScansDirectories(t *testing.T) {
	dir := t.TempDir()
	makeScanTree(t, dir, "videos/", "videos/a.mp4", "videos/b.mov", "videos/notes.txt",
		"vid.mkv", "vid.txt", "other.mp4")

	files, err := collectVideosFromPattern(filepath.Join(dir, "vid*"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sortedNames(files), []string{"a.mp4", "b.mov", "vid.mkv"}; !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	for _, f := range files {
		if f.selected || f.explicit {
			t.Errorf("%s: selected=%v explicit=%v, want a pattern match left unselected", f.name, f.selected, f.explicit)
		}
		if !filepath.IsAbs(f.path) {
			t.Errorf("%s: path %q is not absolute", f.name, f.path)
		}
	}
}

// TestCollectVideosFromArgRefusals pins the errors for a file argument that
// can't be compressed: a non-video file names the supported extensions, and a
// missing path (with no glob metacharacters) returns the stat error itself.
func TestCollectVideosFromArgRefusals(t *testing.T) {
	dir := t.TempDir()
	notes := writeFiles(t, dir, "notes.txt")[0]

	_, err := collectVideosFromArg(notes)
	if err == nil {
		t.Fatal("non-video file accepted")
	}
	for _, want := range []string{notes + " is not a supported video file", "Supported extensions: .mp4, .mov, .avi, .mkv, .m4v, .webm"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}

	missing := filepath.Join(dir, "missing.mp4")
	_, err = collectVideosFromArg(missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing path: error = %v, want ErrNotExist", err)
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != missing {
		t.Errorf("missing path: error = %#v, want a *fs.PathError for %q", err, missing)
	}
}

// TestCollectVideosFromArgNamedFile pins that a named video comes back as one
// absolute, pre-selected, explicit entry.
func TestCollectVideosFromArgNamedFile(t *testing.T) {
	clip := writeFiles(t, t.TempDir(), "Clip.MOV")[0]
	files, err := collectVideosFromArg(clip)
	if err != nil {
		t.Fatal(err)
	}
	want := []videoFile{{path: clip, name: "Clip.MOV", selected: true, explicit: true}}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %+v, want %+v", files, want)
	}
}

// TestCreateModelFromPathsDedups pins that a file reached through several
// arguments — its directory, a pattern, and its own name — is listed once.
func TestCreateModelFromPathsDedups(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, "a.mp4", "b.mov")
	a := paths[0]

	tests := []struct {
		name string
		args []string
	}{
		{"directory and name", []string{dir, a}},
		{"name and directory", []string{a, dir}},
		{"pattern and name", []string{filepath.Join(dir, "*"), a}},
		{"directory and pattern", []string{dir, filepath.Join(dir, "*.m*")}},
		{"name twice", []string{a, a, dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := createModelFromPaths(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := sortedNames(m.files), []string{"a.mp4", "b.mov"}; !reflect.DeepEqual(got, want) {
				t.Errorf("names = %v, want %v", got, want)
			}
		})
	}
}

// TestCreateModelFromPathsErrors pins that arguments holding no video fail
// with "no video files found", and that one bad argument fails the whole call.
func TestCreateModelFromPathsErrors(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	clip := writeFiles(t, dir, "clip.mp4", "notes.txt")[0]

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"empty directory", []string{empty}, "no video files found in the given paths"},
		{"two empty directories", []string{empty, empty}, "no video files found in the given paths"},
		{"missing path among videos", []string{clip, filepath.Join(dir, "gone.mp4")}, "gone.mp4"},
		{"non-video among videos", []string{clip, filepath.Join(dir, "notes.txt")}, "is not a supported video file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := createModelFromPaths(tt.args)
			if err == nil {
				t.Fatalf("got %v, want an error", foundNames(m.files))
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestNewModelSelectionAndInit pins that newModel pre-selects only a lone file,
// and Init returns autoStartMsg only when that lone file was named explicitly.
func TestNewModelSelectionAndInit(t *testing.T) {
	tests := []struct {
		name         string
		files        []videoFile
		wantSelected []bool
		wantStart    bool
	}{
		{"no files", nil, nil, false},
		{"lone named file", []videoFile{{name: "a.mp4", explicit: true}}, []bool{true}, true},
		{"lone scanned file", []videoFile{{name: "a.mp4"}}, []bool{true}, false},
		{"two scanned files", []videoFile{{name: "a.mp4"}, {name: "b.mp4"}}, []bool{false, false}, false},
		{"two named files", []videoFile{
			{name: "a.mp4", selected: true, explicit: true},
			{name: "b.mp4", selected: true, explicit: true},
		}, []bool{true, true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(tt.files)
			var selected []bool
			for _, f := range m.files {
				selected = append(selected, f.selected)
			}
			if !reflect.DeepEqual(selected, tt.wantSelected) {
				t.Errorf("selected = %v, want %v", selected, tt.wantSelected)
			}
			cmd := m.Init()
			if started := cmd != nil; started != tt.wantStart {
				t.Fatalf("auto-start = %v, want %v", started, tt.wantStart)
			}
			if cmd != nil {
				if msg := cmd(); msg != (autoStartMsg{}) {
					t.Errorf("Init sent %T, want autoStartMsg", msg)
				}
			}
		})
	}
}
