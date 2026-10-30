package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// AgentBox is another presentation of native threads, not a task store or
// execution owner. Only the selected thread has the ordinary live attachment.
type agentboxState struct {
	page                                                 client.InboxPage
	prefs                                                config.InboxPreferences
	rows                                                 []client.InboxThread
	selected                                             string
	view                                                 string
	project, environment                                 string
	minimumPriority                                      int
	query                                                textinput.Model
	searching, loaded, loading, decision, reply, summary bool
	serverSearch                                         bool
	readQuery                                            string
	serial                                               uint64
	cancel                                               context.CancelFunc
	cursors                                              map[string]int
	error                                                string
	review                                               *wire.ThreadReview
	reviewTarget                                         client.InboxTarget
	reviewLoading                                        bool
	reviewSerial                                         uint64
	offer                                                *client.InboxTarget
	undo                                                 *inboxUndo
	rankBusy                                             bool
	followLaunch                                         bool
	pendingSources                                       map[string]bool
	ack                                                  *inboxAcknowledgement
	reorderNow                                           bool
}

type inboxUndo struct {
	target        client.InboxTarget
	action, until string
	preferences   *config.InboxPreferences
	expected      uint64
	label         string
}

type inboxMsg struct {
	serial   uint64
	page     client.InboxPage
	prefs    config.InboxPreferences
	err      error
	prefsErr error
}
type inboxSourceMsg struct {
	serial  uint64
	source  client.InboxSource
	updates <-chan client.InboxSource
	ctx     context.Context
}
type inboxReviewMsg struct {
	generation, serial uint64
	target             client.InboxTarget
	review             wire.ThreadReview
	err                error
	title              string
}
type inboxMutationMsg struct {
	action   actionMsg
	target   client.InboxTarget
	receipt  client.InboxActionReceipt
	previous *inboxUndo
	undo     bool
	order    []string
}
type inboxRankMsg struct {
	previous config.InboxPreferences
	next     config.InboxPreferences
	err      error
	undo     bool
}

func (m *Model) initAgentBox() {
	m.mode = m.settings.workspaceMode()
	m.agentbox.view = "needs"
	m.agentbox.summary = true
	m.agentbox.cursors = map[string]int{}
	m.agentbox.query = textinput.New()
	m.agentbox.query.Prompt = ""
	m.agentbox.query.Placeholder = "Search task, project or source"
	m.agentbox.query.CharLimit = 256
	m.agentbox.query.SetVirtualCursor(true)
	m.agentbox.prefs = config.InboxPreferences{Version: 1, Tasks: map[string]int{}, Projects: map[string]int{}}
}

// setMode leaves the actual editors, selection, attachment, prompts, anchors
// and receipts in their current owners. No session is created or detached.
func (m *Model) setMode(mode string) tea.Cmd {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		if m.mode == "agentbox" {
			mode = "developer"
		} else {
			mode = "agentbox"
		}
	}
	if mode != "developer" && mode != "agentbox" {
		m.problem = "Use /mode developer or /mode agentbox"
		return nil
	}
	if err := m.settings.SetMode(mode); err != nil {
		m.problem = "Save mode: " + safeText(err.Error())
		return nil
	}
	m.mode = mode
	m.layoutDirty = true
	m.resize()
	render := m.requestRender(false, false)
	if mode == "agentbox" {
		return tea.Batch(m.loadInbox(true), render, m.restoreFocus())
	}
	return tea.Batch(m.loadNavigator("", "", false), render, m.restoreFocus())
}

