package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Xon333/t3oxide/internal/client"
)

type batchReceiptToken struct {
	Receipt string
	Item    int
}

func (m *Model) batchReceiptMenu() tea.Cmd {
	if m.session == nil {
		return nil
	}
	items := []list.Item{}
	for _, r := range m.session.BatchReceipts() {
		for i, item := range r.Items {
			token, _ := json.Marshal(batchReceiptToken{r.ID, i})
			items = append(items, choiceItem{title: fmt.Sprintf("%s · item %d · %s", r.CreatedAt, i+1, item.State), description: fmt.Sprintf("%s · %s · %s · %s", r.EnvironmentID, item.ProjectID, item.ThreadID, item.OperationID), command: "batch-receipt-item", args: string(token)})
		}
	}
	m.openLocal("Batch receipts · explicit item actions", "batch-receipts", items)
	return nil
}
func (m *Model) batchReceiptItem(args string) tea.Cmd {
	var token batchReceiptToken
	if err := json.Unmarshal([]byte(args), &token); err != nil {
		m.problem = "Invalid batch receipt identity"
		return nil
	}
	session := m.session
	if session == nil {
		return nil
	}
	return m.navigatorRun("batch-receipt-item", func(ctx context.Context) (client.Result, error) {
		return session.BatchReceiptAction(ctx, token.Receipt, token.Item)
	})
}
