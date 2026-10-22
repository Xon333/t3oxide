package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func TestCommandLifecycleStartsWithoutStaleFeedback(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("unsent human text")
	m.preservedDraft = "verbatim preserved draft"
	m.problem, m.notice = "previous failure", "Working · /latest"
	m.beginCommandNotice("model")
	if m.problem != "" || m.notice != "" {
		t.Fatalf("previous feedback obscures current busy state: %q / %q", m.problem, m.notice)
	}
	if m.composer.Value() != "unsent human text" || m.preservedDraft != "verbatim preserved draft" || m.stopBusy {
		t.Fatal("starting command feedback changed a draft or Stop state")
	}
	m.notice = "Ordinary action acknowledgment"
	m.beginCommandNotice("stop")
	if m.notice != "Ordinary action acknowledgment" {
		t.Fatal("Stop admission erased independent ordinary feedback")
	}
}

func TestCommandLifecycleEmptyAndFailedResultsRetireProgress(t *testing.T) {
	for _, tc := range []struct {
		name, notice, want string
		result             client.Result
		err                error
	}{
		{name: "latest", notice: "Working · /latest", want: "Latest navigation acknowledged"},
		{name: "earlier", notice: "Loading earlier history", want: "Earlier navigation acknowledged"},
		{name: "runtime", notice: "Working · /runtime", result: client.Result{DetailTitle: "Runtime", Detail: "Unreported"}},
		{name: "resolve", notice: "Working · /resolve"},
		{name: "requests", notice: "Reviewing T3 requests"},
		{name: "model", notice: "Working · /model", err: context.Canceled},
		{name: "latest", notice: "Working · /latest", err: errors.New("disconnected")},
		{name: "send", notice: "Sending · waiting for T3 acknowledgment", err: fmt.Errorf("detached: %w", context.Canceled)},
		{name: "model", notice: "An action is pending; Stop remains available"},
		{name: "send", notice: "Awaiting the current action; your draft is preserved", err: errors.New("uncertain acknowledgment")},
		{name: "stop", notice: "Stop is already awaiting acknowledgment", err: errors.New("uncertain Stop")},
	} {
		t.Run(tc.name+"/"+tc.notice, func(t *testing.T) {
			m := testModel(t)
			m.notice = tc.notice
			m.finishCommandNotice(tc.name, tc.result, tc.err)
			if m.notice != tc.want {
				t.Fatalf("finished command feedback = %q, want %q", m.notice, tc.want)
			}
		})
	}
}

func TestCommandLifecyclePreservesNewerNoticesAndIndependentLanes(t *testing.T) {
	for _, tc := range []struct {
		name, notice string
		err          error
	}{
		{name: "latest", notice: "T3 awaits your input"},
		{name: "model", notice: "Dismissed locally · no answer sent to T3"},
		{name: "requests", notice: "This request was retired; no answer sent", err: context.Canceled},
		{name: "latest", notice: "Connection interrupted", err: errors.New("disconnected")},
		{name: "stop", notice: commandProgress("latest"), err: errors.New("Stop failed")},
		{name: "latest", notice: commandProgress("stop"), err: context.Canceled},
	} {
		t.Run(tc.name+"/"+tc.notice, func(t *testing.T) {
			m := testModel(t)
			m.notice = tc.notice
			m.finishCommandNotice(tc.name, client.Result{}, tc.err)
			if m.notice != tc.notice {
				t.Fatalf("completion erased a newer or independent notice: %q", m.notice)
			}
		})
	}
}

func TestCommandLifecycleUsesOnlySuccessfulExplicitResultText(t *testing.T) {
	m := testModel(t)
	m.notice = "Working · /send"
	result := client.Result{Text: "T3 acknowledged the message"}
	m.finishCommandNotice("send", result, errors.New("uncertain acknowledgment"))
	if m.notice != "" {
		t.Fatal("failed command claimed successful acknowledgment")
	}
	m.finishCommandNotice("send", result, nil)
	if m.notice != result.Text {
		t.Fatal("successful result lost its authoritative acknowledgment")
	}
}

func TestCommandLifecycleUpdateRejectsStaleCompletions(t *testing.T) {
	m := testModel(t)
	m.generation, m.actionID, m.actionBusy = 2, 4, true
	m.notice = "New request needs review"
	for _, msg := range []actionMsg{
		{generation: 1, id: 4, name: "latest"},
		{generation: 2, id: 3, name: "latest"},
	} {
		m.Update(msg)
		if !m.actionBusy || m.notice != "New request needs review" {
			t.Fatal("stale completion crossed the generation/action boundary")
		}
	}
	m.notice = "Working · /latest"
	m.Update(actionMsg{generation: 2, id: 4, name: "latest"})
	if m.actionBusy || m.notice != "Latest navigation acknowledged" {
		t.Fatalf("current completion left pending state: busy=%v, notice=%q", m.actionBusy, m.notice)
	}
}

func TestCommandLifecycleUpdateErrorAndStopRemainIndependent(t *testing.T) {
	m := testModel(t)
	m.generation, m.actionID, m.actionBusy, m.stopBusy = 2, 4, true, true
	m.notice = "Working · /latest"
	m.Update(actionMsg{generation: 2, stop: true, name: "stop", err: errors.New("Stop acknowledgment uncertain")})
	if !m.actionBusy || m.stopBusy || m.notice != "Working · /latest" {
		t.Fatal("Stop completion disturbed the ordinary action lane")
	}
	m.Update(actionMsg{generation: 2, id: 4, name: "latest", err: errors.New("navigation disconnected")})
	if m.actionBusy || m.notice != "" || m.problem != "navigation disconnected" {
		t.Fatalf("failed navigation left stale progress: busy=%v, notice=%q, error=%q", m.actionBusy, m.notice, m.problem)
	}
}

func TestCommandLifecycleF1OpensHelpWithoutChangingDraft(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("unsent text")
	m.handleKey(keyPress(tea.KeyF1))
	if m.detailTitle != "Keyboard and commands" || !strings.Contains(m.detail, "RECOVERY") || m.composer.Value() != "unsent text" {
		t.Fatal("F1 did not open complete help while preserving the draft")
	}
	m.handleKey(keyPress(tea.KeyEscape))
	if m.detail != "" || m.composer.Value() != "unsent text" {
		t.Fatal("Escape from help changed the existing dismiss/draft behavior")
	}
}