func (m *Model) loadInbox(force bool) tea.Cmd {
	if m.session == nil {
		return nil
	}
	if m.agentbox.loading && !force {
		return nil
	}
	if m.agentbox.cancel != nil {
		m.agentbox.cancel()
	}
	m.agentbox.serial++
	serial, session := m.agentbox.serial, m.session
	query := m.agentbox.query.Value()
	serverSearch := m.agentbox.serverSearch
	if serverSearch {
		query = strings.TrimSpace(query)
	}
	m.agentbox.readQuery = query
	cursors := map[string]int{}
	for id, cursor := range m.agentbox.cursors {
		cursors[id] = cursor
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.agentbox.cancel = cancel
	m.agentbox.loading = true
	m.agentbox.error = ""
	m.agentbox.pendingSources = map[string]bool{}
	for _, source := range session.InboxSources() {
		m.agentbox.pendingSources[source.ProfileID] = true
	}
	updates := make(chan client.InboxSource, 8)
	work := func() tea.Msg {
		defer cancel()
		defer close(updates)
		publish := func(source client.InboxSource) {
			select {
			case updates <- source:
			case <-ctx.Done():
			}
		}
		var page client.InboxPage
		var err error
		if serverSearch {
			page, err = session.InboxSearchProgress(ctx, query, publish)
		} else {
			page, err = session.InboxReadProgress(ctx, query, cursors, publish)
		}
		prefs, prefErr := session.InboxPreferences()
		return inboxMsg{serial: serial, page: page, prefs: prefs, err: err, prefsErr: prefErr}
	}
	return tea.Batch(work, waitInboxSource(ctx, serial, updates))
}

func waitInboxSource(ctx context.Context, serial uint64, updates <-chan client.InboxSource) tea.Cmd {
	return func() tea.Msg {
		select {
		case source, ok := <-updates:
			if ok {
				return inboxSourceMsg{serial: serial, source: source, updates: updates, ctx: ctx}
			}
		case <-ctx.Done():
		}
		return nil
	}
}

func (m *Model) inboxSourceLoaded(msg inboxSourceMsg) tea.Cmd {
	if msg.serial != m.agentbox.serial || !m.agentbox.loading {
		return nil
	}
	delete(m.agentbox.pendingSources, msg.source.ProfileID)
	m.agentbox.page = client.MergeInboxSource(m.agentbox.page, msg.source)
	m.agentbox.loaded = true
	m.filterInbox()
	return tea.Batch(m.restoreFocus(), waitInboxSource(msg.ctx, msg.serial, msg.updates))
}

func (m *Model) inboxSourceLoading(profile string) bool { return m.agentbox.pendingSources[profile] }

func (m *Model) inboxLoaded(msg inboxMsg) tea.Cmd {
	if msg.serial != m.agentbox.serial {
		return nil
	}
	m.agentbox.loading = false
	m.agentbox.pendingSources = nil
	m.agentbox.cancel = nil
	if msg.err != nil {
		m.agentbox.error = safeText(msg.err.Error())
		return nil
	}
	m.agentbox.page = msg.page
	if msg.prefsErr != nil {
		m.agentbox.error = "Presentation priorities: " + safeText(msg.prefsErr.Error())
	} else {
		m.agentbox.prefs = msg.prefs
	}
	m.agentbox.loaded = true
	if m.agentbox.followLaunch {
		for _, row := range msg.page.Rows {
			if m.inboxAttached(row) {
				m.agentbox.selected = row.Target.Key()
				m.agentbox.view = client.InboxBucket(row, time.Now())
				m.agentbox.decision = true
				m.agentbox.followLaunch = false
				break
			}
		}
	}
	m.filterInbox()
	return m.restoreFocus()
}

func (m *Model) filterInbox() {
	a := &m.agentbox
	previous := a.rows
	query := strings.ToLower(strings.TrimSpace(a.query.Value()))
	a.rows = nil
	for _, row := range a.page.Rows {
		bucket := client.InboxBucket(row, time.Now())
		// Keep the exact row owning the live reply visible while its lifecycle
		// changes. Leaving reply reapplies the filter; no editor is retargeted.
		keepReply := (a.reply || a.ack != nil && a.ack.row.Target.Key() == row.Target.Key()) && a.selected == row.Target.Key() && m.inboxAttached(row)
		if !keepReply && a.view != "all" && a.view != bucket {
			continue
		}
		if a.environment != "" && row.Target.ProfileID != a.environment {
			continue
		}
		if a.project != "" && config.InboxIdentity(row.Target.EnvironmentID, row.Target.ProjectID, "") != a.project {
			continue
		}
		if client.InboxPriority(a.prefs, row.Target) < a.minimumPriority {
			continue
		}
		if !keepReply && !a.serverSearch && query != "" && !strings.Contains(strings.ToLower(row.Thread.Title+" "+row.Thread.Project+" "+row.Environment+" "+row.Target.ProfileID), query) {
			continue
		}
		a.rows = append(a.rows, row)
	}
	client.InboxRank(a.rows, a.prefs)
	if a.ack != nil {
		found := false
		for _, row := range a.rows {
			if row.Target.Key() == a.ack.row.Target.Key() {
				a.ack.row = row
				found = true
				break
			}
		}
		if !found && a.selected == a.ack.row.Target.Key() {
			a.rows = append(a.rows, a.ack.row)
		}
	}
	if !a.reorderNow && m.mode == "agentbox" {
		a.rows = freezeInboxRows(previous, a.rows)
	}
	if !a.loading {
		a.reorderNow = false
	}
	for _, row := range a.rows {
		if row.Target.Key() == a.selected {
			return
		}
	}
	if (a.loading || a.reply) && a.selected != "" {
		return
	}
	if len(a.rows) > 0 {
		a.selected = a.rows[0].Target.Key()
	} else {
		a.selected = ""
		a.decision = false
	}
}

// Observe only native lifecycle/attachment transitions. Streaming text does
// not create a shell poll; other hosts remain bounded explicit refreshes.
func (m *Model) inboxObserve(previous client.Status) tea.Cmd {
	if m.mode != "agentbox" || m.session == nil {
		return nil
	}
	current := m.status
	if current.ThreadID != previous.ThreadID || current.EnvironmentID != previous.EnvironmentID ||
		current.ActiveRunID != previous.ActiveRunID || current.RunState != previous.RunState ||
		current.Requests != previous.Requests || current.Connected && !previous.Connected {
		return m.loadInbox(false)
	}
	return nil
}

func (m *Model) selectedInbox() (client.InboxThread, bool) {
	for _, row := range m.agentbox.rows {
		if row.Target.Key() == m.agentbox.selected {
			return row, true
		}
	}
	return client.InboxThread{}, false
}

func (m *Model) inboxIndex() int {
	for i, row := range m.agentbox.rows {
		if row.Target.Key() == m.agentbox.selected {
			return i
		}
	}
	return -1
}

func (m *Model) stepInbox(delta int) tea.Cmd {
	if len(m.agentbox.rows) == 0 {
		return nil
	}
	index := max(0, min(len(m.agentbox.rows)-1, m.inboxIndex()+delta))
	m.agentbox.selected = m.agentbox.rows[index].Target.Key()
	m.agentbox.review = nil
	m.agentbox.decision = false
	m.agentbox.ack = nil
	return nil
}

func (m *Model) inboxAttached(row client.InboxThread) bool {
	return row.Target.ProfileID == m.status.ProfileID && row.Target.EnvironmentID == m.status.EnvironmentID && row.Target.ThreadID == m.status.ThreadID && row.Target.ProjectID == m.status.Location.ProjectID
}

func (m *Model) openInboxThread(reply bool) tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok || m.inboxSourceLoading(row.Target.ProfileID) {
		return nil
	}
	m.agentbox.decision = true
	m.agentbox.reply = reply
	m.agentbox.review = nil
	if m.inboxAttached(row) {
		return m.restoreFocus()
	}
	page, key, session := m.agentbox.page, row.Target.Key(), m.session
	return m.navigatorRun("inbox-open", func(ctx context.Context) (client.Result, error) {
		next, err := session.InboxOpen(page, key)
		return client.Result{Next: next}, err
	})
}

