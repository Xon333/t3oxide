package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestRefinementLivePlainThenAuthoritativeRichThroughModel(t *testing.T) {
	for _, theme := range []string{"violet", "terminal"} {
		for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {170, 40}} {
			t.Run(fmt.Sprint(theme, size), func(t *testing.T) {
				m := testModel(t)
				m.theme = theme
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "source/thread"}, Items: []wire.Row{{Key: "source/thread/row", Kind: "assistant_message", Text: "## Result\n\n**live** 界 👩🏽‍💻\n\n```go\nvar n = 1\n```\x1b]52;c;c2VjcmV0\a", Streaming: true}}}
				m.composer.SetValue("exact\tinput\r\nwith\nnewlines")
				draft := m.currentDraft()
				finishCommand(t, m, m.requestRender(false, false))
				plain := ansi.Strip(m.transcript)
				if !strings.Contains(plain, "**live**") || !strings.Contains(plain, "## Result") || strings.Contains(m.transcript, "\x1b]52") {
					t.Fatalf("live body was not sanitized plain text: %q", plain)
				}
				if m.renderer.counters.rich != 0 || m.renderer.counters.plain != 1 {
					t.Fatal("stream invoked rich conversion")
				}
				m.snapshot.Items[0].Streaming = false
				finishCommand(t, m, m.requestRender(false, false))
				plain = ansi.Strip(m.transcript)
				if strings.Contains(plain, "**live**") || strings.Contains(plain, "```go") || strings.Contains(plain, "streaming") || !strings.Contains(plain, "live") {
					t.Fatalf("authoritative completion was not rich: %q", plain)
				}
				if m.renderer.counters.rich != 1 || m.currentDraft() != draft {
					t.Fatal("rich completion count or exact draft changed")
				}
				m.snapshot.Items[0].Text = "replacement"
				finishCommand(t, m, m.requestRender(false, false))
				if strings.Contains(ansi.Strip(m.transcript), "live") || !strings.Contains(m.transcript, "replacement") {
					t.Fatal("replacement kept an old suffix")
				}
			})
		}
	}
}

func TestRefinementBodyDecorationAndEffectiveGeometryHaveSeparateInvalidation(t *testing.T) {
	var r transcriptRenderer
	s := &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread"}, Items: []wire.Row{{Key: "row", SourceThreadID: "thread", Kind: "assistant_message", Text: "A **formatted** body"}}}
	req := renderRequest{snapshot: s, width: 112, theme: "violet"}
	first, err := r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	initial := r.counters
	req.width = 160
	s.Metadata.Status = "failed"
	s.Revision++
	second, err := r.render(context.Background(), req)
	if err != nil || first != second || r.counters != initial {
		t.Fatal("equivalent geometry/control revision rebuilt content")
	}
	s.Items[0].AttachmentCount = 3
	s.Items[0].Truncated = true
	relation := "subagent"
	s.Metadata.Lineage = &wire.Lineage{RelationshipToParent: &relation}
	third, err := r.render(context.Background(), req)
	if err != nil || r.counters.body != initial.body || r.counters.assembly != initial.assembly+1 || !strings.Contains(ansi.Strip(third), "Subagent") || !strings.Contains(ansi.Strip(third), "3 attachment(s)") || !strings.Contains(ansi.Strip(third), "Message shortened") {
		t.Fatal("decoration did not update independently of body")
	}
	req.width = 60
	if _, err := r.render(context.Background(), req); err != nil || r.counters.body != initial.body+1 {
		t.Fatal("true geometry change failed to reflow")
	}
	req.theme = "terminal"
	if _, err := r.render(context.Background(), req); err != nil || r.counters.body != initial.body+2 {
		t.Fatal("theme change failed to restyle")
	}
}

