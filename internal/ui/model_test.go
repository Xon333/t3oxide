package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(context.Background(), nil, nil)
	t.Cleanup(m.Close)
	return m
}

func keyPress(code rune, mods ...tea.KeyMod) tea.KeyPressMsg {
	k := tea.Key{Code: code}
	for _, mod := range mods {
		k.Mod |= mod
	}
	if code >= 32 && code <= 0x10ffff && k.Mod == 0 {
		k.Text = string(code)
	}
	return tea.KeyPressMsg(k)
}

// Run only finite component/render/settings commands. Session event subscriptions
// intentionally run under the integration program harness, not this helper.
func finishCommand(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	var run func(tea.Cmd, int)
	run = func(next tea.Cmd, depth int) {
		if next == nil {
			return
		}
		if depth > 24 {
			t.Fatal("unexpected unbounded component command chain")
		}
		msg := next()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				run(child, depth+1)
			}
			return
		}
		if msg == nil {
			return
		}
		_, after := m.Update(msg)
		run(after, depth+1)
	}
	run(cmd, 0)
}

func retiredPrompt(id uint64, q client.Question) *client.Prompt {
	done := make(chan struct{})
	close(done)
	return &client.Prompt{ID: id, Question: q, Done: done}
}

func TestComposerKeepsMultilineUnicodeAndRejectsWholeOversizedInput(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "Human @not-a-file — 你好 👩🏽‍💻"})
	m.Update(keyPress(tea.KeyEnter, tea.ModAlt))
	m.Update(tea.PasteMsg{Content: "second line"})
	want := "Human @not-a-file — 你好 👩🏽‍💻\nsecond line"
	if got := m.composer.Value(); got != want {
		t.Fatalf("composer changed human input: %q", got)
	}
	for name, input := range map[string]string{
		"ascii-byte-budget":         strings.Repeat("x", maxInputBytes+1),
		"unicode-byte-budget":       strings.Repeat("界", maxInputBytes/3+1),
		"hard-component-line-limit": strings.Repeat("\n", 10000),
	} {
		t.Run(name, func(t *testing.T) {
			m.composer.SelectAll()
			m.Update(tea.PasteMsg{Content: input})
			if m.composer.Value() != want || !strings.Contains(m.problem, "not added") {
				t.Fatal("oversized paste truncated/replaced the old draft instead of rejecting the edit")
			}
		})
	}
	m.composer.ClearSelection()
	m.composer.SetValue(strings.Repeat("x", maxInputBytes-2))
	m.composer.CursorEnd()
	before := m.composer.Value()
	m.Update(tea.PasteMsg{Content: "界"})
	if m.composer.Value() != before {
		t.Fatal("insertion exceeding the combined UTF-8 byte limit was not atomic")
	}
}

func TestExactDraftAcknowledgmentDoesNotClearNewHumanInput(t *testing.T) {
	m := testModel(t)
	accepted := "original message"
	m.composer.SetValue(accepted)
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 1, DraftAccepted: &accepted}})
	if m.composer.Value() != "" {
		t.Fatal("exact acknowledged draft was not cleared")
	}
	m.Update(tea.PasteMsg{Content: "new unsent message"})
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 2, DraftAccepted: &accepted}})
	if m.composer.Value() != "new unsent message" {
		t.Fatal("late acknowledgment cleared a different draft")
	}
	bootstrap := "stale on-disk text"
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 3, Draft: &bootstrap}})
	if m.composer.Value() != "new unsent message" {
		t.Fatal("late bootstrap overwrote user editing")
	}
}

