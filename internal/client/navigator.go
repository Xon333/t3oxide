package client

import (
	"context"
	"errors"
	"strings"

	"github.com/Xon333/t3oxide/internal/config"
)

// NavigatorPage is a bounded metadata snapshot, not another attachment. Its
// rows are offered targets; opening one still passes through Target and the
// normal environment/hello/snapshot verification on the new session.
type NavigatorPage struct {
	ProfileID  string
	Label      string
	Query      string
	Page       ThreadPage
	Navigation NavigationState
	Tree       bool
}

func (a *Session) NavigatorProfiles() []Choice {
	if a == nil || a.manager == nil {
		return nil
	}
	profiles := make([]Choice, 0, len(a.manager.Environments))
	for _, env := range a.manager.Environments {
		profiles = append(profiles, Choice{ID: env.ID, Label: env.Label})
	}
	return profiles
}

func (a *Session) navigatorEnvironment(profile string) (config.Environment, error) {
	if a == nil || a.manager == nil {
		return config.Environment{}, errors.New("no environment configured")
	}
	for _, env := range a.manager.Environments {
		if env.ID == profile {
			return env, nil
		}
	}
	return config.Environment{}, errors.New("environment was not offered")
}

// NavigatorRead reuses the existing one-shot metadata helper. It never starts
// a detail subscription, reads another transcript or changes the attachment.
func (a *Session) NavigatorRead(ctx context.Context, profile, query string) (NavigatorPage, error) {
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return NavigatorPage{}, err
	}
	page, tree, err := a.browserMetadata(ctx, env, query)
	if err != nil {
		return NavigatorPage{}, err
	}
	return NavigatorPage{ProfileID: profile, Label: env.Label, Query: query, Page: page, Navigation: a.manager.navigation(env.EnvironmentID), Tree: tree}, nil
}

// NavigatorChange only changes local navigation state. Targets and continuation
// are validated against the exact displayed snapshot before an explicit read.
func (a *Session) NavigatorChange(page NavigatorPage, action, target string) error {
	env, err := a.navigatorEnvironment(page.ProfileID)
	if err != nil {
		return err
	}
	n := a.manager.navigation(env.EnvironmentID)
	find := func() *Thread {
		for i := range page.Page.Threads {
			if page.Page.Threads[i].ID == target {
				return &page.Page.Threads[i]
			}
		}
		return nil
	}
	switch action {
	case "next":
		if page.Page.NextCursor == nil {
			return errors.New("no next page offered")
		}
		n.Cursor = *page.Page.NextCursor
	case "previous":
		if page.Page.Cursor == 0 {
			return errors.New("no previous page offered")
		}
		n.Cursor = max(0, page.Page.Cursor-max(1, page.Page.Limit))
	case "toggle", "expand", "collapse":
		row := find()
		if !page.Tree || row == nil || row.ChildCount == 0 {
			return errors.New("no children offered for this conversation")
		}
		if action == "collapse" || action == "toggle" && n.Expanded[target] {
			delete(n.Expanded, target)
		} else {
			if len(n.Expanded) >= 200 && !n.Expanded[target] {
				return errors.New("too many expanded groups")
			}
			n.Expanded[target] = true
		}
		n.Cursor = 0
	case "active", "all", "completed", "failed":
		n.Lifecycle, n.Cursor = action, 0
	case "type:all", "type:main", "type:subagents", "type:forks":
		if !page.Tree {
			return errors.New("this adapter does not expose lineage navigation")
		}
		n.Filter, n.Cursor = strings.TrimPrefix(action, "type:"), 0
	case "search":
		n.Cursor = 0
		if strings.TrimSpace(target) != "" {
			n.Lifecycle = "all"
		}
	default:
		return errors.New("navigation action was not offered")
	}
	a.manager.saveNavigation(env.EnvironmentID, n)
	return nil
}

// NavigatorOpen validates a displayed identity before creating a lazy target.
// Highlighting rows does not invoke it. Even a cached target is reverified by
// the ordinary attachment handshake before the client becomes writable.
func (a *Session) NavigatorOpen(page NavigatorPage, thread string) (*Session, error) {
	if _, err := a.navigatorEnvironment(page.ProfileID); err != nil {
		return nil, err
	}
	for _, row := range page.Page.Threads {
		if row.ID == thread {
			return a.Target(page.ProfileID, thread)
		}
	}
	return nil, errors.New("conversation was not offered in this navigation page")
}

func (a *Session) NavigatorSource(ctx context.Context, page NavigatorPage, thread, action string) (Result, error) {
	if action != "new" && action != "settings" {
		return Result{}, errors.New("source action was not offered")
	}
	if _, err := a.navigatorEnvironment(page.ProfileID); err != nil {
		return Result{}, err
	}
	for _, row := range page.Page.Threads {
		if row.ID == thread {
			return a.withBrowserSource(ctx, page.ProfileID, thread, action)
		}
	}
	return Result{}, errors.New("conversation was not offered in this navigation page")
}
