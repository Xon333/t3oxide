package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

// The fake clock controls only presentation admission. These tests send actual
// production messages through Model.Update and inspect View plus live prompt
// lifetimes. No helper, native dispatch or physical keyboard is simulated by
// executing an admitted command; client/native fixture tests own those claims.
func requestAcceptanceModel(t *testing.T) (*Model, *time.Time) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "private-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "profile", EnvironmentID: "host", Label: "Fixture", Enabled: true}}, state)
	session, err := manager.NewSession("profile", "thread")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(context.Background(), session, nil)
	t.Cleanup(func() { _ = m.Close(); _ = manager.Close(); _ = state.Close() })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	m.attention.now = func() time.Time { return now }
	m.attention.lastInput = now
	return m, &now
}

func requestOffer(id, revision string) map[string]any {
	return map[string]any{"id": id, "revision": revision, "supported": true, "kind": "approval_request", "prompt": "Review exact permission " + id}
}

func publishRequestOffers(m *Model, offers ...map[string]any) {
	status := m.status
	status.ProfileID, status.EnvironmentID, status.ThreadID = "profile", "host", "thread"
	status.Connected, status.Writable, status.State, status.Requests = true, true, "live", len(offers)
	status.Location = client.Location{EnvironmentID: "host", ThreadID: "thread", Title: "Attached task", ProjectID: "project"}
	snapshot := &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", State: "live", Live: true, Requests: offers}}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: status, Snapshot: snapshot}})
}

func requestAcceptancePrompt(t *testing.T, id, revision string) (*client.Prompt, <-chan error) {
	t.Helper()
	prompt, reply, _ := intuitivePrompt(t, client.Question{EnvironmentID: "host", ThreadID: "thread", RequestID: id, RequestRevision: revision, Kind: client.ChooseOne, Title: "Exact native approval", Prompt: "Review the offered decision", ConfirmChoice: true, Choices: []client.Choice{{ID: "accept", Label: "Allow once"}, {ID: "decline", Label: "Decline"}}})
	return prompt, reply
}

func advanceRequestClock(m *Model, now *time.Time, duration time.Duration) {
	*now = now.Add(duration)
	m.Update(requestIdleMsg{generation: m.generation, token: m.attention.token, at: *now})
}

func finishRequestPresentation(m *Model) {
	m.Update(actionMsg{generation: m.generation, id: m.actionID, name: "requests"})
}

func TestAcceptanceRequestIdlePasteQueuedKeysAndStop(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, now := requestAcceptanceModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.Update(tea.PasteMsg{Content: "  exact human draft\nsecond line  "})
			m.composer.CursorStart()
			m.composer.SetCursorColumn(3)
			publishRequestOffers(m, requestOffer("R1", "v1"))
			if m.dialog != nil || m.actionBusy || !m.attention.timer {
				t.Fatal("request stole focused typing before idle")
			}
			if view := ansi.Strip(m.View().Content); !strings.Contains(strings.ToLower(view), "request") {
				t.Fatal("pending cue was not immediately visible", view)
			}
			advanceRequestClock(m, now, 1400*time.Millisecond)
			m.Update(keyPress('界'))
			advanceRequestClock(m, now, requestIdleDelay-time.Millisecond)
			if m.actionBusy || m.dialog != nil {
				t.Fatal("further typing did not restart the full idle interval")
			}
			advanceRequestClock(m, now, time.Millisecond)
			if !m.actionBusy || m.attention.opening != (requestIdentity{"host", "thread", "R1", "v1"}) {
				t.Fatal("exact eligible request did not start at 1.5 seconds")
			}
			prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
			m.Update(promptMsg{generation: m.generation, prompt: prompt})
			if m.dialog == nil || m.dialog.requestForm.focus != -1 || m.attention.armed {
				t.Fatal("automatic form acquired a permission control before deliberate focus")
			}
			m.Update(keyPress(tea.KeyEnter, tea.ModCtrl))
			if len(m.dialog.requestForm.answers) != 0 {
				t.Fatal("queued editor Submit selected a permission")
			}
			assertIntuitiveNoReply(t, reply)
			_, stop := m.Update(keyPress('s', tea.ModCtrl))
			if stop == nil || !m.stopBusy || !m.actionBusy {
				t.Fatal("Stop was blocked by the pending request lane")
			}
			m.Update(actionMsg{generation: m.generation, name: "stop", stop: true})
			before, column := m.currentDraft(), m.composer.Column()
			m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModShift, Text: "X"})
			if m.dialog != nil || m.attention.pending != prompt || m.currentDraft() == before || m.composer.Column() != column+1 || !m.composer.Focused() {
				t.Fatal("queued text was lost or assigned to the permission form")
			}
			m.Update(tea.PasteMsg{Content: " pasted 界 "})
			want, wantLine, wantColumn := m.currentDraft(), m.composer.Line(), m.composer.Column()
			assertIntuitiveNoReply(t, reply)
			advanceRequestClock(m, now, requestIdleDelay-time.Millisecond)
			if m.dialog != nil {
				t.Fatal("active paste did not defer the exact prompt")
			}
			advanceRequestClock(m, now, time.Millisecond)
			if m.dialog == nil || m.dialog.prompt != prompt {
				t.Fatal("deferred exact prompt was replaced or lost")
			}
			m.Update(keyPress(tea.KeyEscape))
			flowAwaitReply(t, reply)
			finishRequestPresentation(m)
			if m.dialog != nil || m.currentDraft() != want || m.composer.Line() != wantLine || m.composer.Column() != wantColumn || !m.composer.Focused() {
				t.Fatal("close failed to restore exact editor text/caret/focus")
			}
			if !m.attention.suppressed[requestIdentity{"host", "thread", "R1", "v1"}] {
				t.Fatal("close did not suppress exactly the shown revision")
			}
		})
	}
}