func TestLivePasteRejectsWholeInputThatTheEditorWouldNormalize(t *testing.T) {
	for _, input := range []string{"\tindented", "first\r\nsecond", "control\x00text", "replacement � text"} {
		for _, modal := range []bool{false, true} {
			for _, clipboardResult := range []bool{false, true} {
				t.Run(fmt.Sprintf("%q/modal=%t/clipboard=%t", input, modal, clipboardResult), func(t *testing.T) {
					m := testModel(t)
					m.composer.SetValue("prior draft")
					m.composer.SelectAll()
					kind, id := "composer", uint64(0)
					if modal {
						m.openPrompt(retiredPrompt(72, client.Question{Kind: client.EnterText, Initial: "prior answer"}))
						m.restoreFocus()
						m.modalInput.SelectAll()
						kind, id = "modal", m.dialog.serial
					}
					var message tea.Msg = tea.PasteMsg{Content: input}
					if clipboardResult {
						message = clipboardPasteMsg{text: input}
					}
					m.updateComponent(kind, id, message)
					if m.composer.Value() != "prior draft" || modal && m.modalInput.Value() != "prior answer" || !strings.Contains(m.problem, "not added") {
						t.Fatal("paste silently changed exact input or replaced the prior selection")
					}
				})
			}
		}
	}
}

func TestSessionSwitchCancelsAttachmentAndIsolatesDraftsAndStaleMessages(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "profile", EnvironmentID: "host", Label: "Test", Enabled: true}}, state)
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	a, err := manager.NewSession("profile", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.NewSession("profile", "b")
	if err != nil {
		t.Fatal(err)
	}
	_ = b.SetDraft("draft B")
	m := NewModel(context.Background(), a, nil)
	t.Cleanup(m.Close)
	m.Update(tea.PasteMsg{Content: "draft A"})
	oldGeneration, oldCtx := m.generation, m.sessionCtx
	latePaste := m.componentCmd("composer", 0, func() tea.Msg { return tea.PasteMsg{Content: "late old clipboard"} })
	m.beginSearch()
	m.searchInput.SetValue("old search")
	m.setSession(b)
	select {
	case <-oldCtx.Done():
	default:
		t.Fatal("switch left the old attachment active")
	}
	m.Update(latePaste())
	m.Update(sessionMsg{generation: oldGeneration, event: client.Event{Revision: 900, Status: client.Status{Context: "STALE"}}})
	m.Update(actionMsg{generation: oldGeneration, result: client.Result{Text: "stale success"}})
	if m.composer.Value() != "draft B" || m.revision != 0 || strings.Contains(m.status.Context, "STALE") || m.notice != "" || m.searching || m.searchInput.Value() != "" {
		t.Fatal("an old attachment changed the new conversation")
	}
	m.setSession(a)
	if m.composer.Value() != "draft A" {
		t.Fatal("thread A draft was not isolated")
	}
}

func TestRestoredDraftBeyondComponentLimitsIsPreservedUntilExplicitClear(t *testing.T) {
	for name, original := range map[string]string{"many-lines": strings.Repeat("\n", 11000) + "do not truncate", "literal-tab": "keep\tthis tab", "CRLF": "first\r\nsecond"} {
		t.Run(name, func(t *testing.T) {
			state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
			if err != nil {
				t.Fatal(err)
			}
			manager := client.NewManager([]config.Environment{{ID: "p", EnvironmentID: "h", Enabled: true}}, state)
			t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
			s, err := manager.NewSession("p", "t")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetDraft(original); err != nil {
				t.Fatal(err)
			}
			m := NewModel(context.Background(), s, nil)
			t.Cleanup(m.Close)
			m.Update(tea.PasteMsg{Content: "do not replace"})
			m.handleKey(keyPress('x'))
			if m.currentDraft() != original || s.Draft() != original || m.composer.Value() != "" || m.composer.Focused() {
				t.Fatal("restored original was normalized/truncated/replaced by the editor")
			}
			if !strings.Contains(m.notice, "Exact draft preserved") || m.handleKey(keyPress('y', tea.ModCtrl)) == nil {
				t.Fatal("preserved draft lacks explicit review/copy path")
			}
			m.handleKey(keyPress(tea.KeyF2))
			if m.detail == "" {
				t.Fatal("preserved draft cannot be inspected")
			}
			m.handleKey(keyPress(tea.KeyEscape))
			m.handleKey(keyPress('x', tea.ModCtrl))
			if m.preservedDraft != "" || s.Draft() != "" || !m.composer.Focused() || m.closed {
				t.Fatal("explicit local clear did not retire preserved draft safely")
			}
		})
	}
}

