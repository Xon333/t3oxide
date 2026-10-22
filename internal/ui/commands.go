package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+s":
		return m.stop()
	case "ctrl+d":
		return m.quit()
	case "f1":
		return m.dispatch("help", "")
	case "ctrl+y":
		if m.dialog != nil && m.dialog.preservedInitial != "" {
			return m.copyText("answer")
		}
	case "ctrl+c":
		if m.detail != "" {
			m.detail = ""
			m.resize()
			return m.restoreFocus()
		}
		if m.dialog != nil {
			return m.cancelDialog()
		}
		if m.searching {
			m.searching = false
			m.resize()
			return m.restoreFocus()
		}
		if m.preservedDraft != "" {
			m.preservedDraft = ""
			m.composer.Placeholder = "Message T3 · Ctrl+P commands"
			m.draftEdits++
			m.problem = ""
			m.notice = "Saved draft cleared locally"
			if m.session != nil {
				if err := m.session.SetDraft(""); err != nil {
					m.problem = "Draft save: " + safeText(err.Error())
				}
			}
			return m.restoreFocus()
		}
		if m.composer.Value() != "" {
			old := m.composer.Value()
			m.composer.SetValue("")
			m.composerChanged(old)
			m.notice = "Draft cleared · Ctrl+D exits"
			return nil
		}
		return m.quit()
	case "alt+m":
		return m.execute("model", "")
	case "alt+e":
		return m.execute("effort", "")
	case "alt+s":
		return m.execute("speed", "")
	case "alt+up":
		if m.dialog == nil {
			return m.execute("parent", "")
		}
	case "ctrl+t":
		return m.execute("switch", "")
	case "ctrl+e":
		return m.execute("env", "")
	case "ctrl+p":
		return m.openPalette()
	case "ctrl+r":
		return m.execute("requests", "")
	}
	if m.detail != "" {
		switch msg.String() {
		case "esc", "q":
			m.detail = ""
			m.resize()
			return m.restoreFocus()
		}
		var cmd tea.Cmd
		m.detailViewport, cmd = m.detailViewport.Update(msg)
		return cmd
	}
	if m.dialog != nil {
		return m.dialogKey(msg)
	}
	if m.searching {
		switch msg.String() {
		case "esc":
			m.searching = false
			m.resize()
			return m.restoreFocus()
		case "enter":
			value := strings.TrimSpace(m.searchInput.Value())
			if value == m.searchTerm && len(m.searchLines) > 0 {
				m.searchIndex = (m.searchIndex + 1) % len(m.searchLines)
			} else {
				m.searchIndex = -1
			}
			m.searchTerm = value
			m.refreshSearch(true)
			m.searching = false
			m.resize()
			return m.restoreFocus()
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m.componentCmd("search", 0, cmd)
	}
	switch msg.String() {
	case "ctrl+f":
		return m.beginSearch()
	case "ctrl+b":
		return m.setting("sidebar", boolWord(!m.sidebar))
	case "ctrl+y":
		if m.preservedDraft != "" {
			return m.copyText("draft")
		}
		return m.copyText("")
	case "f2":
		if m.preservedDraft != "" {
			m.openDetail("Preserved draft · Ctrl+Y copy after closing", m.preservedDraft)
		}
		return nil
	case "alt+left":
		return m.execute("earlier", "")
	case "alt+right":
		return m.execute("latest", "")
	case "tab":
		m.transcriptFocus = !m.transcriptFocus
		return m.restoreFocus()
	case "esc":
		m.problem = ""
		m.notice = ""
		if m.transcriptFocus {
			m.transcriptFocus = false
			return m.restoreFocus()
		}
		return nil
	case "pgup", "pgdown", "ctrl+up", "ctrl+down":
		if msg.String() == "ctrl+up" {
			m.viewport.ScrollUp(1)
		} else if msg.String() == "ctrl+down" {
			m.viewport.ScrollDown(1)
		} else {
			m.viewport, _ = m.viewport.Update(msg)
		}
		m.follow = m.viewport.AtBottom()
		return nil
	}
	if m.transcriptFocus {
		switch msg.String() {
		case "end", "G":
			m.viewport.GotoBottom()
			m.follow = true
			return nil
		case "home", "g":
			m.viewport.GotoTop()
			m.follow = false
			return nil
		case "n":
			if len(m.searchLines) > 0 {
				m.searchIndex = (m.searchIndex + 1) % len(m.searchLines)
				m.refreshSearch(true)
			}
			return nil
		case "N":
			if len(m.searchLines) > 0 {
				m.searchIndex = (m.searchIndex - 1 + len(m.searchLines)) % len(m.searchLines)
				m.refreshSearch(true)
			}
			return nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom()
		return cmd
	}
	if msg.String() == "enter" {
		return m.send()
	}
	return m.updateComponent("composer", 0, msg)
}

func boolWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// Progress belongs to the busy indicator, separately from command feedback.
func commandProgress(name string) string {
	switch name {
	case "send":
		return "Sending · waiting for T3 acknowledgment"
	case "stop":
		return "Stop · waiting for T3 acknowledgment"
	case "earlier":
		return "Loading earlier history"
	case "latest":
		return "Returning to latest"
	case "switch", "env", "threads", "filter", "parent":
		return "Choosing conversation · /" + name
	case "model", "effort", "speed", "presets", "override":
		return "Model settings · /" + name
	case "requests":
		return "Reviewing T3 requests"
	case "reconnect":
		return "Requesting reconnect"
	case "":
		return "Waiting for T3 acknowledgment"
	default:
		return "Command pending · /" + name
	}
}

func (m *Model) beginCommandNotice(name string) {
	m.problem = ""
	// Stop has its own lane; admitting it must not erase ordinary action feedback.
	if name != "stop" {
		m.notice = ""
	}
}

// Call only after checking session generation and the ordinary action ID.
// Empty results can mean a dismissed picker or a declined local resolution;
// they are not evidence that a setting changed or a request was answered.
func (m *Model) finishCommandNotice(name string, result client.Result, err error) {
	if commandOwnsNotice(name, m.notice) {
		m.notice = ""
	}
	if err != nil {
		return
	}
	if result.Text != "" {
		m.notice = safeText(result.Text)
		return
	}
	if m.notice != "" {
		return
	}
	// Navigation's request acknowledgment precedes the rendered snapshot.
	switch name {
	case "earlier":
		m.notice = "Earlier navigation acknowledged"
	case "latest":
		m.notice = "Latest navigation acknowledged"
	}
}

func commandOwnsNotice(name, notice string) bool {
	if notice == "" {
		return false
	}
	if name == "stop" {
		return notice == commandProgress(name) || notice == "Stop is already awaiting acknowledgment"
	}
	return notice == commandProgress(name) || notice == "Working · /"+name ||
		notice == "An action is pending; Stop remains available" ||
		notice == "Awaiting the current action; your draft is preserved"
}

func (m *Model) slash(text string) tea.Cmd {
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if trimmed == "" {
		old := m.composer.Value()
		m.composer.SetValue("")
		m.composerChanged(old)
		return m.openPalette()
	}
	name, args, _ := strings.Cut(trimmed, " ")
	args = strings.TrimSpace(args)
	known := name == "help" || name == "keys" || name == "quit" || name == "theme" || name == "mouse" || name == "search" || name == "copy" || name == "sidebar"
	if m.session != nil {
		for _, cmd := range m.session.Commands() {
			if cmd.Name == name {
				known = true
				break
			}
		}
	}
	if !known {
		m.problem = "Unknown command /" + name + " · Ctrl+P lists supported actions"
		return nil
	}
	old := m.composer.Value()
	m.composer.SetValue("")
	m.composerChanged(old)
	return m.dispatch(name, args)
}
func (m *Model) dispatch(name, args string) tea.Cmd {
	switch name {
	case "quit":
		return m.quit()
	case "help", "keys":
		m.openDetail("Keyboard and commands", helpText)
		return nil
	case "stop":
		return m.stop()
	case "theme":
		if args == "" {
			return m.openThemes()
		}
		if !ValidTheme(args) {
			m.problem = "Choose /theme terminal, /theme violet, /theme cyan or /theme lime"
			return nil
		}
		return m.setting("theme", args)
	case "mouse":
		if args == "" {
			args = boolWord(!m.mouse)
		}
		if args != "on" && args != "off" {
			m.problem = "Use /mouse on or /mouse off"
			return nil
		}
		return m.setting("mouse", args)
	case "sidebar":
		if args == "" {
			args = boolWord(!m.sidebar)
		}
		if args != "on" && args != "off" {
			m.problem = "Use /sidebar on or /sidebar off"
			return nil
		}
		return m.setting("sidebar", args)
	case "search":
		if args == "" {
			return m.beginSearch()
		}
		if args == m.searchTerm && len(m.searchLines) > 0 {
			m.searchIndex = (m.searchIndex + 1) % len(m.searchLines)
		} else {
			m.searchIndex = -1
		}
		m.searchTerm = args
		m.refreshSearch(true)
		return nil
	case "copy":
		return m.copyText(args)
	default:
		return m.execute(name, args)
	}
}

func (m *Model) setting(kind, value string) tea.Cmd {
	if m.settingBusy {
		m.notice = "Finishing the previous appearance change"
		return nil
	}
	m.settingBusy = true
	settings := m.settings
	return func() tea.Msg {
		var err error
		switch kind {
		case "theme":
			err = settings.SetTheme(value)
		case "mouse":
			err = settings.SetMouse(value == "on")
		case "sidebar":
			err = settings.SetSidebar(value == "on")
		}
		return settingMsg{kind, value, err}
	}
}

func (m *Model) copyText(which string) tea.Cmd {
	if m.snapshot == nil && which != "draft" && which != "answer" {
		m.notice = "No transcript to copy"
		return nil
	}
	var text string
	switch which {
	case "answer":
		if m.dialog == nil || m.dialog.preservedInitial == "" {
			m.notice = "No preserved initial answer to copy"
			return nil
		}
		text = m.dialog.preservedInitial
	case "draft":
		text = m.currentDraft()
	case "":
		for i := len(m.snapshot.Items) - 1; i >= 0; i-- {
			row := m.snapshot.Items[i]
			if row.Kind == "assistant_message" || row.Kind == "assistant" {
				text = safeText(row.Text)
				if row.Truncated {
					text += "\n[Shortened by T3oxide]"
				}
				break
			}
		}
	case "page":
		var parts []string
		for _, row := range m.snapshot.Items {
			parts = append(parts, safeText(row.Kind)+"\n"+safeText(row.Text))
		}
		text = strings.Join(parts, "\n\n")
	default:
		m.problem = "Use /copy, /copy page or /copy draft"
		return nil
	}
	if text == "" {
		m.notice = "No text to copy"
		return nil
	}
	if len(text) > 256*1024 {
		m.problem = "Page exceeds clipboard limit; use native selection with /mouse off"
		return nil
	}
	ctx := m.ctx
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		err := nativeClipboard(ctx, text)
		return copyResultMsg{err}
	})
}
func nativeClipboard(parent context.Context, text string) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "linux":
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	default:
		return errors.New("no native helper on this OS")
	}
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, path, candidate[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err = cmd.Run(); err != nil {
			return err
		}
		return nil
	}
	return errors.New("no native helper; terminal OSC 52 support is required")
}

