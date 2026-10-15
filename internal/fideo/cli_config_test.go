package fideo

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestBuildConfig pins what Run decides from the parsed options: case-insensitive
// --format, the --size presets, the mp4 default (and its absence under
// --overwrite, which keeps each file's container), and the rejected
// combinations with the hints Run prints.
func TestBuildConfig(t *testing.T) {
	base := compressionConfig{resolution: "1080", maxConcurrent: 4}
	tests := []struct {
		name    string
		opts    cliOptions
		want    compressionConfig
		wantErr string // the full expected error; empty means success
	}{
		{"no format defaults to mp4", cliOptions{},
			compressionConfig{resolution: "1080", maxConcurrent: 4, outputFormat: "mp4"}, ""},
		{"no format under --overwrite keeps the container", cliOptions{overwrite: true},
			compressionConfig{resolution: "1080", maxConcurrent: 4, inPlace: true}, ""},
		{"format is lowercased", cliOptions{format: "MP4"},
			compressionConfig{resolution: "1080", maxConcurrent: 4, outputFormat: "mp4"}, ""},
		{"mixed-case webm", cliOptions{format: "WebM"},
			compressionConfig{resolution: "1080", maxConcurrent: 4, outputFormat: "webm"}, ""},
		{"format under --overwrite converts", cliOptions{format: "mkv", overwrite: true},
			compressionConfig{resolution: "1080", maxConcurrent: 4, outputFormat: "mkv", inPlace: true}, ""},
		{"hw with the default format", cliOptions{hw: true},
			compressionConfig{resolution: "1080", maxConcurrent: 4, outputFormat: "mp4", hwAccel: true}, ""},
		{"hw under --overwrite without format", cliOptions{hw: true, overwrite: true},
			compressionConfig{resolution: "1080", maxConcurrent: 4, hwAccel: true, inPlace: true}, ""},
		{"unsupported format", cliOptions{format: "AVI"}, compressionConfig{},
			"unsupported format \"AVI\"\nSupported formats: mp4, mov, mkv, webm"},
		{"unsupported size", cliOptions{size: "xl"}, compressionConfig{},
			"unsupported size \"xl\"\nSupported sizes: sm, small, md, medium, lg, large"},
		{"hw refused with webm", cliOptions{hw: true, format: "webm"}, compressionConfig{},
			"--hw is not compatible with --format webm (it requires a software codec)"},
		{"hw refused with uppercase WEBM", cliOptions{hw: true, format: "WEBM"}, compressionConfig{},
			"--hw is not compatible with --format webm (it requires a software codec)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildConfig(tt.opts, base)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("buildConfig(%+v) error = %v, want %q", tt.opts, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildConfig(%+v) unexpected error: %v", tt.opts, err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(compressionConfig{})); diff != "" {
				t.Errorf("buildConfig(%+v) mismatch (-want +got):\n%s", tt.opts, diff)
			}
		})
	}
}

// TestBuildConfigSizes maps every --size spelling, in any case, to its height.
func TestBuildConfigSizes(t *testing.T) {
	tests := []struct {
		size string
		want string
	}{
		{"sm", "540"},
		{"small", "540"},
		{"md", "1080"},
		{"medium", "1080"},
		{"lg", "2160"},
		{"large", "2160"},
		{"LG", "2160"},
		{"Small", "540"},
		{"", "720"}, // no --size keeps the base resolution
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			got, err := buildConfig(cliOptions{size: tt.size}, compressionConfig{resolution: "720"})
			if err != nil {
				t.Fatalf("buildConfig(size %q) unexpected error: %v", tt.size, err)
			}
			if got.resolution != tt.want {
				t.Errorf("buildConfig(size %q).resolution = %q, want %q", tt.size, got.resolution, tt.want)
			}
		})
	}
}

// TestBuildConfigKeepsDefaults checks that the defaults Run starts from survive
// the options untouched.
func TestBuildConfigKeepsDefaults(t *testing.T) {
	got, err := buildConfig(cliOptions{format: "mov", size: "sm"}, defaultConfig)
	if err != nil {
		t.Fatalf("buildConfig unexpected error: %v", err)
	}
	want := defaultConfig
	want.outputFormat, want.resolution = "mov", "540"
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(compressionConfig{})); diff != "" {
		t.Errorf("buildConfig mismatch (-want +got):\n%s", diff)
	}
}

