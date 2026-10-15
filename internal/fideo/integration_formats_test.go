//go:build integration

package fideo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEncodeFormatsIntegration runs the real encode into each non-mp4
// container and probes the codecs that came out: H.264/AAC for mov and mkv,
// VP9/Opus for webm (the container can't carry H.264).
//
//	go test -tags=integration ./...
func TestEncodeFormatsIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	tests := []struct {
		format     string
		videoCodec string
		audioCodec string
	}{
		{"mov", "h264", "aac"},
		{"mkv", "h264", "aac"},
		{"webm", "vp9", "opus"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			dir := t.TempDir()
			src := makeClip(t, dir, "320x240", "yuv420p")

			out := encodeClip(t, src, filepath.Join(dir, "out."+tt.format), "240")

			if got := probeStream(t, out, "v:0", "codec_name"); got != tt.videoCodec {
				t.Errorf("video codec = %q, want %q", got, tt.videoCodec)
			}
			if got := probeStream(t, out, "a:0", "codec_name"); got != tt.audioCodec {
				t.Errorf("audio codec = %q, want %q", got, tt.audioCodec)
			}
		})
	}
}

// TestEncodeSizesIntegration checks the scale filter on real frames: an
// odd-height source comes out even, a downscale hits the target height, and a
// target above the source height leaves it as it is.
func TestEncodeSizesIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	tests := []struct {
		name       string
		size       string
		resolution string
		wantWidth  string
		wantHeight string
	}{
		// 241 is below the cap, so no downscale: the height is rounded down to
		// 240, and -2 keeps the aspect with an even width (320*240/241 ≈ 318).
		{"odd height is made even", "320x241", "1080", "318", "240"},
		{"480p downscaled to 240", "640x480", "240", "320", "240"},
		{"480p not upscaled to 1080", "640x480", "1080", "640", "480"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			// yuv444p, since a 4:2:0 source can't have an odd height.
			src := makeClip(t, dir, tt.size, "yuv444p")

			out := encodeClip(t, src, filepath.Join(dir, "out.mp4"), tt.resolution)

			if got := probeStream(t, out, "v:0", "width"); got != tt.wantWidth {
				t.Errorf("width = %q, want %q", got, tt.wantWidth)
			}
			if got := probeStream(t, out, "v:0", "height"); got != tt.wantHeight {
				t.Errorf("height = %q, want %q", got, tt.wantHeight)
			}
		})
	}
}

// TestEncodePixelFormatIntegration checks a 4:4:4 source is encoded as 8-bit
// 4:2:0, which every H.264/VP9 decoder plays, rather than High 4:4:4.
func TestEncodePixelFormatIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	dir := t.TempDir()
	src := makeClip(t, dir, "320x240", "yuv444p")
	if got := probeStream(t, src, "v:0", "pix_fmt"); got != "yuv444p" {
		t.Fatalf("source pix_fmt = %q, want yuv444p", got)
	}

	out := encodeClip(t, src, filepath.Join(dir, "out.mp4"), "240")

	if got := probeStream(t, out, "v:0", "pix_fmt"); got != "yuv420p" {
		t.Errorf("output pix_fmt = %q, want yuv420p", got)
	}
}

// makeClip synthesizes a 1s H.264/AAC test pattern of the given size and pixel
// format in dir, and returns its path.
func makeClip(t *testing.T, dir, size, pixFmt string) string {
	t.Helper()
	src := filepath.Join(dir, "src.mkv")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size="+size+":rate=15:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", pixFmt,
		"-c:a", "aac", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating source clip failed: %v\n%s", err, out)
	}
	return src
}

// encodeClip probes src and runs the real ffmpeg command fideo builds for it,
// writing out at the given resolution cap; the container follows out's
// extension. It returns out once ffmpeg has written a non-empty file there.
func encodeClip(t *testing.T, src, out, resolution string) string {
	t.Helper()
	cfg := testConfig()
	cfg.maxConcurrent = 1
	cfg.preset = "ultrafast" // speed over ratio; these are correctness tests
	cfg.resolution = resolution
	cfg.outputFormat = containerOf(out)
	vs := newVideoService(cfg)

	meta, err := vs.probeMetadata(context.Background(), src)
	if err != nil {
		t.Fatalf("probing source failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if b, err := vs.buildFFmpegCommand(ctx, src, out, meta, 1).CombinedOutput(); err != nil {
		t.Fatalf("encode to %s failed: %v\n%s", filepath.Base(out), err, b)
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		t.Fatalf("output %s missing or empty: %v", filepath.Base(out), err)
	}
	return out
}

// probeStream returns one field of the selected stream (e.g. "v:0", "a:0") of
// path, as ffprobe reports it.
func probeStream(t *testing.T, path, stream, field string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", stream, "-show_entries", "stream="+field,
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s of %s failed: %v", field, filepath.Base(path), withStderr(err))
	}
	return strings.TrimSpace(string(out))
}
