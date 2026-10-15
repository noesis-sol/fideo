//go:build integration

package fideo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestProcessFileProbeFailureIntegration runs the worker on a text file named
// .mp4: ffprobe rejects it, so the only message is an errorMsg naming the probe
// failure and carrying ffprobe's last stderr line, and no output is written.
func TestProcessFileProbeFailureIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(src, []byte("this is not a video\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs := runWorker(t, workerTestConfig(), src)

	if len(msgs) != 1 {
		t.Fatalf("got messages %#v, want a single errorMsg", msgs)
	}
	got := errorText(t, msgs[0])
	if !strings.HasPrefix(got, "failed to probe video: ") {
		t.Errorf("error = %q, want it to start with %q", got, "failed to probe video: ")
	}
	if want := ffprobeLastStderrLine(t, src); !strings.HasSuffix(got, ": "+want) {
		t.Errorf("error = %q, want it to end with ffprobe's last stderr line %q", got, want)
	}
	if names := dirNames(t, dir); !slices.Equal(names, []string{"clip.mp4"}) {
		t.Errorf("directory holds %v, want only the source", names)
	}
}

// TestProcessFileFFmpegFailureIntegration runs the worker with an x264 preset
// ffmpeg rejects. ffmpeg exits non-zero after creating an empty output file; the
// worker reports "ffmpeg failed: …" with the cause from stderr and removes that
// file (the out_ copy, or under --overwrite the scratch temp), leaving the
// source untouched.
func TestProcessFileFFmpegFailureIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "out_ copy", true: "overwrite"}[inPlace], func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "clip.mov")
			makeLosslessClip(t, src)
			before := readFile(t, src)

			cfg := workerTestConfig()
			cfg.preset = "bogus"
			cfg.inPlace = inPlace
			msgs := runWorker(t, cfg, src)

			checkStartedThen[errorMsg](t, msgs)
			got := errorText(t, msgs[len(msgs)-1])
			if !strings.HasPrefix(got, "ffmpeg failed: exit status ") {
				t.Errorf("error = %q, want it to start with %q", got, "ffmpeg failed: exit status ")
			}
			if !strings.Contains(got, "invalid preset 'bogus'") {
				t.Errorf("error = %q, want ffmpeg's cause %q in it", got, "invalid preset 'bogus'")
			}
			if names := dirNames(t, dir); !slices.Equal(names, []string{"clip.mov"}) {
				t.Errorf("directory holds %v, want only the source: partial output left", names)
			}
			if !bytes.Equal(readFile(t, src), before) {
				t.Error("source was modified by a failed encode")
			}
		})
	}
}

// TestProcessFileCancelIntegration cancels the worker's context once ffmpeg has
// reported progress, i.e. mid-encode with a partial output on disk. The worker
// ends with a cancelMsg and no errorMsg, and removes the partial file: the out_
// copy, or under --overwrite the scratch temp, leaving the source intact.
func TestProcessFileCancelIntegration(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	for _, inPlace := range []bool{false, true} {
		t.Run(map[bool]string{false: "out_ copy", true: "overwrite"}[inPlace], func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "clip.mp4")
			makeLongClip(t, src)
			before := readFile(t, src)

			cfg := workerTestConfig()
			// A slow preset keeps the encode running well past its first
			// progress report, so the cancel lands mid-encode.
			cfg.preset = "veryslow"
			cfg.resolution = "720"
			cfg.inPlace = inPlace
			partial := filepath.Join(dir, "out_clip.mp4")
			if inPlace {
				partial = tempOutputPath(src, src)
			}

			w := startWorker(t, cfg, src)
			var sawPartial bool
			msgs := w.collect(t, func(msg tea.Msg) {
				if _, ok := msg.(progressMsg); ok && !sawPartial {
					sawPartial = pathExists(partial)
					w.cancel()
				}
			})

			if !sawPartial {
				t.Fatalf("%s did not exist when the encode was cancelled; messages %#v",
					filepath.Base(partial), msgs)
			}
			checkStartedThen[cancelMsg](t, msgs)
			if names := dirNames(t, dir); !slices.Equal(names, []string{"clip.mp4"}) {
				t.Errorf("directory holds %v, want only the source: partial output left", names)
			}
			if !bytes.Equal(readFile(t, src), before) {
				t.Error("source was modified by a cancelled encode")
			}
		})
	}
}

// workerTestConfig is a one-job software config with a fast preset and a small
// output size.
func workerTestConfig() compressionConfig {
	cfg := testConfig()
	cfg.maxConcurrent = 1
	cfg.preset = "ultrafast"
	cfg.resolution = "240"
	return cfg
}

