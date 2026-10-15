package fideo

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeLauncher stands in for processFile through model.launch: it records
// which files the batch handlers started, and with what thread budget, without
// probing or encoding anything.
type fakeLauncher struct {
	started []int
	jobs    []int
}

func (f *fakeLauncher) launch(_ model, idx int, _ context.Context, _ context.CancelFunc, jobs int) tea.Cmd {
	f.started = append(f.started, idx)
	f.jobs = append(f.jobs, jobs)
	return func() tea.Msg { return nil }
}

// batchModel is a running batch over the named files in a temp directory (so no
// output path exists and nothing prompts), with maxConcurrent slots. inFlight
// files are registered in m.cancels and marked processing, as if their workers
// had reported processingStartMsg; the rest are selected and pending.
func batchModel(t *testing.T, maxConcurrent int, names []string, inFlight ...int) (model, *fakeLauncher) {
	t.Helper()
	dir := t.TempDir()
	files := make([]videoFile, len(names))
	for i, n := range names {
		files[i] = videoFile{path: filepath.Join(dir, n), name: n, selected: true}
	}
	m := newModel(files)
	m.config.maxConcurrent = maxConcurrent
	f := &fakeLauncher{}
	m.launch = f.launch
	m.processing = true
	m.totalToProcess = len(names)
	for _, i := range inFlight {
		m.cancels[i] = func() {}
		m.files[i].status = statusProcessing
		m.files[i].outPath = m.resolveOutputPath(i)
		m.processingCount++
	}
	return m, f
}

func TestDoneStartsNextPendingFile(t *testing.T) {
	m, f := batchModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)

	m, cmd := m.dispatch(doneMsg{idx: 0, note: "kept original"})

	a := m.files[0]
	if a.status != statusDone || a.progress != 1.0 || a.note != "kept original" {
		t.Errorf("done file: status %v progress %v note %q; want done, 1.0, \"kept original\"", a.status, a.progress, a.note)
	}
	if m.completedCount != 1 {
		t.Errorf("completedCount = %d, want 1", m.completedCount)
	}
	if m.processingCount != 0 {
		t.Errorf("processingCount = %d, want 0 (b has not reported processingStartMsg)", m.processingCount)
	}
	if _, ok := m.cancels[0]; ok {
		t.Error("done file's slot was not released")
	}
	if len(f.started) != 1 || f.started[0] != 1 {
		t.Fatalf("launched %v, want [1]: the freed slot should start the next pending file", f.started)
	}
	if f.jobs[0] != 1 {
		t.Errorf("last file's thread budget divisor = %d, want 1", f.jobs[0])
	}
	if _, ok := m.cancels[1]; !ok {
		t.Error("next file was launched without registering its cancel")
	}
	if cmd == nil {
		t.Error("starting the next file should return its launch cmd")
	}
	if !m.processing || m.done {
		t.Errorf("batch settled while b is in flight: processing %v done %v", m.processing, m.done)
	}
}

func TestLastDoneSettlesBatch(t *testing.T) {
	tests := []struct {
		name      string
		cancelled bool
		wantDone  bool
	}{
		{"finished batch is done", false, true},
		{"cancelled batch is not done", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, f := batchModel(t, 2, []string{"a.mp4"}, 0)
			m.userCancelled = tt.cancelled

			m, cmd := m.dispatch(doneMsg{idx: 0})

			if cmd != nil || len(f.started) != 0 {
				t.Errorf("nothing is left to start; cmd %v launched %v", cmd != nil, f.started)
			}
			if m.processing {
				t.Error("the last done should leave the processing state")
			}
			if m.done != tt.wantDone {
				t.Errorf("done = %v, want %v", m.done, tt.wantDone)
			}
		})
	}
}

