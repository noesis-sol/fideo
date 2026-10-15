package fideo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// makeScanTree creates each named empty file (and each name ending in "/" as a
// directory) under dir.
func makeScanTree(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		p := filepath.Join(dir, name)
		if name[len(name)-1] == '/' {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func foundNames(files []videoFile) []string {
	names := []string{}
	for _, f := range files {
		names = append(names, f.name)
	}
	return names
}

// TestFindVideosScanRules pins which entries of a directory findVideos lists:
// video extensions in any case, but not other files, subdirectories, or an
// out_ output whose source (under any video extension) sits beside it.
func TestFindVideosScanRules(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []string
	}{
		{"output skipped beside its source", []string{"clip.mp4", "out_clip.mp4"}, []string{"clip.mp4"}},
		{"converted output skipped beside its source", []string{"clip.mov", "out_clip.mp4"}, []string{"clip.mov"}},
		{"output kept with no source", []string{"out_x.mp4"}, []string{"out_x.mp4"}},
		{"upper and mixed case extensions", []string{"a.MP4", "b.Mov"}, []string{"a.MP4", "b.Mov"}},
		{"non-videos and subdirectories ignored", []string{"clip.mp4", "notes.txt", "sub/", "dir.mp4/"}, []string{"clip.mp4"}},
		{"empty directory", nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeScanTree(t, dir, tt.files...)
			files := findVideos(dir)
			if got := foundNames(files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findVideos found %q, want %q", got, tt.want)
			}
			for _, f := range files {
				if want := filepath.Join(dir, f.name); f.path != want {
					t.Errorf("%s: path %q, want %q", f.name, f.path, want)
				}
				if f.selected || f.explicit || f.status != statusPending {
					t.Errorf("%s: got %+v, want an unselected, pending, scanned file", f.name, f)
				}
			}
		})
	}
}

// TestFindVideosNotADirectory: a missing path, a regular file, or an empty
// path yields no files rather than an error.
func TestFindVideosNotADirectory(t *testing.T) {
	tests := []struct {
		name string
		path func(dir string) string
	}{
		{"missing path", func(dir string) string { return filepath.Join(dir, "nope") }},
		{"regular file", func(dir string) string { return filepath.Join(dir, "clip.mp4") }},
		{"empty path", func(string) string { return "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeScanTree(t, dir, "clip.mp4")
			path := tt.path(dir)
			if files := findVideos(path); len(files) != 0 {
				t.Errorf("findVideos(%q) = %v, want no files", path, files)
			}
		})
	}
}

// TestFindVideosDashedNames: a file whose name starts with "-", found through
// the relative directory ".", gets a "./" prefix so ffmpeg can't read it as an
// option; found through a named subdirectory or an absolute directory, its path
// already starts with something else and is left as joined.
func TestFindVideosDashedNames(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name string
		scan func(dir string) string // the directory handed to findVideos
		want func(dir string) string
	}{
		{"current directory", func(string) string { return "." },
			func(string) string { return "." + sep + "-clip.mp4" }},
		{"relative subdirectory", func(string) string { return "sub" },
			func(string) string { return "sub" + sep + "-clip.mp4" }},
		{"absolute directory", func(dir string) string { return filepath.Join(dir, "sub") },
			func(dir string) string { return filepath.Join(dir, "sub", "-clip.mp4") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeScanTree(t, dir, "-clip.mp4", "sub/", "sub/-clip.mp4")
			t.Chdir(dir)
			scan, want := tt.scan(dir), tt.want(dir)
			files := findVideos(scan)
			if len(files) != 1 {
				t.Fatalf("findVideos(%q) = %v, want one file", scan, files)
			}
			if files[0].path != want || files[0].name != "-clip.mp4" {
				t.Errorf("got path %q name %q, want path %q name %q", files[0].path, files[0].name, want, "-clip.mp4")
			}
		})
	}
}

// TestSafeArgPathLeavesSafePathsAlone: only a relative path that begins with
// "-" (or names a protocol) is prefixed; an absolute path, even to a dashed
// name, and a relative path that doesn't begin with "-" are returned as given.
func TestSafeArgPathLeavesSafePathsAlone(t *testing.T) {
	sep := string(filepath.Separator)
	for _, p := range []string{
		filepath.Join(sep, "videos", "-clip.mp4"),
		filepath.Join(sep, "videos", "clip.mp4"),
		"clip.mp4",
		"clip-1.mp4",
		filepath.Join("sub", "-clip.mp4"),
		"." + sep + "-clip.mp4",
	} {
		if got := safeArgPath(p); got != p {
			t.Errorf("safeArgPath(%q) = %q, want it unchanged", p, got)
		}
	}
}
