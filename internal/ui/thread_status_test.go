package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestThreadStatusRenderersDistinguishNativeStates(t *testing.T) {
	for _, tc := range []struct {
		name, status, label, role string
		inbox                     *wire.InboxMetadata
	}{
		{"active", "running", "> Active", "operation", nil},
		{"preparing", "preparing", "> Active", "operation", nil},
		{"starting", "starting", "> Active", "operation", nil},
		{"request", "running", "? Waiting", "warning", &wire.InboxMetadata{PendingRequestCount: 1}},
		{"waiting", "waiting", "? Waiting", "warning", nil},
		{"failed with request", "failed", "! Failed", "error", &wire.InboxMetadata{PendingRequestCount: 1}},
		{"error", "error", "! Failed", "error", nil},
		{"blocked", "blocked", "# Blocked", "warning", nil},
		{"completed", "completed", "+ Completed", "operation", nil},
		{"reviewed unreported", "unreported", "? Unknown", "plain", &wire.InboxMetadata{Settled: true}},
		{"reviewed idle", "idle", "- Idle", "plain", &wire.InboxMetadata{Settled: true}},
		{"missing", "", "? Unknown", "plain", nil},
		{"future state", "future-state", "? Unknown", "plain", nil},
		{"queued", "queued", "= Queued", "operation", nil},
		{"queue metadata", "idle", "= Queued", "operation", &wire.InboxMetadata{QueuedCount: 2}},
		{"completed with queued followup", "completed", "= Queued", "operation", &wire.InboxMetadata{QueuedCount: 1}},
		{"active with queued followup", "running", "> Active", "operation", &wire.InboxMetadata{QueuedCount: 2}},
		{"cancelled", "cancelled", "x Stopped", "warning", nil},
		{"interrupted", "interrupted", "x Stopped", "warning", nil},
	} {
		for _, theme := range []string{"violet", "terminal"} {
			for _, selected := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/selected=%t", tc.name, theme, selected), func(t *testing.T) {
					thread := client.Thread{ID: "thread", Title: "Investigate export rendering", Status: tc.status, Inbox: tc.inbox}
					target := client.InboxTarget{ProfileID: "host", EnvironmentID: "env", ProjectID: "project", ThreadID: "thread"}
					m := testModel(t)
					m.theme = theme
					m.agentbox.rows = []client.InboxThread{{Target: target, Environment: "Host", Thread: thread}}
					if selected {
						m.agentbox.selected = target.Key()
					}
					for _, width := range []int{22, 26, 34} {
						d := navigatorDelegate{theme: theme, focused: selected}
						item := navigatorItem{profile: "host", label: "Host", thread: thread}
						l := list.New([]list.Item{item}, d, width, 3)
						var nav bytes.Buffer
						d.Render(&nav, l, 0, item)
						for _, output := range []string{nav.String(), strings.Split(m.inboxListView(width, 4), "\n")[0]} {
							plain := ansi.Strip(output)
							if !strings.HasSuffix(plain, tc.label) || !strings.Contains(plain, projectBubble(thread.Project, "unknown")) || !strings.Contains(plain, "I") || ansi.StringWidth(output) != width {
								t.Fatalf("width %d lost badge/title/identity or bounds: %q", width, plain)
							}
							if selected && !strings.HasPrefix(plain, ">") {
								t.Fatalf("selection missing: %q", plain)
							}
							p := colors(theme)
							want := map[string]string{"operation": p.operation, "warning": p.warning, "error": p.error, "plain": p.fg}[tc.role]
							assertSemanticSlotText(t, output, tc.label, want)
							for _, cell := range semanticSlotCells(t, output, tc.label) {
								if cell.Style.Attrs&uv.AttrBold == 0 {
									t.Fatal("badge lost bold emphasis")
								}
								if selected && theme == "terminal" && cell.Style.Attrs&uv.AttrReverse == 0 {
									t.Fatal("badge lost terminal row selection")
								}
							}
							if theme == "terminal" {
								assertTerminalPalette(t, output)
							}
						}
					}
				})
			}
		}
	}
}

func TestThreadStatusAgentBoxRealViewBoundsAndLiveDecision(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.agentbox.rows = m.agentbox.page.Rows
	m.agentbox.rows[0].Thread.Title = "Mouse launch"
	m.theme = "terminal"
	m.status.RunState = "failed"
	m.status.Requests = 1
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {170, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		out := m.View().Content
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "! Failed") || !strings.Contains(plain, "1 request") || !strings.Contains(plain, "+ Completed") || !strings.Contains(plain, "> Active") {
			t.Fatalf("%v lost live failure/request or list distinctions: %q", size, plain)
		}
		geometry := m.inboxGeometry()
		if list := ansi.Strip(m.inboxListView(geometry.left, geometry.body)); !strings.Contains(list, "Mouse launch") {
			t.Fatalf("%v badge displaced the ordinary thread title: %s", size, list)
		}
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height overflow at %v", size)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("width overflow at %v", size)
			}
		}
		assertTerminalPalette(t, out)
	}
}
