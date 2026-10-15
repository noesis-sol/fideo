package fideo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctxLauncher stands in for processFile through model.launch, recording which
// files were started and the context each was given, so a test can tell
// whether a worker's encode was cancelled.
type ctxLauncher struct {
	started []int
	ctxs    map[int]context.Context
}

func (l *ctxLauncher) launch(_ model, idx int, ctx context.Context, _ context.CancelFunc, _ int) tea.Cmd {
	l.started = append(l.started, idx)
	l.ctxs[idx] = ctx
	return func() tea.Msg { return nil }
}

// promptModel is a fresh picker over the named files in a temp directory, all
// selected, with maxConcurrent slots. Each file in clobbered already has its
// out_ output on disk, so starting it raises the overwrite prompt.
func promptModel(t *testing.T, maxConcurrent int, names []string, clobbered ...int) (model, *ctxLauncher) {
	t.Helper()
	dir := t.TempDir()
	files := make([]videoFile, len(names))
	for i, n := range names {
		files[i] = videoFile{path: filepath.Join(dir, n), name: n, selected: true}
	}
	for _, i := range clobbered {
		if err := os.WriteFile(getOutputPath(files[i].path, ""), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(files)
	m.config.maxConcurrent = maxConcurrent
	l := &ctxLauncher{ctxs: make(map[int]context.Context)}
	m.launch = l.launch
	return m, l
}

// raisePrompt starts the batch with enter and fails unless it stopped on the
// overwrite prompt.
func raisePrompt(t *testing.T, m model) (model, tea.Cmd) {
	t.Helper()
	m, cmd := m.dispatch(key("enter"))
	if !m.showOverwritePrompt {
		t.Fatal("an existing out_ file should raise the overwrite prompt")
	}
	return m, cmd
}

// choose moves the prompt's cursor to option (0 Overwrite, 1 Skip, 2 Cancel
// all), presses enter, and feeds back the message the prompt's cmd emits.
func choose(t *testing.T, m model, option int) (model, tea.Cmd) {
	t.Helper()
	m.overwriteCursor = option
	m, cmd := m.dispatch(key("enter"))
	if cmd == nil {
		return m, nil
	}
	return m.dispatch(cmd())
}

func TestExistingOutputRaisesPrompt(t *testing.T) {
	m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4"}, 0)

	m, cmd := raisePrompt(t, m)

	if want := getOutputPath(m.files[0].path, ""); m.pendingOutputFile != want {
		t.Errorf("pendingOutputFile = %q, want %q", m.pendingOutputFile, want)
	}
	if m.currentIdx != 0 || m.overwriteCursor != 0 {
		t.Errorf("currentIdx %d overwriteCursor %d; want 0, 0 (Overwrite)", m.currentIdx, m.overwriteCursor)
	}
	if cmd != nil || len(l.started) != 0 || len(m.cancels) != 0 {
		t.Errorf("the modal prompt must start nothing, not even b; cmd %v launched %v", cmd != nil, l.started)
	}
}

func TestPromptCursorMovesAndClamps(t *testing.T) {
	tests := []struct {
		name string
		from int
		keys []string
		want int
	}{
		{"down moves to Skip", 0, []string{"down"}, 1},
		{"j moves down", 0, []string{"j", "j"}, 2},
		{"down clamps at Cancel all", 2, []string{"down", "j"}, 2},
		{"up moves to Skip", 2, []string{"up"}, 1},
		{"k moves up", 2, []string{"k", "k"}, 0},
		{"up clamps at Overwrite", 0, []string{"up", "k"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, l := promptModel(t, 1, []string{"a.mp4"}, 0)
			m, _ = raisePrompt(t, m)
			m.overwriteCursor = tt.from

			for _, k := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.dispatch(key(k))
				if cmd != nil {
					t.Errorf("%q returned a cmd", k)
				}
			}

			if m.overwriteCursor != tt.want {
				t.Errorf("overwriteCursor = %d, want %d", m.overwriteCursor, tt.want)
			}
			if !m.showOverwritePrompt || len(l.started) != 0 {
				t.Error("moving the cursor must neither dismiss the prompt nor start a file")
			}
		})
	}
}

func TestOverwriteStartsConfirmedFile(t *testing.T) {
	m, l := promptModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)
	m, _ = raisePrompt(t, m)

	m, cmd := m.dispatch(key("enter"))
	if cmd == nil {
		t.Fatal("enter on Overwrite should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(overwriteConfirmMsg); !ok {
		t.Fatalf("enter on Overwrite emitted %T, want overwriteConfirmMsg", msg)
	}
	if len(l.started) != 0 {
		t.Fatal("the file starts on overwriteConfirmMsg, not on the key")
	}

	m, cmd = m.dispatch(msg)

	if m.showOverwritePrompt {
		t.Error("prompt should be dismissed")
	}
	if !m.processing {
		t.Error("batch should be processing once the confirmed file starts")
	}
	if len(l.started) != 1 || l.started[0] != 0 {
		t.Fatalf("launched %v, want [0]: the one slot goes to the confirmed file, not b", l.started)
	}
	if _, ok := m.cancels[0]; !ok || cmd == nil {
		t.Error("the confirmed file should be registered in flight and its launch cmd returned")
	}
	if m.files[1].status != statusPending || !m.files[1].selected {
		t.Error("b should stay queued for the next free slot")
	}
}

func TestOverwriteFillsRemainingSlots(t *testing.T) {
	m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4", "c.mp4"}, 0)
	m, _ = raisePrompt(t, m)

	m, _ = choose(t, m, 0)

	if len(l.started) != 2 || l.started[0] != 0 || l.started[1] != 1 {
		t.Errorf("launched %v, want [0 1]: the confirmed file, then b in the second slot", l.started)
	}
	if !m.files[2].selected || m.files[2].status != statusPending {
		t.Error("c should wait for a slot")
	}
}

func TestSkipDeselectsAndStartsNext(t *testing.T) {
	m, l := promptModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)
	m, _ = raisePrompt(t, m)

	m.overwriteCursor = 1
	m, cmd := m.dispatch(key("enter"))
	if cmd == nil {
		t.Fatal("enter on Skip should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(overwriteSkipMsg); !ok {
		t.Fatalf("enter on Skip emitted %T, want overwriteSkipMsg", msg)
	}

	m, cmd = m.dispatch(msg)

	a := m.files[0]
	if a.selected || a.outPath != "" {
		t.Errorf("skipped file should be deselected with its claim released; selected=%v outPath=%q", a.selected, a.outPath)
	}
	if m.totalToProcess != 1 {
		t.Errorf("totalToProcess = %d, want 1", m.totalToProcess)
	}
	if m.showOverwritePrompt {
		t.Error("prompt should be dismissed")
	}
	if len(l.started) != 1 || l.started[0] != 1 || cmd == nil {
		t.Fatalf("launched %v, want [1]: the skip frees the slot for b", l.started)
	}
	if !m.processing || m.done {
		t.Errorf("batch settled while b is in flight: processing %v done %v", m.processing, m.done)
	}
	if _, err := os.Stat(getOutputPath(a.path, "")); err != nil {
		t.Errorf("skipping must leave the existing output alone: %v", err)
	}
}

func TestSkipLastFileSettlesBatch(t *testing.T) {
	m, l := promptModel(t, 1, []string{"a.mp4"}, 0)
	m, _ = raisePrompt(t, m)

	m, cmd := choose(t, m, 1)

	if cmd != nil || len(l.started) != 0 {
		t.Errorf("nothing is left to start; cmd %v launched %v", cmd != nil, l.started)
	}
	if m.processing || !m.done {
		t.Errorf("skipping the last file should settle the batch: processing %v done %v", m.processing, m.done)
	}
	if m.totalToProcess != 0 {
		t.Errorf("totalToProcess = %d, want 0", m.totalToProcess)
	}
}

func TestSkipWaitsForInFlightSibling(t *testing.T) {
	m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4"}, 1)
	m, _ = raisePrompt(t, m)
	if len(l.started) != 1 || l.started[0] != 0 {
		t.Fatalf("launched %v, want [0] before the prompt for b", l.started)
	}

	m, _ = choose(t, m, 1)

	if !m.processing || m.done {
		t.Errorf("batch settled while a is still in flight: processing %v done %v", m.processing, m.done)
	}

	m, _ = m.dispatch(doneMsg{idx: 0})
	if m.processing || !m.done {
		t.Errorf("a's done should settle the batch: processing %v done %v", m.processing, m.done)
	}
}

func TestCancelAllDropsQueue(t *testing.T) {
	m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4", "c.mp4"}, 1)
	m, _ = raisePrompt(t, m)

	m, cmd := choose(t, m, 2)

	if cmd != nil {
		t.Error("Cancel all should not return a cmd")
	}
	if m.showOverwritePrompt || !m.userCancelled {
		t.Errorf("prompt shown %v userCancelled %v; want false, true", m.showOverwritePrompt, m.userCancelled)
	}
	if l.ctxs[0].Err() == nil {
		t.Error("the in-flight encode of a should be cancelled")
	}
	for _, i := range []int{1, 2} {
		if f := m.files[i]; f.selected || f.outPath != "" {
			t.Errorf("file %d should be dropped from the queue; selected=%v outPath=%q", i, f.selected, f.outPath)
		}
	}
	if m.totalToProcess != 1 {
		t.Errorf("totalToProcess = %d, want 1 (only a was started)", m.totalToProcess)
	}
	if !m.processing {
		t.Fatal("batch settled before a's worker reported its cancel")
	}

	m, _ = m.dispatch(cancelMsg{idx: 0})
	if m.processing || m.done {
		t.Errorf("a cancelled batch settles without being done: processing %v done %v", m.processing, m.done)
	}
	if len(l.started) != 1 {
		t.Errorf("launched %v, want only [0]: nothing new starts after Cancel all", l.started)
	}
}

func TestCtrlCOnPromptCancelsAndQuits(t *testing.T) {
	m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4"}, 1)
	m, _ = raisePrompt(t, m)

	m, cmd := m.dispatch(tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should return tea.Quit")
	}
	if !m.userCancelled {
		t.Error("ctrl+c should mark the batch cancelled")
	}
	if l.ctxs[0].Err() == nil {
		t.Error("the in-flight encode of a should be cancelled before quitting")
	}
}

func TestPromptSwallowsOtherKeys(t *testing.T) {
	keys := []tea.KeyMsg{
		key("q"), {Type: tea.KeyEscape}, key("c"), key(" "), key("a"), key("x"),
		key("g"), key("G"), key("pgdown"), key("end"),
	}
	for _, k := range keys {
		t.Run(k.String(), func(t *testing.T) {
			m, l := promptModel(t, 2, []string{"a.mp4", "b.mp4", "c.mp4"}, 1)
			m, _ = raisePrompt(t, m)
			m.overwriteCursor = 1
			m.cursor = 1

			m, cmd := m.dispatch(k)

			if cmd != nil {
				t.Error("key returned a cmd")
			}
			if !m.showOverwritePrompt || m.overwriteCursor != 1 || m.currentIdx != 1 {
				t.Errorf("prompt state changed: shown %v cursor %d currentIdx %d", m.showOverwritePrompt, m.overwriteCursor, m.currentIdx)
			}
			if m.cursor != 1 {
				t.Errorf("list cursor moved to %d beneath the prompt", m.cursor)
			}
			if m.userCancelled || l.ctxs[0].Err() != nil {
				t.Error("key cancelled the batch beneath the prompt")
			}
			for i, f := range m.files {
				if !f.selected {
					t.Errorf("file %d was deselected beneath the prompt", i)
				}
			}
			if len(l.started) != 1 {
				t.Errorf("launched %v, want only [0]", l.started)
			}
		})
	}
}
