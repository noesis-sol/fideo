package fideo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Installation-dialog styles (static; built once rather than rebuilt on every
// View() call, matching the package-level style vars in view.go).
var (
	errBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 2).
			Width(70)
	errTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))
	errCommandBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("82")).
				Padding(0, 1).
				Foreground(lipgloss.Color("82")).
				Bold(true)
	errCopyButtonStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("117")).
				Background(lipgloss.Color("236")).
				Padding(0, 2)
)

// getInstallCommand names the platform and its ffmpeg install command. The
// target OS and the PATH lookup are parameters (production passes runtime.GOOS
// and exec.LookPath) so every branch is unit-testable on one host.
func getInstallCommand(goos string, lookPath func(string) (string, error)) (osName, command string) {
	switch goos {
	case "darwin":
		return "macOS", "brew install ffmpeg"
	case "linux":
		if _, err := lookPath("apt"); err == nil {
			return "Ubuntu/Debian", "sudo apt install ffmpeg"
		} else if _, err := lookPath("dnf"); err == nil {
			return "Fedora", "sudo dnf install ffmpeg"
		}
		return "Linux", "sudo apt install ffmpeg  # or use your package manager"
	case "windows":
		return "Windows", "choco install ffmpeg"
	default:
		return "Your system", "Please visit https://ffmpeg.org/download.html"
	}
}

// clipboard copies text with the platform's clipboard tool. The target OS, the
// PATH lookup and the command constructor are fields (systemClipboard fills in
// runtime.GOOS, exec.LookPath and exec.CommandContext), so tests pick the tool
// for every OS and run a stand-in without touching the real clipboard. The
// zero value runs nothing: its empty OS is reported as unsupported.
type clipboard struct {
	goos     string
	lookPath func(string) (string, error)
	command  func(ctx context.Context, name string, arg ...string) *exec.Cmd
}

func systemClipboard() clipboard {
	return clipboard{goos: runtime.GOOS, lookPath: exec.LookPath, command: exec.CommandContext}
}

// clipboardTool names the clipboard tool for goos and the arguments that make
// it read the text from stdin. On Linux it takes the first of xclip, xsel and
// wl-copy that lookPath finds.
func clipboardTool(goos string, lookPath func(string) (string, error)) (name string, args []string, err error) {
	switch goos {
	case "darwin":
		return "pbcopy", nil, nil
	case "linux":
		for _, tool := range []struct {
			name string
			args []string
		}{
			{"xclip", []string{"-selection", "clipboard"}},
			{"xsel", []string{"--clipboard", "--input"}},
			{"wl-copy", nil},
		} {
			if _, err := lookPath(tool.name); err == nil {
				return tool.name, tool.args, nil
			}
		}
		return "", nil, fmt.Errorf("no clipboard utility found (install xclip, xsel, or wl-copy)")
	case "windows":
		return "clip", nil, nil
	default:
		return "", nil, fmt.Errorf("clipboard not supported on this platform")
	}
}

func (c clipboard) copy(text string) error {
	name, args, err := clipboardTool(c.goos, c.lookPath)
	if err != nil {
		return err
	}
	// Bound the helper so a wedged clipboard tool (e.g. a stuck wl-copy/xclip)
	// can't hang forever; copyCmd already runs this off the UI goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := c.command(ctx, name, args...)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Always reap the child, even if the Write/Close below fails (e.g. the helper
	// exits early and the pipe write returns EPIPE) — otherwise the started
	// process is never Wait()ed and leaks as a zombie.
	defer func() { _ = cmd.Wait() }()
	if _, err := pipe.Write([]byte(text)); err != nil {
		return err
	}
	return pipe.Close()
}

type errorModel struct {
	osName  string
	command string
	copied  bool
	err     string
	clip    clipboard
}

func newErrorModel() errorModel {
	osName, command := getInstallCommand(runtime.GOOS, exec.LookPath)
	return errorModel{osName: osName, command: command, clip: systemClipboard()}
}

func (m errorModel) Init() tea.Cmd { return nil }

// clipboardResultMsg carries the outcome of an async clipboard copy back to the
// Update loop.
type clipboardResultMsg struct{ err error }

// copyCmd runs the (potentially slow, process-spawning) clipboard copy as a
// tea.Cmd so it never blocks the Update goroutine, per Bubble Tea's contract.
func copyCmd(clip clipboard, command string) tea.Cmd {
	return func() tea.Msg {
		return clipboardResultMsg{err: clip.copy(command)}
	}
}

func (m errorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case clipboardResultMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.copied = true
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "c", "enter":
			return m, copyCmd(m.clip, m.command)
		}
	}
	return m, nil
}

func (m errorModel) View() string {
	var content strings.Builder
	content.WriteString(errTitleStyle.Render("⚠️  Missing Dependency"))
	content.WriteString("\n\n")
	content.WriteString(normalStyle.Render("ffmpeg is not installed on your system."))
	content.WriteString("\n")
	content.WriteString(dimStyle.Render("This tool requires ffmpeg for video compression."))
	content.WriteString("\n\n")
	content.WriteString(infoStyle.Render("Installation Instructions for " + m.osName + ":"))
	content.WriteString("\n\n")
	content.WriteString(errCommandBoxStyle.Render(m.command))
	content.WriteString("\n\n")

	if m.copied {
		content.WriteString(successStyle.Render("✓ Copied to clipboard!"))
		content.WriteString("\n")
		content.WriteString(helpTextStyle.Render("Paste it in your terminal to install ffmpeg."))
	} else if m.err != "" {
		content.WriteString(errorStyle.Render("✗ " + m.err))
		content.WriteString("\n")
		content.WriteString(helpTextStyle.Render("Please select and copy the command manually."))
	} else {
		content.WriteString(errCopyButtonStyle.Render("Press 'c' or 'enter' to copy"))
		content.WriteString("\n")
		content.WriteString(helpTextStyle.Render("Or select the command above with your mouse"))
	}

	content.WriteString("\n\n")
	content.WriteString(dimStyle.Render("Press ") + keyStyle.Render("q") + dimStyle.Render(" to exit"))

	return "\n" + errBoxStyle.Render(content.String()) + "\n"
}

func displayInstallationHelp() {
	m := newErrorModel()
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}
