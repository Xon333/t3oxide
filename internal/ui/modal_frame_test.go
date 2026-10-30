package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func TestRefinementModalCapturedGeometryRejectsChangedDescription(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	prompt, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Exact form", Prompt: "Short explanation", Initial: "exact input"})
	m.openPrompt(prompt)
	point := flowPoint(t, m, "Submit")
	old := m.dialog
	// Simulate a revised offered explanation between draw and input without
	// relying on Update's independent geometry invalidation guard.
	m.dialog.description = "A longer changed explanation that must move the visible controls onto a different row."
	m.Update(point)
	if m.dialog != old {
		t.Fatal("changed offer reused the old captured control")
	}
	assertIntuitiveNoReply(t, reply)
}

func TestRefinementModalUnencodableAuthorityHasNoClickableControls(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	prompt, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Exact question", RequestQuestions: []client.RequestQuestion{{ID: "answer", Question: "Exact answer", Required: true, InitialAnswer: "exact"}}})
	m.openPrompt(prompt)
	// Wire decoding cannot produce this value, but failure of a future
	// extension's fingerprint must fail closed rather than compare two errors.
	m.dialog.requestForm.answers["unknown"] = func() {}
	point := flowPoint(t, m, "Submit answers")
	if len(m.modalHitZones()) != 0 {
		t.Fatal("invalid authority retained a usable hit table")
	}
	old := m.dialog
	m.Update(point)
	if m.dialog != old {
		t.Fatal("unencodable offer dispatched from cached geometry")
	}
	assertIntuitiveNoReply(t, reply)
}
