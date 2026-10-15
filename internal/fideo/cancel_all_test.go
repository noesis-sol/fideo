package fideo

import (
	"context"
	"strings"
	"testing"
)

// cancelAllModel is a batch paused on the overwrite prompt for file 1, with
// file 2 still queued. inFlight lists files already launched by startFile.
func cancelAllModel(inFlight map[int]context.CancelFunc) model {
	return model{
		files: []videoFile{
			{path: "a.mp4", name: "a.mp4", selected: true},
			{path: "b.mp4", name: "b.mp4", selected: true, outPath: "out_b.mp4"},
			{path: "c.mp4", name: "c.mp4", selected: true},
		},
		cancels:             inFlight,
		config:              compressionConfig{maxConcurrent: 2},
		processing:          true,
		totalToProcess:      3,
		showOverwritePrompt: true,
		overwriteCursor:     2, // Cancel all
		currentIdx:          1,
	}
}

func footer(m model) string {
	var s strings.Builder
	m.renderFooter(&s)
	return s.String()
}

// TestCancelAllWithNothingInFlight: choosing "Cancel all" with no worker
// running returns straight to the picker, without claiming the batch is done.
func TestCancelAllWithNothingInFlight(t *testing.T) {
	m := cancelAllModel(map[int]context.CancelFunc{})

	m, _ = m.handleKeyPress(key("enter"))

	if m.showOverwritePrompt {
		t.Error("prompt should be dismissed")
	}
	if m.processing {
		t.Error("nothing is in flight, so the batch should be settled")
	}
	if m.done {
		t.Error("a cancelled batch must not be marked done")
	}
	if strings.Contains(footer(m), "All done") {
		t.Errorf("footer reports success for a cancelled batch: %q", footer(m))
	}
	for i, f := range m.files {
		if f.selected || f.outPath != "" {
			t.Errorf("file %d should be dropped from the batch; selected=%v outPath=%q", i, f.selected, f.outPath)
		}
	}
}

// TestCancelAllWaitsForLaunchingWorker: a worker launched by startFile but
// whose processingStartMsg hasn't landed (processingCount still 0) is alive.
// "Cancel all" must cancel it and keep the batch processing — so enter can't
// start a new batch beneath it — until its own cancelMsg settles the batch.
func TestCancelAllWaitsForLaunchingWorker(t *testing.T) {
	cancelled := 0
	m := cancelAllModel(map[int]context.CancelFunc{0: func() { cancelled++ }})

	m, _ = m.handleKeyPress(key("enter"))

	if cancelled != 1 {
		t.Fatalf("launching worker should be cancelled once; cancelled %d times", cancelled)
	}
	if !m.processing || m.batchSettled() {
		t.Fatal("batch settled while a launched worker is still registered")
	}
	if m.done {
		t.Error("a cancelled batch must not be marked done")
	}
	if !m.files[0].selected {
		t.Error("the in-flight file is cancelled by its worker, not dropped from the queue")
	}
	if m.files[1].selected || m.files[2].selected {
		t.Error("queued files should be dropped from the batch")
	}

	// enter is ignored while the worker winds down.
	if m2, cmd := m.handleKeyPress(key("enter")); cmd != nil || len(m2.cancels) != 1 {
		t.Fatal("enter started work while a worker is still alive")
	}

	// Its late processingStartMsg doesn't settle the batch either.
	m, _ = m.handleProcessingStart(processingStartMsg{idx: 0})
	if !m.processing {
		t.Fatal("batch settled before the worker's cancelMsg")
	}

	m, _ = m.handleCancel(cancelMsg{idx: 0})
	if m.processing {
		t.Error("batch should settle once the last worker reports its cancel")
	}
	if m.done || strings.Contains(footer(m), "All done") {
		t.Errorf("footer reports success for a cancelled batch: %q", footer(m))
	}
}