func TestAcceptanceRequestSuppressionNewOfferRevisionAndExplicitReopen(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	*now = now.Add(requestIdleDelay)
	publishRequestOffers(m, requestOffer("R1", "v1"))
	prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
	m.Update(promptMsg{generation: m.generation, prompt: prompt})
	m.Update(keyPress(tea.KeyEscape))
	flowAwaitReply(t, reply)
	finishRequestPresentation(m)
	advanceRequestClock(m, now, requestIdleDelay)
	for range 4 {
		publishRequestOffers(m, requestOffer("R1", "v1"))
	}
	if m.actionBusy || m.dialog != nil {
		t.Fatal("unchanged native snapshots reopened locally closed request")
	}
	m.Update(keyPress('r', tea.ModCtrl))
	if !m.actionBusy || m.attention.automatic {
		t.Fatal("explicit Requests could not reopen a suppressed revision")
	}
	opened, openedReply := requestAcceptancePrompt(t, "R1", "v1")
	m.Update(promptMsg{generation: m.generation, prompt: opened})
	if m.dialog == nil || m.dialog.requestForm.focus < 0 {
		t.Fatal("explicit reopen incorrectly waited for automatic idle")
	}
	m.Update(keyPress(tea.KeyEscape))
	flowAwaitReply(t, openedReply)
	finishRequestPresentation(m)
	advanceRequestClock(m, now, requestIdleDelay)
	publishRequestOffers(m, requestOffer("R1", "v1"), requestOffer("R2", "r2"))
	if m.attention.opening.id != "R2" {
		t.Fatal("suppressed sibling prevented a new offered request", m.attention.opening)
	}
	second, secondReply := requestAcceptancePrompt(t, "R2", "r2")
	m.Update(promptMsg{generation: m.generation, prompt: second})
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyEnter)) // stage only; nothing submitted
	oldClick := flowPoint(t, m, "Allow once")
	publishRequestOffers(m, requestOffer("R1", "v2"))
	flowAwaitReply(t, secondReply)
	if m.dialog != nil || m.attention.active.id != "" {
		t.Fatal("changed revision did not retire the staged old form")
	}
	finishRequestPresentation(m)
	advanceRequestClock(m, now, requestIdleDelay)
	if m.attention.opening != (requestIdentity{"host", "thread", "R1", "v2"}) {
		t.Fatal("new revision was hidden by old revision suppression")
	}
	third, thirdReply := requestAcceptancePrompt(t, "R1", "v2")
	m.Update(promptMsg{generation: m.generation, prompt: third})
	m.Update(oldClick) // old painted geometry has not been republished
	m.Update(keyPress(tea.KeyEnter, tea.ModCtrl))
	assertIntuitiveNoReply(t, thirdReply)
	if len(m.dialog.requestForm.answers) != 0 || m.attention.armed {
		t.Fatal("old geometry or Submit acquired the new request revision")
	}
}

