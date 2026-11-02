package ui

import (
	"context"
	"testing"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

func TestWorkingPinnedFinishesThenReadingHoldsNativeRemoval(t *testing.T) {
	m := refinementNavigation(t, 2)
	m.navigator.focused = false
	page := m.navigator.pages["source"]
	page.Page.Threads[0].Inbox.PinnedAt = "pin"
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	if m.navigator.ordered[0].shelf != "Working" {
		t.Fatal("pin displaced active work")
	}
	page.Page.Threads[0].Status = "completed"
	page.Page.Threads[0].Inbox = &wire.InboxMetadata{NativeRevision: "revision", PinnedAt: "pin", Working: new(false)}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	var finished navigatorItem
	for _, row := range m.navigator.ordered {
		if row.thread.ID == "row-00" {
			finished = row
		}
	}
	if finished.shelf != "Finished" {
		t.Fatalf("completed pin stayed in %s", finished.shelf)
	}
	m.navigator.reading = finished.identity()
	before := append([]navigatorItem(nil), m.navigator.ordered...)
	page.Page.Threads = page.Page.Threads[1:]
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	m.applyNavigatorOrder()
	if len(m.navigator.ordered) != len(before) || m.navigator.ordered[1].identity() != before[1].identity() {
		t.Fatal("reading moved cleared row")
	}
	m.navigator.reading = ""
	m.applyNavigatorOrder()
	if len(m.navigator.ordered) != 1 || m.navigator.ordered[0].thread.ID != "row-01" {
		t.Fatal("leaving reading retained cleared result")
	}
}

func TestOpeningBusyDoesNotAdmitReview(t *testing.T) {
	for _, browser := range []bool{false, true} {
		m := testModel(t)
		m.browserBusy, m.actionBusy = browser, !browser
		called := false
		cmd := m.navigatorRun("open", func(context.Context) (client.Result, error) { called = true; return client.Result{}, nil })
		if cmd != nil || called {
			t.Fatal("pending action admitted clear-on-open")
		}
	}
}

func TestFinishedInboxHoldsReviewedRowUntilAnotherSelection(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	held := m.agentbox.selected
	m.agentbox.reading = held
	for i := range m.agentbox.page.Rows {
		row := &m.agentbox.page.Rows[i]
		if row.Target.Key() == held {
			row.Thread.Inbox.Settled = true
		}
	}
	m.Update(inboxMsg{serial: m.agentbox.serial, page: m.agentbox.page, prefs: m.agentbox.prefs})
	if len(m.agentbox.rows) != 2 || m.agentbox.selected != held {
		t.Fatal("native review removed current reading row")
	}
	m.Update(keyPress('j'))
	m.Update(inboxMsg{serial: m.agentbox.serial, page: m.agentbox.page, prefs: m.agentbox.prefs})
	if m.agentbox.reading != "" || len(m.agentbox.rows) != 1 || m.agentbox.rows[0].Target.Key() == held {
		t.Fatal("moving selection retained cleared Finished item")
	}
}