// checkStartedThen checks msgs is file 0's sequence for an encode that started
// and then ended with a final message of type T: videoInfoMsg,
// processingStartMsg, any progressMsgs, then T.
func checkStartedThen[T tea.Msg](t *testing.T, msgs []tea.Msg) {
	t.Helper()
	if len(msgs) < 3 {
		t.Fatalf("got %d messages %#v, want at least 3", len(msgs), msgs)
	}
	if m, ok := msgs[0].(videoInfoMsg); !ok || m.idx != 0 {
		t.Errorf("message 0 = %#v, want videoInfoMsg for file 0", msgs[0])
	}
	if m, ok := msgs[1].(processingStartMsg); !ok || m.idx != 0 {
		t.Errorf("message 1 = %#v, want processingStartMsg for file 0", msgs[1])
	}
	for i, msg := range msgs[2 : len(msgs)-1] {
		if p, ok := msg.(progressMsg); !ok || p.idx != 0 {
			t.Errorf("message %d = %#v, want progressMsg for file 0", i+2, msg)
		}
	}
	last := msgs[len(msgs)-1]
	if _, ok := last.(T); !ok || msgIdx(last) != 0 {
		var want T
		t.Errorf("last message = %#v, want %T for file 0", last, want)
	}
}

// msgIdx returns the file index a final worker message is for (-1 if none).
func msgIdx(msg tea.Msg) int {
	switch m := msg.(type) {
	case errorMsg:
		return m.idx
	case cancelMsg:
		return m.idx
	case doneMsg:
		return m.idx
	}
	return -1
}

// errorText returns msg's error text, failing unless msg is an errorMsg for
// file 0.
func errorText(t *testing.T, msg tea.Msg) string {
	t.Helper()
	m, ok := msg.(errorMsg)
	if !ok || m.idx != 0 || m.err == nil {
		t.Fatalf("message = %#v, want errorMsg for file 0", msg)
	}
	return m.err.Error()
}

// worker is a processFile goroutine started on a one-file model whose service
// sends to ch; cancel cancels its context.
type worker struct {
	vs     *videoService
	ch     chan tea.Msg
	cancel context.CancelFunc
}

// startWorker starts processFile on src under cfg, failing if it returns a
// command instead of starting the goroutine.
func startWorker(t *testing.T, cfg compressionConfig, src string) *worker {
	t.Helper()
	m := newModel([]videoFile{{path: src, name: filepath.Base(src), selected: true}})
	m.config = cfg
	m.videoService = newVideoService(cfg)
	w := &worker{vs: m.videoService, ch: make(chan tea.Msg, 1024)}
	w.vs.send = func(msg tea.Msg) { w.ch <- msg }

	ctx, cancel := context.WithTimeout(context.Background(), workerDeadline)
	t.Cleanup(cancel)
	w.cancel = cancel
	if cmd := m.processFile(0, ctx, cancel, 1); cmd != nil {
		t.Fatalf("processFile returned a command (%#v), want a started worker", cmd())
	}
	return w
}

// collect returns every message the worker sends, calling each(msg) as it
// arrives. It waits for the final doneMsg, errorMsg or cancelMsg and for the
// goroutine to exit, failing past a deadline or on a message after the final one.
func (w *worker) collect(t *testing.T, each func(tea.Msg)) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	deadline := time.After(workerDeadline)
	for done := false; !done; {
		select {
		case msg := <-w.ch:
			msgs = append(msgs, msg)
			each(msg)
			switch msg.(type) {
			case doneMsg, errorMsg, cancelMsg:
				done = true
			}
		case <-deadline:
			t.Fatalf("no final message within %v; got %#v", workerDeadline, msgs)
		}
	}
	if !w.vs.waitWorkers(workerDeadline) {
		t.Fatalf("worker still running %v after its final message", workerDeadline)
	}
	for len(w.ch) > 0 {
		t.Errorf("message after the final one: %#v", <-w.ch)
	}
	return msgs
}

// makeLongClip synthesizes a 20s 1280x720 H.264/AAC test pattern at path:
// cheap to generate with ultrafast, slow to re-encode with veryslow.
func makeLongClip(t *testing.T, path string) {
	t.Helper()
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30:duration=20",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=20",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", path)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating source clip failed: %v\n%s", err, out)
	}
}

// ffprobeLastStderrLine runs ffprobe on path as probeMetadata does, expecting
// it to fail, and returns the last line of its stderr.
func ffprobeLastStderrLine(t *testing.T, path string) string {
	t.Helper()
	_, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", path).Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || len(ee.Stderr) == 0 {
		t.Fatalf("ffprobe of %s: err = %v, want a failure with stderr", filepath.Base(path), err)
	}
	return lastLine(string(ee.Stderr))
}

// readFile returns path's contents, failing on error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
