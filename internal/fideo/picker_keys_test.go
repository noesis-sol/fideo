package fideo

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pickerKey is key() plus the special keys it doesn't build: esc and ctrl+c.
func pickerKey(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return key(s)
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestSpaceTogglesSelection(t *testing.T) {
	tests := []struct {
		name        string
		status      fileStatus
		selected    bool
		wantOutPath string
	}{
		{"selects a pending file and drops its claim", statusPending, false, ""},
		{"deselects a pending file and drops its claim", statusPending, true, ""},
		{"keeps a finished file's output path", statusDone, false, "stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, f := pickerModel(t, "a.mp4", "b.mp4")
			m.cursor = 1
			m.files[1].status = tt.status
			m.files[1].selected = tt.selected
			m.files[1].outPath = "stale"

			m, cmd := m.dispatch(key("space"))

			if cmd != nil || m.processing || len(f.started) != 0 {
				t.Fatal("space started the batch; it should only toggle the selection")
			}
			if m.files[1].selected == tt.selected {
				t.Errorf("selected stayed %v", tt.selected)
			}
			if m.files[0].selected {
				t.Error("space changed a file other than the one under the cursor")
			}
			if m.files[1].outPath != tt.wantOutPath {
				t.Errorf("outPath = %q, want %q", m.files[1].outPath, tt.wantOutPath)
			}
		})
	}
}

func TestSpaceIgnoredWhileProcessing(t *testing.T) {
	m, _ := batchModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)
	m.cursor = 1

	m, _ = m.dispatch(key("space"))

	if !m.files[1].selected {
		t.Error("space changed the selection while a batch was running")
	}
}

func TestEnterStartsOnlySelected(t *testing.T) {
	m, f := pickerModel(t, "a.mp4", "b.mp4", "c.mp4")
	m.config.maxConcurrent = 3
	m.files[0].selected = true
	m.files[2].selected = true

	m, cmd := m.dispatch(key("enter"))

	if cmd == nil || !m.processing {
		t.Fatal("enter with a selection should start the batch")
	}
	if m.totalToProcess != 2 {
		t.Errorf("totalToProcess = %d, want 2", m.totalToProcess)
	}
	if len(f.started) != 2 || f.started[0] != 0 || f.started[1] != 2 {
		t.Errorf("started %v, want [0 2]", f.started)
	}
	if m.files[1].status != statusPending {
		t.Errorf("unselected file's status = %v, want pending", m.files[1].status)
	}
}

func TestEnterWithNothingSelected(t *testing.T) {
	m, f := pickerModel(t, "a.mp4", "b.mp4") // two, as a lone file is pre-selected

	m, cmd := m.dispatch(key("enter"))

	if cmd != nil || m.processing || len(f.started) != 0 {
		t.Error("enter with nothing selected started a batch")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		t.Run(k+" quits when idle", func(t *testing.T) {
			m, _ := pickerModel(t, "a.mp4")

			_, cmd := m.dispatch(pickerKey(k))

			if !isQuit(cmd) {
				t.Errorf("%s did not quit an idle picker", k)
			}
		})
	}
	for _, k := range []string{"q", "esc"} {
		t.Run(k+" does nothing while processing", func(t *testing.T) {
			cancelled := 0
			m, _ := batchModel(t, 1, []string{"a.mp4"}, 0)
			m.cancels[0] = func() { cancelled++ }

			m, cmd := m.dispatch(pickerKey(k))

			if cmd != nil {
				t.Errorf("%s returned a command while processing", k)
			}
			if !m.processing || m.userCancelled || cancelled != 0 {
				t.Errorf("%s disturbed the running batch (processing %v, userCancelled %v, cancels %d)",
					k, m.processing, m.userCancelled, cancelled)
			}
		})
	}
}

func TestCIsIgnoredWhenIdle(t *testing.T) {
	m, _ := pickerModel(t, "a.mp4")

	m, cmd := m.dispatch(key("c"))

	if cmd != nil || m.userCancelled {
		t.Error("c on an idle picker should do nothing")
	}
}

func TestCancelKeysCancelEveryInFlightEncode(t *testing.T) {
	for _, k := range []string{"c", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			m, _ := batchModel(t, 3, []string{"a.mp4", "b.mp4", "c.mp4", "d.mp4"}, 0, 1, 2)
			cancelled := map[int]int{}
			for i := range m.cancels {
				m.cancels[i] = func() { cancelled[i]++ }
			}

			m, cmd := m.dispatch(pickerKey(k))

			if cmd != nil {
				t.Errorf("%s returned a command; while processing it should only cancel", k)
			}
			if !m.userCancelled {
				t.Error("userCancelled not set")
			}
			for _, i := range []int{0, 1, 2} {
				if cancelled[i] != 1 {
					t.Errorf("worker %d cancelled %d times, want 1", i, cancelled[i])
				}
			}
			if len(m.cancels) != 3 {
				t.Errorf("cancels has %d entries; slots are released by each worker's cancelMsg, not the key", len(m.cancels))
			}
		})
	}
}

func TestKeysOnEmptyList(t *testing.T) {
	for _, k := range []string{"space", "a", "enter", "c", "up", "down", "pgdown", "end", "G"} {
		t.Run(k, func(t *testing.T) {
			m := newModel(nil)
			f := &fakeLauncher{}
			m.launch = f.launch

			m, cmd := m.dispatch(key(k))

			if cmd != nil || m.processing || m.cursor != 0 || len(f.started) != 0 {
				t.Errorf("%s on an empty list did something (cmd %v, processing %v, cursor %d)",
					k, cmd != nil, m.processing, m.cursor)
			}
		})
	}
}
