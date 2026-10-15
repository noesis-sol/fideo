package fideo

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestProcessFileInvalidIndex checks an index outside the file list starts no
// worker: the returned command yields an errorMsg for that index, the cancel
// func is called so its context is released, and nothing goes through send.
func TestProcessFileInvalidIndex(t *testing.T) {
	for _, idx := range []int{-1, 1, 5} {
		m := newModel([]videoFile{{path: "clip.mp4", name: "clip.mp4"}})
		var sent []tea.Msg
		m.videoService.send = func(msg tea.Msg) { sent = append(sent, msg) }

		ctx, cancel := context.WithCancel(context.Background())
		cmd := m.processFile(idx, ctx, cancel, 1)
		if cmd == nil {
			t.Fatalf("idx %d: processFile returned no command", idx)
		}
		em, ok := cmd().(errorMsg)
		if !ok {
			t.Fatalf("idx %d: command returned %T, want errorMsg", idx, cmd())
		}
		if em.idx != idx || em.err == nil {
			t.Errorf("idx %d: errorMsg = %+v, want idx %d with an error", idx, em, idx)
		}
		if ctx.Err() == nil {
			t.Errorf("idx %d: cancel was not called", idx)
		}
		if !m.videoService.waitWorkers(time.Second) {
			t.Errorf("idx %d: a worker was started", idx)
		}
		if len(sent) != 0 {
			t.Errorf("idx %d: sent %v, want nothing", idx, sent)
		}
	}
}
