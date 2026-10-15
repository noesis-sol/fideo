//go:build integration

package fideo

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestScaleOnePixelSideIntegration encodes 1-pixel-thin clips through the real
// ffmpeg: rounding the short side down to even would give 0, which the scale
// filter reads as "keep the input size", so the odd 1 reached libx264/yuv420p
// and the encode failed. The short side is floored at 2 instead, and the long
// side follows the aspect ratio.
func TestScaleOnePixelSideIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	tests := []struct {
		name, src    string
		wantW, wantH string
	}{
		{"one pixel tall", "64x1", "128", "2"},
		{"one pixel wide", "1x64", "2", "128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.mkv")
			gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "testsrc=size="+tt.src+":rate=15:duration=0.5",
				"-c:v", "ffv1", "-slices", "1", src)
			if out, err := gen.CombinedOutput(); err != nil {
				t.Fatalf("generating source clip failed: %v\n%s", err, out)
			}

			cfg := testConfig()
			cfg.preset = "ultrafast"
			cfg.maxConcurrent = 1
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
