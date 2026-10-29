package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keyMap()
	// Only deliberate global controls precede the active component.
	switch {
	case key.Matches(msg, k.Stop):
		return m.stop()
	case key.Matches(msg, k.Quit):
		return m.quit()
	case key.Matches(msg, k.Mode):
		return m.setMode("")
	case key.Matches(msg, k.Detail) && m.detail == "":
		return m.dispatch("info", "")
	case key.Matches(msg, k.Help):
		return m.dispatch("help", "")
	case key.Matches(msg, k.Error):
		m.openFeedbackDetail()
		return nil
	case key.Matches(msg, k.Palette):
		return m.openPalette()
	}
	if msg.String() == "ctrl+c" {
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
		if m.currentDraft() != "" {
			m.dismissCompletion()
			m.notice = "Draft kept · Ctrl+X clear · Ctrl+Q exit"
			return nil
		}
		return m.quit()
	}
	if m.detail != "" {
		if key.Matches(msg, k.Copy) {
			return m.copyText("detail")
		}
		if key.Matches(msg, k.Dismiss) {
			m.detail = ""
			m.resize()
			return m.restoreFocus()
		}
		switch msg.String() {
		case "tab", "shift+tab":
			return m.leaveActivityDetail()
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
		// Error detail overlays the current request, never replacing its state.
		if key.Matches(msg, k.Detail) && !m.dialog.details && m.dialog.preservedInitial == "" && (m.problem != "" || m.streamProblem != "") && m.openFeedbackDetail() {
			return nil
		}
		if key.Matches(msg, k.Copy) && (m.dialog.preservedInitial != "" || m.dialog.launch != nil && m.dialog.launch.preservedMessage != "") {
			return m.copyText("answer")
		}
		return m.dialogKey(msg)
	}
	if m.browserPage || m.launchPage {
		if key.Matches(msg, k.Dismiss) {
			if m.cancelBrowser != nil {
				m.cancelBrowser()
			} else if m.cancelAction != nil {
				m.cancelAction()
			}
		}
		return nil
	}
	// Find temporarily owns the keyboard while retaining its invoking pane
	// for Escape. Those pane flags must not route edits behind the search.
	if m.searching {
		if key.Matches(msg, k.Dismiss) {
			m.searching = false
			m.resize()
			return m.restoreFocus()
		}
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
		return m.updateComponent("search", 0, msg)
	}
	if handled, cmd := m.agentboxKey(msg); handled {
		return cmd
	}
	for number := 1; number <= 9; number++ {
		if key.Matches(msg, m.actionBinding(fmt.Sprintf("thread.%d", number))) {
			return m.openVisibleNavigatorNumber(number)
		}
	}
	// Application commands share the resolver before pane-specific movement.
	// Editor-owned bindings are absent from the composer's resolved context.
	switch {
	case key.Matches(msg, k.New):
		return m.dispatch("new", "")
	case key.Matches(msg, k.NewSame):
		return m.dispatch("new-same", "")
	case key.Matches(msg, k.Settings):
		return m.dispatch("settings", "")
	case key.Matches(msg, k.Refresh):
		if m.navigator.focused && m.navigationVisible() && m.mode != "agentbox" {
			return m.navigatorAction("refresh")
		}
		return m.dispatch("refresh", "")
	case key.Matches(msg, k.Filter):
		return m.execute("filter", "")
	case key.Matches(msg, k.FocusNavigation):
		return m.focusNavigator()
	case key.Matches(msg, k.FocusComposer):
		m.navigator.focused, m.contextFocused, m.activityFocused, m.transcriptFocus = false, false, false, false
		return m.resizeWorkspaceFocus()
	case key.Matches(msg, k.Sidebar):
		return m.setting("sidebar", boolWord(!m.sidebar))
	case key.Matches(msg, k.Model):
		return m.execute("model", "")
	case key.Matches(msg, k.Effort):
		return m.execute("effort", "")
	case key.Matches(msg, k.Speed):
		return m.execute("speed", "")
	case key.Matches(msg, k.Requests):
		return m.execute("requests", "")
	}
	if m.mode != "agentbox" && m.navigator.focused && m.navigationVisible() {
		return m.navigatorKey(msg)
	}
	if m.mode != "agentbox" && m.activityFocused {
		return m.activityKey(msg)
	}
	if m.mode != "agentbox" && m.contextFocused && m.contextVisible() {
		return m.contextKey(msg)
	}
	switch {
	case key.Matches(msg, k.Edit):
		return m.editDraft()
	case key.Matches(msg, k.Clear):
		return m.clearDraft()
	case key.Matches(msg, k.Recall):
		return m.openRecall()
	case key.Matches(msg, k.Threads):
		if m.navigationVisible() {
			return m.focusNavigator()
		}
		return m.execute("switch", "")
	case key.Matches(msg, k.Environments):
		return m.execute("env", "")
	case key.Matches(msg, k.Requests):
		return m.execute("requests", "")
	case key.Matches(msg, k.Model):
		return m.execute("model", "")
	case key.Matches(msg, k.Effort):
		return m.execute("effort", "")
	case key.Matches(msg, k.Speed):
		return m.execute("speed", "")
	case key.Matches(msg, k.Parent):
		return m.execute("parent", "")
	case key.Matches(msg, k.Search):
		return m.beginSearch()
	case key.Matches(msg, k.Sidebar):
		return m.setting("sidebar", boolWord(!m.sidebar))
	case key.Matches(msg, k.Earlier):
		return m.execute("earlier", "")
	case key.Matches(msg, k.Latest):
		return m.execute("latest", "")
	case key.Matches(msg, k.Detail):
		return m.dispatch("details", "")
	case key.Matches(msg, k.Copy):
		if m.preservedDraft != "" {
			return m.copyText("draft")
		}
		return m.copyText("")
	case key.Matches(msg, k.Literal):
		return m.sendText(m.currentDraft())
	case key.Matches(msg, k.Steer):
		return m.sendTextWithDelivery(m.currentDraft(), "steer")
	case key.Matches(msg, k.Send):
		return m.send()
	}
	if handled, cmd := m.completionKey(msg); handled {
		return cmd
	}
	if key.Matches(msg, k.Focus) || key.Matches(msg, k.FocusBack) {
		return m.cycleWorkspace(key.Matches(msg, k.Focus))
	}
	if key.Matches(msg, k.Dismiss) {
		m.problem, m.notice = "", ""
		if m.transcriptFocus {
			m.transcriptFocus = false
			return m.restoreFocus()
		}
		return nil
	}
	if m.mode != "agentbox" && m.transcriptFocus {
		switch msg.String() {
		case "end", "G":
			m.viewport.GotoBottom()
			m.follow = true
			return nil
		case "home", "g":
			m.viewport.GotoTop()
			m.follow = false
			return nil
		case "n", "N":
			if len(m.searchLines) > 0 {
				delta := 1
				if msg.String() == "N" {
					delta = -1
				}
				m.searchIndex = (m.searchIndex + delta + len(m.searchLines)) % len(m.searchLines)
				m.refreshSearch(true)
			}
			return nil
		case "ctrl+up":
			m.viewport.ScrollUp(1)
			m.follow = m.viewport.AtBottom()
			return nil
		case "ctrl+down":
			m.viewport.ScrollDown(1)
			m.follow = m.viewport.AtBottom()
			return nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom()
		return cmd
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
	if name != "stop" && !m.durableProblem() {
		m.problem = ""
	}
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
	if name == "send" || name == "switch" || name == "env" || name == "threads" || name == "model" || name == "effort" || name == "speed" || name == "reconnect" || name == "settings" {
		return
	}
	if result.Text != "" {
		m.notice = safeText(result.Text)
	}
	// History position is already visible. Keep explicit result text and
	// unrelated notices without adding a persistent navigation acknowledgment.
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
	name, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	args = strings.TrimSpace(args)
	if err := m.validateCommand(name, args); err != nil {
		m.setInputProblem("composer", 0, err.Error())
		return nil
	}
	if name == "override" && args == "clear" {
		// Clearing a local override does not require model controls.
	} else if reason := m.commandUnavailable(name); reason != "" {
		m.problem = reason
		return nil
	}
	if name == "switch" || name == "threads" {
		cmd, err := m.executeNavigatorCommand(name, args)
		if err != nil {
			m.problem = safeText(err.Error())
			return cmd
		}
		old := m.composer.Value()
		m.preservedDraft = ""
		m.composer.SetValue("")
		m.composerChanged(old)
		return cmd
	}
	local := name == "help" || name == "keys" || name == "theme" || name == "mouse" || name == "sidebar" || name == "search" || name == "copy" || name == "quit" || name == "edit" || name == "clear" || name == "recall" || name == "error" || name == "details" || name == "info" || name == "keyboard" || name == "shortcuts" || name == "mode" || name == "inbox"
	if local {
		old := m.composer.Value()
		m.preservedDraft = ""
		m.composer.SetValue("")
		m.composerChanged(old)
		return m.dispatch(name, args)
	}
	cmd := m.dispatch(name, args)
	if cmd == nil {
		return nil
	}
	// A remote argument or capability rejection leaves the exact command
	// editable. Only its correlated successful result may clear it.
	return func() tea.Msg {
		msg := cmd()
		if result, ok := msg.(actionMsg); ok {
			result.commandDraft = text
			return result
		}
		return msg
	}
}

func (m *Model) dispatch(name, args string) tea.Cmd {
	switch name {
	case "mode":
		return m.setMode(args)
	case "inbox", "file", "file-many", "review":
		return m.agentboxAction(name, args)
	case "refresh":
		if m.mode == "agentbox" {
			return m.loadInbox(true)
		}
		return m.navigatorAction("refresh")
	case "new-same":
		if m.navigator.focused && m.navigationVisible() {
			return m.navigatorAction("new-same")
		}
		return m.execute("new-same", args)
	case "send", "send-literal", "steer":
		if m.dialog != nil || m.detail != "" || m.searching {
			m.notice = "Return to the composer before sending"
			return nil
		}
		if strings.TrimSpace(m.currentDraft()) == "/"+name {
			m.notice = "Write a message, then use its send shortcut or Commands action"
			return nil
		}
		if name == "steer" {
			return m.sendTextWithDelivery(m.currentDraft(), "steer")
		}
		if name == "send-literal" {
			return m.sendText(m.currentDraft())
		}
		return m.send()
	case "shortcuts":
		m.openDetail("Shortcuts", m.shortcutsText())
		return nil
	case "navigator-action":
		return m.navigatorAction(args)
	case "navigator-profile":
		m.navigator.focused = true
		return tea.Batch(m.restoreFocus(), m.loadNavigator(args, "", true))
	case "navigator":
		if args == "" {
			args = boolWord(m.navigationHidden)
		}
		if args != "on" && args != "off" {
			m.problem = "Use /navigator on or off"
			return nil
		}
		return m.setting("navigator", args)
	case "context":
		return m.openContext()
	case "keyboard":
		if args != "modern" && args != "legacy" {
			m.problem = "Use /keyboard modern or /keyboard legacy"
			return nil
		}
		return m.setting("keyboard", args)
	case "use-exact":
		return m.useExactRejectedInput()
	case "settings":
		return m.execute("settings", "")
	case "details", "info":
		if m.dialog != nil {
			if m.dialog.details {
				m.dialog.details = false
				m.questionViewport.SetContent(safeText(m.dialog.description))
				m.resize()
				return m.restoreFocus()
			}
			if m.dialog.preservedInitial == "" && (m.problem != "" || m.streamProblem != "") && m.openFeedbackDetail() {
				return nil
			}
			if m.dialog.requestForm != nil {
				m.saveRequestAnswer()
			}
			m.refreshDialogDetails()
			m.dialog.details = true
			m.questionViewport.SetContent(m.dialog.detailText)
			m.questionViewport.GotoTop()
			m.resize()
			return m.restoreFocus()
		}
		if m.agentboxOwnsKeys() {
			return m.inboxInfo()
		}
		if m.navigator.focused && m.navigationVisible() {
			return m.openNavigatorDetail()
		}
		if m.preservedDraft != "" {
			m.openDetail("Exact draft", m.preservedDraft)
			m.detailCopy = m.preservedDraft
		} else if !m.openFeedbackDetail() {
			m.openDetail("Conversation Info", m.conversationDetailText())
		}
		return nil
	case "quit":
		return m.quit()
	case "help", "keys":
		m.openDetail("Keyboard and commands", m.helpText())
		return nil
	case "stop":
		return m.stop()
	case "theme":
		if args == "" {
			return m.openThemes()
		}
		if !ValidTheme(args) && args != "default" {
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
	case "edit":
		return m.editDraft()
	case "clear":
		return m.clearDraft()
	case "recall":
		return m.recallDraft(args)
	case "error":
		if !m.openFeedbackDetail() {
			m.notice = "No error to review"
		}
		return nil
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
	profile := m.appearanceProfile()
	return func() tea.Msg {
		var err error
		switch kind {
		case "keyboard":
			err = settings.SetLegacyEditorCtrlP(value == "legacy")
		case "theme":
			if profile != "" {
				err = settings.SetEnvironmentTheme(profile, value)
			} else {
				err = settings.SetTheme(value)
			}
		case "mouse":
			err = settings.SetMouse(value == "on")
		case "navigator":
			err = settings.SetNavigationHidden(value == "off")
		case "sidebar":
			err = settings.SetSidebar(value == "on")
		}
		return settingMsg{kind, value, err}
	}
}

func (m *Model) copyText(which string) tea.Cmd {
	if m.snapshot == nil && which != "draft" && which != "answer" && which != "error" && which != "detail" {
		m.notice = "No transcript to copy"
		return nil
	}
	var text string
	switch which {
	case "error":
		text = m.feedbackDetailText()
	case "detail":
		text = m.detailCopyText()
	case "answer":
		if m.dialog != nil && m.dialog.requestForm != nil {
			m.saveRequestAnswer()
			f := m.dialog.requestForm
			answer := f.answers[f.questions[f.index].ID]
			switch value := answer.(type) {
			case string:
				text = value
			case []string:
				text = strings.Join(value, "\n")
			}
			break
		}
		if m.dialog != nil && m.dialog.launch != nil {
			text = m.launchInput().Message
			break
		}
		if m.dialog != nil && m.dialog.kind == client.EnterText && m.dialog.preservedInitial == "" {
			text = m.modalInput.Value()
			break
		}
		if m.dialog == nil || m.dialog.preservedInitial == "" {
			m.notice = "No current answer to copy"
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
			part := safeText(row.Kind) + "\n" + safeText(row.Text)
			if row.Truncated {
				part += "\n[Shortened by T3oxide]"
			}
			parts = append(parts, part)
		}
		text = "[Loaded page only; bounded history]\n\n" + strings.Join(parts, "\n\n")
	default:
		m.problem = "Use /copy [page|draft|answer|error|detail]"
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
func (m *Model) detailCopyText() string {
	if m.detail == "" {
		return ""
	}
	if m.detailCopy != "" {
		return m.detailCopy
	}
	return redactFeedback(m.detail)
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
			return p.highlight()
		}
		return lipgloss.NewStyle()
	}
	m.notice = fmt.Sprintf("Find %d/%d matching lines · current page only", m.searchIndex+1, len(m.searchLines))
}