func TestAcceptanceDeferredRequestRetiresOnDisconnectHistoricalAndAttachment(t *testing.T) {
	for _, reason := range []string{"disconnect", "historical", "attachment", "revision"} {
		t.Run(reason, func(t *testing.T) {
			m, now := requestAcceptanceModel(t)
			*now = now.Add(requestIdleDelay)
			publishRequestOffers(m, requestOffer("R1", "v1"))
			m.Update(keyPress('x')) // typing resumed while exact client read ran
			prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
			m.Update(promptMsg{generation: m.generation, prompt: prompt})
			if m.attention.pending != prompt || m.dialog != nil {
				t.Fatal("late prompt ignored the resumed typing interval")
			}
			oldGeneration, oldToken := m.generation, m.attention.token
			switch reason {
			case "disconnect":
				m.Update(sessionEndedMsg{generation: m.generation})
			case "historical":
				m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: m.status, Snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", State: "historical", Historical: true}}}})
			case "attachment":
				m.setSession(nil)
			case "revision":
				publishRequestOffers(m, requestOffer("R1", "v2"))
			}
			flowAwaitReply(t, reply)
			*now = now.Add(2 * requestIdleDelay)
			m.Update(requestIdleMsg{generation: oldGeneration, token: oldToken, at: *now})
			m.Update(promptMsg{generation: oldGeneration, prompt: prompt})
			if m.dialog != nil || m.attention.pending != nil || m.attention.visible.id != "" {
				t.Fatal("stale deferred form reopened after its authority retired")
			}
		})
	}
}

func TestAcceptanceExplicitRequestPromotesDeferredReadAndRestoresSearch(t *testing.T) {
	for _, mode := range []string{"developer", "agentbox"} {
		t.Run(mode, func(t *testing.T) {
			m, now := requestAcceptanceModel(t)
			m.mode = mode
			m.searching = true
			m.searchInput.SetValue("exact unsent find 界")
			m.searchInput.SetCursor(5)
			m.restoreFocus()
			*now = now.Add(requestIdleDelay)
			publishRequestOffers(m, requestOffer("R1", "v1"))
			m.Update(keyPress('X'))
			want, cursor := m.searchInput.Value(), m.searchInput.Position()
			prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
			m.Update(promptMsg{generation: m.generation, prompt: prompt})
			if m.attention.pending != prompt {
				t.Fatal("focused search was not deferred")
			}
			id := m.actionID
			m.Update(keyPress('r', tea.ModCtrl))
			if m.dialog == nil || m.dialog.prompt != prompt || m.actionID != id || m.attention.automatic {
				t.Fatalf("explicit Requests did not promote the existing exact read immediately: dialog=%v pending=%v action=%d/%d auto=%v context=%s keys=%v notice=%q", m.dialog != nil, m.attention.pending != nil, m.actionID, id, m.attention.automatic, m.shortcutContext(), m.actionBinding("requests").Keys(), m.notice)
			}
			m.Update(keyPress(tea.KeyEscape))
			flowAwaitReply(t, reply)
			finishRequestPresentation(m)
			if !m.searching || !m.searchInput.Focused() || m.searchInput.Value() != want || m.searchInput.Position() != cursor {
				t.Fatal("request close lost invoking search ownership/caret/text")
			}
		})
	}
}

func TestAcceptanceRequestFailureRetiresOpeningAndBoundedSuppression(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	*now = now.Add(requestIdleDelay)
	publishRequestOffers(m, requestOffer("R1", "v1"))
	m.Update(actionMsg{generation: m.generation, id: m.actionID, name: "requests", err: errors.New("exact request unavailable")})
	if m.actionBusy || m.attention.opening.id != "" || !m.attention.suppressed[requestIdentity{"host", "thread", "R1", "v1"}] {
		t.Fatal("failed client reader left opening stuck or retried automatically")
	}
	publishRequestOffers(m, requestOffer("R1", "v1"), requestOffer("R2", "v1"))
	if m.attention.opening.id != "R2" {
		t.Fatal("failed reader hid the next eligible request")
	}
	for i := 0; i < maxRequestSuppressions+7; i++ {
		m.suppressRequest(requestIdentity{"host", "thread", fmt.Sprintf("request-%d", i), "v1"})
	}
	if len(m.attention.suppressed) != maxRequestSuppressions || len(m.attention.order) != maxRequestSuppressions {
		t.Fatal("attachment-local identity suppression grew unbounded")
	}
	m.setSession(nil)
	if len(m.attention.suppressed) != 0 || m.attention.pending != nil || m.attention.active.id != "" {
		t.Fatal("attachment lifetime did not reset local request presentation")
	}
}

