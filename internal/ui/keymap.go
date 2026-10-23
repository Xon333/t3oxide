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
	Stop, Quit, Help, Palette, Threads, Environments key.Binding
	Requests, Search, Sidebar, Model, Effort, Speed  key.Binding
	Parent, Earlier, Latest, Copy, Detail, Focus     key.Binding
	Clear, Edit, Recall, Literal, Dismiss, Send      key.Binding
	Error                                            key.Binding
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

var applicationKeys = appKeyMap{
	Stop: bind("Ctrl+S", "Stop/hold", "ctrl+s"), Quit: bind("Ctrl+Q", "exit", "ctrl+q"),
	Help: bind("F1", "help", "f1"), Palette: bind("F3", "commands", "f3"),
	Threads: bind("F6", "conversations", "f6"), Environments: bind("Ctrl+E", "environments", "ctrl+e"),
	Requests: bind("Ctrl+R", "requests", "ctrl+r"), Search: bind("Ctrl+F", "find", "ctrl+f"),
	Sidebar: bind("Ctrl+B", "sidebar", "ctrl+b"), Model: bind(optionKey("M"), "model", "alt+m"),
	Effort: bind(optionKey("E"), "effort", "alt+e"), Speed: bind(optionKey("S"), "speed", "alt+s"),
	Parent: bind(optionKey("↑"), "parent", "alt+up"), Earlier: bind(optionKey("←"), "earlier", "alt+left"),
	Latest: bind(optionKey("→"), "latest", "alt+right"), Copy: bind("Ctrl+Y", "copy", "ctrl+y"),
	Detail: bind("F2", "details", "f2"), Focus: bind("Tab", "read/write", "tab"),
	Clear: bind("Ctrl+X", "clear draft", "ctrl+x"), Edit: bind("F4", "editor", "f4"),
	Recall: bind("F5", "prompts", "f5"), Literal: bind("Ctrl+Enter", "send text", "ctrl+enter"),
	Dismiss: bind("Esc", "back", "esc"), Send: bind("Enter", "send", "enter"),
	Error: bind("F8", "error", "f8"),
}

func (m *Model) keyMap() appKeyMap {
	k := applicationKeys
	normal := m.detail == "" && m.dialog == nil && !m.searching
	reading := normal && m.transcriptFocus
	k.Error.SetEnabled(m.problem != "" || m.streamProblem != "")
	if m.searching || m.dialog != nil && m.dialog.prompt != nil {
		k.Palette.SetHelp("F3", "help")
	}
	for _, b := range []*key.Binding{&k.Threads, &k.Requests, &k.Model, &k.Effort, &k.Speed, &k.Parent, &k.Focus, &k.Edit, &k.Clear, &k.Recall, &k.Literal, &k.Send} {
		b.SetEnabled(normal)
	}
	for _, b := range []*key.Binding{&k.Environments, &k.Search, &k.Sidebar, &k.Earlier, &k.Latest} {
		b.SetEnabled(reading)
	}
	if normal && (m.transcriptFocus || m.currentDraft() == "") {
		k.Palette.SetKeys("f3", "ctrl+p")
		k.Threads.SetKeys("f6", "ctrl+t")
		k.Quit.SetKeys("ctrl+q", "ctrl+d")
	}
	k.Edit.SetEnabled(normal && !m.transcriptFocus)
	k.Clear.SetEnabled(normal && !m.transcriptFocus && m.currentDraft() != "")
	k.Literal.SetEnabled(normal && !m.transcriptFocus)
	k.Send.SetEnabled(normal && !m.transcriptFocus)
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
	global := []key.Binding{k.Help, k.Palette, k.Stop, k.Quit, k.Error}
	var active []key.Binding
	switch {
	case m.detail != "":
		active = []key.Binding{k.Dismiss, k.Copy, m.detailViewport.KeyMap.Up, m.detailViewport.KeyMap.Down}
	case m.dialog != nil:
		active = []key.Binding{bind("Enter", "select", "enter"), k.Detail, k.Dismiss}
		if m.dialog.details {
			active = []key.Binding{bind("F2", "back", "f2"), k.Dismiss, m.questionViewport.KeyMap.Up, m.questionViewport.KeyMap.Down}
		} else if m.dialog.kind == client.EnterText {
			active = []key.Binding{bind("Enter", "answer", "enter"), m.modalInput.KeyMap.InsertNewline, k.Detail, k.Dismiss}
		} else if m.dialog.kind == client.ChooseMany {
			if m.dialog.finishFocused {
				active = []key.Binding{bind("Enter", "continue", "enter"), bind("Tab", "choices", "tab"), k.Detail, k.Dismiss}
			} else {
				active = []key.Binding{bind("Enter", "toggle", "enter"), bind("Tab", "continue", "tab"), k.Detail, k.Dismiss}
			}
		}
	case m.searching:
		active = []key.Binding{bind("Enter", "find", "enter"), k.Dismiss}
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
	full := [][]key.Binding{global, active}
	if m.detail == "" && m.dialog == nil && !m.searching {
		full = append(full, []key.Binding{k.Threads, k.Environments, k.Requests, k.Model, k.Effort, k.Speed, k.Parent, k.Sidebar, k.Edit, k.Clear, k.Recall, k.Literal})
		if !m.transcriptFocus {
			e := m.composer.KeyMap
			full = append(full, []key.Binding{e.LineStart, e.LineEnd, e.CharacterBackward, e.CharacterForward, e.WordBackward, e.WordForward, e.LinePrevious, e.LineNext, e.DeleteBeforeCursor, e.DeleteAfterCursor, e.DeleteCharacterForward, e.Paste, e.TransposeCharacterBackward})
		}
	}
	return contextHelp{short, full}
}

func (m *Model) helpText() string {
	h := help.New()
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
	parts = append(parts, "RECOVERY\nCtrl+C dismisses locally and keeps the draft. Ctrl+X clears it into F5 prompt history. Local exit leaves T3 running. Stop holds the reviewed turn's queue; retries reuse the saved operation ID.")
	if runtime.GOOS == "darwin" {
		parts = append(parts, "⌥ is Option. Chords depend on terminal mapping; the Ghostty launcher uses left Option as Alt. Slash commands remain available regardless of terminal key mapping.")
	}
	return strings.Join(parts, "\n\n")
}
