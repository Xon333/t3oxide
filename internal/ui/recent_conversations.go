package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/client"
)

type recentConversation struct {
	page   client.NavigatorPage
	thread client.Thread
}

func (m *Model) rememberRecentConversation() {
	var found *recentConversation
	for _, page := range m.navigator.pages {
		if page.ProfileID == m.status.ProfileID && page.EnvironmentID == m.status.EnvironmentID {
			for _, row := range page.Page.Threads {
				if row.ID == m.status.ThreadID {
					v := recentConversation{page, row}
					found = &v
				}
			}
		}
	}
	if found == nil {
		for _, row := range m.agentbox.rows {
			if row.Target.ProfileID == m.status.ProfileID && row.Target.EnvironmentID == m.status.EnvironmentID && row.Target.ThreadID == m.status.ThreadID {
				found = &recentConversation{page: client.NavigatorPage{ProfileID: row.Target.ProfileID, EnvironmentID: row.Target.EnvironmentID, Label: row.Environment, Page: client.ThreadPage{Threads: []client.Thread{row.Thread}}}, thread: row.Thread}
			}
		}
	}
	if found == nil {
		return
	}
	found.page.Page.Threads = []client.Thread{found.thread}
	next := []recentConversation{*found}
	for _, old := range m.recentConversations {
		if old.page.ProfileID != found.page.ProfileID || old.page.EnvironmentID != found.page.EnvironmentID || old.thread.ID != found.thread.ID {
			next = append(next, old)
			if len(next) == 32 {
				break
			}
		}
	}
	m.recentConversations = next
}
func (m *Model) recentConversationsMenu(previous bool) tea.Cmd {
	items := []list.Item{}
	for _, r := range m.recentConversations {
		if r.page.EnvironmentID == m.status.EnvironmentID && r.page.ProfileID == m.status.ProfileID && r.thread.ID == m.status.ThreadID {
			continue
		}
		token := r.page.ProfileID + "\x1f" + r.page.EnvironmentID + "\x1f" + r.thread.ID
		if previous {
			return m.openRecentConversation(token)
		}
		items = append(items, choiceItem{title: r.thread.Title, description: fmt.Sprintf("%s · %s", r.page.Label, r.thread.Project), command: "recent-open", args: token})
	}
	m.openLocal("Recent conversations", "recent-conversations", items)
	return nil
}
func (m *Model) openRecentConversation(token string) tea.Cmd {
	for _, r := range m.recentConversations {
		if token == r.page.ProfileID+"\x1f"+r.page.EnvironmentID+"\x1f"+r.thread.ID {
			if m.session == nil {
				return nil
			}
			session := m.session
			profile, environment, thread := r.page.ProfileID, r.page.EnvironmentID, r.thread.ID
			var fresh client.NavigatorPage
			cmd := m.navigatorRun("open", func(ctx context.Context) (client.Result, error) {
				page, err := session.NavigatorRecentRead(ctx, profile, thread)
				if err != nil {
					return client.Result{}, err
				}
				if page.EnvironmentID != environment {
					return client.Result{}, errors.New("recent conversation source changed")
				}
				found := false
				for _, row := range page.Page.Threads {
					if row.ID == thread {
						found = true
						break
					}
				}
				if !found {
					return client.Result{}, errors.New("recent conversation is absent from the bounded source read; use Conversations")
				}
				fresh = page
				next, err := session.NavigatorOpenReviewed(ctx, page, thread)
				return client.Result{Next: next}, err
			})
			if cmd == nil {
				return nil
			}
			return func() tea.Msg { msg := cmd(); return navigatorOpenedMsg{message: msg, page: fresh, thread: thread} }
		}
	}
	m.notice = "Recent conversation is no longer loaded"
	return nil
}
