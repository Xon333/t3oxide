package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func shortcutFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shortcuts.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShortcutDefaultsHaveNoDispatchCollision(t *testing.T) {
	if err := (&ShortcutConfig{Version: 1}).validate(); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	if key.Matches(keyPress(tea.KeyEnter), m.keyMap().Send) || key.Matches(keyPress('i', tea.ModCtrl), m.keyMap().Detail) || key.Matches(keyPress(tea.KeyBackspace, tea.ModCtrl), m.keyMap().Stop) {
		t.Fatal("ambiguous terminal key claimed a distinct application action")
	}
	if !key.Matches(keyPress(']', tea.ModCtrl), m.keyMap().Send) || !key.Matches(keyPress('\\', tea.ModCtrl), m.keyMap().Literal) || !key.Matches(keyPress(tea.KeyEnter, tea.ModAlt, tea.ModCtrl), m.keyMap().Steer) {
		t.Fatal("send, literal and steer lack their distinct bindings")
	}
}

func TestCommandActionsRetainControlFallbacksInTheirContexts(t *testing.T) {
	c := &ShortcutConfig{Version: 1}
	for _, context := range shortcutContexts {
		for _, action := range shortcutActions {
			command, control := false, false
			for _, k := range c.keysFor(action, context) {
				command = command || strings.Contains(k, "super+")
				control = control || strings.HasPrefix(k, "ctrl+")
			}
			if command && !control {
				t.Errorf("%s.%s has Command bindings without a Control fallback", context, action.id)
			}
		}
	}
}

func TestControlFallbacksPreserveComposerAndReachLocalActions(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("keep exact draft")
	m.composer.CursorEnd()
	m.Update(keyPress('n', tea.ModCtrl))
	m.Update(keyPress('g', tea.ModCtrl))
	if m.dialog != nil || m.actionBusy || m.currentDraft() != "keep exact draft" || m.composer.SelectedText() != "keep exact draft" {
		t.Fatal("Control editing keys became New or Settings")
	}
	selection, column := m.composer.SelectedText(), m.composer.Column()
	m.Update(keyPress('h', tea.ModCtrl, tea.ModAlt))
	if m.detailTitle != "Keyboard and commands" {
		t.Fatal("Control Help fallback did not open help")
	}
	m.Update(keyPress(tea.KeyEscape))
	sidebar := m.sidebar
	_, cmd := m.Update(keyPress('a', tea.ModCtrl, tea.ModAlt))
	finishCommand(t, m, cmd)
	if m.sidebar == sidebar || m.currentDraft() != "keep exact draft" || m.composer.SelectedText() != selection || m.composer.Column() != column {
		t.Fatal("Control Activity fallback failed or altered the editor")
	}
	m.transcriptFocus = true
	m.Update(keyPress(tea.KeyRight, tea.ModCtrl, tea.ModAlt))
	if m.transcriptFocus || !m.composer.Focused() || m.composer.SelectedText() != selection {
		t.Fatal("Control Compose fallback did not preserve the editor")
	}
}

func TestShortcutLabelsPreferConditionalCommandOnMac(t *testing.T) {
	for _, tc := range []struct {
		keys                    []string
		command, platform, want string
	}{
		{[]string{"ctrl+n", "super+n"}, "new", "darwin", "Cmd+N*"},
		{[]string{"ctrl+n", "super+n"}, "new", "linux", "Ctrl+N"},
		{[]string{"ctrl+s"}, "stop", "darwin", "Ctrl+S"},
		{[]string{"f1", "super+h"}, "help", "darwin", "Cmd+H*"},
		{[]string{"f1", "super+h"}, "help", "linux", "/help"},
		{[]string{"super+right"}, "", "darwin", "Cmd+→*"},
	} {
		if got := shortcutLabelForOS(tc.keys, tc.command, tc.platform); got != tc.want {
			t.Fatalf("%s %v: got %q want %q", tc.platform, tc.keys, got, tc.want)
		}
	}
}

