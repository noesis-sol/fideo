//go:build integration

package fideo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These tests run --hw end to end on whatever hardware H.264 encoder this host
// has (VideoToolbox, NVENC, QSV or AMF), picked by the same resolveHWEncoder
// that --hw uses. Without one they skip. On a GPU host, name the encoder you
// expect so a detection regression fails instead of skipping:
//
//	FIDEO_HW_ENCODER=h264_nvenc go test -tags=integration -run HW ./internal/fideo

// TestHWEncodeIntegration encodes 8-bit 4:2:0, 4:4:4 and 10-bit sources with
// the hardware encoder and checks each comes out as 8-bit 4:2:0 H.264. fideo
// asks for yuv420p while h264_qsv takes only nv12, so this is where a build
// that refused the request instead of converting would show.
func TestHWEncodeIntegration(t *testing.T) {
	enc := hwEncoderUnderTest(t)

	for _, pixFmt := range []string{"yuv420p", "yuv444p", "yuv420p10le"} {
		t.Run(pixFmt, func(t *testing.T) {
			dir := t.TempDir()
			src := makeClip(t, dir, "320x240", pixFmt)

			cfg := hwTestConfig(enc)
			out := encodeHW(t, cfg, src, filepath.Join(dir, "out.mp4"))

			if got := probeStream(t, out, "v:0", "codec_name"); got != "h264" {
				t.Errorf("video codec = %q, want h264", got)
			}
			if got := probeStream(t, out, "v:0", "pix_fmt"); got != "yuv420p" {
				t.Errorf("output pix_fmt = %q, want yuv420p", got)
			}
			if got := probeStream(t, out, "v:0", "height"); got != "240" {
				t.Errorf("output height = %q, want 240", got)
			}
		})
	}
}

// TestHWQualityIntegration checks the encoder's quality knob (NVENC -cq, QSV
// -global_quality, AMF -qp_*, VideoToolbox -q:v) moves the output size, and
// that the high-quality end isn't held to a bitrate target: an NVENC VBR run
// without -b:v 0 stays at ffmpeg's 2 Mb/s default whatever -cq says.
func TestHWQualityIntegration(t *testing.T) {
	enc := hwEncoderUnderTest(t)

	dir := t.TempDir()
	src := makeNoiseClip(t, dir)

	high, low := hwTestConfig(enc), hwTestConfig(enc)
	high.crf, high.hwQuality = "18", "80"
	low.crf, low.hwQuality = "40", "20"
	highSize := fileSize(t, encodeHW(t, high, src, filepath.Join(dir, "high.mp4")))
	lowSize := fileSize(t, encodeHW(t, low, src, filepath.Join(dir, "low.mp4")))

	if highSize <= lowSize {
		t.Errorf("high-quality output (%d bytes) not larger than low-quality (%d bytes): the quality setting has no effect", highSize, lowSize)
	}
	// The noise clip lasts 1s, so bytes*8 is its bitrate in b/s.
	if bitrate := highSize * 8; bitrate <= 2_000_000 {
		t.Errorf("high-quality bitrate = %d b/s, want above 2 Mb/s on incompressible noise: capped by a bitrate target", bitrate)
	}
}

// hwEncoderUnderTest resolves the host's hardware encoder as --hw does. With
// FIDEO_HW_ENCODER set it fails unless that encoder is the one chosen;
// otherwise it skips when there is none.
func hwEncoderUnderTest(t *testing.T) string {
	t.Helper()
	requireBinaries(t, "ffmpeg", "ffprobe")
	want := os.Getenv("FIDEO_HW_ENCODER")
	enc, err := resolveHWEncoder()
	switch {
	case want != "" && err != nil:
		t.Fatalf("FIDEO_HW_ENCODER=%s, but no hardware encoder resolved: %v", want, err)
	case want != "" && enc != want:
		t.Fatalf("FIDEO_HW_ENCODER=%s, but resolveHWEncoder chose %s", want, enc)
	case err != nil:
		t.Skipf("no hardware H.264 encoder on this host: %v", err)
	}
	t.Logf("hardware encoder: %s", enc)
	return enc
}

// hwTestConfig is the --hw configuration for enc at fideo's default quality,
// capped at 240 lines so the encodes stay quick.
func hwTestConfig(enc string) compressionConfig {
	cfg := testConfig()
	cfg.maxConcurrent = 1
	cfg.resolution = "240"
	cfg.outputFormat = "mp4"
	cfg.hwAccel = true
	cfg.hwEncoder = enc
	return cfg
}

// encodeHW probes src and runs the ffmpeg command fideo builds for it under
// cfg, returning out once a non-empty file is there.
func encodeHW(t *testing.T, cfg compressionConfig, src, out string) string {
	t.Helper()
	vs := newVideoService(cfg)
	meta, err := vs.probeMetadata(context.Background(), src)
	if err != nil {
		t.Fatalf("probing source failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if b, err := vs.buildFFmpegCommand(ctx, src, out, meta, 1).CombinedOutput(); err != nil {
		t.Fatalf("%s encode to %s failed: %v\n%s", cfg.hwEncoder, filepath.Base(out), err, b)
	}
	if fileSize(t, out) == 0 {
		t.Fatalf("output %s is empty", filepath.Base(out))
	}
	return out
}

// makeNoiseClip synthesizes a 1s 320x240 clip of per-frame noise, which no
// encoder can squeeze below a few Mb/s at high quality, near-losslessly in dir.
func makeNoiseClip(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "noise.mkv")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=30:duration=1,noise=alls=40:allf=t",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "8", "-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating noise clip failed: %v\n%s", err, out)
	}
	return src
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", filepath.Base(path), err)
	}
	return info.Size()
}
