package fideo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSafeArgPath pins which paths get a "./" prefix: those ffmpeg/ffprobe
// would read as an option (leading "-") or as a URL protocol ("NAME:" in the
// first path element). Everything else is passed through unchanged.
func TestSafeArgPath(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain name", "clip.mp4", "clip.mp4"},
		{"leading dash", "-clip.mp4", "." + sep + "-clip.mp4"},
		{"colon in bare name", "12:30.mp4", "." + sep + "12:30.mp4"},
		{"scheme-like bare name", "file:clip.mp4", "." + sep + "file:clip.mp4"},
		{"hidden temp with colon", ".12:30.mp4.fideo-tmp.mp4", "." + sep + ".12:30.mp4.fideo-tmp.mp4"},
		{"colon in first dir", filepath.Join("a:b", "clip.mp4"), "." + sep + filepath.Join("a:b", "clip.mp4")},
		{"colon below a dir", filepath.Join("sub", "12:30.mp4"), filepath.Join("sub", "12:30.mp4")},
		{"already dot-slash", "." + sep + "12:30.mp4", "." + sep + "12:30.mp4"},
		{"absolute with colon", filepath.Join(sep, "videos", "12:30.mp4"), filepath.Join(sep, "videos", "12:30.mp4")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeArgPath(tt.in); got != tt.want {
				t.Errorf("safeArgPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestFindVideosGuardsColonNames reproduces the bug: a file in the current
// directory named "12:30.mp4" was handed to ffprobe bare and failed with
// "Protocol not found". findVideos(".") must yield a "./"-prefixed path.
func TestFindVideosGuardsColonNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "12:30.mp4"), nil, 0o644); err != nil {
		t.Skipf("filesystem does not allow ':' in names: %v", err)
	}
	t.Chdir(dir)

	files := findVideos(".")
	if len(files) != 1 {
		t.Fatalf("findVideos(.) = %v, want one file", files)
	}
	want := "." + string(filepath.Separator) + "12:30.mp4"
	if files[0].path != want || files[0].name != "12:30.mp4" {
		t.Errorf("got path %q name %q, want path %q name %q", files[0].path, files[0].name, want, "12:30.mp4")
	}
}

// TestFFmpegArgsGuardColonPaths checks the guard at the ffmpeg boundary itself:
// the in-place scratch temp is built with filepath.Join, which drops a "./"
// prefix, so for a cwd source it comes out as ".12:30.mp4.fideo-tmp.mp4" — a
// valid URL scheme followed by ":" — and must be guarded when passed to ffmpeg.
func TestFFmpegArgsGuardColonPaths(t *testing.T) {
	sep := string(filepath.Separator)
	src := "." + sep + "12:30.mp4"
	tmp := tempOutputPath(inPlaceDest(src, ""), src)
	if tmp != ".12:30.mp4"+tempOutputMarker+".mp4" {
		t.Fatalf("tempOutputPath = %q; test assumes Join drops the ./ prefix", tmp)
	}

	vs := newVideoService(testConfig())
	args := vs.ffmpegArgs("12:30.mp4", tmp, videoMetadata{codec: "h264", height: "1080"}, 1)

	i := indexOf(args, "-i")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("no -i in %v", args)
	}
	if got, want := args[i+1], "."+sep+"12:30.mp4"; got != want {
		t.Errorf("input arg = %q, want %q", got, want)
	}
	if got, want := args[len(args)-1], "."+sep+tmp; got != want {
		t.Errorf("output arg = %q, want %q", got, want)
	}
}