// These are real upstream byte-decoder fixtures, not physical keyboard tests.
func TestTerminalByteDecoderKeepsAmbiguousKeysSafe(t *testing.T) {
	for _, tc := range []struct{ bytes, name string }{
		{"\r", "enter"}, {"\t", "tab"}, {"\x1d", "ctrl+]"}, {"\x1c", "ctrl+\\"},
		{"\x1b[13;9u", "super+enter"}, {"\x1b[13;5u", "ctrl+enter"}, {"\x1b[13;7u", "ctrl+alt+enter"},
		{"\x1bh", "alt+h"}, {"\x1b\x08", "ctrl+alt+h"}, {"\x1b[1;7D", "ctrl+alt+left"}, {"\x1b[49;7u", "ctrl+alt+1"},
	} {
		decoder := uv.EventDecoder{}
		n, event := decoder.Decode([]byte(tc.bytes))
		press, ok := event.(uv.KeyPressEvent)
		if !ok || n != len(tc.bytes) || press.String() != tc.name {
			t.Fatalf("decode %q: %d %#v", tc.bytes, n, event)
		}
		m := testModel(t)
		m.composer.SetValue("line")
		m.composer.CursorEnd()
		m.Update(tea.KeyPressMsg(press))
		if tc.name == "enter" && (m.currentDraft() != "line\n" || m.problem != "") {
			t.Fatal("legacy Enter encoded as an unsafe send")
		}
		if tc.name == "tab" && m.detail != "" {
			t.Fatal("legacy Tab/Ctrl+I became Info")
		}
	}
}

