package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/wire"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/config"
)

type batchLaunchKey struct{}
type batchLaunchDraft struct {
	Environment, Source, Before, Consumed, ReceiptID string
	ItemIndex                                        int
}

func batchLaunchContext(ctx context.Context) *batchLaunchDraft {
	b, _ := ctx.Value(batchLaunchKey{}).(*batchLaunchDraft)
	return b
}
func batchLaunchFormSource(ctx context.Context, source string) string {
	if b := batchLaunchContext(ctx); b != nil {
		digest := sha256.Sum256([]byte(b.Source + "\x00" + b.Before))
		return source + "/batch/" + hex.EncodeToString(digest[:])
	}
	return source
}
func bindBatchLaunch(ctx context.Context, fields map[string]any) {
	if b := batchLaunchContext(ctx); b != nil {
		binding := map[string]any{"environment": b.Environment, "source": b.Source, "before": b.Before, "consumed": b.Consumed}
		if b.ReceiptID != "" {
			binding["receiptId"], binding["itemIndex"] = b.ReceiptID, b.ItemIndex
		}
		fields["batchDraft"] = binding
	}
}

type fileTaskLine struct{ Title, Message, Consumed string }

func parseFileTaskLines(text string) ([]fileTaskLine, error) {
	if !utf8.ValidString(text) || len(text) > 32*1024 {
		return nil, errors.New("task list must be valid UTF-8 and at most 32 KiB")
	}
	var result []fileTaskLine
	consumed := ""
	for _, segment := range strings.SplitAfter(text, "\n") {
		consumed += segment
		message := strings.TrimSuffix(segment, "\n")
		if strings.TrimSpace(message) == "" {
			continue
		}
		if len(message) > 8*1024 {
			return nil, errors.New("each task line must be at most 8 KiB; expand its brief in the native review")
		}
		title := strings.TrimSpace(message)
		for len(title) > 512 {
			_, size := utf8.DecodeLastRuneInString(title)
			title = title[:len(title)-size]
		}
		result = append(result, fileTaskLine{Title: title, Message: message, Consumed: consumed})
		consumed = ""
		if len(result) > 20 {
			return nil, errors.New("file at most 20 explicitly reviewed tasks per list")
		}
	}
	if len(result) == 0 {
		return nil, errors.New("enter at least one task, one per line")
	}
	return result, nil
}

