//go:build integration

package fideo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The CLI end-to-end tests build ./cmd/fideo once and run the binary the way a
// user does, checking its output and exit code. Every case exits before the TUI
// starts; a timeout guards against one that doesn't.

// buildFideo compiles the fideo command into a temp dir and returns its path.
// TestCLIEndToEnd builds it once and runs every case as a subtest.
func buildFideo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg/ffprobe are shell scripts; not supported on windows")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found on PATH; cannot build the fideo binary")
	}
	bin := filepath.Join(t.TempDir(), "fideo")
	if out, err := exec.Command(goTool, "build", "-o", bin, "fideo/cmd/fideo").CombinedOutput(); err != nil {
		t.Fatalf("building fideo: %v\n%s", err, out)
	}
	return bin
}

// fakeToolsDir returns a directory holding stub ffmpeg and ffprobe scripts, so
// the dependency check passes whatever the host has installed.
func fakeToolsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		script := "#!/bin/sh\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type cliResult struct {
	stdout, stderr string
	code           int
}

// runFideo runs bin with args in dir, with PATH set to pathDir only.
func runFideo(t *testing.T, bin, dir, pathDir string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+pathDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("fideo %v did not exit (reached the TUI?)\nstdout: %s\nstderr: %s", args, stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("running fideo %v: %v", args, err)
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), code: cmd.ProcessState.ExitCode()}
}

func TestCLIEndToEnd(t *testing.T) {
	bin := buildFideo(t)
	t.Run("with ffmpeg", func(t *testing.T) { testCLIWithFFmpeg(t, bin) })
	t.Run("without ffmpeg", func(t *testing.T) { testCLIWithoutFFmpeg(t, bin) })
}

// testCLIWithFFmpeg runs the cases that reach the dependency check with stub
// ffmpeg/ffprobe on PATH, so the result doesn't depend on the host.
func testCLIWithFFmpeg(t *testing.T, bin string) {
	tools := fakeToolsDir(t)

	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "notes.txt"), []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(work, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	const usageHint = "Run 'fideo --help' for usage."
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string
		wantStderr []string
	}{
		{"--help", []string{"--help"}, 0, []string{"Usage: fideo [options]", "--format <fmt>", "Examples:"}, nil},
		{"-h", []string{"-h"}, 0, []string{"Usage: fideo [options]"}, nil},
		{"help wins over paths", []string{"missing.mp4", "--help"}, 0, []string{"Usage: fideo [options]"}, nil},
		{"unknown option", []string{"--verbose"}, 1, nil,
			[]string{`Error: unknown option "--verbose"`, usageHint}},
		{"missing flag value", []string{"--format"}, 1, nil,
			[]string{"--format requires a value", "Supported formats: mp4, mov, mkv, webm", usageHint}},
		{"bogus format", []string{"--format", "bogus", "notes.txt"}, 1, nil,
			[]string{`Error: unsupported format "bogus"`, "Supported formats: mp4, mov, mkv, webm"}},
		{"bogus size", []string{"--size", "bogus", "notes.txt"}, 1, nil,
			[]string{`Error: unsupported size "bogus"`, "Supported sizes: sm, small, md, medium, lg, large"}},
		{"hw with webm", []string{"--hw", "--format", "webm", "notes.txt"}, 1, nil,
			[]string{"Error: --hw is not compatible with --format webm"}},
		{"missing path", []string{"missing.mp4"}, 1, nil,
			[]string{"Error: stat missing.mp4: no such file or directory"}},
		{"non-video file", []string{"notes.txt"}, 1, nil,
			[]string{"Error: notes.txt is not a supported video file", "Supported extensions: .mp4"}},
		{"empty directory", []string{"empty"}, 1, nil,
			[]string{"Error: no video files found in the given paths"}},
		{"pattern with no match", []string{"*.nomatch"}, 1, nil,
			[]string{"Error: no files or directories match pattern: *.nomatch"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runFideo(t, bin, work, tools, tt.args...)
			checkCLIResult(t, got, tt.wantCode, tt.wantStdout, tt.wantStderr)
			if tt.wantCode != 0 && got.stdout != "" {
				t.Errorf("stdout = %q, want empty on a usage error", got.stdout)
			}
			if tt.wantCode == 0 && got.stderr != "" {
				t.Errorf("stderr = %q, want empty", got.stderr)
			}
		})
	}
}

// testCLIWithoutFFmpeg pins that --help and usage errors don't need ffmpeg:
// they are handled before the dependency check, whose help dialog is a TUI.
func testCLIWithoutFFmpeg(t *testing.T, bin string) {
	noTools := t.TempDir()

	got := runFideo(t, bin, t.TempDir(), noTools, "--help")
	checkCLIResult(t, got, 0, []string{"Usage: fideo [options]"}, nil)

	got = runFideo(t, bin, t.TempDir(), noTools, "--bogus")
	checkCLIResult(t, got, 1, nil, []string{`unknown option "--bogus"`, "Run 'fideo --help' for usage."})
}

func checkCLIResult(t *testing.T, got cliResult, wantCode int, wantStdout, wantStderr []string) {
	t.Helper()
	if got.code != wantCode {
		t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", got.code, wantCode, got.stdout, got.stderr)
	}
	for _, s := range wantStdout {
		if !strings.Contains(got.stdout, s) {
			t.Errorf("stdout missing %q\nstdout: %s", s, got.stdout)
		}
	}
	for _, s := range wantStderr {
		if !strings.Contains(got.stderr, s) {
			t.Errorf("stderr missing %q\nstderr: %s", s, got.stderr)
		}
	}
}
