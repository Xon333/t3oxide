package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/Xon333/t3oxide/internal/client"
)

// dispatchBrowseSlice must be called before the main dispatch switch.
func (m *Model) dispatchBrowseSlice(name, args string) (bool, tea.Cmd) {
	switch name {
	case "batch-receipts":
		return true, m.batchReceiptMenu()
	case "batch-receipt-item":
		return true, m.batchReceiptItem(args)
	case "browse-scope":
		return true, m.chooseBrowseScope()
	}
	return false, nil
}
func (m *Model) chooseBrowseScope() tea.Cmd {
	session := m.session
	if session == nil {
		return nil
	}
	return m.navigatorRun("browse-scope", func(ctx context.Context) (client.Result, error) {
		scope, err := session.ChooseBrowseScope(ctx)
		if errors.Is(err, context.Canceled) && ctx.Err() == nil {
			return client.Result{}, nil
		}
		if err != nil {
			return client.Result{}, err
		}
		page, err := session.NavigatorRead(ctx, scope.ProfileID, "")
		if err != nil {
			return client.Result{}, err
		}
		page, err = client.FilterBrowsePage(page, scope)
		if err != nil {
			return client.Result{}, err
		}
		choices := []client.Choice{}
		for _, row := range page.Page.Threads {
			choices = append(choices, client.Choice{ID: row.ID, Label: row.Title, Description: row.Project})
		}
		// Keep the existing prompt owner and explicit open semantics.
		return session.OpenBrowsePage(ctx, page, choices)
	})
}

// BrowseScopeItems lets the integrator expose the chooser without a new chord.
func BrowseScopeItems() []list.Item {
	return []list.Item{choiceItem{title: "Browse environment / project", description: "Bounded metadata · New unchanged", command: "browse-scope"}}
}
