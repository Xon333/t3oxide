package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// ProfileIDs is the complete configured identity set, independent of scope.
func (m *Manager) ProfileIDs() []string {
	ids := make([]string, 0, len(m.Environments))
	for _, e := range m.Environments {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return ids
}
func (m *Manager) browserScope(fallback string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.profileScope) > 0 {
		return append([]string(nil), m.profileScope...)
	}
	// Browsing and creation are separate choices. In particular an explicit
	// --env creation target or an old single-profile browser preference must
	// not hide the other configured environments at startup.
	ids := make([]string, 0, len(m.Environments))
	for _, env := range m.Environments {
		ids = append(ids, env.ID)
	}
	return ids
}
func (m *Manager) selectScope(ids []string) error {
	offered := map[string]bool{}
	for _, e := range m.Environments {
		offered[e.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !offered[id] || seen[id] {
			return errors.New("environment was not offered")
		}
		seen[id] = true
	}
	if len(ids) == 0 {
		return errors.New("select at least one environment")
	}
	m.mu.Lock()
	m.profileScope = append([]string(nil), ids...)
	m.mu.Unlock()
	if m.State != nil {
		p := m.State.BrowserPreferences()
		p.ProfileIDs = append([]string(nil), ids...)
		found := false
		for _, id := range ids {
			found = found || p.LastProfileID == id
		}
		if !found {
			p.LastProfileID = ids[0]
		}
		return m.State.SetBrowserPreferences(p)
	}
	return nil
}
func (a *Session) chooseScope(ctx context.Context, current []string) ([]string, error) {
	choices := make([]Choice, 0, len(a.manager.Environments))
	for _, e := range a.manager.Environments {
		choices = append(choices, Choice{ID: e.ID, Label: e.Label, Description: e.ID})
	}
	initial, _ := json.Marshal(current)
	answer, err := a.asker.Ask(ctx, Question{Surface: SurfaceBrowser, Kind: ChooseMany, Title: "Environments", Choices: choices, Initial: string(initial), BrowserPage: &BrowserPage{ProfileIDs: append([]string(nil), current...), Profiles: choices, Actions: []string{"continue", "cancel"}}})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if answer.Cancelled || answer.Action == "cancel" {
		return nil, nil
	}
	if answer.Action != "" && answer.Action != "continue" {
		return nil, errors.New("environment action was not offered")
	}
	if err = a.manager.selectScope(answer.ChoiceIDs); err != nil {
		return nil, err
	}
	return append([]string(nil), answer.ChoiceIDs...), nil
}

type browserTab struct {
	threads          []Thread
	more, navigation bool
	query, initial   string
	err              error
	loaded           bool
	observedAt       string
	nextCursor       *int
	loadedAt         time.Time
	pageLimit        int
}

func browserChoiceID(profile, thread string) string {
	b, _ := json.Marshal([]string{profile, thread})
	return string(b)
}
func listFields(query string, n NavigationState, enabled bool) map[string]any {
	fields := map[string]any{"query": query, "cursor": n.Cursor}
	if n.Lifecycle != "" {
		fields["lifecycle"] = n.Lifecycle
	}
	if enabled {
		expanded := []string{}
		for id, open := range n.Expanded {
			if open {
				expanded = append(expanded, id)
			}
		}
		sort.Strings(expanded)
		fields["grouped"], fields["typeFilter"], fields["expanded"] = true, n.Filter, expanded
	}
	return fields
}

// browserMetadata never subscribes to detail or changes the active attachment.
func (a *Session) browserMetadata(ctx context.Context, env config.Environment, query string) (ThreadPage, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	a.mu.Lock()
	conn := a.conn
	reuse := a.environment != nil && a.environment.ID == env.ID && a.helloVerified && a.ready && conn != nil
	navigation := a.features["threadNavigation"]
	a.mu.Unlock()
	if !reuse {
		var err error
		conn, err = wire.Start(ctx, env)
		if err != nil {
			return ThreadPage{}, false, err
		}
		defer func() {
			conn.Close()
			wait, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			_ = conn.WaitClosed(wait)
		}()
		hello, err := conn.Request(ctx, "hello", nil, "")
		if err != nil {
			return ThreadPage{}, false, err
		}
		if stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
			return ThreadPage{}, false, &wire.Fault{Code: "identity_mismatch", Message: "browser helper identity or protocol mismatch"}
		}
		features, _ := hello["features"].(map[string]any)
		navigation = boolValue(features["threadNavigation"])
	}
	result, err := conn.Request(ctx, "list", listFields(query, a.manager.navigation(env.EnvironmentID), navigation), "")
	if err != nil {
		return ThreadPage{}, navigation, err
	}
	if err = ctx.Err(); err != nil {
		return ThreadPage{}, navigation, err
	}
	page, err := DecodeThreadPage(result)
	return page, navigation, err
}

