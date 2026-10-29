package ui

import (
	"runtime"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/Xon333/t3oxide/internal/client"
)

// Native bindings own both dispatch and help, following Bubbles/Huh and
// LazyGit's contextual ownership. Editors keep their upstream keymaps.
type appKeyMap struct {
	Stop, Quit, Help, Palette, Threads, Environments            key.Binding
	Requests, Search, Sidebar, Model, Effort, Speed             key.Binding
	Parent, Earlier, Latest, Copy, Detail, Focus                key.Binding
	Clear, Edit, Recall, Literal, Dismiss, Send                 key.Binding
	Error, Steer, New, NewSame, Settings, Refresh, Filter, Mode key.Binding
	FocusBack, FocusNavigation, FocusComposer                   key.Binding
}

func bind(label, description string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, description))
}

func optionKey(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌥" + key
	}
	return "Alt+" + key
}

func (m *Model) keyMap() appKeyMap {
	k := appKeyMap{
		Stop: m.actionBinding("stop"), Quit: m.actionBinding("quit"), Help: m.actionBinding("help"), Palette: m.actionBinding("palette"),
		Threads: m.actionBinding("switch"), Environments: m.actionBinding("environments"), Requests: m.actionBinding("requests"), Search: m.actionBinding("search"),
		Sidebar: m.actionBinding("sidebar"), Model: m.actionBinding("model"), Effort: m.actionBinding("effort"), Speed: m.actionBinding("speed"),
		Parent: m.actionBinding("parent"), Earlier: m.actionBinding("earlier"), Latest: m.actionBinding("latest"), Copy: m.actionBinding("copy"),
		Detail: m.actionBinding("info"), Focus: m.actionBinding("focus-next"), FocusBack: m.actionBinding("focus-previous"),
		Clear: m.actionBinding("clear"), Edit: m.actionBinding("edit"), Recall: m.actionBinding("recall"), Literal: m.actionBinding("send-literal"),
		Dismiss: m.actionBinding("dismiss"), Send: m.actionBinding("send"), Steer: m.actionBinding("steer"), Error: m.actionBinding("error"),
		New: m.actionBinding("new"), NewSame: m.actionBinding("new-same"), Settings: m.actionBinding("settings"), Refresh: m.actionBinding("refresh"),
		Filter: m.actionBinding("filter"), Mode: m.actionBinding("mode"), FocusNavigation: m.actionBinding("focus-navigation"), FocusComposer: m.actionBinding("focus-composer"),
	}
	normal := m.detail == "" && m.dialog == nil && !m.searching && !m.browserPage && !m.launchPage
	if !normal {
		for _, b := range []*key.Binding{&k.Threads, &k.Requests, &k.Model, &k.Effort, &k.Speed, &k.Parent, &k.Focus, &k.FocusBack, &k.Edit, &k.Clear, &k.Recall, &k.Literal, &k.Send, &k.Steer, &k.New, &k.NewSame, &k.Settings, &k.Refresh, &k.Filter, &k.FocusNavigation, &k.FocusComposer} {
			b.SetEnabled(false)
		}
	}
	k.Error.SetEnabled(k.Error.Enabled() && (m.problem != "" || m.streamProblem != ""))
	k.Clear.SetEnabled(k.Clear.Enabled() && m.currentDraft() != "")
	return k
}

type contextHelp struct {
	short []key.Binding
	full  [][]key.Binding
}

func (h contextHelp) ShortHelp() []key.Binding  { return h.short }
func (h contextHelp) FullHelp() [][]key.Binding { return h.full }

