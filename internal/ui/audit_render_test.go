package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func auditRenderSnapshot(version int) *wire.Snapshot {
	return &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", State: "live", Live: true}, Items: []wire.Row{{Key: "thread/tail", Kind: "assistant_message", Text: fmt.Sprintf("frame %d", version), Streaming: true}}}
}

// Gate a real render command's return, modelling arbitrary renderer latency
// without sleeps, production test hooks, or depending on Markdown speed.
func auditDelayedRender(t *testing.T, cmd tea.Cmd) (release func(), result <-chan tea.Msg) {
	t.Helper()
	ready, gate, output := make(chan struct{}), make(chan struct{}), make(chan tea.Msg, 1)
	go func() {
		frame := cmd()
		close(ready)
		<-gate
		output <- frame
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("render did not reach controlled completion gate")
	}
	return func() { close(gate) }, output
}

func TestAuditSlowRenderAdvancesDuringContinuouslySupersedingStream(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditRenderSnapshot(0)
	cmd := m.requestRender(false, false)
	var presented uint64
	for turn := range 12 {
		if cmd == nil {
			t.Fatal("render lane stopped while latest work remained")
		}
		release, result := auditDelayedRender(t, cmd)
		for update := range 5 {
			m.snapshot = auditRenderSnapshot(turn*5 + update + 1)
			if extra := m.requestRender(false, false); extra != nil {
				t.Fatal("superseding stream started a second active render")
			}
		}
		if m.pendingRender == nil || m.pendingRender.snapshot != m.snapshot {
			t.Fatal("pending slot did not keep only the latest authoritative page")
		}
		release()
		frame := (<-result).(renderedMsg)
		_, cmd = m.Update(frame)
		if m.transcript != frame.content || m.presentedSerial <= presented || cmd == nil {
			t.Fatal("completed render was starved by newer pending wire updates")
		}
		presented = m.presentedSerial
	}
	finishCommand(t, m, cmd)
	if !strings.Contains(ansi.Strip(m.transcript), "frame 60") || m.rendering || m.pendingRender != nil {
		t.Fatal("latest keyed replacement did not finish after the producer stopped")
	}
}

func TestAuditRenderBoundariesNeverPublishSupersededPage(t *testing.T) {
	for _, boundary := range []string{"attachment", "thread", "page-reset", "page-cursor", "width", "theme"} {
		t.Run(boundary, func(t *testing.T) {
			m := testModel(t)
			m.snapshot = auditRenderSnapshot(1)
			if boundary == "page-cursor" {
				cursor := "page-a"
				m.snapshot.Metadata = wire.Metadata{ThreadID: "thread", State: "historical", Historical: true, HistoryCursor: &cursor}
			}
			old := m.requestRender(false, false)().(renderedMsg)
			nextSnapshot := auditRenderSnapshot(2)
			switch boundary {
			case "attachment":
				m.setSession(nil)
			case "thread":
				nextSnapshot.Metadata.ThreadID = "other-thread"
			case "width":
				m.contentWidth = 60
			case "theme":
				m.theme = "lime"
			case "page-cursor":
				cursor := "page-b"
				nextSnapshot.Metadata = wire.Metadata{ThreadID: "thread", State: "historical", Historical: true, HistoryCursor: &cursor}
			}
			m.snapshot = nextSnapshot
			if cmd := m.requestRender(boundary == "page-reset", false); cmd != nil {
				t.Fatal("boundary started concurrent renderer access")
			}
			_, next := m.Update(old)
			if m.transcript != "" || next == nil {
				t.Fatal("obsolete frame published across a rendering boundary")
			}
			finishCommand(t, m, next)
			if !strings.Contains(ansi.Strip(m.transcript), "frame 2") {
				t.Fatal("current page failed to publish after obsolete work retired")
			}
		})
	}
}

