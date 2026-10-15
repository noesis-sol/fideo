package fideo

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The clipboard tool stand-in is this test binary re-run as
// TestClipboardHelperProcess: it copies its stdin to the file named by
// clipboardOutEnv, so a test sees what would have reached the clipboard.
const (
	clipboardHelperEnv = "FIDEO_CLIPBOARD_HELPER"
	clipboardOutEnv    = "FIDEO_CLIPBOARD_OUT"
)

func TestClipboardHelperProcess(t *testing.T) {
	if os.Getenv(clipboardHelperEnv) != "1" {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err == nil {
		err = os.WriteFile(os.Getenv(clipboardOutEnv), data, 0o600)
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// fakeClipboardTool is a clipboard command constructor that records the tool
// asked for and runs the stand-in in its place.
type fakeClipboardTool struct {
	t     *testing.T
	out   string
	calls [][]string
}

func newFakeClipboardTool(t *testing.T) *fakeClipboardTool {
	return &fakeClipboardTool{t: t, out: filepath.Join(t.TempDir(), "clipboard")}
}

func (f *fakeClipboardTool) command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	f.calls = append(f.calls, append([]string{name}, arg...))
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClipboardHelperProcess$")
	cmd.Env = append(os.Environ(), clipboardHelperEnv+"=1", clipboardOutEnv+"="+f.out)
	return cmd
}

// received is the text the stand-in read from its stdin.
func (f *fakeClipboardTool) received() string {
	data, err := os.ReadFile(f.out)
	if err != nil {
		f.t.Fatalf("clipboard tool never ran: %v", err)
	}
	return string(data)
}

func TestClipboardCopyTool(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		tools   []string
		want    []string // the tool and its arguments; nil when none runs
		wantErr string
	}{
		{"darwin", "darwin", nil, []string{"pbcopy"}, ""},
		{"windows", "windows", nil, []string{"clip"}, ""},
		{"linux prefers xclip", "linux", []string{"xclip", "xsel", "wl-copy"},
			[]string{"xclip", "-selection", "clipboard"}, ""},
		{"linux falls back to xsel", "linux", []string{"xsel", "wl-copy"},
			[]string{"xsel", "--clipboard", "--input"}, ""},
		{"linux falls back to wl-copy", "linux", []string{"wl-copy"}, []string{"wl-copy"}, ""},
		{"linux with no tool", "linux", nil, nil,
			"no clipboard utility found (install xclip, xsel, or wl-copy)"},
		{"unsupported OS", "plan9", []string{"xclip"}, nil, "clipboard not supported on this platform"},
		{"zero value", "", nil, nil, "clipboard not supported on this platform"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeClipboardTool(t)
			clip := clipboard{goos: tt.goos, lookPath: fakeLookPath(tt.tools...), command: fake.command}

			err := clip.copy("brew install ffmpeg")
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("copy() err = %v, want %q", err, tt.wantErr)
				}
				if len(fake.calls) != 0 {
					t.Errorf("ran %v, want no clipboard tool", fake.calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("copy() err = %v, want nil", err)
			}
			if !reflect.DeepEqual(fake.calls, [][]string{tt.want}) {
				t.Errorf("ran %v, want %v", fake.calls, [][]string{tt.want})
			}
			if got := fake.received(); got != "brew install ffmpeg" {
				t.Errorf("clipboard tool received %q, want the copied text", got)
			}
		})
	}
}

// TestClipboardCopyStartFailure checks a tool that can't be started is
// reported rather than taken for a successful copy.
func TestClipboardCopyStartFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-tool")
	clip := clipboard{
		goos:     "darwin",
		lookPath: fakeLookPath(),
		command: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, missing)
		},
	}
	err := clip.copy("brew install ffmpeg")
	if err == nil || !strings.Contains(err.Error(), "no-such-tool") {
		t.Errorf("copy() err = %v, want the start failure", err)
	}
}

// TestErrorModelCopyFailure checks a copy that fails comes back as its error,
// and the dialog then shows it.
func TestErrorModelCopyFailure(t *testing.T) {
	m := testErrorModel()
	m.clip = clipboard{goos: "linux", lookPath: fakeLookPath(), command: newFakeClipboardTool(t).command}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	msg, ok := cmd().(clipboardResultMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want clipboardResultMsg", msg)
	}
	if msg.err == nil {
		t.Fatal("clipboardResultMsg.err = nil, want the missing-tool error")
	}
	got, _ := m.Update(msg)
	if em := got.(errorModel); em.copied || em.err != msg.err.Error() {
		t.Errorf("copied, err = %v, %q; want false, %q", em.copied, em.err, msg.err)
	}
}