func (m *Model) agentboxOwnsKeys() bool {
	return m.mode == "agentbox" && m.dialog == nil && m.detail == "" && !m.searching && !m.browserPage && !m.launchPage && !m.agentbox.reply
}

func (m *Model) agentboxKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.mode != "agentbox" {
		return false, nil
	}
	if m.agentbox.reply {
		if key.Matches(msg, m.actionBinding("dismiss")) {
			m.agentbox.reply = false
			m.filterInbox()
			return true, m.restoreFocus()
		}
		if !m.completionVisible() && key.Matches(msg, m.actionBinding("focus-next"), m.actionBinding("focus-previous")) {
			m.agentbox.reply = false
			m.filterInbox()
			return true, m.restoreFocus()
		}
		return false, nil
	}
	if !m.agentboxOwnsKeys() {
		return false, nil
	}
	if m.agentbox.searching {
		switch {
		case key.Matches(msg, m.actionBinding("dismiss")):
			m.agentbox.searching = false
			return true, m.restoreFocus()
		case msg.String() == "enter":
			if m.inboxServerSearchOffered() {
				return true, m.searchInboxServer()
			}
			m.agentbox.searching = false
			m.agentbox.cursors = map[string]int{}
			return true, tea.Batch(m.loadInbox(true), m.restoreFocus())
		}
		return true, m.updateComponent("agentbox-search", m.agentbox.serial, msg)
	}
	if msg.String() == "enter" && m.inboxServerSearchOffered() {
		return true, m.searchInboxServer()
	}
	for _, item := range []struct{ id, action string }{
		{"agentbox.previous", "previous"}, {"agentbox.next", "next"}, {"agentbox.open", "open"}, {"agentbox.reply", "reply"}, {"agentbox.file", "file"}, {"agentbox.review", "mark-reviewed"}, {"agentbox.later", "later-menu"}, {"agentbox.summary", "summary"}, {"agentbox.undo", "undo"}, {"agentbox.inbox", "back"}, {"agentbox.view", "views"}, {"agentbox.priority", "priority"}, {"agentbox.diff", "diff"}, {"agentbox.developer", "developer"},
	} {
		if key.Matches(msg, m.actionBinding(item.id)) {
			return true, m.agentboxAction("inbox", item.action)
		}
	}
	if key.Matches(msg, m.actionBinding("dismiss")) {
		return true, m.agentboxAction("inbox", "back")
	}
	if key.Matches(msg, m.actionBinding("agentbox.search")) {
		m.agentbox.searching = true
		return true, m.componentCmd("agentbox-search", m.agentbox.serial, m.agentbox.query.Focus())
	}
	if key.Matches(msg, m.actionBinding("agentbox.focus-next"), m.actionBinding("agentbox.focus-previous")) {
		m.agentbox.decision = !m.agentbox.decision
		return true, m.restoreFocus()
	}
	for index := 1; index <= 3; index++ {
		if m.agentbox.decision && key.Matches(msg, m.actionBinding(fmt.Sprintf("agentbox.choice-%d", index))) {
			return true, m.inboxOfferedChoice(index)
		}
	}
	if key.Matches(msg, m.actionBinding("agentbox.page-next")) {
		return true, m.stepInbox(max(1, m.height-7))
	}
	if key.Matches(msg, m.actionBinding("agentbox.page-previous")) {
		return true, m.stepInbox(-max(1, m.height-7))
	}
	if key.Matches(msg, m.actionBinding("agentbox.refresh")) {
		return true, m.loadInbox(true)
	}

	return true, nil
}

