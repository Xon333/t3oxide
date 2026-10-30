package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/config"
	"strings"
)

func (a *Session) BatchReceipts() []config.BatchReceipt {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return nil
	}
	return a.manager.State.BatchReceipts()
}

func batchReceiptText(receipt config.BatchReceipt) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Batch %s\nEnvironment: %s\nOriginal source: %q\nCreated: %s\n", receipt.ID, receipt.EnvironmentID, receipt.Source, receipt.CreatedAt)
	if receipt.PauseReason != "" {
		fmt.Fprintf(&out, "\n%s\n", receipt.PauseReason)
	}
	for index, item := range receipt.Items {
		fmt.Fprintf(&out, "\n%d. %s\n", index+1, item.State)
		if item.OperationID != "" {
			fmt.Fprintf(&out, "Operation: %s\nTarget: %s · %s · %s\n", item.OperationID, item.EnvironmentID, item.ProjectID, item.ThreadID)
		}
		out.WriteString(receipt.ItemText(index))
		out.WriteString("\n")
	}
	out.WriteString("\nAcknowledged means native receipt, not run completion. Remaining draft is unsubmitted human text. Nothing is automatically resumed. Use Review saved action to reconcile the original operation ID.\n")
	return out.String()
}

func (a *Session) batchReceiptDetail(id string) string {
	for _, r := range a.BatchReceipts() {
		if r.ID == id {
			return batchReceiptText(r)
		}
	}
	return "Batch receipt unavailable; preserve the original drafts and saved action."
}

func (a *Session) ReviewBatchReceipts(ctx context.Context) (Result, error) {
	receipts := a.BatchReceipts()
	if len(receipts) == 0 {
		return Result{Text: "No batch receipts."}, nil
	}
	chosen := receipts[0]
	if len(receipts) > 1 {
		choices := make([]Choice, 0, len(receipts))
		for _, r := range receipts {
			choices = append(choices, Choice{ID: r.ID, Label: r.CreatedAt + " · " + r.EnvironmentID, Description: fmt.Sprintf("%d human task(s) · %s", len(r.Items), r.PauseReason)})
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Batch receipts", Choices: choices})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one offered receipt")
		}
		found := false
		for _, r := range receipts {
			if r.ID == answer.ChoiceIDs[0] {
				chosen = r
				found = true
			}
		}
		if !found {
			return Result{}, errors.New("batch receipt was not offered")
		}
	}
	return Result{DetailTitle: "Batch receipt", Detail: batchReceiptText(chosen)}, nil
}
