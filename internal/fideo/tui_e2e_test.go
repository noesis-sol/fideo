//go:build integration

package fideo

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

// The TUI end-to-end tests run the built fideo binary in a pseudo-terminal of a
// fixed size, the way a user does: they read the frames it draws, type keys,
// and check the files it leaves behind and its exit code. Every wait has a
// deadline and returns as soon as the expected text is drawn.

const (
	tuiRows = 40
	tuiCols = 120
	// tuiTimeout bounds each wait: generous for a default (CRF 28, preset
	// medium) encode of a 1s clip on a loaded CI runner.
	tuiTimeout = 60 * time.Second
)

func TestTUIEndToEnd(t *testing.T) {
	requireBinaries(t, "ffmpeg", "ffprobe")
	bin := buildFideo(t)
	clip := makeClip(t, t.TempDir(), "320x240", "yuv420p")

	t.Run("lone file auto-starts", func(t *testing.T) {
		dir := t.TempDir()
		copyClip(t, clip, filepath.Join(dir, "clip.mp4"))

		app := startTUI(t, bin, dir, "clip.mp4")
		app.waitFor("All done!")
		app.send("q")
		app.waitExit(0)

		wantFiles(t, dir, "clip.mp4", "out_clip.mp4")
		checkOutput(t, filepath.Join(dir, "out_clip.mp4"))
	})

	t.Run("directory batch encodes the selection", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"a.mp4", "b.mp4", "c.mp4"} {
			copyClip(t, clip, filepath.Join(dir, name))
		}

		app := startTUI(t, bin, dir, dir)
		app.waitFor("c.mp4")
		app.waitFor("enter start")
		// Select a.mp4, move down, select b.mp4, start.
		for _, key := range []string{" ", "j", " ", "\r"} {
			app.send(key)
		}
		app.waitFor("All done!")
		app.send("q")
		app.waitExit(0)

		wantFiles(t, dir, "a.mp4", "b.mp4", "c.mp4", "out_a.mp4", "out_b.mp4")
		checkOutput(t, filepath.Join(dir, "out_a.mp4"))
		checkOutput(t, filepath.Join(dir, "out_b.mp4"))
	})

	t.Run("q quits the picker", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"a.mp4", "b.mp4"} {
			copyClip(t, clip, filepath.Join(dir, name))
		}

		app := startTUI(t, bin, dir, ".")
		app.waitFor("enter start")
		app.send("q")
		app.waitExit(0)

		wantFiles(t, dir, "a.mp4", "b.mp4")
	})
}

// tuiApp is a fideo process running in a pseudo-terminal. A goroutine drains
// the terminal into out, so the program never blocks on a full pty buffer.
type tuiApp struct {
	t   *testing.T
	cmd *exec.Cmd
	pty *os.File

	mu      sync.Mutex
	out     bytes.Buffer
	changed chan struct{} // closed and replaced whenever out grows
	eof     bool          // the terminal returned EOF or an error

	exited chan struct{} // closed when cmd.Wait returns
	err    error         // cmd.Wait's result, set before exited is closed
}

// startTUI starts bin with args in dir, attached to a tuiRows×tuiCols pty.
func startTUI(t *testing.T, bin, dir string, args ...string) *tuiApp {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: tuiRows, Cols: tuiCols})
	if err != nil {
		t.Fatalf("starting fideo in a pty: %v", err)
	}
	app := &tuiApp{t: t, cmd: cmd, pty: f, changed: make(chan struct{}), exited: make(chan struct{})}
	go app.read()
	go func() {
		app.err = cmd.Wait()
		close(app.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-app.exited:
		default:
			_ = cmd.Process.Kill()
			<-app.exited
		}
		_ = f.Close()
	})
	return app
}

// cursorQuery is the cursor position request lipgloss sends (after an OSC 11
// background query) to learn the terminal's colors at startup; it waits up to
// 5s for the answer.
const cursorQuery = "\x1b[6n"

func (a *tuiApp) read() {
	buf := make([]byte, 4096)
	answered := 0
	for {
		n, err := a.pty.Read(buf)
		a.mu.Lock()
		a.out.Write(buf[:n])
		if err != nil {
			a.eof = true
		}
		close(a.changed)
		a.changed = make(chan struct{})
		queries := bytes.Count(a.out.Bytes(), []byte(cursorQuery))
		a.mu.Unlock()
		if err != nil {
			return
		}
		// Answer like a terminal without OSC 11 support: the cursor position
		// alone, so the query returns at once and the default (dark) is used.
		for ; answered < queries; answered++ {
			_, _ = io.WriteString(a.pty, "\x1b[1;1R")
		}
	}
}

// screen returns everything drawn so far, without escape sequences.
func (a *tuiApp) screen() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ansi.Strip(a.out.String())
}

// waitFor returns once text has been drawn, failing the test if it hasn't been
// within tuiTimeout or the terminal closed without it.
func (a *tuiApp) waitFor(text string) {
	a.t.Helper()
	deadline := time.NewTimer(tuiTimeout)
	defer deadline.Stop()
	for {
		a.mu.Lock()
		found := strings.Contains(ansi.Strip(a.out.String()), text)
		changed, eof := a.changed, a.eof
		a.mu.Unlock()
		switch {
		case found:
			return
		case eof:
			a.t.Fatalf("fideo's terminal closed before %q was drawn; output:\n%s", text, a.screen())
		}
		select {
		case <-changed:
		case <-deadline.C:
			a.t.Fatalf("%q not drawn within %v; output:\n%s", text, tuiTimeout, a.screen())
		}
	}
}

// send types key into the terminal.
func (a *tuiApp) send(key string) {
	a.t.Helper()
	if _, err := io.WriteString(a.pty, key); err != nil {
		a.t.Fatalf("typing %q: %v", key, err)
	}
}

// waitExit waits for fideo to exit and checks its exit code.
func (a *tuiApp) waitExit(want int) {
	a.t.Helper()
	select {
	case <-a.exited:
	case <-time.After(tuiTimeout):
		a.t.Fatalf("fideo did not exit within %v; output:\n%s", tuiTimeout, a.screen())
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(a.err, &exitErr) {
		code = exitErr.ExitCode()
	} else if a.err != nil {
		a.t.Fatalf("waiting for fideo: %v", a.err)
	}
	if code != want {
		a.t.Fatalf("exit code = %d, want %d; output:\n%s", code, want, a.screen())
	}
}

// copyClip copies the generated source clip to dst.
func copyClip(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantFiles checks that dir holds exactly the named files: the outputs the run
// should have written, and no others (no stray out_ or scratch files).
func wantFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files in dir = %v, want %v", got, want)
	}
}

// checkOutput checks that path is an H.264 video ffprobe can read.
func checkOutput(t *testing.T, path string) {
	t.Helper()
	if codec := probeStream(t, path, "v:0", "codec_name"); codec != "h264" {
		t.Errorf("%s video codec = %q, want h264", filepath.Base(path), codec)
	}
}