func (m *Model) agentboxAction(name, args string) tea.Cmd {
	if name == "file-many" {
		return m.fileInboxMany()
	}
	if name == "file" || args == "file" {
		return m.fileInboxWork()
	}
	if name == "review" {
		if m.status.Requests > 0 {
			return m.execute("requests", "")
		}
		if m.mode != "agentbox" {
			target := client.InboxTarget{ProfileID: m.status.ProfileID, EnvironmentID: m.status.EnvironmentID, ProjectID: m.status.Location.ProjectID, ThreadID: m.status.ThreadID}
			return m.loadAttachedReview(target, m.status.Location.Title)
		}
		return m.loadInboxReview()
	}
	if m.mode != "agentbox" {
		start := m.setMode("agentbox")
		if m.mode != "agentbox" {
			return start
		}
		return tea.Batch(start, m.agentboxAction(name, args))
	}
	if args == "" {
		m.agentbox.reply = false
		m.agentbox.decision = false
		return tea.Batch(m.loadInbox(true), m.restoreFocus())
	}
	if args == "needs-you" {
		args = "needs"
	}
	if args == "reviewed" {
		args = "done"
	}
	switch args {
	case "needs", "working", "queued", "later", "later-view", "done", "all", "unknown":
		m.agentbox.view = strings.TrimSuffix(args, "-view")
		m.agentbox.ack = nil
		m.agentbox.reorderNow = true
		m.agentbox.reply = false
		m.agentbox.decision = false
		m.filterInbox()
		return m.restoreFocus()
	case "previous":
		return m.stepInbox(-1)
	case "next":
		return m.stepInbox(1)
	case "open":
		return m.openInboxThread(false)
	case "reply":
		return m.openInboxThread(true)
	case "back":
		m.agentbox.ack = nil
		m.agentbox.reorderNow = true
		m.agentbox.reply = false
		m.agentbox.decision = false
		m.filterInbox()
		return m.restoreFocus()
	case "summary":
		m.agentbox.summary = !m.agentbox.summary
		return nil
	case "views":
		return m.inboxViews()
	case "refresh":
		m.agentbox.reorderNow = true
		return m.loadInbox(true)
	case "next-acknowledged":
		return m.nextAcknowledgedInbox()
	case "diff":
		return m.loadInboxReview()
	case "mark-reviewed":
		return m.markInboxReviewed()
	case "later-menu":
		return m.inboxLaterMenu()
	case "priority":
		return m.inboxPriorityMenu()
	case "undo":
		return m.undoInbox()
	case "developer":
		return m.setMode("developer")
	case "requests":
		row, ok := m.selectedInbox()
		if !ok || !m.inboxAttached(row) {
			m.notice = "Open this thread before reviewing its requests"
			return nil
		}
		return m.execute("requests", "")
	case "filters":
		return m.inboxFilters()
	case "pages":
		return m.inboxPages()
	case "info":
		return m.inboxInfo()
	case "server-search":
		return m.searchInboxServer()
	}
	if value, ok := strings.CutPrefix(args, "priority:"); ok {
		n, err := strconv.Atoi(value)
		if err == nil {
			return m.setInboxRank(n, false)
		}
	}
	if value, ok := strings.CutPrefix(args, "project-rank:"); ok {
		n, err := strconv.Atoi(value)
		if err == nil {
			return m.setInboxRank(n, true)
		}
	}
	if value, ok := strings.CutPrefix(args, "until:"); ok {
		return m.mutateInbox("snooze", value, false)
	}
	if args == "settle" || args == "unsnooze" || args == "unsettle" {
		return m.mutateInbox(args, "", false)
	}
	if value, ok := strings.CutPrefix(args, "environment:"); ok {
		m.agentbox.environment = value
		m.filterInbox()
		return nil
	}
	if value, ok := strings.CutPrefix(args, "project:"); ok {
		m.agentbox.project = value
		m.filterInbox()
		return nil
	}
	if value, ok := strings.CutPrefix(args, "minimum:"); ok {
		n, err := strconv.Atoi(value)
		if err == nil && n >= 0 && n <= 3 {
			m.agentbox.minimumPriority = n
			m.filterInbox()
		}
		return nil
	}
	if value, ok := strings.CutPrefix(args, "page:"); ok {
		id, cursor, found := strings.Cut(value, "\x1f")
		if !found {
			return nil
		}
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return nil
		}
		for _, source := range m.agentbox.page.Sources {
			if source.ProfileID == id && (n == 0 || source.Page.NextCursor != nil && n == *source.Page.NextCursor || n == max(0, source.Page.Cursor-max(1, source.Page.Limit))) {
				m.agentbox.cursors[id] = n
				return m.loadInbox(true)
			}
		}
	}
	m.problem = "Unknown inbox action"
	return nil
}

