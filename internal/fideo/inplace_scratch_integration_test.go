//go:build integration

package fideo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestInPlaceScratchPrivateIntegration pins fideo-nr2 end to end: while ffmpeg
// is writing an --overwrite encode's scratch file it is owner-only, though the
// usual 022 umask would make a file ffmpeg creates world-readable, and the
// replaced source still ends up with its own 0600 mode.
func TestInPlaceScratchPrivateIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permission bits on Windows")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	makeLosslessClip(t, src)
	if err := os.Chmod(src, 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := tempOutputPath(src, src)

	cfg := testConfig()
	cfg.maxConcurrent = 1
	cfg.preset = "ultrafast"
	cfg.resolution = "240"
	cfg.inPlace = true
	m := newModel([]videoFile{{path: src, name: filepath.Base(src), selected: true}})
	m.config = cfg
	m.videoService = newVideoService(cfg)
	ch := make(chan tea.Msg, 1024)
	var midEncode os.FileMode
	m.videoService.send = func(msg tea.Msg) {
		// processingStartMsg is sent once ffmpeg is running, while the
		// encode is still under way: the scratch file's mode then is what
		// other users could read.
		if _, ok := msg.(processingStartMsg); ok {
			if fi, err := os.Stat(tmp); err == nil {
				midEncode = fi.Mode().Perm()
			}
		}
		ch <- msg
	}

	ctx, cancel := context.WithTimeout(context.Background(), workerDeadline)
	defer cancel()
	m.processFile(0, ctx, cancel, 1)
	var last tea.Msg
	for done := false; !done; {
		select {
		case last = <-ch:
			switch last.(type) {
			case doneMsg, errorMsg:
				done = true
			}
		case <-time.After(workerDeadline):
			t.Fatalf("no doneMsg or errorMsg within %v", workerDeadline)
		}
	}
	m.videoService.waitWorkers(workerDeadline)

	if _, ok := last.(doneMsg); !ok {
		t.Fatalf("final message = %#v, want doneMsg", last)
	}
	if midEncode == 0 {
		t.Fatal("scratch file did not exist once ffmpeg had started")
	}
	if midEncode&0o077 != 0 {
		t.Errorf("scratch mode mid-encode = %v, want no group/other access", midEncode)
	}
	fi, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("replaced source mode = %v, want 0600", fi.Mode().Perm())
	}
	if pathExists(tmp) {
		t.Error("scratch file left behind")
	}
}