func (m *Model) helpMap() contextHelp {
	k := m.keyMap()
	global := []key.Binding{k.Help, k.Palette, k.Stop, k.Quit, k.Mode, k.Error}
	var active []key.Binding
	switch {
	case m.detail != "":
		active = []key.Binding{k.Dismiss, k.Copy, m.detailViewport.KeyMap.Up, m.detailViewport.KeyMap.Down}
	case m.dialog != nil:
		active = []key.Binding{bind("Enter", "select", "enter"), k.Detail, k.Dismiss}
		if m.dialog.details {
			active = []key.Binding{k.Detail, k.Dismiss, m.questionViewport.KeyMap.Up, m.questionViewport.KeyMap.Down}
		} else if m.dialog.kind == client.EnterText {
			active = []key.Binding{bind("Enter", "answer", "enter"), m.modalInput.KeyMap.InsertNewline, k.Detail, k.Dismiss}
		} else if m.dialog.kind == client.ChooseMany {
			if m.dialog.finishFocused {
				active = []key.Binding{bind("Enter", "continue", "enter"), bind("Tab", "choices", "tab"), k.Detail, k.Dismiss}
			} else {
				active = []key.Binding{bind("Enter", "toggle", "enter"), bind("Tab", "continue", "tab"), k.Detail, k.Dismiss}
			}
		}
		if m.dialog.requestForm != nil && !m.dialog.details {
			active = append(active, m.actionBinding("request-submit"))
		}
		if !m.dialog.details {
			active = append(active, m.actionBinding("browser.search"), m.actionBinding("browser.filter"))
		}
	case m.searching:
		active = []key.Binding{bind("Enter", "find", "enter"), k.Dismiss}
	case m.agentboxOwnsKeys():
		active = []key.Binding{m.actionBinding("agentbox.open"), m.actionBinding("agentbox.reply"), m.actionBinding("agentbox.file"), m.actionBinding("agentbox.diff"), m.actionBinding("agentbox.inbox")}
	case m.mode == "agentbox" && m.agentbox.reply:
		active = []key.Binding{k.Send, k.Steer, m.composer.KeyMap.InsertNewline}
	case m.navigator.focused && m.navigationVisible():
		active = []key.Binding{bind("Enter", "open", "enter"), k.Focus, k.Search, m.actionBinding("navigator.more")}
	case m.activityFocused:
		active = []key.Binding{bind("Enter", "inspect", "enter"), bind("a", "activity", "a"), k.Focus}
	case m.contextFocused && m.contextVisible():
		active = []key.Binding{bind("Enter", "inspect", "enter"), k.Focus}
	case m.transcriptFocus:
		active = []key.Binding{k.Focus, k.Search, k.Earlier, k.Latest, k.Copy}
	default:
		active = []key.Binding{k.Palette, k.Stop, k.Send, m.composer.KeyMap.InsertNewline, k.Focus}
		if m.preservedDraft != "" {
			active = []key.Binding{k.Edit, k.Detail, k.Send, k.Palette, k.Stop}
		}
		if m.problem != "" || m.streamProblem != "" {
			detail := k.Detail
			if m.preservedDraft != "" {
				detail = k.Error
			}
			active = append([]key.Binding{detail}, active...)
		}
	}
	short := active
	if m.detail != "" || m.dialog != nil || m.searching || m.transcriptFocus {
		short = append(short, k.Palette, k.Stop)
	}
	if m.detail == "" && m.dialog == nil && !m.searching && !m.browserPage && !m.launchPage {
		// Put Stop ahead of optional navigation so native help truncation
		// cannot hide it. Requests has one mnemonic and no repeated count.
		short = []key.Binding{k.Stop}
		if m.problem != "" || m.streamProblem != "" {
			short = append(short, k.Error)
		}
		if m.status.Requests > 0 {
			short = append(short, k.Requests)
		}
		short = append(short, k.Palette)
		if m.mode == "agentbox" {
			short = append(short, active...)
		} else if m.activityFocused || m.navigator.focused && m.navigationVisible() || m.contextFocused && m.contextVisible() {
			short = append(short, active...)
		} else if m.transcriptFocus {
			short = append(short, k.Search, k.Help, k.Focus, k.Earlier, k.Latest, k.Copy)
		} else if m.preservedDraft != "" {
			short = append(short, k.Edit, k.Detail, k.Send)
		} else {
			short = append(short, k.Send, m.composer.KeyMap.InsertNewline, k.Focus)
		}
	}
	full := [][]key.Binding{global, active}
	if m.detail == "" && m.dialog == nil && !m.searching {
		full = append(full, []key.Binding{k.Threads, k.Environments, k.Requests, k.Model, k.Effort, k.Speed, k.Parent, k.Sidebar, k.Edit, k.Clear, k.Recall, k.Literal, k.Steer, k.New, k.NewSame, k.Settings, k.Mode})
		if !m.transcriptFocus {
			e := m.composer.KeyMap
			full = append(full, []key.Binding{e.LineStart, e.LineEnd, e.CharacterBackward, e.CharacterForward, e.WordBackward, e.WordForward, e.LinePrevious, e.LineNext, e.DeleteBeforeCursor, e.DeleteAfterCursor, e.DeleteCharacterForward, e.Paste, e.TransposeCharacterBackward})
		}
	}
	return contextHelp{short, full}
}

func (m *Model) helpText() string {
	h := help.New()
	h.Styles = colors(m.theme).helpStyles()
	h.SetWidth(max(20, m.modalWidth))
	h.ShowAll = true
	var parts []string
	// Render one native FullHelp group per block so narrow terminals retain
	// every binding in the scrollable viewport instead of losing columns.
	for _, group := range m.helpMap().FullHelp() {
		parts = append(parts, h.View(contextHelp{full: [][]key.Binding{group}}))
	}
	parts = append(parts, "COMMANDS")
	for _, spec := range m.commandCatalog() {
		line := "/" + spec.name
		if spec.arguments != "" {
			line += " " + spec.arguments
		}
		parts = append(parts, line+"  "+spec.title)
	}
	composer := func(id string) string { return m.actionBindingFor(id, "composer").Help().Key }
	parts = append(parts,
		"RECOVERY\nCtrl+C dismisses locally and keeps the draft. "+composer("clear")+" clears it into /recall prompt history. Local exit leaves T3 running. Ctrl+S Stop has an independent lane; retries preserve the saved operation ID.",
		"COMPOSER\nEnter inserts a newline. "+composer("send")+" sends or executes a recognized single-line command. "+composer("send-literal")+" sends literal text, including a slash command. "+composer("steer")+" explicitly steers without changing the selected delivery preference. Request Submit remains a separate explicit form action.",
		"CONTROLS\n"+composer("palette")+" opens Commands from the composer. /keyboard legacy retains previous-line Ctrl+P editing; /keyboard modern restores its Commands shortcut. Ctrl+G selects all inside editors. /info (also /details), /new, /new-same, /review, /mode, /shortcuts and /help remain discoverable in Commands.",
		"TERMINAL KEYS\nCommand/Super aliases marked * work only when delivered by the terminal. Ctrl+I stays Tab and Ctrl+Backspace stays deletion. No terminal or OS settings are changed. /shortcuts shows every resolved manual-file binding; docs/shortcuts.md lists the portable defaults.")
	return strings.Join(parts, "\n\n")
}
