package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func searchInboxFixture(t *testing.T, size [2]int) (*Model, map[string]client.NavigatorPage) {
	t.Helper()
	m, pages := membershipNavigator(t)
	m.mode = "agentbox"
	m.agentbox.view = "all"
	m.agentbox.loaded = true
	for _, profile := range []string{"alpha", "beta"} {
		p := pages[profile]
		m.agentbox.page = client.MergeInboxSource(m.agentbox.page, client.InboxSource{ProfileID: profile, EnvironmentID: p.EnvironmentID, Label: p.Label, NativeWorkflows: true, Page: p.Page})
	}
	m.filterInbox()
	m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	m.restoreFocus()
	return m, pages
}

func TestAcceptanceAgentBoxZeroResultServerSearchAndStaleQueryRetirement(t *testing.T) {
	for _, size := range flowSizes {
		for _, activation := range []string{"keyboard", "mouse"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], activation), func(t *testing.T) {
				m, pages := searchInboxFixture(t, size)
				draft, thread := m.currentDraft(), m.status.ThreadID
				m.Update(keyPress('/'))
				serial := m.agentbox.serial
				m.Update(tea.PasteMsg{Content: "Markdown"})
				view := ansi.Strip(m.View().Content)
				if !m.agentbox.searching || m.agentbox.loading || m.agentbox.serverSearch || len(m.agentbox.rows) != 0 || !strings.Contains(view, "Search server conversations") {
					t.Fatalf("local zero matches lack the visible explicit server escape:\n%s", view)
				}
				if m.agentbox.serial == serial {
					t.Fatal("edited local query retained old result/editor generation")
				}
				// Broader native search explicitly changes only the presentation
				// to All, so an active message match cannot be hidden by Needs you.
				m.agentbox.view = "needs"
				if activation == "mouse" {
					flowClick(t, m, "Search server conversations")
				} else {
					m.Update(keyPress(tea.KeyEnter))
				}
				if !m.agentbox.serverSearch || !m.agentbox.loading || m.agentbox.searching || m.agentbox.view != "all" || m.agentbox.readQuery != "Markdown" || len(m.agentbox.rows) != 0 || len(m.agentbox.page.Sources) != 0 || len(m.agentbox.pendingSources) != 2 {
					t.Fatal("explicit search did not capture a bounded new result scope", m.agentbox)
				}
				serial = m.agentbox.serial
				fresh := client.InboxSource{ProfileID: "alpha", EnvironmentID: "env-alpha", Label: "Alpha", NativeWorkflows: true, Page: pages["alpha"].Page}
				fresh.Page.SearchIncomplete, fresh.Page.SearchScope = true, "native recent message search"
				m.Update(inboxSourceMsg{serial: serial, source: fresh})
				m.View()
				row, ok := m.selectedInbox()
				if !ok || row.Target.ProfileID != "alpha" || row.Target.EnvironmentID != "env-alpha" || len(m.agentbox.rows) != 2 || !m.agentbox.loading || m.inboxSourceLoading("alpha") || !strings.Contains(m.inboxSourceHealth(), "1 incomplete") || !strings.Contains(m.inboxSourceHealth(), "1 loading") {
					t.Fatal("content-only matches were lost, or healthy source waited for its peer", row, m.inboxSourceHealth())
				}
				m.Update(keyPress('/'))
				m.Update(keyPress(tea.KeyLeft))
				if !m.agentbox.serverSearch || m.agentbox.serial != serial || len(m.agentbox.rows) != 2 {
					t.Fatal("cursor-only movement retired submitted search results")
				}
				m.Update(keyPress(tea.KeyEnd))
				m.Update(keyPress('u', tea.ModCtrl))
				m.Update(tea.PasteMsg{Content: "Task fork alpha"})
				if m.agentbox.serverSearch || m.agentbox.loading || len(m.agentbox.rows) != 1 || m.agentbox.query.Value() != "Task fork alpha" {
					t.Fatal("editing did not return to local loaded-result filtering")
				}
				late := client.InboxSource{ProfileID: "beta", EnvironmentID: "env-beta", Label: "Beta", NativeWorkflows: true, Page: pages["beta"].Page}
				m.Update(inboxSourceMsg{serial: serial, source: late})
				m.Update(inboxMsg{serial: serial, page: client.MergeInboxSource(client.InboxPage{}, late)})
				m.Update(componentMsg{generation: m.generation, kind: "agentbox-search", id: serial, message: tea.PasteMsg{Content: "retired"}})
				m.View()
				row, ok = m.selectedInbox()
				if !ok || row.Target.ProfileID != "alpha" || row.Thread.ID != "fork" || len(m.agentbox.rows) != 1 || m.agentbox.query.Value() != "Task fork alpha" || m.currentDraft() != draft || m.status.ThreadID != thread || m.actionBusy || m.stopBusy {
					t.Fatal("retired query changed source, input, attachment or operation authority")
				}
				if !strings.Contains(m.inboxSourceHealth(), "Loaded search results") || !strings.Contains(m.inboxSourceHealth(), "incomplete") || !strings.Contains(m.inboxSourceHealth(), "failed") {
					t.Fatal("canceled peer or partial loaded-search scope became complete success", m.inboxSourceHealth())
				}
			})
		}
	}
}

func TestAcceptanceAgentBoxSearchPasteRespectsFindAndRetiredGeometry(t *testing.T) {
	m, _ := searchInboxFixture(t, [2]int{120, 35})
	m.Update(keyPress('/'))
	m.Update(tea.PasteMsg{Content: "Markdown"})
	point := flowPoint(t, m, "Search server conversations")
	m.Update(keyPress('x'))
	m.Update(point)
	if m.agentbox.loading || m.agentbox.serverSearch {
		t.Fatal("old query's rendered search action admitted a new read")
	}
	query, draft := m.agentbox.query.Value(), m.currentDraft()
	m.searching = true
	m.restoreFocus()
	m.Update(tea.PasteMsg{Content: "exact Find text 界"})
	if m.searchInput.Value() != "exact Find text 界" || m.agentbox.query.Value() != query || m.currentDraft() != draft {
		t.Fatal("AgentBox query intercepted Find or changed the suspended draft")
	}
}

func TestAcceptanceAgentBoxSearchKeepsQueuedInputWhenAutomaticRequestArrives(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	m.mode, m.agentbox.searching = "agentbox", true
	m.composer.SetValue("exact suspended conversation draft")
	m.restoreFocus()
	m.Update(tea.PasteMsg{Content: "original query"})
	publishRequestOffers(m, requestOffer("R1", "v1"))
	advanceRequestClock(m, now, requestIdleDelay)
	prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
	m.Update(promptMsg{generation: m.generation, prompt: prompt})
	if m.dialog == nil || m.attention.returnFocus == nil || m.attention.returnFocus.inputKind != "agentbox-search" {
		t.Fatal("automatic request did not retain the exact AgentBox query owner")
	}
	m.Update(tea.PasteMsg{Content: " suffix"})
	assertIntuitiveNoReply(t, reply)
	if m.dialog != nil || m.attention.pending != prompt || !m.agentbox.searching || m.agentbox.query.Value() != "original query suffix" || m.currentDraft() != "exact suspended conversation draft" || m.agentbox.loading || m.agentbox.serverSearch {
		t.Fatal("queued query paste answered a request, read remotely or changed editor ownership")
	}
}