func TestErrorSettlesBatchAsDone(t *testing.T) {
	m, _ := batchModel(t, 2, []string{"a.mp4", "b.mp4"}, 0)
	m.files[1].status = statusDone
	m.completedCount = 1
	boom := errors.New("ffmpeg exited 1")

	m, cmd := m.dispatch(errorMsg{idx: 0, err: boom})

	a := m.files[0]
	if a.status != statusError || a.err != boom || m.err != boom {
		t.Errorf("file status %v err %v, model err %v; want the error recorded on both", a.status, a.err, m.err)
	}
	if a.outPath != "" {
		t.Errorf("failed file still claims output %q", a.outPath)
	}
	if _, ok := m.cancels[0]; ok {
		t.Error("failed file's slot was not released")
	}
	if m.processingCount != 0 {
		t.Errorf("processingCount = %d, want 0", m.processingCount)
	}
	if cmd != nil {
		t.Error("nothing is left to start")
	}
	if m.processing || !m.done {
		t.Errorf("batch ending on an error: processing %v done %v; want false, true", m.processing, m.done)
	}
	if m.completedCount != 1 {
		t.Errorf("an error must not count as completed; completedCount = %d", m.completedCount)
	}
}

func TestErrorStartsNextPendingFile(t *testing.T) {
	m, f := batchModel(t, 1, []string{"a.mp4", "b.mp4"}, 0)

	m, cmd := m.dispatch(errorMsg{idx: 0, err: errors.New("probe failed")})

	if len(f.started) != 1 || f.started[0] != 1 || cmd == nil {
		t.Fatalf("launched %v (cmd %v), want [1]", f.started, cmd != nil)
	}
	if !m.processing || m.done {
		t.Errorf("batch settled while b is in flight: processing %v done %v", m.processing, m.done)
	}
}

// TestErrorForLaunchingFileKeepsProcessingCount: a worker that fails before its
// processingStartMsg (a probe failure) was never counted, so its error must not
// take a sibling's place in processingCount.
func TestErrorForLaunchingFileKeepsProcessingCount(t *testing.T) {
	m, _ := batchModel(t, 2, []string{"a.mp4", "b.mp4"}, 0)
	m.cancels[1] = func() {} // launched, still pending

	m, _ = m.dispatch(errorMsg{idx: 1, err: errors.New("probe failed")})

	if m.processingCount != 1 {
		t.Errorf("processingCount = %d, want 1 (a is still encoding)", m.processingCount)
	}
	if !m.processing {
		t.Error("batch settled while a is in flight")
	}
}

func TestCancelResetsFile(t *testing.T) {
	tests := []struct {
		name                string
		processing          bool // the cancelled file had reported processingStartMsg
		wantProcessingCount int
	}{
		{"processing file frees its count", true, 1},
		{"launching file leaves the count alone", false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// a keeps encoding; b is the file being cancelled.
			m, f := batchModel(t, 2, []string{"a.mp4", "b.mp4"}, 0)
			m.userCancelled = true
			m.cancels[1] = func() {}
			m.files[1].outPath = m.resolveOutputPath(1)
			m.files[1].progress = 0.4
			if tt.processing {
				m.files[1].status = statusProcessing
				m.processingCount++
			}

			m, cmd := m.dispatch(cancelMsg{idx: 1})

			b := m.files[1]
			if b.status != statusPending || b.progress != 0 || b.err != nil || b.outPath != "" {
				t.Errorf("cancelled file: status %v progress %v err %v outPath %q; want pending, 0, nil, \"\"",
					b.status, b.progress, b.err, b.outPath)
			}
			if _, ok := m.cancels[1]; ok {
				t.Error("cancelled file's slot was not released")
			}
			if m.processingCount != tt.wantProcessingCount {
				t.Errorf("processingCount = %d, want %d", m.processingCount, tt.wantProcessingCount)
			}
			if cmd != nil || len(f.started) != 0 {
				t.Errorf("a cancel must not start other work; launched %v", f.started)
			}
			if !m.processing {
				t.Error("batch settled while a is still in flight")
			}
		})
	}
}

