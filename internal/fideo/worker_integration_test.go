//go:build integration

package fideo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// workerDeadline bounds how long a test waits on a worker's final message.
const workerDeadline = 60 * time.Second

// TestProcessFileSuccessIntegration runs the processFile worker on a real clip
// and checks the messages it sends, in order, and the files it leaves: an out_
// copy by default; under --overwrite the source replaced in place, or moved to
// its new container's name after a --format conversion, with no scratch file.
func TestProcessFileSuccessIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")

	tests := []struct {
		name      string
		src       string
		inPlace   bool
		format    string
		wantFiles []string // the directory's contents afterwards
		wantOut   string   // the file probed as the encode's result
	}{
		{"out_ copy", "clip.mov", false, "mp4", []string{"clip.mov", "out_clip.mp4"}, "out_clip.mp4"},
		{"overwrite in place", "clip.mp4", true, "", []string{"clip.mp4"}, "clip.mp4"},
		{"overwrite with format", "clip.mov", true, "mp4", []string{"clip.mp4"}, "clip.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tt.src)
			makeLosslessClip(t, src)
			srcInfo, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}

			cfg := testConfig()
			cfg.maxConcurrent = 1
			cfg.preset = "ultrafast"
			cfg.resolution = "240"
			cfg.inPlace = tt.inPlace
			cfg.outputFormat = tt.format
			msgs := runWorker(t, cfg, src)

			checkSuccessMessages(t, msgs)
			if got := dirNames(t, dir); !slices.Equal(got, tt.wantFiles) {
				t.Errorf("directory holds %v, want %v", got, tt.wantFiles)
			}
			out := filepath.Join(dir, tt.wantOut)
			if got := probeStream(t, out, "v:0", "codec_name"); got != "h264" {
				t.Errorf("%s video codec = %q, want h264", tt.wantOut, got)
			}
			if tt.inPlace {
				// The lossless source is far larger than any CRF 28 re-encode,
				// so a smaller file proves the source was replaced, not kept.
				outInfo, err := os.Stat(out)
				if err != nil {
					t.Fatal(err)
				}
				if outInfo.Size() >= srcInfo.Size() {
					t.Errorf("%s is %d bytes, not smaller than the %d-byte source: not replaced",
						tt.wantOut, outInfo.Size(), srcInfo.Size())
				}
			}
		})
	}
}

// checkSuccessMessages checks msgs is a successful encode's sequence for file
// 0: videoInfoMsg, processingStartMsg, one or more non-decreasing progressMsgs,
// outputInfoMsg, then a doneMsg with no note.
func checkSuccessMessages(t *testing.T, msgs []tea.Msg) {
	t.Helper()
	if len(msgs) < 5 {
		t.Fatalf("got %d messages %#v, want at least 5", len(msgs), msgs)
	}
	if m, ok := msgs[0].(videoInfoMsg); !ok || m.idx != 0 || m.info == "" {
		t.Errorf("message 0 = %#v, want videoInfoMsg for file 0 with info", msgs[0])
	}
	if m, ok := msgs[1].(processingStartMsg); !ok || m.idx != 0 {
		t.Errorf("message 1 = %#v, want processingStartMsg for file 0", msgs[1])
	}
	last := -1.0
	for i, msg := range msgs[2 : len(msgs)-2] {
		p, ok := msg.(progressMsg)
		if !ok || p.idx != 0 {
			t.Errorf("message %d = %#v, want progressMsg for file 0", i+2, msg)
			continue
		}
		if p.progress < last || p.progress > 1 {
			t.Errorf("progress %v after %v, want non-decreasing within [0,1]", p.progress, last)
		}
		last = p.progress
	}
	if m, ok := msgs[len(msgs)-2].(outputInfoMsg); !ok || m.idx != 0 || m.info == "" {
		t.Errorf("next-to-last message = %#v, want outputInfoMsg for file 0 with info", msgs[len(msgs)-2])
	}
	if m, ok := msgs[len(msgs)-1].(doneMsg); !ok || m.idx != 0 || m.note != "" {
		t.Errorf("last message = %#v, want doneMsg for file 0 with no note", msgs[len(msgs)-1])
	}
}

// runWorker runs processFile on a one-file model whose service sends to a
// collector, and returns every message it sent. It waits for the final doneMsg
// or errorMsg, and for the worker goroutine to exit, failing past a deadline.
func runWorker(t *testing.T, cfg compressionConfig, src string) []tea.Msg {
	t.Helper()
	m := newModel([]videoFile{{path: src, name: filepath.Base(src), selected: true}})
	m.config = cfg
	m.videoService = newVideoService(cfg)
	ch := make(chan tea.Msg, 1024)
	m.videoService.send = func(msg tea.Msg) { ch <- msg }

	ctx, cancel := context.WithTimeout(context.Background(), workerDeadline)
	defer cancel()
	if cmd := m.processFile(0, ctx, cancel, 1); cmd != nil {
		t.Fatalf("processFile returned a command (%#v), want a started worker", cmd())
	}

	var msgs []tea.Msg
	deadline := time.After(workerDeadline)
	for done := false; !done; {
		select {
		case msg := <-ch:
			msgs = append(msgs, msg)
			switch msg.(type) {
			case doneMsg, errorMsg:
				done = true
			}
		case <-deadline:
			t.Fatalf("no doneMsg or errorMsg within %v; got %#v", workerDeadline, msgs)
		}
	}
	if !m.videoService.waitWorkers(workerDeadline) {
		t.Fatalf("worker still running %v after its final message", workerDeadline)
	}
	// The worker has exited, so anything still buffered was sent after the end.
	for len(ch) > 0 {
		t.Errorf("message after the final one: %#v", <-ch)
	}
	return msgs
}

// makeLosslessClip synthesizes a 1s 320x240 lossless H.264/AAC test pattern at
// path, its container following the extension. Lossless keeps it far larger
// than a CRF 28 re-encode, so --overwrite replaces it rather than keeping it.
func makeLosslessClip(t *testing.T, path string) {
	t.Helper()
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-preset", "ultrafast", "-qp", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", path)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating source clip failed: %v\n%s", err, out)
	}
}

// dirNames returns the sorted names in dir, failing on any scratch file an
// in-place encode left behind.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.Contains(e.Name(), tempOutputMarker) {
			t.Errorf("scratch file %s left behind", e.Name())
		}
		names = append(names, e.Name())
	}
	return names
}