func TestAuditResetScrollRetiresActivePageAndSurvivesCoalescing(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditRenderSnapshot(1)
	old := m.requestRender(false, false) // deliberately not run before replacement
	page := auditRenderSnapshot(2)
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 1, Snapshot: page, ResetScroll: true, Follow: false}})
	pageGeneration := m.renderPage
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 2, Snapshot: auditRenderSnapshot(3)}})
	retired := old().(renderedMsg)
	if retired.err == nil || m.renderPage != pageGeneration {
		t.Fatal("page replacement did not cancel obsolete work or stream changed page generation")
	}
	_, next := m.Update(retired)
	finishCommand(t, m, next)
	if !strings.Contains(ansi.Strip(m.transcript), "frame 3") || m.viewport.YOffset() != 0 || m.renderReset {
		t.Fatal("coalescing lost the explicit page's top/reset intent")
	}
}

func auditReadingPage() *wire.Snapshot {
	s := auditRenderSnapshot(0)
	s.Items = nil
	for i := range 12 {
		s.Items = append(s.Items, wire.Row{Key: fmt.Sprintf("thread/%02d", i), Kind: "assistant_message", Text: strings.Repeat(fmt.Sprintf("item %02d text with enough words for wrapping 界 café. ", i), 8)})
	}
	return s
}

func auditAssertAnchor(t *testing.T, m *Model, key string, offset int) {
	t.Helper()
	got := anchorAt(m.transcriptRows, m.viewport.YOffset())
	if got.key != key || got.offset != offset {
		t.Fatalf("reading position = %#v, want %q + %d at viewport line %d", got, key, offset, m.viewport.YOffset())
	}
}

func TestAuditReadingAnchorSurvivesReflowGrowthAndRollingWindow(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.snapshot = auditReadingPage()
	finishCommand(t, m, m.requestRender(false, false))
	m.follow = false
	key, offset := "thread/05", 3
	m.viewport.SetYOffset(m.transcriptRows[5].start + offset)
	for _, width := range []int{60, 120} {
		_, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		finishCommand(t, m, cmd)
		auditAssertAnchor(t, m, key, offset)
	}
	// Growth above the reader changes absolute lines but not the reading item.
	m.snapshot.Items[0].Text += strings.Repeat(" growing above the anchor ", 100)
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, key, offset)
	// The bounded latest window drops a prefix and adds a new authoritative row.
	m.snapshot.Items = append(m.snapshot.Items[3:], wire.Row{Key: "thread/12", Kind: "assistant_message", Text: "new latest row"})
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, key, offset)
	// Removing the anchor chooses the next equally near retained item at its top.
	m.snapshot.Items = append(m.snapshot.Items[:2], m.snapshot.Items[3:]...)
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, "thread/06", 0)
	// A subsequent reflow remains anchored to the chosen fallback.
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	finishCommand(t, m, cmd)
	auditAssertAnchor(t, m, "thread/06", 0)
}

func TestAuditReadingAnchorClampsAndRestoresWithinShrinkingItem(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditReadingPage()
	finishCommand(t, m, m.requestRender(false, false))
	m.follow = false
	row := m.transcriptRows[5]
	offset := row.lines - 1
	m.viewport.SetYOffset(row.start + offset)
	original := m.snapshot.Items[5].Text
	m.snapshot.Items[5].Text = "short"
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, row.key, m.transcriptRows[5].lines-1)
	m.snapshot.Items[5].Text = original
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, row.key, offset)
}

func TestAuditBottomFollowAndDisjointWindowFallback(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditReadingPage()
	finishCommand(t, m, m.requestRender(true, true))
	m.snapshot.Items[0].Text += strings.Repeat("growth ", 100)
	finishCommand(t, m, m.requestRender(false, false))
	if !m.follow || !m.viewport.AtBottom() {
		t.Fatal("stream growth lost bottom following")
	}
	m.follow = false
	m.viewport.SetYOffset(m.transcriptRows[4].start + 2)
	for i := range m.snapshot.Items {
		m.snapshot.Items[i].Key = fmt.Sprintf("replacement/%d", i)
	}
	finishCommand(t, m, m.requestRender(false, false))
	auditAssertAnchor(t, m, "replacement/0", 0)
}