func TestShortcutFileValidatesUnknownAmbiguousAndConflictingKeys(t *testing.T) {
	for name, content := range map[string]string{
		"version": `{"version":2}`, "missing-version": `{}`, "unknown-field": `{"version":1,"watch":true}`,
		"unknown-context":    `{"version":1,"bindings":{"thread":{"send":["ctrl+]"]}}}`,
		"unknown-action":     `{"version":1,"bindings":{"composer":{"submit-all":["ctrl+]"]}}}`,
		"invalid-key":        `{"version":1,"bindings":{"composer":{"send":["ctrl+enterr"]}}}`,
		"ambiguous-tab":      `{"version":1,"bindings":{"global":{"info":["ctrl+i"]}}}`,
		"editor-delete":      `{"version":1,"bindings":{"global":{"stop":["ctrl+s","ctrl+backspace"]}}}`,
		"steal-stop":         `{"version":1,"bindings":{"reading":{"settings":["ctrl+s"]}}}`,
		"remove-stop":        `{"version":1,"bindings":{"global":{"stop":["alt+s"]}}}`,
		"remove-dismiss":     `{"version":1,"bindings":{"global":{"dismiss":["alt+q"]}}}`,
		"steal-cancel":       `{"version":1,"bindings":{"global":{"mode":["ctrl+c"]}}}`,
		"editor-newline":     `{"version":1,"bindings":{"composer":{"send":["enter"]}}}`,
		"editor-select-all":  `{"version":1,"bindings":{"composer":{"settings":["ctrl+g"]}}}`,
		"ordinary-character": `{"version":1,"bindings":{"composer":{"send":["s"]}}}`,
		"navigator-query":    `{"version":1,"bindings":{"navigator":{"new":["n"]}}}`,
		"request-send":       `{"version":1,"bindings":{"form":{"send":["ctrl+]"]}}}`,
		"request-in-reply":   `{"version":1,"bindings":{"composer":{"request-submit":["ctrl+enter"]}}}`,
		"same-context":       `{"version":1,"bindings":{"composer":{"send":["ctrl+\\"]}}}`,
		"global-local":       `{"version":1,"bindings":{"global":{"mode":["ctrl+]"]}}}`,
		"equivalent":         `{"version":1,"bindings":{"global":{"palette":["ctrl+space","ctrl+@"]}}}`,
		"multiple-documents": `{"version":1} {"version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadShortcuts(shortcutFile(t, content)); err == nil {
				t.Fatal("invalid or conflicting shortcut file was accepted")
			}
		})
	}
}

func TestResolvedInfoBindingPreservesFocusedForms(t *testing.T) {
	config, err := LoadShortcuts(shortcutFile(t, `{"version":1,"bindings":{"global":{"info":["alt+i"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		q    client.Question
	}{
		{"answer", client.Question{Kind: client.EnterText, Title: "Answer", Initial: "exact answer"}},
		{"model", client.Question{ModelSettings: auditSettingsReview()}},
		{"request", client.Question{RequestQuestions: []client.RequestQuestion{{ID: "q", Required: true, InitialAnswer: "exact custom answer", AllowCustomAnswer: true}}}},
		{"launch", launchQuestion(client.LaunchInput{Message: "first exact message", Title: "Exact title"})},
		{"browser", browserQuestion("a")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.settings.shortcuts = config
			prompt, answers, _ := intuitivePrompt(t, tc.q)
			m.openPrompt(prompt)
			m.restoreFocus()
			d := m.dialog
			before := m.modalInput.Value()
			m.dialogKey(keyPress('l', tea.ModCtrl))
			if m.dialog != d || d.details {
				t.Fatal("overridden Info key still opened form details")
			}
			m.dialogKey(keyPress('i', tea.ModAlt))
			if m.dialog != d || !d.details {
				t.Fatal("resolved Info did not inspect exact focused form")
			}
			m.dialogKey(keyPress('i', tea.ModAlt))
			if m.dialog != d || d.details || m.modalInput.Value() != before {
				t.Fatal("Info return changed focused form or exact editor")
			}
			assertIntuitiveNoReply(t, answers)
		})
	}
}

func TestShortcutOverrideDrivesDispatchHelpAndPalette(t *testing.T) {
	path := shortcutFile(t, `{"version":1,"bindings":{"global":{"info":["alt+i"]},"composer":{"send":["ctrl+alt+]","cmd+enter"]}}}`)
	config, err := LoadShortcuts(path)
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.settings.shortcuts = config
	m.composer.SetValue("retained draft")
	m.composer.CursorEnd()
	m.Update(keyPress(tea.KeyLeft, tea.ModShift))
	selection, column := m.composer.SelectedText(), m.composer.Column()
	k := m.keyMap()
	if key.Matches(keyPress(']', tea.ModCtrl), k.Send) || !key.Matches(keyPress(']', tea.ModCtrl, tea.ModAlt), k.Send) || !key.Matches(keyPress(tea.KeyEnter, tea.ModSuper), k.Send) {
		t.Fatal("resolved Send differs from file override")
	}
	if !strings.Contains(ansi.Strip(m.bindingHelp(120, m.helpMap())), k.Send.Help().Key+" send") {
		t.Fatal("context help did not use the resolved send binding")
	}
	found := false
	for _, raw := range m.paletteItems() {
		row := raw.(choiceItem)
		if row.command == "info" {
			found = strings.Contains(row.description, "Alt+I")
		}
	}
	if !found {
		t.Fatal("palette did not use the resolved Info binding")
	}
	m.Update(keyPress('l', tea.ModCtrl))
	if m.detail != "" {
		t.Fatal("overridden Info key still dispatched")
	}
	m.Update(keyPress('i', tea.ModAlt))
	if m.detail == "" || !strings.Contains(m.detailTitle, "Info") {
		t.Fatal("configured Info failed to dispatch through the real Model")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.currentDraft() != "retained draft" || m.composer.SelectedText() != selection || m.composer.Column() != column {
		t.Fatal("Info override changed draft caret or selection")
	}
}

func TestShortcutFileLoadsOnceAndRemovalRestoresDefaults(t *testing.T) {
	path := shortcutFile(t, `{"version":1,"bindings":{"composer":{"send":["ctrl+alt+]"]}}}`)
	settings, err := LoadSettings(filepath.Join(filepath.Dir(path), "terminal.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.settings = settings
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !key.Matches(keyPress(']', tea.ModCtrl, tea.ModAlt), m.keyMap().Send) {
		t.Fatal("runtime unexpectedly watched/reloaded the removed file")
	}
	settings, err = LoadSettings(filepath.Join(filepath.Dir(path), "terminal.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.settings = settings
	if !key.Matches(keyPress(']', tea.ModCtrl), m.keyMap().Send) {
		t.Fatal("next startup did not restore defaults after file removal")
	}
}

func TestComposerEnterIsNewlineIncludingSlashCompletion(t *testing.T) {
	for _, draft := range []string{"a message", "/help", "/hel", "first\nsecond"} {
		m := testModel(t)
		m.Update(tea.PasteMsg{Content: draft})
		m.Update(keyPress(tea.KeyEnter))
		if m.currentDraft() != draft+"\n" || m.actionBusy || m.dialog != nil || m.detail != "" {
			t.Fatalf("Enter dispatched or completed %q: draft=%q", draft, m.currentDraft())
		}
	}
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "/hel"})
	m.Update(keyPress(tea.KeyTab))
	if m.currentDraft() != "/help " || m.detail != "" {
		t.Fatal("Tab no longer explicitly accepts completion")
	}
	m.Update(keyPress(']', tea.ModCtrl))
	if m.detail == "" || m.currentDraft() != "" {
		t.Fatal("explicit Send did not execute completed command")
	}
}

func TestSendAndSteerNeverSubmitFocusedRequest(t *testing.T) {
	for _, input := range []tea.KeyPressMsg{keyPress(']', tea.ModCtrl), keyPress('\\', tea.ModCtrl), keyPress(tea.KeyEnter, tea.ModSuper), keyPress(tea.KeyEnter, tea.ModCtrl, tea.ModAlt), keyPress(tea.KeyEnter, tea.ModShift, tea.ModSuper)} {
		m := testModel(t)
		m.composer.SetValue("ordinary draft")
		prompt, answers, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Exact request", Initial: "offered answer"})
		m.openPrompt(prompt)
		m.restoreFocus()
		before := m.dialog
		m.Update(input)
		if m.dialog != before || m.currentDraft() != "ordinary draft" || m.modalInput.Value() != "offered answer" || m.actionBusy {
			t.Fatalf("%s escaped formal request ownership", input.String())
		}
		assertIntuitiveNoReply(t, answers)
	}
}

func TestResolvedRequestSubmitKeepsOrdinarySendAndLaunchSeparate(t *testing.T) {
	config, err := LoadShortcuts(shortcutFile(t, `{"version":1,"bindings":{"form":{"request-submit":["ctrl+alt+u"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.settings.shortcuts = config
	m.composer.SetValue("ordinary source draft")
	prompt, answers, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{{ID: "offered-question", Required: true, AllowCustomAnswer: true, InitialAnswer: "exact form answer"}}})
	m.openPrompt(prompt)
	m.restoreFocus()
	form := m.dialog.requestForm
	if !strings.Contains(strings.Join(m.formDialogHelpPairs(), " "), "Ctrl+Alt+U submit") {
		t.Fatal("form help did not advertise the resolved request submit")
	}
	m.Update(keyPress(tea.KeyEnter, tea.ModCtrl))
	assertIntuitiveNoReply(t, answers)
	if m.dialog == nil || m.modalInput.Value() != "exact form answer" {
		t.Fatal("replaced submit binding still changed the form")
	}
	m.Update(keyPress('u', tea.ModCtrl, tea.ModAlt))
	select {
	case err := <-answers:
		if err == nil || err.Error() != "select at least one environment" {
			t.Fatalf("request submit did not reach its original local broker: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved request submit did not complete its original prompt")
	}
	if form.answers["offered-question"] != "exact form answer" || prompt.Respond(client.Answer{RequestAction: "submit"}) {
		t.Fatal("resolved submit changed the exact answer or accepted the same prompt twice")
	}
	if m.currentDraft() != "ordinary source draft" || m.actionBusy || key.Matches(keyPress('u', tea.ModCtrl, tea.ModAlt), m.actionBinding("request-submit")) {
		t.Fatal("request submit escaped into the ordinary composer")
	}
	m.openPrompt(retiredPrompt(9801, launchQuestion(client.LaunchInput{Message: "new exact message"})))
	before := m.dialog
	m.Update(keyPress('u', tea.ModCtrl, tea.ModAlt))
	if m.dialog != before || m.actionBusy || m.modalInput.Value() != "new exact message" || m.actionBinding("request-submit").Enabled() {
		t.Fatal("request submit was available in a New launch form")
	}
}

func TestEditableNavigatorAndBrowserOwnNativeCaretKeys(t *testing.T) {
	for _, surface := range []string{"navigator", "browser"} {
		for _, tc := range []struct {
			name     string
			key      tea.KeyPressMsg
			from, to int
		}{
			{"backward", keyPress('b', tea.ModCtrl), 2, 1},
			{"end", keyPress('e', tea.ModCtrl), 2, 10},
			{"forward", keyPress('f', tea.ModCtrl), 2, 3},
			{"word-backward", keyPress(tea.KeyLeft, tea.ModAlt), 10, 6},
			{"word-forward", keyPress(tea.KeyRight, tea.ModAlt), 0, 5},
		} {
			t.Run(surface+"/"+tc.name, func(t *testing.T) {
				m := navigatorFixture(t, 120, 35)
				m.navigator.focused = true
				if surface == "browser" {
					m.openPrompt(retiredPrompt(9802, browserQuestion("a")))
					m.dialog.list.SetFilterState(list.Filtering)
				}
				m.restoreFocus()
				editor := &m.navigator.list.FilterInput
				if surface == "browser" {
					editor = &m.dialog.list.FilterInput
				}
				editor.SetValue("alpha beta")
				editor.SetCursor(tc.from)
				dialog, sidebar := m.dialog, m.sidebar
				m.Update(tc.key)
				if m.dialog != dialog || m.sidebar != sidebar || m.searching || m.actionBusy || m.browserBusy || editor.Value() != "alpha beta" || editor.Position() != tc.to {
					t.Fatalf("%s escaped native filter editor: caret=%d value=%q", tc.key.String(), editor.Position(), editor.Value())
				}
			})
		}
	}
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.restoreFocus()
	if !key.Matches(keyPress('f', tea.ModCtrl, tea.ModAlt), m.actionBinding("search")) || !key.Matches(keyPress('e', tea.ModCtrl, tea.ModAlt), m.actionBinding("environments")) {
		t.Fatal("navigation lost its noncolliding host search/environment shortcuts")
	}
}

func TestPaletteReturnRetainsActivityTabSourceAtNarrowWidth(t *testing.T) {
	m := navigatorFixture(t, 60, 16)
	m.openDetail("Exact tool", "source-bound tool detail")
	m.detailIsActivity, m.detailGeneration = true, m.generation
	generation, snapshot, draft := m.detailGeneration, m.snapshot, m.currentDraft()
	m.Update(keyPress('p', tea.ModCtrl))
	if m.dialog == nil || m.dialog.local != "palette" {
		t.Fatal("Commands did not cover the tool inspector")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.detail != "source-bound tool detail" || !m.detailIsActivity || m.detailGeneration != generation {
		t.Fatal("Commands return lost the exact activity detail source")
	}
	m.Update(keyPress(tea.KeyTab))
	if m.detail != "" || !m.activityFocused || m.snapshot != snapshot || m.currentDraft() != draft || m.actionBusy {
		t.Fatal("restored tool Tab did not return locally to visible activity")
	}
}

func TestConditionalCommandAliasesKeepEditorOwnership(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("one\ntwo")
	m.composer.CursorEnd()
	m.Update(keyPress('n', tea.ModCtrl))
	if m.dialog != nil || m.actionBusy {
		t.Fatal("Ctrl+N left the multiline editor")
	}
	m.Update(keyPress('g', tea.ModCtrl))
	if m.composer.SelectedText() != "one\ntwo" || m.dialog != nil {
		t.Fatal("Ctrl+G Settings stole native SelectAll")
	}
	m.composer.ClearSelection()
	m.Update(keyPress('i', tea.ModSuper))
	if m.detail == "" {
		t.Fatal("delivered Command/Super Info event was not recognized")
	}
}

func TestEnhancedArrowPaneFocusPreservesComposerAndConversation(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {120, 35}} {
		for _, modifiers := range []string{"9", "7", "kitty"} { // Command CSI, Control+Alt, Command Kitty
			m := navigatorFixture(t, size[0], size[1])
			m.composer.SetValue("first 界\nsecond line")
			m.composer.CursorEnd()
			m.Update(keyPress(tea.KeyLeft, tea.ModShift))
			draft, snapshot, width := m.currentDraft(), m.snapshot, m.contentWidth
			line, column, selection := m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
			decode := func(suffix string) tea.KeyPressMsg {
				t.Helper()
				bytes := "\x1b[1;" + modifiers + suffix
				if modifiers == "kitty" {
					bytes = "\x1b[57350;9u"
					if suffix == "C" {
						bytes = "\x1b[57351;9u"
					}
				}
				n, event := new(uv.EventDecoder).Decode([]byte(bytes))
				press, ok := event.(uv.KeyPressEvent)
				if !ok || n != len(bytes) {
					t.Fatalf("decode pane key %q: %d %#v", bytes, n, event)
				}
				return tea.KeyPressMsg(press)
			}
			left := decode("D")
			if !key.Matches(left, m.keyMap().FocusNavigation) {
				t.Fatalf("decoded left %q does not match %v; context=%s mode=%s", left.String(), m.keyMap().FocusNavigation.Keys(), m.shortcutContext(), m.mode)
			}
			_, cmd := m.Update(left)
			finishCommand(t, m, cmd)
			if !m.navigator.focused || m.composer.Focused() {
				t.Fatalf("width %d modifier %s: left did not focus conversations: navigator=%v composer=%v visible=%v context=%s", size[0], modifiers, m.navigator.focused, m.composer.Focused(), m.navigationVisible(), m.shortcutContext())
			}
			_, cmd = m.Update(decode("C"))
			finishCommand(t, m, cmd)
			if m.navigator.focused || !m.composer.Focused() || m.transcriptFocus || m.activityFocused || m.contextFocused {
				t.Fatalf("width %d modifier %s: right did not return to composer", size[0], modifiers)
			}
			if m.contentWidth != width || !strings.Contains(ansi.Strip(m.View().Content), "Message") {
				t.Fatal("return to composer retained the compact navigator layout")
			}
			if m.currentDraft() != draft || m.snapshot != snapshot || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selection || m.actionBusy {
				t.Fatal("pane focus changed draft/caret/selection, attached conversation or mutation state")
			}
		}
	}
}

func TestAgentBoxReplyCharactersRemainText(t *testing.T) {
	m := testModel(t)
	m.mode = "agentbox"
	m.agentbox.reply = true
	m.transcriptFocus = false
	m.restoreFocus()
	for _, r := range "jkenrlsz123" {
		m.Update(keyPress(r))
	}
	if m.currentDraft() != "jkenrlsz123" || m.dialog != nil || m.actionBusy {
		t.Fatal("AgentBox action keys stole ordinary reply input")
	}
}

func TestResolvedShortcutHelpFitsSupportedViews(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {180, 45}} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(tea.PasteMsg{Content: "draft"})
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("height %d got %d lines", size[1], len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("width %d overflow: %q", size[0], line)
			}
		}
		if strings.Contains(view, "Enter send") || !strings.Contains(view, "Ctrl+S") {
			t.Fatal("view hides Stop or advertises obsolete Enter send")
		}
	}
}
