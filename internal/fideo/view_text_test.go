package fideo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// viewLines renders m and returns the frame's lines with styling stripped.
func viewLines(m model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// TestStatusHeaderOnlyWhileProcessing pins the "Processing X of Y" summary on
// the line under the title: present with the batch counts while a batch runs,
// empty otherwise.
func TestStatusHeaderOnlyWhileProcessing(t *testing.T) {
	tests := []struct {
		name       string
		processing bool
		done       bool
		want       string
	}{
		{"idle", false, false, ""},
		{"processing", true, false, "Processing 2 of 5 files (1 completed)"},
		{"done", false, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel([]videoFile{{name: "a.mp4"}, {name: "b.mp4"}})
			m.processing, m.done = tt.processing, tt.done
			m.processingCount, m.totalToProcess, m.completedCount = 2, 5, 1

			lines := viewLines(m)
			if got := lines[1]; got != tt.want {
				t.Errorf("header line = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFooterText pins the four footers: the idle key help, the cancel hint
// while processing, and the clean and failed completions.
func TestFooterText(t *testing.T) {
	tests := []struct {
		name       string
		processing bool
		done       bool
		failed     bool
		want       string
	}{
		{"idle help", false, false, false, "↑/↓ navigate • space select • a all • enter start • q quit"},
		{"processing", true, false, false, "Processing... (c or ctrl+c to cancel)"},
		{"all done", false, true, false, "All done! Press q to quit."},
		{"done with errors", false, true, true, "Done — some files had errors (see above). Press q to quit."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []videoFile{{name: "a.mp4"}, {name: "b.mp4"}}
			if tt.failed {
				files[1].status = statusError
			}
			m := newModel(files)
			m.processing, m.done = tt.processing, tt.done

			lines := viewLines(m)
			if got := lines[len(lines)-1]; got != tt.want {
				t.Errorf("footer = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestViewEmptyList shows the "no videos" notice and the exit hint instead of
// a file list and the key-help footer.
func TestViewEmptyList(t *testing.T) {
	view := ansi.Strip(newModel(nil).View())

	for _, want := range []string{"No video files found in current directory.", "Press esc or q to exit."} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "navigate") {
		t.Errorf("empty view shows the key-help footer:\n%s", view)
	}
}

// TestOverwriteDialogText checks the dialog names only the output's base name
// and marks exactly the option under the cursor.
func TestOverwriteDialogText(t *testing.T) {
	options := []string{"Overwrite existing file", "Skip this file", "Cancel all"}
	for cursor := range options {
		t.Run(options[cursor], func(t *testing.T) {
			m := newModel([]videoFile{{name: "clip.mp4"}})
			m.showOverwritePrompt = true
			m.pendingOutputFile = "/videos/holiday/out_clip.mp4"
			m.overwriteCursor = cursor

			dialog := ansi.Strip(m.renderOverwriteDialog())
			if !strings.Contains(dialog, "out_clip.mp4") {
				t.Errorf("dialog does not name the file:\n%s", dialog)
			}
			if strings.Contains(dialog, "/videos/holiday") {
				t.Errorf("dialog shows the directory, want the base name only:\n%s", dialog)
			}
			for i, opt := range options {
				marked := strings.Contains(dialog, cursorActive+opt)
				if marked != (i == cursor) {
					t.Errorf("option %q marked = %v, want %v:\n%s", opt, marked, i == cursor, dialog)
				}
				if !marked && !strings.Contains(dialog, cursorInactive+opt) {
					t.Errorf("option %q missing:\n%s", opt, dialog)
				}
			}
			if view := ansi.Strip(m.View()); !strings.Contains(view, "out_clip.mp4") {
				t.Errorf("View does not include the dialog:\n%s", view)
			}
		})
	}
}

// TestFileGlyph pins each row's leading indicator: the spinner for an encoding
// file, a dim hollow circle for one still queued or unselected, and a filled
// circle for a selected one.
func TestFileGlyph(t *testing.T) {
	tests := []struct {
		name       string
		processing bool
		file       videoFile
		wantGlyph  string
		wantStyle  lipgloss.Style
		wantSpin   bool
	}{
		{"encoding", true, videoFile{selected: true, status: statusProcessing}, "", lipgloss.Style{}, true},
		{"queued", true, videoFile{selected: true, status: statusPending}, markerUnselected, dimStyle, false},
		{"selected idle", false, videoFile{selected: true}, markerSelected, normalStyle, false},
		{"selected and done in a batch", true, videoFile{selected: true, status: statusDone}, markerSelected, normalStyle, false},
		{"unselected idle", false, videoFile{}, markerUnselected, dimStyle, false},
		{"unselected in a batch", true, videoFile{}, markerUnselected, dimStyle, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(nil)
			m.processing = tt.processing

			glyph, style, spin := m.fileGlyph(tt.file)
			if spin != tt.wantSpin {
				t.Fatalf("spin = %v, want %v", spin, tt.wantSpin)
			}
			if spin {
				return // glyph and style are unused
			}
			if glyph != tt.wantGlyph {
				t.Errorf("glyph = %q, want %q", glyph, tt.wantGlyph)
			}
			if got, want := style.GetForeground(), tt.wantStyle.GetForeground(); got != want {
				t.Errorf("style foreground = %v, want %v", got, want)
			}
		})
	}
}

// TestFileRowShowsSpinnerWhileEncoding checks the rendered row carries the
// spinner's frame, not a circle, while its file encodes.
func TestFileRowShowsSpinnerWhileEncoding(t *testing.T) {
	m := newModel([]videoFile{{name: "a.mp4", selected: true, status: statusProcessing}})
	m.processing = true

	var s strings.Builder
	m.renderFileRow(&s, 0, m.files[0])
	row := ansi.Strip(s.String())
	if want := cursorActive + ansi.Strip(m.spinner.View()) + " a.mp4"; !strings.HasPrefix(row, want) {
		t.Errorf("row = %q, want prefix %q", row, want)
	}
	if strings.Contains(row, markerSelected) || strings.Contains(row, markerUnselected) {
		t.Errorf("encoding row shows a circle glyph: %q", row)
	}
}

// TestPercentReadout pins the percentage beside the progress bar: rounded to
// the nearest whole percent and clamped to 0–100.
func TestPercentReadout(t *testing.T) {
	tests := []struct {
		progress float64
		want     string
	}{
		{0, "0%"},
		{0.5, "50%"},
		{0.994, "99%"},
		{0.996, "100%"},
		{0.426, "43%"},
		{1, "100%"},
		{-0.3, "0%"},
		{1.7, "100%"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.progress), func(t *testing.T) {
			m := newModel(nil)
			var s strings.Builder
			m.renderProgress(&s, videoFile{progress: tt.progress})

			lines := strings.Split(ansi.Strip(s.String()), "\n")
			if len(lines) != 2 {
				t.Fatalf("renderProgress wrote %d lines, want 2: %q", len(lines), lines)
			}
			fields := strings.Fields(lines[1])
			if got := fields[len(fields)-1]; got != tt.want {
				t.Errorf("readout = %q, want %q (line %q)", got, tt.want, lines[1])
			}
		})
	}
}

// TestGetProgressColor checks the gradient's ends hit the first and last stops
// and that progress outside [0, 1] is clamped to them.
func TestGetProgressColor(t *testing.T) {
	first := lipgloss.Color(progressColorStops[0].Hex())
	last := lipgloss.Color(progressColorStops[len(progressColorStops)-1].Hex())
	tests := []struct {
		progress float64
		want     lipgloss.Color
	}{
		{0, first},
		{1, last},
		{-0.5, first},
		{-100, first},
		{1.5, last},
		{100, last},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.progress), func(t *testing.T) {
			if got := getProgressColor(tt.progress); got != tt.want {
				t.Errorf("getProgressColor(%v) = %v, want %v", tt.progress, got, tt.want)
			}
		})
	}
}

// TestPercentLabels pins the precomputed readout labels "0%".."100%", one per
// whole percent, each colored by the gradient at that percent.
func TestPercentLabels(t *testing.T) {
	styles, labels := buildPercentTables()
	for p := 0; p <= 100; p++ {
		if want := fmt.Sprintf("%d%%", p); labels[p] != want || percentLabels[p] != want {
			t.Errorf("label %d = %q (package table %q), want %q", p, labels[p], percentLabels[p], want)
		}
		if got, want := styles[p].GetForeground(), getProgressColor(float64(p)/100); got != want {
			t.Errorf("style %d foreground = %v, want %v", p, got, want)
		}
	}
}