func TestAcceptanceRequestDefersOtherModalBlurAdmitsAndRestoresReadingAnchor(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	m.openLocal("Keep this menu", "request-acceptance-menu", []list.Item{choiceItem{title: "Local choice"}})
	menu := m.dialog
	publishRequestOffers(m, requestOffer("R1", "v1"))
	advanceRequestClock(m, now, 3*requestIdleDelay)
	if m.dialog != menu || m.actionBusy {
		t.Fatal("automatic request replaced an active unrelated modal")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != nil || m.actionBusy {
		t.Fatal("closing another modal did not restore focused editor idle gate")
	}
	m.viewport.SetContent(strings.Repeat("stable transcript row\n", 120))
	m.viewport.SetYOffset(21)
	m.follow = false
	m.readingAnchor = readingAnchor{key: "thread/stable-row", offset: 3}
	anchor, offset := m.readingAnchor, m.viewport.YOffset()
	m.Update(tea.BlurMsg{})
	if !m.actionBusy || m.attention.opening.id != "R1" {
		t.Fatal("editor blur did not admit an exact eligible request")
	}
	prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
	m.Update(promptMsg{generation: m.generation, prompt: prompt})
	m.Update(tea.FocusMsg{})
	m.Update(keyPress(tea.KeyEscape))
	flowAwaitReply(t, reply)
	finishRequestPresentation(m)
	if m.follow || m.readingAnchor != anchor || m.viewport.YOffset() != offset || !m.composer.Focused() {
		t.Fatalf("request close changed reading anchor/follow/source focus: follow=%v anchor=%+v offset=%d want=%d", m.follow, m.readingAnchor, m.viewport.YOffset(), offset)
	}
}

func TestAcceptanceRequestCancelDoesNotRequireValidUnsentAnswer(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	*now = now.Add(requestIdleDelay)
	publishRequestOffers(m, requestOffer("R1", "v1"))
	prompt, reply, _ := intuitivePrompt(t, client.Question{EnvironmentID: "host", ThreadID: "thread", RequestID: "R1", RequestRevision: "v1", Kind: client.EnterText, Title: "Exact native question", RequestQuestions: []client.RequestQuestion{{ID: "answer", Required: true}}})
	m.Update(promptMsg{generation: m.generation, prompt: prompt})
	m.Update(keyPress(tea.KeyTab))
	m.dialog.preservedInitial = strings.Repeat("x", maxInputBytes+1)
	m.Update(keyPress(tea.KeyEscape))
	flowAwaitReply(t, reply)
	if m.dialog != nil || !m.attention.suppressed[requestIdentity{"host", "thread", "R1", "v1"}] {
		t.Fatal("invalid unsent answer prevented local close")
	}
}

func TestAcceptanceHistoricalPageKeepsExplicitLiveRequestAuthorityWithoutAutoPopup(t *testing.T) {
	m, now := requestAcceptanceModel(t)
	publishRequestOffers(m, requestOffer("R1", "v1"))
	status := m.status
	status.Historical = true
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: status, Snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", State: "historical", Historical: true}}}})
	advanceRequestClock(m, now, 4*requestIdleDelay)
	if m.actionBusy || m.dialog != nil {
		t.Fatal("historical page automatically opened a request")
	}
	m.Update(keyPress('r', tea.ModCtrl))
	if !m.actionBusy || m.attention.automatic {
		t.Fatal("explicit historical request review was unavailable")
	}
	prompt, reply := requestAcceptancePrompt(t, "R1", "v1")
	m.Update(promptMsg{generation: m.generation, prompt: prompt})
	if m.dialog == nil || m.dialog.prompt != prompt {
		t.Fatal("exact client-issued live form was discarded merely for viewing history")
	}
	m.Update(keyPress(tea.KeyEscape))
	flowAwaitReply(t, reply)
	finishRequestPresentation(m)
	if !m.status.Historical || m.snapshot == nil || !m.snapshot.Metadata.Historical || m.actionBusy {
		t.Fatal("explicit request close replaced historical reading authority")
	}
}