// TestApplyHW pins the --hw settling step: the encoder is recorded and
// concurrency capped at hwMaxConcurrent; a failed resolve falls back to software
// with a warning; and under --overwrite without --format each WebM source is
// counted in a warning, since it is encoded with software VP9.
func TestApplyHW(t *testing.T) {
	ok := func() (string, error) { return "h264_videotoolbox", nil }
	failed := func() (string, error) { return "", errors.New("no hardware encoder") }
	files := func(names ...string) []videoFile {
		var fs []videoFile
		for _, n := range names {
			fs = append(fs, videoFile{path: "/videos/" + n, name: n})
		}
		return fs
	}
	tests := []struct {
		name         string
		cfg          compressionConfig
		files        []videoFile
		resolve      func() (string, error)
		want         compressionConfig
		wantWarnings []string
	}{
		{"caps maxConcurrent at 2",
			compressionConfig{hwAccel: true, outputFormat: "mp4", maxConcurrent: 4}, files("a.mp4"), ok,
			compressionConfig{hwAccel: true, outputFormat: "mp4", maxConcurrent: 2, hwEncoder: "h264_videotoolbox"}, nil},
		{"keeps a lower maxConcurrent",
			compressionConfig{hwAccel: true, outputFormat: "mp4", maxConcurrent: 1}, nil, ok,
			compressionConfig{hwAccel: true, outputFormat: "mp4", maxConcurrent: 1, hwEncoder: "h264_videotoolbox"}, nil},
		{"failed resolve falls back to software",
			compressionConfig{hwAccel: true, outputFormat: "mp4", maxConcurrent: 4}, files("a.mp4"), failed,
			compressionConfig{outputFormat: "mp4", maxConcurrent: 4},
			[]string{"no hardware encoder\nFalling back to software (libx264) encoding."}},
		{"counts WebM sources under --overwrite",
			compressionConfig{hwAccel: true, inPlace: true, maxConcurrent: 4},
			files("a.webm", "b.mp4", "c.WEBM", "d.mov"), ok,
			compressionConfig{hwAccel: true, inPlace: true, maxConcurrent: 2, hwEncoder: "h264_videotoolbox"},
			[]string{"--hw cannot produce WebM (VP9) output; 2 source(s) will be encoded with software VP9 instead."}},
		{"no warning without WebM sources",
			compressionConfig{hwAccel: true, inPlace: true, maxConcurrent: 4}, files("a.mp4", "b.mkv"), ok,
			compressionConfig{hwAccel: true, inPlace: true, maxConcurrent: 2, hwEncoder: "h264_videotoolbox"}, nil},
		{"no WebM warning when --format is set",
			compressionConfig{hwAccel: true, inPlace: true, outputFormat: "mp4", maxConcurrent: 4}, files("a.webm"), ok,
			compressionConfig{hwAccel: true, inPlace: true, outputFormat: "mp4", maxConcurrent: 2, hwEncoder: "h264_videotoolbox"}, nil},
		{"no WebM warning after a software fallback",
			compressionConfig{hwAccel: true, inPlace: true, maxConcurrent: 4}, files("a.webm"), failed,
			compressionConfig{inPlace: true, maxConcurrent: 4},
			[]string{"no hardware encoder\nFalling back to software (libx264) encoding."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := applyHW(tt.cfg, tt.files, tt.resolve)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(compressionConfig{})); diff != "" {
				t.Errorf("applyHW config mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantWarnings, warnings); diff != "" {
				t.Errorf("applyHW warnings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestApplyHWWithoutHW checks that without --hw the encoder is never resolved
// (it would exec ffmpeg) and the config passes through unchanged.
func TestApplyHWWithoutHW(t *testing.T) {
	cfg := compressionConfig{outputFormat: "mp4", maxConcurrent: 4}
	got, warnings := applyHW(cfg, nil, func() (string, error) {
		t.Fatal("resolve called without --hw")
		return "", nil
	})
	if diff := cmp.Diff(cfg, got, cmp.AllowUnexported(compressionConfig{})); diff != "" {
		t.Errorf("applyHW mismatch (-want +got):\n%s", diff)
	}
	if warnings != nil {
		t.Errorf("applyHW warnings = %q, want none", warnings)
	}
}

// TestParseArgsSingleDashAliases pins the single-dash spellings of the value
// and boolean options, in both the separate and the inline form.
func TestParseArgsSingleDashAliases(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    cliOptions
		wantErr string // substring of the expected error; empty means success
	}{
		{"-format value", []string{"-format", "mkv", "a.mp4"}, cliOptions{format: "mkv", paths: []string{"a.mp4"}}, ""},
		{"-format=value", []string{"-format=webm"}, cliOptions{format: "webm"}, ""},
		{"-size value", []string{"-size", "lg"}, cliOptions{size: "lg"}, ""},
		{"-size=value", []string{"-size=sm", "b.mov"}, cliOptions{size: "sm", paths: []string{"b.mov"}}, ""},
		{"-hw", []string{"a.mp4", "-hw"}, cliOptions{hw: true, paths: []string{"a.mp4"}}, ""},
		{"all three", []string{"-hw", "-format", "mov", "-size", "md"}, cliOptions{hw: true, format: "mov", size: "md"}, ""},
		{"-format without a value", []string{"-format"}, cliOptions{}, "--format requires a value"},
		{"-size with an empty inline value", []string{"-size="}, cliOptions{}, "--size requires a value"},
		{"-hw with a value", []string{"-hw=1"}, cliOptions{}, "option -hw takes no value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseArgs(%v) error = %v, want one containing %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(cliOptions{})); diff != "" {
				t.Errorf("parseArgs(%v) mismatch (-want +got):\n%s", tt.args, diff)
			}
		})
	}
}