func TestHelperRevisionRestartStillReplacesVisibleTranscript(t *testing.T) {
	m := testModel(t)
	first := &wire.Snapshot{Revision: 1, Metadata: wire.Metadata{ThreadID: "same"}, Items: []wire.Row{{Key: "same", Kind: "assistant_message", Text: "before disconnect"}}}
	m.snapshot = first
	finishCommand(t, m, m.requestRender(false, false))
	second := &wire.Snapshot{Revision: 1, Metadata: wire.Metadata{ThreadID: "same"}, Items: []wire.Row{{Key: "same", Kind: "assistant_message", Text: "authoritative after reconnect"}}}
	_, commands := m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 3, Snapshot: second}})
	batch, ok := commands().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatal("same-number authoritative replacement did not schedule a render")
	}
	// The first command waits for another network event. Render the second.
	finishCommand(t, m, batch[1])
	plain := ansi.Strip(m.transcript)
	if strings.Contains(plain, "before disconnect") || !strings.Contains(plain, "authoritative after reconnect") {
		t.Fatalf("helper revision collision retained old text: %q", plain)
	}
}

func TestDraftMarkersSurviveRenderCoalescingAndOutdatedRevisions(t *testing.T) {
	m := testModel(t)
	accepted := "acknowledged"
	m.composer.SetValue(accepted)
	first := &wire.Snapshot{Revision: 1, Items: []wire.Row{{Key: "x", Kind: "assistant_message", Text: "old"}}}
	m.snapshot = first
	oldCmd := m.requestRender(true, false)
	if oldCmd == nil || !m.rendering {
		t.Fatal("first async render was not scheduled")
	}
	latest := &wire.Snapshot{Revision: 2, Items: []wire.Row{{Key: "x", Kind: "assistant_message", Text: "current authoritative replacement"}}}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 2, Snapshot: latest, DraftAccepted: &accepted}})
	if m.composer.Value() != "" || m.pendingRender == nil {
		t.Fatal("render coalescing lost the acknowledgment boundary")
	}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 1, Snapshot: first, Status: client.Status{Context: "old"}}})
	if m.snapshot != latest || m.revision != 2 {
		t.Fatal("outdated revision replaced the current page")
	}
	_, next := m.Update(oldCmd())
	if m.transcript != "" || next == nil {
		t.Fatal("obsolete render became visible or latest render was dropped")
	}
	finishCommand(t, m, next)
	if !strings.Contains(ansi.Strip(m.transcript), "current authoritative replacement") || strings.Contains(ansi.Strip(m.transcript), "old") {
		t.Fatal("latest keyed replacement was not rendered")
	}
	if m.rendering || m.pendingRender != nil || m.viewport.YOffset() != 0 {
		t.Fatal("coalesced history reset was lost or renderer did not retire")
	}
}

func TestOldRenderCannotCrossSessionOrThemeBoundary(t *testing.T) {
	m := testModel(t)
	m.snapshot = &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: "old thread"}}}
	old := m.requestRender(false, false)
	m.setSession(nil)
	if !m.rendering {
		t.Fatal("switch allowed concurrent access to the shared renderer")
	}
	m.theme = "lime"
	m.snapshot = &wire.Snapshot{Items: []wire.Row{{Key: "b", Kind: "assistant_message", Text: "new thread"}}}
	if cmd := m.requestRender(false, false); cmd != nil {
		t.Fatal("new render ran concurrently with retiring render")
	}
	_, next := m.Update(old())
	if m.transcript != "" || next == nil {
		t.Fatal("old session render leaked")
	}
	finishCommand(t, m, next)
	if strings.Contains(ansi.Strip(m.transcript), "old thread") || !strings.Contains(ansi.Strip(m.transcript), "new thread") || m.renderer.theme != "lime" {
		t.Fatal("current thread/theme was not rendered after retirement")
	}
}