func TestRefinementControlOnlySnapshotSkipsBodyInstallAndSearchThroughUpdate(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditReadingPage()
	m.searchTerm = "enough"
	finishCommand(t, m, m.requestRender(false, false))
	body, assembly, installs, scans := m.renderer.counters.body, m.renderer.counters.assembly, m.contentInstalls, m.searchScans
	if len(m.searchLines) == 0 || scans != 0 {
		t.Fatal("worker did not prepare active search")
	}
	for i := range 50 {
		next := auditReadingPage()
		next.Metadata.Status = "failed"
		next.Revision = int64(i + 1)
		// Update applies every current status/request immediately. Its returned
		// subscription waits intentionally remain under the program harness.
		m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: uint64(i + 1), Snapshot: next, Status: client.Status{State: "failed", Requests: i + 1}}})
		if m.pendingRender != nil || m.rendering || m.status.Requests != i+1 {
			t.Fatal("control update entered body lane or lost pending state")
		}
	}
	if m.renderer.counters.body != body || m.renderer.counters.assembly != assembly || m.contentInstalls != installs || m.searchScans != scans {
		t.Fatal("control-only snapshot performed content work")
	}
}

func TestRefinementPreparedFrameUsesCurrentSearchAndViewportState(t *testing.T) {
	m := testModel(t)
	m.snapshot = auditReadingPage()
	m.searchTerm = "item 03"
	frame := m.requestRender(false, false)().(renderedMsg)
	if frame.prepared == nil || len(frame.prepared.matches) == 0 {
		t.Fatal("worker did not prepare viewport and search")
	}
	m.searchTerm = "item 07"
	m.mouse = false
	m.viewport.MouseWheelEnabled = false
	m.Update(frame)
	if m.viewport.MouseWheelEnabled || m.transcriptSearch.term != "item 07" || len(m.searchLines) == 0 || m.searchScans != 1 {
		t.Fatal("stale worker search or mouse state replaced current UI")
	}
	// A control-only response must not reset the active search marker/anchor.
	m.viewport.SetYOffset(m.searchLines[0])
	offset := m.viewport.YOffset()
	if cmd := m.requestRender(false, false); cmd != nil || m.viewport.YOffset() != offset {
		t.Fatal("unchanged content reinstalled")
	}
}