func (m *Model) inboxAdoptLaunch(name string) {
	if m.mode != "agentbox" || name != "file" && name != "file-many" && name != "retry-launch" {
		return
	}
	m.agentbox.followLaunch = true
	m.agentbox.reply = false
	m.agentbox.query.SetValue("")
	m.agentbox.serverSearch = false
	m.agentbox.environment, m.agentbox.project = "", ""
	m.agentbox.minimumPriority = 0
	m.agentbox.cursors = map[string]int{}
}

func (m *Model) fileInboxWork() tea.Cmd {
	if m.session == nil {
		return nil
	}
	if m.actionBusy {
		m.notice = "An action is pending; Stop remains available"
		return nil
	}
	m.agentbox.offer = nil
	session := m.session
	return m.navigatorRun("file", func(ctx context.Context) (client.Result, error) { return session.FileWork(ctx, "") })
}

func (m *Model) inboxViews() tea.Cmd {
	items := []list.Item{}
	for _, view := range []struct{ id, label string }{{"needs", "Needs you"}, {"working", "Working"}, {"queued", "Queued"}, {"later-view", "Later"}, {"done", "Reviewed"}, {"all", "All"}, {"unknown", "Unknown / unavailable"}} {
		items = append(items, choiceItem{title: view.label, command: "inbox", args: view.id})
	}
	items = append(items, choiceItem{title: "Environment, project, priority", command: "inbox", args: "filters"}, choiceItem{title: "More / previous native pages", command: "inbox", args: "pages"}, choiceItem{title: "Refresh", command: "inbox", args: "refresh"}, choiceItem{title: "Info", command: "inbox", args: "info"})
	m.openLocal("Inbox", "inbox-views", items)
	return nil
}

