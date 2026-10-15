//go:build integration

package fideo

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestScaleShortSideIntegration encodes synthetic clips through the real ffmpeg
// and checks the output size: the resolution caps the short side, so a portrait
// clip keeps its width at the cap instead of shrinking to fit a height cap, and
// a clip already under the cap is not upscaled but rounded to even dimensions.
func TestScaleShortSideIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	tests := []struct {
		name, src, res string
		wantW, wantH   string
	}{
		{"portrait downscaled by width", "240x320", "120", "120", "160"},
		{"landscape downscaled by height", "320x240", "120", "160", "120"},
		{"square downscaled", "200x200", "120", "120", "120"},
		{"odd portrait not upscaled", "135x241", "1080", "134", "240"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.mp4")
			gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "testsrc=size="+tt.src+":rate=15:duration=0.5",
				"-c:v", "libx264", "-pix_fmt", "yuv444p", src)
			if out, err := gen.CombinedOutput(); err != nil {
				t.Fatalf("generating source clip failed: %v\n%s", err, out)
			}

			cfg := testConfig()
			cfg.preset = "ultrafast"
			cfg.maxConcurrent = 1
			cfg.resolution = tt.res
			vs := newVideoService(cfg)
			meta, err := vs.probeMetadata(context.Background(), src)
			if err != nil {
				t.Fatalf("probing source failed: %v", err)
			}

			out := filepath.Join(dir, "out.mp4")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if b, err := vs.buildFFmpegCommand(ctx, src, out, meta, 1).CombinedOutput(); err != nil {
				t.Fatalf("encode failed: %v\n%s", err, b)
			}
			outMeta, err := vs.probeMetadata(context.Background(), out)
			if err != nil {
				t.Fatalf("probing output failed: %v", err)
			}
			if outMeta.width != tt.wantW || outMeta.height != tt.wantH {
				t.Errorf("output = %sx%s, want %sx%s", outMeta.width, outMeta.height, tt.wantW, tt.wantH)
			}
		})
	}
}