func TestRefinementFinalRichReflowFollowsExactSourceAnchor(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	source := strings.Repeat("## Section\n\nA **formatted** paragraph with enough words to wrap.\n\n", 18) + "## Anchorword\n\nThe exact reading target.\n\n" + strings.Repeat("Following paragraph with text.\n\n", 30)
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread"}, Items: []wire.Row{{Key: "thread/row", Kind: "assistant_message", Text: source, Streaming: true}}}
	finishCommand(t, m, m.requestRender(false, false))
	line := -1
	for i, text := range strings.Split(ansi.Strip(m.transcript), "\n") {
		if strings.Contains(text, "Anchorword") {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("missing live target")
	}
	m.follow = false
	m.viewport.SetYOffset(line)
	m.snapshot.Items[0].Streaming = false
	finishCommand(t, m, m.requestRender(false, false))
	visible := ansi.Strip(m.viewport.View())
	if !strings.Contains(visible, "Anchorword") || m.follow || m.viewport.AtBottom() {
		t.Fatalf("completion lost source reading target: %q", visible)
	}
	if m.readingAnchor.key != "thread/row" || m.readingAnchor.plain {
		t.Fatal("source anchor did not transition with authoritative completion")
	}
}

func TestRefinementIntrinsicOverflowIsBoundedAndPrefixBudgetRejectionIsRetryable(t *testing.T) {
	var r transcriptRenderer
	text := strings.Repeat("A **bold** sample with `inline` text and Unicode 界.\n\n", 430)
	s := &wire.Snapshot{Items: []wire.Row{{Key: "oversize", Kind: "assistant_message", Text: text}, {Key: "tail", Kind: "system_notice", Text: "one"}}}
	req := renderRequest{snapshot: s, width: 112, theme: "violet"}
	out, err := r.render(context.Background(), req)
	if err != nil || !strings.Contains(ansi.Strip(out), "Display limit") || !r.cache["oversize"].overflow {
		t.Fatalf("intrinsic fixture did not overflow: bytes=%d, err=%v", len(out), err)
	}
	before := r.counters.body
	s.Items[1].Text = "two"
	if _, err := r.render(context.Background(), req); err != nil || r.counters.body != before {
		t.Fatal("intrinsic overflow repeated conversion")
	}
	if c := r.cache["oversize"]; c.body != "" || c.rendered != "" || len(r.cache) > wire.MaxRows {
		t.Fatal("negative cache retained oversized output")
	}
	// Three independently admissible rich rows exceed the page budget together.
	short := strings.Repeat("A **bold** sample with `inline` text and Unicode 界.\n\n", 145)
	s.Items = []wire.Row{{Key: "a", Kind: "assistant_message", Text: short}, {Key: "b", Kind: "assistant_message", Text: short}, {Key: "c", Kind: "assistant_message", Text: short}}
	out, err = r.render(context.Background(), req)
	if err != nil || !strings.Contains(ansi.Strip(out), "Display limit") {
		t.Fatal("prefix-budget fixture did not reach cap")
	}
	if c, ok := r.cache["c"]; ok && c.overflow {
		t.Fatal("remaining budget rejection became an intrinsic sentinel")
	}
	s.Items = s.Items[2:]
	out, err = r.render(context.Background(), req)
	if err != nil || strings.Contains(ansi.Strip(out), "Display limit") || len(r.rows) != 1 || r.rows[0].key != "c" {
		t.Fatal("prefix removal did not make the rejected row eligible")
	}
}

func TestRefinementIdenticalAuthoritativePageResetsReadingWithoutContentWork(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.snapshot = auditReadingPage()
	m.searchTerm = "item"
	finishCommand(t, m, m.requestRender(true, true))
	m.viewport.SetYOffset(4)
	m.follow = false
	body, assembly, installs, searches := m.renderer.counters.body, m.renderer.counters.assembly, m.contentInstalls, m.searchScans
	oldPage := m.renderPage
	// A new historical projection may contain exact same visible rows. Page
	// ownership and explicit reading intent must change without rebuilding it.
	copy := *m.snapshot
	copy.Metadata.Live, copy.Metadata.Historical, copy.Metadata.State = false, true, "historical"
	copy.Metadata.HistoryCursor = new("synthetic-history")
	m.snapshot = &copy
	if cmd := m.requestRender(true, false); cmd != nil || m.viewport.YOffset() != 0 || m.renderPage <= oldPage || m.renderReset {
		t.Fatal("identical historical page did not apply cheap explicit top/reset")
	}
	copy.Metadata.Live, copy.Metadata.Historical, copy.Metadata.State = true, false, "live"
	copy.Metadata.HistoryCursor = nil
	m.follow = true
	if cmd := m.requestRender(true, true); cmd != nil || !m.viewport.AtBottom() || m.renderReset {
		t.Fatal("identical Latest did not resume bottom following without worker")
	}
	if body != m.renderer.counters.body || assembly != m.renderer.counters.assembly || installs != m.contentInstalls || searches != m.searchScans {
		t.Fatal("identical authoritative page rerendered/reinstalled/rescanned content")
	}
}

func TestRefinementFinalLongTokenKeepsUnmappedReadingLine(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	source := strings.Repeat("unbroken", 800)
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread"}, Items: []wire.Row{{Key: "thread/long", Kind: "assistant_message", Text: source, Streaming: true}}}
	finishCommand(t, m, m.requestRender(false, false))
	m.follow = false
	m.viewport.SetYOffset(17)
	before := m.viewport.YOffset()
	m.snapshot.Items[0].Streaming = false
	finishCommand(t, m, m.requestRender(false, false))
	if m.viewport.YOffset() != before || m.follow {
		t.Fatalf("unmapped long token fabricated source0 and jumped: before=%d after=%d", before, m.viewport.YOffset())
	}
}