func (m *Model) inboxFilters() tea.Cmd {
	items := []list.Item{choiceItem{title: "Every environment", command: "inbox", args: "environment:"}, choiceItem{title: "Every project", command: "inbox", args: "project:"}}
	for _, source := range m.agentbox.page.Sources {
		items = append(items, choiceItem{title: "Environment · " + source.Label, command: "inbox", args: "environment:" + source.ProfileID})
	}
	seen := map[string]bool{}
	for _, row := range m.agentbox.page.Rows {
		id := config.InboxIdentity(row.Target.EnvironmentID, row.Target.ProjectID, "")
		if !seen[id] {
			seen[id] = true
			items = append(items, choiceItem{title: "Project · " + row.Thread.Project + " · " + row.Environment, command: "inbox", args: "project:" + id})
		}
	}
	for i, label := range []string{"Every priority", "Medium or higher", "High or higher", "Urgent"} {
		items = append(items, choiceItem{title: label, command: "inbox", args: fmt.Sprintf("minimum:%d", i)})
	}
	m.openLocal("Filter inbox", "inbox-filters", items)
	return nil
}

func (m *Model) inboxPages() tea.Cmd {
	items := []list.Item{}
	for _, source := range m.agentbox.page.Sources {
		if source.Page.NextCursor != nil {
			items = append(items, choiceItem{title: source.Label + " · Next", command: "inbox", args: fmt.Sprintf("page:%s\x1f%d", source.ProfileID, *source.Page.NextCursor)})
		}
		if source.Page.Cursor > 0 {
			items = append(items, choiceItem{title: source.Label + " · Previous", command: "inbox", args: fmt.Sprintf("page:%s\x1f%d", source.ProfileID, max(0, source.Page.Cursor-max(1, source.Page.Limit)))})
		}
	}
	if len(items) == 0 {
		m.notice = "No additional native metadata pages are offered"
		return nil
	}
	m.openLocal("Native pages", "inbox-pages", items)
	return nil
}