// FileMany only runs while this interactive review is open. Each line gets an
// independent native project/worktree review, stable operation and thread ID.
// A pending or cancelled item pauses the list. No queued dispatcher survives
// UI exit; already-created native runs retain their backend owner.
func (a *Session) FileMany(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("filing needs an active context")
	}
	if err := a.flushDraft(); err != nil {
		return Result{}, err
	}
	env, err := a.creationEnvironment(ctx)
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if pending := a.manager.State.Pending(env.EnvironmentID, false); pending != nil {
		return Result{}, errors.New("a saved operation needs recovery before filing; use /retry-launch for a saved native launch")
	}
	status := a.Status()
	source := "\x00file-many/" + config.InboxIdentity(status.EnvironmentID, status.Location.ProjectID, status.ThreadID)
	store := func(text string) error {
		if len(text) > 32*1024 {
			return errors.New("task list exceeds 32 KiB")
		}
		return a.manager.State.SetDraft(env.EnvironmentID, source, text)
	}
	initial := a.manager.State.Draft(env.EnvironmentID, source)
	answer, err := a.asker.Ask(ctx, Question{Kind: EnterText, Title: "File multiple tasks", Prompt: "One task per line. Each opens its own native project and launch review. Create is explicit for every task; Cancel pauses the remaining list.", Initial: initial, StoreText: store})
	if err != nil {
		return Result{}, err
	}
	if answer.Cancelled {
		return Result{}, nil
	}
	if err = store(answer.Text); err != nil {
		return Result{}, err
	}
	tasks, err := parseFileTaskLines(answer.Text)
	if err != nil {
		return Result{}, err
	}
	var receipt config.BatchReceipt
	firstItem := 0
	// An explicitly reopened, unchanged remainder continues the same receipt;
	// acknowledged tasks are not refiled and no background continuation exists.
	for _, saved := range a.manager.State.BatchReceipts() {
		if saved.EnvironmentID != env.EnvironmentID || saved.Source != source {
			continue
		}
		index := 0
		for index < len(saved.Items) && saved.Items[index].State == config.BatchAcknowledged {
			index++
		}
		if index == len(saved.Items) || saved.Original[saved.Items[index].Start:] != answer.Text {
			continue
		}
		safe := true
		for _, item := range saved.Items[index:] {
			if item.State != config.BatchRemainingDraft || item.OperationID != "" {
				safe = false
			}
		}
		if safe {
			receipt = saved
			firstItem = index
			break
		}
	}
	if receipt.ID == "" {
		receipt = config.BatchReceipt{ID: wire.NewID(), EnvironmentID: env.EnvironmentID, Source: source, Original: answer.Text, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		offset := 0
		for _, task := range tasks {
			receipt.Items = append(receipt.Items, config.BatchReceiptItem{Start: offset, End: offset + len(task.Consumed), State: config.BatchRemainingDraft})
			offset += len(task.Consumed)
		}
		if err = a.manager.State.BeginBatchReceipt(receipt); err != nil {
			return Result{}, fmt.Errorf("nothing filed: preserve batch receipt: %w", err)
		}
	} else if err = a.manager.State.PauseBatchReceipt(receipt.ID, ""); err != nil {
		return Result{}, err
	}
	remaining := answer.Text
	var last *Session
	var filed []string
	finish := func(message string) Result {
		return Result{Next: last, Text: message, DetailTitle: "Batch receipt", Detail: a.batchReceiptDetail(receipt.ID)}
	}
	for index, task := range tasks {
		binding := &batchLaunchDraft{Environment: env.EnvironmentID, Source: source, Before: remaining, Consumed: task.Consumed, ReceiptID: receipt.ID, ItemIndex: firstItem + index}
		input := LaunchInput{Title: task.Title, Message: task.Message}
		itemCtx := context.WithValue(ctx, batchLaunchKey{}, binding)
		result, launchErr := a.newConversationInEnvironment(itemCtx, task.Title, "", true, env.ID, &input)
		if launchErr != nil {
			if last != nil {
				last.Close()
			}
			prefix := ""
			if len(filed) > 0 {
				prefix = "Already filed native threads: " + strings.Join(filed, "; ") + ". "
			}
			pause := "Paused after error; inspect the original operation before continuing. Remaining items are drafts."
			if a.manager.State.Pending(env.EnvironmentID, false) != nil {
				pause = "May already be sent — Review saved action. Further filing paused; remaining items are drafts."
			}
			if saveErr := a.manager.State.PauseBatchReceipt(receipt.ID, pause); saveErr != nil {
				launchErr = errors.Join(launchErr, saveErr)
			}
			return Result{DetailTitle: "Batch receipt", Detail: a.batchReceiptDetail(receipt.ID)}, fmt.Errorf("%sList paused; remaining human text is saved. %w", prefix, launchErr)
		}
		if result.Next == nil {
			if err := a.manager.State.PauseBatchReceipt(receipt.ID, "Paused before dispatch. Current and later items remain human drafts."); err != nil {
				return finish("List paused; receipt update failed."), err
			}
			return finish(fmt.Sprintf("Filed %d task(s); remaining list saved.", len(filed))), nil
		}
		if last != nil {
			last.Close()
		}
		last = result.Next
		created := last.Status()
		filed = append(filed, created.EnvironmentID+" · "+created.ThreadID+" · "+task.Title)
		// State.Acknowledge already advanced this exact prefix atomically.
		// Compare instead of silently overwriting later human edits.
		expected := strings.TrimPrefix(remaining, task.Consumed)
		if strings.TrimSpace(expected) == "" {
			expected = ""
		}
		if a.manager.State.Draft(env.EnvironmentID, source) != expected {
			if err := a.manager.State.PauseBatchReceipt(receipt.ID, "Newer human edits were preserved; review the edited list before any further filing."); err != nil {
				return finish("List changed; receipt update failed."), err
			}
			return finish(fmt.Sprintf("Filed %d task(s); the list changed and needs review.", len(filed))), nil
		}
		remaining = expected
	}
	return finish(fmt.Sprintf("Filed %d native task(s).", len(filed))), nil
}
