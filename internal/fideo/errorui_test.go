package fideo

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeLookPath returns a lookPath that finds only the named tools.
func fakeLookPath(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

func TestGetInstallCommand(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		tools       []string
		wantOS      string
		wantCommand string
	}{
		{"darwin", "darwin", nil, "macOS", "brew install ffmpeg"},
		{"windows", "windows", nil, "Windows", "choco install ffmpeg"},
		{"unknown OS", "plan9", nil, "Your system", "Please visit https://ffmpeg.org/download.html"},
		{"linux with apt", "linux", []string{"apt", "dnf"}, "Ubuntu/Debian", "sudo apt install ffmpeg"},
		{"linux with dnf", "linux", []string{"dnf"}, "Fedora", "sudo dnf install ffmpeg"},
		{"linux with neither", "linux", nil, "Linux", "sudo apt install ffmpeg  # or use your package manager"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			osName, command := getInstallCommand(tt.goos, fakeLookPath(tt.tools...))
			if osName != tt.wantOS || command != tt.wantCommand {
				t.Errorf("getInstallCommand(%q) = (%q, %q), want (%q, %q)",
					tt.goos, osName, command, tt.wantOS, tt.wantCommand)
			}
		})
	}
}

// testErrorModel is the dialog as a macOS host would show it.
func testErrorModel() errorModel {
	return errorModel{osName: "macOS", command: "brew install ffmpeg"}
}

func TestErrorModelQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		t.Run(key.String(), func(t *testing.T) {
			_, cmd := testErrorModel().Update(key)
			if cmd == nil {
				t.Fatal("cmd = nil, want tea.Quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
			}
		})
	}
}

// TestErrorModelCopyKeys checks c and enter run the clipboard copy, through a
// stand-in for the clipboard tool, and report its result.
func TestErrorModelCopyKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("c")},
		{Type: tea.KeyEnter},
	} {
		t.Run(key.String(), func(t *testing.T) {
			m := testErrorModel()
			fake := newFakeClipboardTool(t)
			m.clip = clipboard{goos: "darwin", lookPath: fakeLookPath(), command: fake.command}

			got, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("cmd = nil, want the clipboard copy")
			}
			if m := got.(errorModel); m.copied || m.err != "" {
				t.Errorf("model changed before the copy finished: %+v", m)
			}
			msg, ok := cmd().(clipboardResultMsg)
			if !ok {
				t.Fatalf("cmd() = %T, want clipboardResultMsg", msg)
			}
			if msg.err != nil {
				t.Errorf("clipboardResultMsg.err = %v, want nil", msg.err)
			}
			if got := fake.received(); got != "brew install ffmpeg" {
				t.Errorf("clipboard tool received %q, want the install command", got)
			}
		})
	}
}

func TestErrorModelOtherKeysIgnored(t *testing.T) {
	got, cmd := testErrorModel().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd != nil {
		t.Error("cmd != nil for an unbound key")
	}
	if m, want := got.(errorModel), testErrorModel(); m.osName != want.osName ||
		m.command != want.command || m.copied != want.copied || m.err != want.err {
		t.Errorf("model = %+v, want it unchanged", got)
	}
}

func TestErrorModelClipboardResult(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCopied bool
		wantErr    string
	}{
		{"success", nil, true, ""},
		{"failure", errors.New("no clipboard utility found"), false, "no clipboard utility found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cmd := testErrorModel().Update(clipboardResultMsg{err: tt.err})
			if cmd != nil {
				t.Error("cmd != nil after a clipboard result")
			}
			m := got.(errorModel)
			if m.copied != tt.wantCopied || m.err != tt.wantErr {
				t.Errorf("copied, err = %v, %q; want %v, %q", m.copied, m.err, tt.wantCopied, tt.wantErr)
			}
		})
	}
}

// TestErrorModelView checks each state shows the OS, the command and its own
// status lines, and none of the other states' lines.
func TestErrorModelView(t *testing.T) {
	idle := []string{"Press 'c' or 'enter' to copy", "Or select the command above with your mouse"}
	copied := []string{"✓ Copied to clipboard!", "Paste it in your terminal to install ffmpeg."}
	failed := []string{"✗ clipboard not supported", "Please select and copy the command manually."}

	tests := []struct {
		name   string
		copied bool
		err    string
		want   []string
		absent [][]string
	}{
		{"idle", false, "", idle, [][]string{copied, failed}},
		{"copied", true, "", copied, [][]string{idle, failed}},
		{"error", false, "clipboard not supported", failed, [][]string{idle, copied}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testErrorModel()
			m.copied, m.err = tt.copied, tt.err
			view := ansi.Strip(m.View())

			for _, s := range append([]string{
				"Installation Instructions for macOS:", "brew install ffmpeg",
			}, tt.want...) {
				if !strings.Contains(view, s) {
					t.Errorf("view lacks %q:\n%s", s, view)
				}
			}
			for _, lines := range tt.absent {
				for _, s := range lines {
					if strings.Contains(view, s) {
						t.Errorf("view shows %q in the %s state:\n%s", s, tt.name, view)
					}
				}
			}
		})
	}
}
