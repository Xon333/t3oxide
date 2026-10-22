package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type NavigationState struct {
	Filter   string
	Expanded map[string]bool
}

func (m *Manager) navigation(env string) NavigationState {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nav[env]
	if n.Filter == "" {
		n.Filter = "all"
	}
	copy := NavigationState{Filter: n.Filter, Expanded: map[string]bool{}}
	for id, open := range n.Expanded {
		copy.Expanded[id] = open
	}
	return copy
}
func (m *Manager) saveNavigation(env string, n NavigationState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nav == nil {
		m.nav = make(map[string]NavigationState)
	}
	copy := NavigationState{Filter: n.Filter, Expanded: map[string]bool{}}
	for id, open := range n.Expanded {
		if open {
			copy.Expanded[id] = true
		}
	}
	m.nav[env] = copy
}
func validThreadFilter(s string) bool {
	return s == "all" || s == "main" || s == "subagents" || s == "forks"
}

func ThreadChoices(threads []Thread) []Choice {
	choices := make([]Choice, 0, len(threads))
	for _, t := range threads {
		prefix := ""
		switch t.Lineage.Type() {
		case "main":
			prefix = "MAIN "
		case "subagent":
			prefix = "SUB "
		case "fork":
			prefix = "FORK "
		default:
			prefix = "? "
		}
		marker := ""
		if t.ChildCount > 0 {
			marker = "▸ "
			if t.Expanded {
				marker = "▾ "
			}
		}
		label := strings.Repeat("  ", min(t.Depth, 12)) + marker + prefix + t.Title
		detail := t.Project + " · " + t.Status
		if t.Model != "" {
			detail += " · " + t.Model
		}
		if t.ChildCount > 0 {
			detail += fmt.Sprintf(" · %d subagent(s)", t.ChildCount)
		}
		if t.Lineage != nil && t.Lineage.ParentThreadID != nil {
			parent := t.ParentTitle
			if parent == "" {
				parent = *t.Lineage.ParentThreadID
			}
			detail += " · parent: " + parent
		}
		if t.ContextOnly {
			detail += " · search context"
		}
		choices = append(choices, Choice{ID: t.ID, Label: label, Description: detail})
	}
	return choices
}
func (a *Session) parentID() (string, error) {
	s := a.snapshot()
	if s == nil || s.Metadata.Lineage == nil || s.Metadata.Lineage.ParentThreadID == nil {
		return "", errors.New("T3 has not reported a parent for this conversation")
	}
	id := *s.Metadata.Lineage.ParentThreadID
	if id == s.Metadata.ThreadID {
		return "", errors.New("T3 reported a cyclic parent relationship")
	}
	return id, nil
}
func (a *Session) Parent(ctx context.Context) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := a.parentID()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	env := a.environment
	a.mu.Unlock()
	if env == nil {
		return nil, errors.New("select an environment")
	}
	return a.Target(env.ID, id)
}
func (a *Session) pickThreadFilter(ctx context.Context, current string) (string, error) {
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Thread types", Prompt: "T3 lineage only. Forks are not subagents; missing lineage is unknown, never inferred from a title.", Initial: current, Choices: []Choice{{ID: "all", Label: "All", Description: "Main threads first; collapsible subagents; forks separate"}, {ID: "main", Label: "Main", Description: "Authoritative root conversations"}, {ID: "subagents", Label: "Subagents", Description: "All subagent threads, with parent context"}, {ID: "forks", Label: "Forks", Description: "Forks only, never classified as subagents"}}})
	if err != nil {
		return "", err
	}
	if answer.Cancelled {
		return current, nil
	}
	if len(answer.ChoiceIDs) != 1 || !validThreadFilter(answer.ChoiceIDs[0]) {
		return "", errors.New("invalid thread type filter")
	}
	return answer.ChoiceIDs[0], nil
}
func (a *Session) filterTarget(ctx context.Context, filter string) (*Session, error) {
	a.mu.Lock()
	env := a.environment
	enabled := a.features["threadNavigation"]
	a.mu.Unlock()
	if env == nil {
		return nil, errors.New("select an environment")
	}
	if !enabled {
		return nil, errors.New("this adapter does not expose lineage navigation")
	}
	n := a.manager.navigation(env.EnvironmentID)
	if filter == "" {
		var err error
		filter, err = a.pickThreadFilter(ctx, n.Filter)
		if err != nil {
			return nil, err
		}
	}
	if !validThreadFilter(filter) {
		return nil, errors.New("use /filter all, main, subagents, or forks")
	}
	n.Filter = filter
	a.manager.saveNavigation(env.EnvironmentID, n)
	return a.filteredTarget(ctx, "")
}