func TestEnterTextDialogResizeThemeFocusAndRetiredPrompt(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("draft stays here")
	p := retiredPrompt(7, client.Question{Kind: client.EnterText, Title: "Provider question", Prompt: "Enter an exact answer", Initial: "initial"})
	m.openPrompt(p)
	m.restoreFocus()
	for _, size := range []tea.WindowSizeMsg{{Width: 44, Height: 16}, {Width: 120, Height: 38}} {
		m.Update(size)
		m.Update(settingMsg{kind: "theme", value: "cyan"})
		_ = m.View()
	}
	if !m.modalInput.Focused() || m.composer.Focused() || m.modalInput.Value() != "initial" {
		t.Fatal("free-text dialog did not own focus/preserve initial value")
	}
	m.Update(tea.BlurMsg{})
	if m.modalInput.Focused() {
		t.Fatal("blur left textarea focused")
	}
	m.Update(tea.FocusMsg{})
	m.modalInput.SelectAll()
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", maxInputBytes+1)})
	if m.modalInput.Value() != "initial" || !strings.Contains(m.problem, "not added") {
		t.Fatal("free-text answer was silently truncated")
	}
	m.Update(promptClosedMsg{generation: m.generation - 1, id: p.ID})
	m.Update(promptClosedMsg{generation: m.generation, id: p.ID + 1})
	if m.dialog == nil {
		t.Fatal("unrelated request retirement closed current dialog")
	}
	m.Update(promptClosedMsg{generation: m.generation, id: p.ID})
	if m.dialog != nil || !m.composer.Focused() || m.composer.Value() != "draft stays here" {
		t.Fatal("request cleanup did not restore isolated composer")
	}
}

func TestDialogDetailsSuspendEditingAndRestoreFocus(t *testing.T) {
	for _, kind := range []client.Kind{client.EnterText, client.ChooseOne} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := testModel(t)
			m.openPrompt(retiredPrompt(71, client.Question{Kind: kind, Initial: "original", Choices: []client.Choice{{ID: "a", Label: "Alpha"}}}))
			if kind != client.EnterText {
				m.dialog.list.SetFilterState(list.Filtering)
				m.dialog.list.FilterInput.SetValue("Alpha")
			}
			m.restoreFocus()
			m.Update(tea.BlurMsg{})
			if m.modalInput.Focused() || m.dialog.list.FilterInput.Focused() {
				t.Fatal("terminal blur left a dialog editor focused")
			}
			m.Update(tea.FocusMsg{})
			if kind == client.EnterText && !m.modalInput.Focused() || kind != client.EnterText && !m.dialog.list.FilterInput.Focused() {
				t.Fatal("terminal focus did not restore the active dialog editor")
			}
			m.dialogKey(keyPress(tea.KeyF2))
			m.Update(tea.FocusMsg{})
			m.Update(tea.PasteMsg{Content: "hidden paste"})
			if m.modalInput.Focused() || m.dialog.list.FilterInput.Focused() {
				t.Fatal("details left a hidden editor focused")
			}
			if kind == client.EnterText && m.modalInput.Value() != "original" || kind != client.EnterText && m.dialog.list.FilterInput.Value() != "Alpha" {
				t.Fatal("paste changed a hidden dialog editor while reviewing details")
			}
			m.dialogKey(keyPress(tea.KeyEscape))
			if kind == client.EnterText && !m.modalInput.Focused() || kind != client.EnterText && !m.dialog.list.FilterInput.Focused() {
				t.Fatal("closing details did not restore dialog editor focus")
			}
		})
	}
}