// Browse keeps the current detailed session alive. Only an explicit offered
// Open returns a lazy replacement; cancellation and Resume return no target.
func (a *Session) Browse(ctx context.Context, query string) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("browser requires an active context")
	}
	a.mu.Lock()
	initialEnv, activeThread := "", a.thread
	if a.environment != nil {
		initialEnv = a.environment.ID
	}
	a.mu.Unlock()
	scope := a.manager.browserScope(initialEnv)
	var err error
	if len(scope) == 0 && len(a.manager.Environments) == 1 {
		scope = []string{a.manager.Environments[0].ID}
		if err = a.manager.selectScope(scope); err != nil {
			return Result{}, err
		}
	}
	if len(scope) == 0 {
		scope, err = a.chooseScope(ctx, scope)
		if err != nil || len(scope) == 0 {
			return Result{}, err
		}
	}
	current := scope[0]
	if initialEnv == "" && a.manager.State != nil {
		last := a.manager.State.BrowserPreferences().LastProfileID
		for _, id := range scope {
			if id == last {
				current = id
			}
		}
	}
	for _, id := range scope {
		if id == initialEnv {
			current = id
		}
	}
	tabs := map[string]*browserTab{}
	for {
		if err = ctx.Err(); err != nil {
			return Result{}, err
		}
		var env config.Environment
		for _, e := range a.manager.Environments {
			if e.ID == current {
				env = e
				break
			}
		}
		tab := tabs[current]
		if tab == nil {
			tab = &browserTab{query: query}
			tabs[current] = tab
		}
		if tab.loaded && time.Since(tab.loadedAt) > 30*time.Second {
			tab.loaded = false
		}
		if !tab.loaded {
			page, navigation, loadErr := a.browserMetadata(ctx, env, tab.query)
			tab.threads, tab.more, tab.navigation, tab.err = page.Threads, page.More, navigation, loadErr
			tab.observedAt, tab.nextCursor, tab.loadedAt = page.ObservedAt, page.NextCursor, time.Now()
			tab.pageLimit = page.Limit
			tab.loaded = true
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
		}
		profiles := []Choice{}
		for _, e := range a.manager.Environments {
			profiles = append(profiles, Choice{ID: e.ID, Label: e.Label, Description: e.ID})
		}
		actions := []string{"environments", "new", "settings", "refresh", "search"}
		if activeThread != "" {
			actions = append(actions, "resume")
		}
		if tab.navigation {
			actions = append(actions, "types", "filter", "lifecycle", "toggle", "expand", "collapse", "parent")
		}
		for _, id := range scope {
			for _, e := range a.manager.Environments {
				if e.ID == id {
					actions = append(actions, "profile:"+e.ID)
				}
			}
		}
		n := a.manager.navigation(env.EnvironmentID)
		if tab.nextCursor != nil {
			actions = append(actions, "next-page")
		}
		if n.Cursor > 0 {
			actions = append(actions, "previous-page")
		}
		choices := ThreadChoices(tab.threads)
		groups := []BrowserGroup{}
		groupIndex := map[string]int{}
		for i := range choices {
			choices[i].ID = browserChoiceID(current, tab.threads[i].ID)
			row := tab.threads[i]
			key := row.ProjectID
			if key == "" {
				key = "label:" + row.Project
			}
			index, exists := groupIndex[key]
			if !exists {
				index = len(groups)
				groupIndex[key] = index
				groups = append(groups, BrowserGroup{ID: row.ProjectID, Label: row.Project})
			}
			groups[index].ChoiceIDs = append(groups[index].ChoiceIDs, choices[i].ID)
		}
		message := ""
		if tab.err != nil {
			message = tab.err.Error()
		}
		answer, e := a.asker.Ask(ctx, Question{Surface: SurfaceBrowser, Kind: ChooseOne, Title: "Conversations", Choices: choices, Initial: tab.initial, Navigation: tab.navigation, BrowserPage: &BrowserPage{ProfileIDs: append([]string(nil), scope...), ProfileID: current, Profiles: profiles, Groups: groups, Location: env.Label, Actions: actions, Query: tab.query, More: tab.more, Error: message, ObservedAt: tab.observedAt, Lifecycle: n.Lifecycle, Cursor: n.Cursor, NextCursor: tab.nextCursor}})
		if e != nil {
			return Result{}, e
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if answer.Cancelled {
			return Result{}, nil
		}
		var selected *Thread
		if len(answer.ChoiceIDs) > 0 {
			if len(answer.ChoiceIDs) != 1 {
				return Result{}, errors.New("choose one offered conversation")
			}
			for i := range tab.threads {
				if browserChoiceID(current, tab.threads[i].ID) == answer.ChoiceIDs[0] {
					selected = &tab.threads[i]
					tab.initial = answer.ChoiceIDs[0]
					break
				}
			}
			if selected == nil {
				return Result{}, errors.New("selected thread was not offered in this environment")
			}
		}
		if answer.Action == "" {
			if selected == nil {
				return Result{}, errors.New("selected thread was not offered")
			}
			next, e := a.Target(current, selected.ID)
			return Result{Next: next}, e
		}
		offered := false
		for _, action := range actions {
			if action == answer.Action {
				offered = true
				break
			}
		}
		if !offered {
			return Result{}, errors.New("browser action was not offered")
		}
		switch answer.Action {
		case "resume":
			return Result{}, nil
		case "environments":
			next, e := a.chooseScope(ctx, scope)
			if e != nil {
				return Result{}, e
			}
			if len(next) > 0 {
				scope = next
				found := false
				for _, id := range scope {
					if id == current {
						found = true
					}
				}
				if !found {
					current = scope[0]
				}
			}
		case "refresh":
			tab.loaded = false
		case "next-page", "previous-page":
			n := a.manager.navigation(env.EnvironmentID)
			if answer.Action == "next-page" && tab.nextCursor != nil {
				n.Cursor = *tab.nextCursor
			} else if answer.Action == "previous-page" {
				if tab.pageLimit > 0 {
					n.Cursor = max(0, n.Cursor-tab.pageLimit)
				} else {
					n.Cursor = 0
				}
			}
			a.manager.saveNavigation(env.EnvironmentID, n)
			tab.loaded = false
		case "lifecycle":
			n := a.manager.navigation(env.EnvironmentID)
			reply, e := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Conversation lifecycle", Initial: n.Lifecycle, Choices: []Choice{{ID: "active", Label: "Active", Description: "Unsettled conversations and working children"}, {ID: "all", Label: "All", Description: "Include settled and finished children"}, {ID: "completed", Label: "Completed"}, {ID: "failed", Label: "Failed"}}})
			if e != nil {
				return Result{}, e
			}
			if !reply.Cancelled && len(reply.ChoiceIDs) == 1 {
				switch reply.ChoiceIDs[0] {
				case "active", "all", "completed", "failed":
					n.Lifecycle = reply.ChoiceIDs[0]
					n.Cursor = 0
					a.manager.saveNavigation(env.EnvironmentID, n)
					tab.loaded = false
				}
			}
		case "search":
			reply := Answer{Text: answer.Text}
			var e error
			if answer.Text == "" {
				reply, e = a.asker.Ask(ctx, Question{Kind: EnterText, SingleLine: true, Title: "Find a conversation", Initial: answer.Text})
			}
			if e != nil {
				return Result{}, e
			}
			if !reply.Cancelled {
				tab.query = reply.Text
				n := a.manager.navigation(env.EnvironmentID)
				n.Cursor = 0
				if strings.TrimSpace(reply.Text) != "" {
					n.Lifecycle = "all"
				}
				a.manager.saveNavigation(env.EnvironmentID, n)
				tab.loaded = false
			}
		case "types", "filter":
			n := a.manager.navigation(env.EnvironmentID)
			n.Filter, e = a.pickThreadFilter(ctx, n.Filter)
			n.Cursor = 0
			if e != nil {
				return Result{}, e
			}
			a.manager.saveNavigation(env.EnvironmentID, n)
			tab.loaded = false
		case "toggle", "expand", "collapse":
			if selected == nil {
				return Result{}, errors.New("select an offered group")
			}
			if selected.ChildCount > 0 {
				n := a.manager.navigation(env.EnvironmentID)
				if answer.Action == "collapse" || answer.Action == "toggle" && n.Expanded[selected.ID] {
					delete(n.Expanded, selected.ID)
				} else if len(n.Expanded) < 200 || n.Expanded[selected.ID] {
					n.Expanded[selected.ID] = true
				} else {
					return Result{}, errors.New("too many expanded groups")
				}
				n.Cursor = 0
				a.manager.saveNavigation(env.EnvironmentID, n)
				tab.loaded = false
			}
		case "parent":
			if selected == nil || selected.Lineage == nil || selected.Lineage.ParentThreadID == nil || *selected.Lineage.ParentThreadID == selected.ID {
				return Result{}, errors.New("selected conversation has no reported parent")
			}
			next, e := a.Target(current, *selected.Lineage.ParentThreadID)
			return Result{Next: next}, e
		case "new", "settings":
			if selected == nil {
				a.notice("Select a conversation in the workspace first.")
				continue
			}
			result, e := a.withBrowserSource(ctx, current, selected.ID, answer.Action)
			if e != nil {
				return Result{}, e
			}
			if result.Next != nil {
				return result, nil
			}
			tab.loaded = false
		default:
			if strings.HasPrefix(answer.Action, "profile:") {
				current = strings.TrimPrefix(answer.Action, "profile:")
				if a.manager.State != nil {
					prefs := config.BrowserPreferences{ProfileIDs: append([]string(nil), scope...), LastProfileID: current}
					if e := a.manager.State.SetBrowserPreferences(prefs); e != nil {
						return Result{}, e
					}
				}
			}
		}
	}
}

