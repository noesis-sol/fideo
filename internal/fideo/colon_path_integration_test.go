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

// TestColonNameIntegration probes and encodes a clip named "12:30.mp4" found in
// the current directory, in place, through the real ffprobe/ffmpeg. Before the
// fix both tools read "12:" / ".12:" as a URL protocol and failed with
// "Protocol not found".
func TestColonNameIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	dir := t.TempDir()
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=15:duration=1",
		"-c:v", "libx264", "file:"+filepath.Join(dir, "12:30.mp4"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot create a ':' file name here: %v\n%s", err, out)
	}
	t.Chdir(dir)

	files := findVideos(".")
	if len(files) != 1 {
		t.Fatalf("findVideos(.) = %v, want one file", files)
	}
	src := files[0].path

	cfg := testConfig()
	cfg.preset = "ultrafast"
	cfg.resolution = "120"
	vs := newVideoService(cfg)

	meta, err := vs.probeMetadata(context.Background(), src)
	if err != nil {
		t.Fatalf("probing %q failed: %v", src, err)
	}

	tmp := tempOutputPath(inPlaceDest(src, ""), src)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if b, err := vs.buildFFmpegCommand(ctx, src, tmp, meta, 1).CombinedOutput(); err != nil {
		t.Fatalf("encode to %q failed: %v\n%s", tmp, err, b)
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		t.Fatalf("temp output %q missing or empty: %v", tmp, err)
	}
}