func TestInitialAnswerCannotBeSilentlyNormalizedOrTruncated(t *testing.T) {
	for name, original := range map[string]string{"literal-tab": "exact\tanswer", "CRLF": "first\r\nsecond", "many-lines": strings.Repeat("\n", 11000) + "final answer line"} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t)
			m.openPrompt(retiredPrompt(4, client.Question{Kind: client.EnterText, Title: "Exact answer", Initial: original}))
			m.restoreFocus()
			m.Update(tea.PasteMsg{Content: "must not replace"})
			m.chooseDialog()
			if m.dialog == nil || m.dialog.preservedInitial != original || m.modalInput.Value() != "" || m.modalInput.Focused() {
				t.Fatal("initial answer was altered or submitted without explicit replacement")
			}
			if m.handleKey(keyPress('y', tea.ModCtrl)) == nil {
				t.Fatal("original exact answer cannot be copied")
			}
			m.dialogKey(keyPress(tea.KeyF2))
			if !strings.Contains(m.dialog.detailText, safeText(original)) {
				t.Fatal("original answer is not available for full review")
			}
			m.dialogKey(keyPress(tea.KeyEscape))
			m.dialogKey(keyPress('u', tea.ModCtrl))
			if m.dialog.preservedInitial != "" || !m.modalInput.Focused() {
				t.Fatal("explicit answer replacement did not enable editor")
			}
			m.Update(tea.PasteMsg{Content: "human replacement 👩🏽‍💻"})
			if m.modalInput.Value() != "human replacement 👩🏽‍💻" {
				t.Fatal("new human-authored answer changed")
			}
		})
	}
}

func TestFilteredMultipleChoiceUpdatesExactMasterItem(t *testing.T) {
	m := testModel(t)
	p := retiredPrompt(11, client.Question{Kind: client.ChooseMany, Title: "Select exact answers", Choices: []client.Choice{{ID: "a", Label: "alpha"}, {ID: "b", Label: "beta"}, {ID: "z", Label: "zebra"}}})
	m.openPrompt(p)
	m.dialog.list.SetFilterText("zebra")
	if m.dialog.list.SelectedItem().(choiceItem).id != "z" {
		t.Fatal("fixture filtering failed")
	}
	finishCommand(t, m, m.dialogKey(keyPress(' ')))
	if !m.dialog.selected["z"] || m.dialog.selected["a"] || m.dialog.selected["b"] {
		t.Fatal("filtered index selected the wrong request value")
	}
	for _, item := range m.dialog.list.Items() {
		choice := item.(choiceItem)
		if choice.checked != (choice.id == "z") {
			t.Fatalf("wrong backing list item changed: %+v", choice)
		}
	}
	if m.dialog.list.FilterState() != list.FilterApplied {
		t.Fatal("multi-selection lost applied filtering")
	}
}

func TestFullChoiceDetailsAreReachableInNarrowPaneWithoutAnswering(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 44, Height: 16})
	label := strings.Repeat("long choice ", 12) + "EXACT LABEL END"
	description := strings.Repeat("specific consequence\n", 20) + "COMPLETE DESCRIPTION END"
	m.openPrompt(retiredPrompt(5, client.Question{Kind: client.ChooseOne, Title: "Permission", Prompt: "Review this operation", Choices: []client.Choice{{ID: "specific-allow", Label: label, Description: description}}}))
	if !strings.Contains(ansi.Strip(m.View().Content), "F2 details") {
		t.Fatalf("full choice details were not discoverable:\n%s", ansi.Strip(m.View().Content))
	}
	m.dialogKey(keyPress(tea.KeyF2))
	if !m.dialog.details || !strings.Contains(m.dialog.detailText, label) || !strings.Contains(m.dialog.detailText, description) {
		t.Fatal("the full exact choice was not retained")
	}
	m.dialogKey(keyPress(tea.KeyEnd))
	if !strings.Contains(ansi.Strip(m.View().Content), "COMPLETE DESCRIPTION END") {
		t.Fatalf("cannot scroll to complete selected description:\n%s", ansi.Strip(m.View().Content))
	}
	m.dialogKey(keyPress(tea.KeyEscape))
	if m.dialog == nil || m.dialog.details {
		t.Fatal("Escape from details answered/dismissed the request")
	}
}