func (m *Model) highlightSearchLine() {
	if m.searchIndex < 0 || m.searchIndex >= len(m.searchLines) {
		m.viewport.StyleLineFunc = nil
		return
	}
	line := m.searchLines[m.searchIndex]
	p := colors(m.theme)
	m.viewport.StyleLineFunc = func(i int) lipgloss.Style {
		if i == line {
			return p.highlight().Underline(true)
		}
		return lipgloss.NewStyle()
	}
	m.notice = fmt.Sprintf("Find %d/%d matching lines · current page only", m.searchIndex+1, len(m.searchLines))
}

const helpText = `START HERE
F1 /help /keys  Keyboard and command reference
Ctrl+P          Type to filter commands; request help inside a dialog
Ctrl+T /switch  Choose a grouped conversation
Ctrl+E /env     Choose an environment

WRITE AND REVIEW
Enter           Submit one human-authored message
Ctrl+J          Newline; Alt+Enter or Shift+Enter also work
/delivery MODE  auto, queue or steer for subsequent sends
Ctrl+R /requests Review exact T3 approvals and questions
Ctrl+S /stop    Stop the reviewed active turn + hold queue
/new [TITLE]    New conversation in this existing workspace
Ctrl+C          Dismiss dialog/detail/search; clear draft; quit if empty
Ctrl+D /quit    Detach locally; T3 continues running

CONVERSATIONS AND HISTORY
/threads WORDS  Filter thread titles/projects on the host
Space / Right  Expand subagents in chooser; Left collapses
F6 /filter     All / Main / Subagents / Forks in chooser
Ctrl+F         Host search in chooser, including hidden children
Alt+Up /parent Open the authoritative parent conversation
Alt+Left /earlier  Replace page with history; pause live detail
Alt+Right /latest  Resume the current live page

READ AND COPY
Tab            Switch composer/transcript focus
PgUp / PgDown  Scroll transcript; Ctrl+Up/Down scroll one line
Home / g       Top of page with transcript focus
End / G        Bottom of page; resume following with transcript focus
Ctrl+F /search Find in displayed page; repeat for next match
n / N          Next/previous match with transcript focus
Ctrl+Y /copy   Copy last assistant message
/copy page     Copy displayed page; /copy draft copies complete draft
F2             Review a preserved draft; Ctrl+Y copies it
Escape         Close detail/search, clear feedback, or focus composer

MODEL AND NEXT MESSAGE
Alt+M /model   Current thread model (default scope)
Alt+E /effort  Reasoning effort advertised by this model
Alt+S /speed   Advertised service tier / fast mode
/presets       Apply/save/remove model + effort + speed favorites
/override      Inspect/stage next-message selection; /override clear
/model next    Stage one dispatch without pre-setting the thread
/effort next   Staged effort; /speed next stages speed
/runtime       Saved settings vs run request vs provider evidence
T3 may adopt a dispatched override as the thread preference.
No hidden restore or automatic retry; missing telemetry is unreported.

REQUEST DIALOGS
Enter          Select/answer; Space toggles multiple choice
Ctrl+Enter     Confirm multiple selection, or choose Continue
Ctrl+Up/Down   Scroll the complete request description
F2             Full question and selected-option details
Ctrl+Y         Copy a preserved initial answer
Ctrl+U         Replace a preserved, uneditable initial answer
Escape         Dismiss locally without answering T3
Ctrl+S         Stop remains available inside dialogs

APPEARANCE AND RECOVERY
/theme NAME    terminal, violet, cyan or lime; /theme opens chooser
/mouse on|off  Mouse scrolling or native terminal selection
Ctrl+B /sidebar Toggle optional detail sidebar on wide terminals
/pending       Review saved operation IDs and original payloads
/retry [stop]  Explicit same-ID retry in the original lane
/resolve [stop] Clear local record only after reviewing T3
/reconnect     Reconnect reads; never replay saved writes
/web           Display this environment's configured full T3 address

Drafts survive switches and uncertain acknowledgments. Local quit or
cancellation never means remote Stop. Full tools, attachments, provider
configuration and broader orchestration remain in full T3.`