// Only explicit New/Settings requests acquire a source detail snapshot. Merely
// moving the browser cursor never starts this helper or a watch.
func (a *Session) withBrowserSource(ctx context.Context, profile, thread, action string) (Result, error) {
	if action == "new" {
		return a.NewConversation(ctx, "", false, false)
	}
	// The source may be a temporary attachment. Its eventual Target call saves
	// that source's draft, so preserve the attached composer at this boundary
	// instead of relying on the autosave timer or source connection latency.
	if err := a.flushDraft(); err != nil {
		return Result{}, fmt.Errorf("save the current draft before opening %s: %w", action, err)
	}
	a.mu.Lock()
	same := a.environment != nil && a.environment.ID == profile && a.thread == thread
	a.mu.Unlock()
	source := a
	if !same {
		var err error
		source, err = a.manager.NewSession(profile, thread)
		if err != nil {
			return Result{}, err
		}
		source.asker = a.asker
		attach, cancel := context.WithCancel(ctx)
		defer cancel()
		defer source.Close()
		ready := make(chan error, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			sent := false
			for event, err := range source.Events(attach) {
				if !sent && (err != nil || event.Snapshot != nil && event.Snapshot.Metadata.Live && event.Status.Connected) {
					ready <- err
					sent = true
				}
				if err != nil {
					return
				}
			}
			if !sent {
				ready <- context.Canceled
			}
		}()
		defer func() { cancel(); source.Close(); <-finished }()
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case err := <-ready:
			if err != nil {
				return Result{}, err
			}
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-timer.C:
			return Result{}, errors.New("source conversation did not synchronize; refresh the browser and try explicitly")
		}
	}
	if action == "new" {
		return source.newConversation(ctx, "")
	}
	return source.settings(ctx)
}
