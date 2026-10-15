//go:build integration

package fideo

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The cancel tests stop an encode part-way, from the keyboard or with a signal,
// and check what is left: no partial out_ or scratch file, an untouched source,
// no ffmpeg child, and a terminal handed back in the state it was found in.
// They reuse the pty harness of tui_e2e_test.go.

const (
	// showCursor and bracketedPasteOff are what Bubble Tea writes when it
	// restores the terminal: the cursor it hid at startup comes back and the
	// bracketed paste it enabled is switched off.
	showCursor        = "\x1b[?25h"
	bracketedPasteOff = "\x1b[?2004l"
)

// encodingStarted matches a nonzero progress label: ffmpeg is writing. 0% shows
// as soon as the row starts, before any frame is encoded, and ffmpeg reports
// progress every half second, so the first one drawn can be any percentage.
var encodingStarted = regexp.MustCompile(`\b[1-9][0-9]*%`)

func TestTUICancel(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	bin := buildFideo(t)
	clip := makeNoisyClip(t, t.TempDir())

	t.Run("c mid-encode returns to the picker", func(t *testing.T) {
		dir := t.TempDir()
		copyClip(t, clip, filepath.Join(dir, "clip.mp4"))

		app := startTUI(t, bin, dir, "clip.mp4")
		app.waitForMatch(0, encodingStarted)
		requireFile(t, filepath.Join(dir, "out_clip.mp4"))
		mark := app.mark()
		app.send("c")
		app.waitForMatch(mark, regexp.MustCompile("enter start"))

		wantFiles(t, dir, "clip.mp4")
		app.send("q")
		app.waitExit(0)
	})

	t.Run("c under --overwrite keeps the source", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "clip.mp4")
		copyClip(t, clip, src)

		app := startTUI(t, bin, dir, "--overwrite", "clip.mp4")
		app.waitForMatch(0, encodingStarted)
		if scratch, _ := filepath.Glob(filepath.Join(dir, "*"+tempOutputMarker+"*")); len(scratch) == 0 {
			t.Fatalf("no %s scratch file while encoding in place", tempOutputMarker)
		}
		mark := app.mark()
		app.send("c")
		app.waitForMatch(mark, regexp.MustCompile("enter start"))

		wantFiles(t, dir, "clip.mp4")
		if !sameContents(t, src, clip) {
			t.Errorf("cancelling an in-place encode changed the source")
		}
		app.send("q")
		app.waitExit(0)
	})

	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		t.Run(sig.String()+" mid-encode cleans up and exits", func(t *testing.T) {
			if _, err := exec.LookPath("ps"); err != nil {
				t.Skip("ps not found on PATH; cannot find fideo's ffmpeg child")
			}
			dir := t.TempDir()
			copyClip(t, clip, filepath.Join(dir, "clip.mp4"))

			app := startTUI(t, bin, dir, "clip.mp4")
			app.waitForMatch(0, encodingStarted)
			requireFile(t, filepath.Join(dir, "out_clip.mp4"))
			ffmpeg := ffmpegChild(t, app.cmd.Process.Pid)
			mark := app.mark()
			if err := app.cmd.Process.Signal(sig); err != nil {
				t.Fatalf("sending %v: %v", sig, err)
			}
			app.waitExit(0)

			restore := app.rawAfter(mark)
			for name, seq := range map[string]string{"show the cursor": showCursor, "turn off bracketed paste": bracketedPasteOff} {
				if !strings.Contains(restore, seq) {
					t.Errorf("after %v fideo did not %s (%q); output after the signal: %q", sig, name, seq, restore)
				}
			}
			if processExists(ffmpeg) {
				t.Errorf("ffmpeg (pid %d) still running after fideo exited on %v", ffmpeg, sig)
			}
			wantFiles(t, dir, "clip.mp4")
		})
	}
}

// makeNoisyClip writes a 60s 640x360 clip of noise into dir, slow enough that
// fideo's default encode of it runs for seconds: a test sees it start and
// stops it well before it can finish.
func makeNoisyClip(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "long.mkv")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=60,noise=alls=60:allf=t",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=60",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "35", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating long source clip failed: %v\n%s", err, out)
	}
	return src
}

// mark returns how much fideo has drawn so far, for waitForMatch and rawAfter.
func (a *tuiApp) mark() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.out.Len()
}

// waitForMatch is waitFor for a pattern, limited to what was drawn after mark,
// so text that is already on screen (or was earlier) doesn't count.
func (a *tuiApp) waitForMatch(mark int, re *regexp.Regexp) {
	a.t.Helper()
	deadline := time.NewTimer(tuiTimeout)
	defer deadline.Stop()
	for {
		a.mu.Lock()
		found := re.MatchString(ansi.Strip(string(a.out.Bytes()[mark:])))
		changed, eof := a.changed, a.eof
		a.mu.Unlock()
		switch {
		case found:
			return
		case eof:
			a.t.Fatalf("fideo's terminal closed before %q was drawn; output:\n%s", re, a.screen())
		}
		select {
		case <-changed:
		case <-deadline.C:
			a.t.Fatalf("%q not drawn within %v; output:\n%s", re, tuiTimeout, a.screen())
		}
	}
}

// rawAfter returns the bytes written after mark, escape sequences included,
// once the terminal has closed (so nothing written at exit is still unread).
func (a *tuiApp) rawAfter(mark int) string {
	a.t.Helper()
	deadline := time.NewTimer(tuiTimeout)
	defer deadline.Stop()
	for {
		a.mu.Lock()
		eof, changed := a.eof, a.changed
		out := string(a.out.Bytes()[mark:])
		a.mu.Unlock()
		if eof {
			return out
		}
		select {
		case <-changed:
		case <-deadline.C:
			a.t.Fatalf("fideo's terminal still open %v after it exited", tuiTimeout)
		}
	}
}

// ffmpegChild returns the pid of the ffmpeg process parent started, failing
// the test if it has none.
func ffmpegChild(t *testing.T, parent int) int {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "pid=", "-o", "ppid=", "-o", "comm=").Output()
	if err != nil {
		t.Fatalf("listing processes: %v", withStderr(err))
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != strconv.Itoa(parent) {
			continue
		}
		if filepath.Base(strings.Join(fields[2:], " ")) != "ffmpeg" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("parsing pid in %q: %v", line, err)
		}
		return pid
	}
	t.Fatalf("fideo (pid %d) has no ffmpeg child; processes:\n%s", parent, out)
	return 0
}

// processExists reports whether pid names a process, a zombie included: fideo
// reaps its ffmpeg before exiting, so even an unreaped one is a leak.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// requireFile fails the test unless path exists: an output the cancel that
// follows must clean up.
func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s not written while encoding: %v", filepath.Base(path), err)
	}
}

// sameContents reports whether files a and b hold the same bytes.
func sameContents(t *testing.T, a, b string) bool {
	t.Helper()
	da, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(da, db)
}