func (a *Session) chooseThreadFrom(ctx context.Context, threads []Thread, queries ...string) (string, error) {
	query := ""
	if len(queries) > 0 {
		query = queries[0]
	}
	a.mu.Lock()
	env := a.environment
	initial := a.thread
	enabled := a.features["threadNavigation"]
	a.mu.Unlock()
	if env == nil {
		return "", errors.New("select an environment")
	}
	for {
		n := a.manager.navigation(env.EnvironmentID)
		a.mu.Lock()
		more := a.moreThreads
		a.mu.Unlock()
		prompt := "Enter opens · Space/Right expands · Left collapses · F6 types · Ctrl+F searches hidden children · Alt+Up parent"
		if !enabled {
			prompt = "This adapter omits lineage; thread types are unknown. /threads <text> filters on the host."
		}
		if query != "" {
			prompt += "\nSearch: " + query
		}
		if more {
			prompt += "\nBounded results; refine the search to see omitted threads."
		}
		if len(threads) == 0 {
			prompt += "\nNo matches. Change the type filter or search."
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Threads · " + n.Filter, Prompt: prompt, Choices: ThreadChoices(threads), Initial: initial, Navigation: enabled})
		if err != nil {
			return "", err
		}
		if answer.Cancelled {
			return "", errors.New("conversation selection dismissed")
		}
		var selected *Thread
		if len(answer.ChoiceIDs) == 1 {
			for i := range threads {
				if threads[i].ID == answer.ChoiceIDs[0] {
					selected = &threads[i]
					initial = selected.ID
					break
				}
			}
		}
		if answer.Action == "" {
			if selected == nil {
				return "", errors.New("selected thread was not offered")
			}
			return selected.ID, nil
		}
		if !enabled {
			return "", errors.New("lineage navigation is unsupported by this adapter")
		}
		switch answer.Action {
		case "toggle", "expand", "collapse":
			if selected != nil && selected.ChildCount > 0 {
				if answer.Action == "collapse" || answer.Action == "toggle" && n.Expanded[selected.ID] {
					delete(n.Expanded, selected.ID)
				} else if n.Expanded[selected.ID] || len(n.Expanded) < 200 {
					n.Expanded[selected.ID] = true
				} else {
					return "", errors.New("too many expanded groups; collapse one before expanding another")
				}
				a.manager.saveNavigation(env.EnvironmentID, n)
			}
		case "filter":
			filter, err := a.pickThreadFilter(ctx, n.Filter)
			if err != nil {
				return "", err
			}
			n.Filter = filter
			a.manager.saveNavigation(env.EnvironmentID, n)
		case "search":
			result, err := a.asker.Ask(ctx, Question{Kind: EnterText, Title: "Find a conversation", Prompt: "Search all matching T3 thread titles and projects, including collapsed subagents. Empty search returns to browsing.", Initial: query})
			if err != nil {
				return "", err
			}
			if !result.Cancelled {
				query = strings.TrimSpace(result.Text)
			}
		case "parent":
			if selected != nil && selected.Lineage != nil && selected.Lineage.ParentThreadID != nil && *selected.Lineage.ParentThreadID != selected.ID {
				return *selected.Lineage.ParentThreadID, nil
			}
		default:
			return "", errors.New("unknown thread navigation action")
		}
		threads, err = a.List(ctx, query)
		if err != nil {
			return "", err
		}
	}
}