func TestLateDialogComponentResultCannotModifyNewRequest(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(1, client.Question{Kind: client.EnterText, Title: "old"}))
	m.restoreFocus()
	old := m.componentCmd("modal", m.dialog.serial, func() tea.Msg { return tea.PasteMsg{Content: "old async paste"} })
	m.openPrompt(retiredPrompt(2, client.Question{Kind: client.EnterText, Title: "new", Initial: "new exact answer"}))
	m.restoreFocus()
	m.Update(old())
	if m.modalInput.Value() != "new exact answer" {
		t.Fatal("old prompt clipboard result crossed to a new request")
	}
}

func TestNavigationRetiresPriorDialogBeforeOpeningNext(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "a", Label: "Alpha", EnvironmentID: "a", Enabled: true}, {ID: "b", Label: "Beta", EnvironmentID: "b", Enabled: true}}, state)
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	s, err := manager.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(context.Background(), s, nil)
	t.Cleanup(m.Close)
	readPrompt := func() *client.Prompt {
		t.Helper()
		select {
		case p := <-s.Prompts():
			return p
		case <-time.After(2 * time.Second):
			t.Fatal("navigation did not open the next exact prompt")
			return nil
		}
	}
	for range 12 {
		first, second := make(chan tea.Msg, 1), make(chan tea.Msg, 1)
		initial := m.execute("env", "")
		go func() { first <- initial() }()
		p := readPrompt()
		m.openPrompt(p)
		replacement := m.execute("env", "")
		go func() { second <- replacement() }()
		next := readPrompt()
		if next == p || next.ID == p.ID {
			t.Fatal("navigation reused the old modal")
		}
		select {
		case <-p.Done:
		default:
			t.Fatal("new navigation opened before the old request retired")
		}
		m.Update(<-first)
		if !m.actionBusy {
			t.Fatal("late canceled command cleared replacement action state")
		}
		next.Respond(client.Answer{Cancelled: true})
		select {
		case result := <-second:
			if msg := result.(actionMsg); msg.err != nil && !strings.Contains(msg.err.Error(), "selection dismissed") {
				t.Fatalf("replacement navigation collided with old prompt: %v", msg.err)
			}
			m.Update(result)
		case <-time.After(2 * time.Second):
			t.Fatal("replacement dialog did not retire")
		}
	}
}

func TestLocalInterruptAndExitDoNotClaimRemoteStop(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("unsent")
	m.handleKey(keyPress('c', tea.ModCtrl))
	if m.closed || m.stopBusy || m.composer.Value() != "unsent" || m.ctx.Err() != nil {
		t.Fatal("Ctrl+C did not preserve the local draft")
	}
	m.composer.SetValue("preserved until shutdown")
	cmd := m.handleKey(keyPress('q', tea.ModCtrl))
	if !m.closed || m.ctx.Err() == nil || m.stopBusy || m.composer.Value() != "preserved until shutdown" {
		t.Fatal("Ctrl+Q changed remote state or lost draft")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("exit did not request normal Bubble Tea terminal cleanup")
	}
}

func TestCurrentPageSearchAndReadingDoesNotFollowStreaming(t *testing.T) {
	m := testModel(t)
	var lines []string
	for i := range 60 {
		text := fmt.Sprintf("row %d", i)
		if i == 8 || i == 40 {
			text += " Café"
		}
		lines = append(lines, text)
	}
	m.transcript = strings.Join(lines, "\n")
	m.viewport.SetContent(m.transcript)
	m.viewport.GotoBottom()
	m.handleKey(keyPress(tea.KeyTab))
	m.handleKey(keyPress(tea.KeyPgUp))
	if m.follow || m.viewport.AtBottom() {
		t.Fatal("reading older visible content did not pause follow")
	}
	oldOffset := m.viewport.YOffset()
	m.Update(renderedMsg{request: renderRequest{generation: m.generation, width: m.contentWidth, theme: m.theme}, content: m.transcript + "\nnew streaming line"})
	if m.viewport.YOffset() != oldOffset {
		t.Fatal("streaming jumped away from reading position")
	}
	m.dispatch("search", "cAFÉ")
	if len(m.searchLines) != 2 || m.searchLines[0] != 8 || m.searchLines[1] != 40 || m.follow {
		t.Fatal("case-insensitive current-page search did not find displayed lines")
	}
	m.transcriptFocus = true
	m.handleKey(keyPress('n'))
	if m.searchIndex != 1 || m.viewport.YOffset() != 38 {
		t.Fatal("next search match did not navigate viewport")
	}
	m.handleKey(keyPress(tea.KeyEnd))
	if !m.follow || !m.viewport.AtBottom() {
		t.Fatal("explicit Latest visible scroll did not resume following")
	}
}