// TestCancelSettlesOnLastWorker: with several workers winding down after 'c',
// the batch leaves the processing state only on the last one's cancelMsg, and
// is not reported done.
func TestCancelSettlesOnLastWorker(t *testing.T) {
	m, f := batchModel(t, 3, []string{"a.mp4", "b.mp4", "c.mp4"}, 0, 1)
	m.cancels[2] = func() {} // launched, still pending

	m, _ = m.handleKeyPress(key("c"))
	if !m.userCancelled {
		t.Fatal("'c' should mark the batch cancelled")
	}

	for _, idx := range []int{1, 2} {
		m, _ = m.dispatch(cancelMsg{idx: idx})
		if !m.processing {
			t.Fatalf("batch settled after file %d's cancelMsg with %d workers left", idx, len(m.cancels))
		}
	}
	if m.processingCount != 1 {
		t.Errorf("processingCount = %d, want 1 (only a is left)", m.processingCount)
	}

	m, _ = m.dispatch(cancelMsg{idx: 0})
	if m.processing || m.done {
		t.Errorf("after the last cancelMsg: processing %v done %v; want false, false", m.processing, m.done)
	}
	if m.processingCount != 0 || len(m.cancels) != 0 {
		t.Errorf("processingCount %d, %d cancels left; want 0, 0", m.processingCount, len(m.cancels))
	}
	for i, file := range m.files {
		if file.status != statusPending || !file.selected {
			t.Errorf("file %d: status %v selected %v; want pending and still selected", i, file.status, file.selected)
		}
	}
	if len(f.started) != 0 {
		t.Errorf("cancels started new work: %v", f.started)
	}
}

func TestInfoMessagesSetFields(t *testing.T) {
	m, _ := batchModel(t, 2, []string{"a.mp4", "b.mp4"}, 0, 1)

	m, _ = m.dispatch(progressMsg{idx: 1, progress: 0.42})
	m, _ = m.dispatch(videoInfoMsg{idx: 1, info: "1920x1080 h264"})
	m, _ = m.dispatch(outputInfoMsg{idx: 1, info: "out 12 MB"})

	b := m.files[1]
	if b.progress != 0.42 || b.info != "1920x1080 h264" || b.outInfo != "out 12 MB" {
		t.Errorf("file b: progress %v info %q outInfo %q", b.progress, b.info, b.outInfo)
	}
	if a := m.files[0]; a.progress != 0 || a.info != "" || a.outInfo != "" {
		t.Errorf("messages for b changed a: progress %v info %q outInfo %q", a.progress, a.info, a.outInfo)
	}
}

func TestSpinnerTick(t *testing.T) {
	m, _ := batchModel(t, 1, []string{"a.mp4"})
	tick := m.spinner.Tick()

	if _, cmd := m.dispatch(tick); cmd != nil {
		t.Error("a spinner tick with nothing processing should stop the tick loop")
	}

	m.processingCount = 1
	if _, cmd := m.dispatch(tick); cmd == nil {
		t.Error("a spinner tick while processing should schedule the next tick")
	}
}

func TestAutoStartStartsProcessing(t *testing.T) {
	m, f := batchModel(t, 2, []string{"a.mp4"})
	m.processing = false
	m.totalToProcess = 0

	m, cmd := m.dispatch(autoStartMsg{})

	if !m.processing || m.totalToProcess != 1 {
		t.Errorf("processing %v totalToProcess %d; want true, 1", m.processing, m.totalToProcess)
	}
	if len(f.started) != 1 || f.started[0] != 0 || cmd == nil {
		t.Fatalf("launched %v (cmd %v), want [0]", f.started, cmd != nil)
	}
	if _, ok := m.cancels[0]; !ok {
		t.Error("auto-started file has no registered cancel")
	}
	if m.files[0].outPath == "" {
		t.Error("auto-started file has no output path claimed")
	}
}
