//go:build integration

package fideo

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWebMSideLayoutIntegration encodes a 5.1(side) AC3 track — the usual
// surround layout of an MKV rip — to webm. libopus refuses that layout outright,
// so without the aformat remap the encode fails; with it the Opus track keeps
// all six channels as 5.1.
func TestWebMSideLayoutIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mkv")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=15:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
		"-filter_complex", "[1:a]pan=5.1(side)|FL=c0|FR=c0|FC=c0|LFE=c0|SL=c0|SR=c0[a]",
		"-map", "0:v", "-map", "[a]",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "ac3", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating 5.1(side) source failed: %v\n%s", err, out)
	}
	if got := probeStream(t, src, "a:0", "channel_layout"); got != "5.1(side)" {
		t.Fatalf("source layout = %q, want 5.1(side)", got)
	}

	out := encodeClip(t, src, filepath.Join(dir, "out.webm"), "120")

	if got := probeStream(t, out, "a:0", "codec_name"); got != "opus" {
		t.Errorf("audio codec = %q, want opus", got)
	}
	if got := probeStream(t, out, "a:0", "channels"); got != "6" {
		t.Errorf("channels = %q, want 6 (surround kept, not downmixed)", got)
	}
}