func TestThemeChangePersistsBeforeApplyingAndMouseFlagsFollow(t *testing.T) {
	m := testModel(t)
	cmd := m.dispatch("theme", "cyan")
	if m.theme != "violet" || !m.settingBusy {
		t.Fatal("theme changed before persistence result")
	}
	finishCommand(t, m, cmd)
	if m.theme != "cyan" || m.settingBusy {
		t.Fatal("saved theme was not applied")
	}
	finishCommand(t, m, m.dispatch("mouse", "off"))
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("mouse capture remained on")
	}
	finishCommand(t, m, m.dispatch("mouse", "on"))
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse capture was not enabled")
	}
	m.dispatch("theme", "nonexistent")
	if m.theme != "cyan" || m.settingBusy || !strings.Contains(m.problem, "/theme") {
		t.Fatal("unsupported theme masqueraded as successful")
	}
}

func TestResponsiveViewsHaveBoundedUnicodeRowsAndNoRequiredSidebar(t *testing.T) {
	m := testModel(t)
	m.status = client.Status{Context: "M1 Pro / Unicode 界 · 👩🏽‍💻", State: "running · auto", ProfileID: "m1-pro", Connected: true, Writable: true, Delivery: "auto"}
	m.snapshot = &wire.Snapshot{Revision: 1, Metadata: wire.Metadata{Title: "Unicode 界", ThreadID: "fixture", Live: true}, Items: []wire.Row{
		{Key: "u", Kind: "user_message", Text: "Review the queue-holding Stop behavior."},
		{Key: "a", Kind: "assistant_message", Text: "## Check complete\n\nThe independent Stop path is available while a send is pending.\n\n- Preserve the operation ID\n- Return to **Latest** to resume live detail", Streaming: true},
	}}
	m.composer.SetValue("Follow up when ready…")
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 44, Height: 16}, {Width: 28, Height: 10}, {Width: 160, Height: 42}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			m.width, m.height = size.Width, size.Height
			m.resize()
			finishCommand(t, m, m.requestRender(true, true))
			view := m.View()
			plain := ansi.Strip(view.Content)
			if !view.AltScreen || !view.ReportFocus || !utf8.ValidString(plain) {
				t.Fatal("terminal contract or Unicode output invalid")
			}
			rows := strings.Split(plain, "\n")
			if len(rows) != size.Height {
				t.Fatalf("view height %d, want %d", len(rows), size.Height)
			}
			for row, line := range rows {
				if width := ansi.StringWidth(line); width != size.Width {
					t.Fatalf("row %d width %d, want %d: %q", row, width, size.Width, line)
				}
			}
			if !strings.Contains(plain, "t3oxide") || !strings.Contains(plain, "Stop") || !strings.Contains(strings.ToLower(plain), "message") {
				t.Fatalf("essential context/actions disappeared:\n%s", plain)
			}
			if size.Width == 80 || size.Width == 44 {
				t.Log("Actual fixture View:\n" + plain)
				if dir := os.Getenv("T3OXIDE_UI_PREVIEW_DIR"); dir != "" {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("view-%dx%d.txt", size.Width, size.Height)), []byte(plain+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
	finishCommand(t, m, m.dispatch("sidebar", "on"))
	if !strings.Contains(strings.ToLower(ansi.Strip(m.View().Content)), "writes enabled") {
		t.Fatal("optional wide context sidebar not rendered")
	}
	m.width = 44
	m.resize()
	if m.contentWidth != 42 || strings.Contains(ansi.Strip(m.View().Content), "writes enabled") {
		t.Fatal("wide sidebar consumed narrow transcript space")
	}
}
