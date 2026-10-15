package fideo

import (
	"path/filepath"
	"testing"
)

// pickerModel is an idle picker over the named files in a temp directory, with
// a fake launcher so a key that starts the batch records it instead of encoding.
func pickerModel(t *testing.T, names ...string) (model, *fakeLauncher) {
	t.Helper()
	dir := t.TempDir()
	files := make([]videoFile, len(names))
	for i, n := range names {
		files[i] = videoFile{path: filepath.Join(dir, n), name: n}
	}
	m := newModel(files)
	f := &fakeLauncher{}
	m.launch = f.launch
	return m, f
}

func selection(m model) []bool {
	sel := make([]bool, len(m.files))
	for i, f := range m.files {
		sel[i] = f.selected
	}
	return sel
}

func TestSelectAllKey(t *testing.T) {
	tests := []struct {
		name   string
		status []fileStatus
		before []bool
		want   []bool
	}{
		{"selects every pending file", []fileStatus{statusPending, statusPending, statusPending}, []bool{false, true, false}, []bool{true, true, true}},
		{"deselects when all are selected", []fileStatus{statusPending, statusPending}, []bool{true, true}, []bool{false, false}},
		{"leaves finished files alone", []fileStatus{statusDone, statusPending, statusError}, []bool{false, false, true}, []bool{false, true, true}},
		{"ignores finished files when toggling off", []fileStatus{statusDone, statusPending}, []bool{false, true}, []bool{false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, f := pickerModel(t, "a.mp4", "b.mp4", "c.mp4")
			m.files = m.files[:len(tt.status)]
			for i := range m.files {
				m.files[i].status = tt.status[i]
				m.files[i].selected = tt.before[i]
				m.files[i].outPath = "stale"
			}

			m, cmd := m.dispatch(key("a"))

			if cmd != nil || m.processing || len(f.started) != 0 {
				t.Fatalf("a started the batch (cmd %v, processing %v, started %v); it should only select", cmd != nil, m.processing, f.started)
			}
			got := selection(m)
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("selection = %v, want %v", got, tt.want)
				}
				if m.files[i].status == statusPending && m.files[i].outPath != "" {
					t.Errorf("pending file %d kept a stale outPath %q", i, m.files[i].outPath)
				}
			}
		})
	}
}

func TestSelectAllThenEnterStartsBatch(t *testing.T) {
	m, f := pickerModel(t, "a.mp4", "b.mp4")
	m.config.maxConcurrent = 2

	m, _ = m.dispatch(key("a"))
	m, cmd := m.dispatch(key("enter"))

	if cmd == nil || !m.processing {
		t.Fatal("enter after a should start the batch")
	}
	if m.totalToProcess != 2 || len(f.started) != 2 {
		t.Errorf("batch of %d, started %v; want both files", m.totalToProcess, f.started)
	}
}

func TestSelectAllIgnoredWhileProcessing(t *testing.T) {
	m, _ := batchModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)
	m.files[1].selected = false

	m, cmd := m.dispatch(key("a"))

	if cmd != nil || m.files[1].selected {
		t.Error("a changed the selection while a batch was running")
	}
}
