package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Xon333/t3oxide/internal/wire"
)

const activityCacheEntries = 8
const activityCacheBytes = 256 * 1024

// ActivityItems is a copy of the bounded metadata already attached. It never
// reads another thread or starts a helper simply because a panel is visible.
func (a *Session) ActivityItems() *wire.ActivityPage {
	view := a.snapshot()
	if view == nil {
		return nil
	}
	return view.Metadata.Activity.Clone()
}

func (a *Session) activityConnection() (*wire.Connection, Location, error) {
	status := a.Status()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil || !a.ready || !a.helloVerified || a.thread == "" {
		return nil, Location{}, errors.New("connect to a conversation to inspect activity")
	}
	if !a.features["activityInspection"] {
		return nil, Location{}, errors.New("this host adapter does not support activity inspection")
	}
	return a.conn, status.Location, nil
}

func activityCacheKey(environment string, item wire.ActivityItem) string {
	return strings.Join([]string{environment, item.SourceThreadID, item.ItemID, item.UpdatedAt}, "\x00")
}

// InspectActivity binds the exact source-qualified identity offered on the
// current page. It cannot turn a highlighted tool into a remote command.
func (a *Session) InspectActivity(ctx context.Context, key string) (Result, error) {
	page := a.ActivityItems()
	if page == nil {
		return Result{}, errors.New("this page has no activity metadata")
	}
	for _, item := range page.Items {
		if item.Key == key {
			return a.inspectOfferedActivity(ctx, item, true)
		}
	}
	return Result{}, errors.New("this activity page changed; select the item again")
}

func (a *Session) inspectOfferedActivity(ctx context.Context, item wire.ActivityItem, onPage bool) (Result, error) {
	conn, location, err := a.activityConnection()
	if err != nil {
		return Result{}, err
	}
	guard := func() bool {
		if ctx.Err() != nil {
			return false
		}
		a.mu.Lock()
		valid := a.conn == conn && a.ready && a.helloVerified && a.environment != nil &&
			a.environment.EnvironmentID == location.EnvironmentID && a.thread == location.ThreadID
		a.mu.Unlock()
		if !valid || !onPage {
			return valid
		}
		page := a.ActivityItems()
		if page != nil {
			for _, current := range page.Items {
				if current.Key == item.Key && current.UpdatedAt == item.UpdatedAt {
					return true
				}
			}
		}
		return false
	}
	if !guard() {
		return Result{}, errors.New("this activity page changed; select the item again")
	}
	cacheKey := activityCacheKey(location.EnvironmentID, item)
	a.mu.Lock()
	detail, cached := a.activityCache[cacheKey]
	a.mu.Unlock()
	if cached && item.UpdatedAt != "" {
		return activityResult(location, detail), nil
	}
	args := map[string]any{"threadId": item.SourceThreadID, "itemId": item.ItemID, "revision": item.UpdatedAt}
	// A retained historical page after reconnect has no current adapter page;
	// the explicit source identity still permits the same read-only inspection.
	if onPage && a.store.Snapshot() != nil {
		args["viewThreadId"] = location.ThreadID
	}
	reply, err := conn.Request(ctx, "inspect-item", args, "")
	if err != nil {
		return Result{}, err
	}
	if !guard() {
		return Result{}, errors.New("activity inspection was cancelled because the page changed")
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return Result{}, err
	}
	detail, err = wire.DecodeActivityDetail(raw)
	if err != nil {
		return Result{}, err
	}
	if detail.Key != item.Key || detail.SourceThreadID != item.SourceThreadID || detail.ItemID != item.ItemID || detail.RequestedRevision != item.UpdatedAt {
		return Result{}, errors.New("activity inspection returned a different source or revision request")
	}
	// T3's revision is a cache hint. A changed/current result is shown honestly
	// but never stored under the older requested revision.
	if item.UpdatedAt != "" && detail.UpdatedAt == item.UpdatedAt {
		a.mu.Lock()
		if a.activityCache == nil {
			a.activityCache = make(map[string]wire.ActivityDetail)
		}
		if _, exists := a.activityCache[cacheKey]; !exists {
			a.activityCacheOrder = append(a.activityCacheOrder, cacheKey)
			a.activityCache[cacheKey] = detail
			a.activityCacheSize += activityDetailSize(detail)
		}
		for len(a.activityCacheOrder) > activityCacheEntries || a.activityCacheSize > activityCacheBytes {
			old := a.activityCacheOrder[0]
			a.activityCacheOrder = a.activityCacheOrder[1:]
			a.activityCacheSize -= activityDetailSize(a.activityCache[old])
			delete(a.activityCache, old)
		}
		a.mu.Unlock()
	}
	return activityResult(location, detail), nil
}

func activityDetailSize(d wire.ActivityDetail) int {
	return len(d.Key) + len(d.SourceThreadID) + len(d.ItemID) + len(d.Kind) + len(d.Title) + len(d.Status) +
		len(d.Input) + len(d.Output) + len(d.UpdatedAt) + len(d.RequestedRevision) + 64
}

// A variable-length fence cannot be closed by tool output containing Markdown
// fences. Terminal control sanitization stays in the maintained UI renderer.
func activityCode(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "text\n" + text + "\n" + fence
}

func activityResult(location Location, d wire.ActivityDetail) Result {
	parts := []string{d.Status}
	if d.SourceThreadID != location.ThreadID {
		parts = append(parts, "Source: "+d.SourceThreadID)
	}
	if d.Changed {
		parts = append(parts, "Different revision: "+d.UpdatedAt)
	} else if d.RequestedRevision != "" && d.UpdatedAt == "" {
		parts = append(parts, "Result revision unreported.")
	}
	if d.Input != "" {
		parts = append(parts, "## Input\n\n"+activityCode(d.Input))
	}
	if d.OutputPresent {
		parts = append(parts, "## Result\n\n"+activityCode(d.Output))
	} else if d.OutputOmitted {
		parts = append(parts, "Output remains omitted by T3.")
	} else {
		parts = append(parts, "No result reported.")
	}
	if d.Truncated {
		parts = append(parts, "Details shortened.")
	}
	if d.OutputPresent && d.OutputOmitted {
		parts = append(parts, "T3 reports additional omitted output.")
	}
	return Result{DetailTitle: location.Environment + " / " + location.Title + " · " + d.Title, Detail: strings.Join(parts, "\n\n")}
}

func (a *Session) activity(ctx context.Context, args string) (Result, error) {
	if args != "" {
		return a.InspectActivity(ctx, args)
	}
	conn, location, err := a.activityConnection()
	if err != nil {
		return Result{}, err
	}
	page := a.ActivityItems()
	load := page == nil
	var cursor *string
	for {
		if load {
			request := map[string]any{"threadId": location.ThreadID}
			if cursor != nil {
				request["cursor"] = *cursor
			}
			reply, err := conn.Request(ctx, "activity", request, "")
			if err != nil {
				return Result{}, err
			}
			raw, err := json.Marshal(reply)
			if err != nil {
				return Result{}, err
			}
			list, err := wire.DecodeActivityList(raw)
			if err != nil {
				return Result{}, err
			}
			if list.ThreadID != location.ThreadID {
				return Result{}, errors.New("activity list returned a different thread")
			}
			page = &list.Activity
		}
		choices := make([]Choice, 0, len(page.Items)+2)
		for i := len(page.Items) - 1; i >= 0; i-- {
			item := page.Items[i]
			choices = append(choices, Choice{ID: item.Key, Label: item.Title, Description: item.Status + " · " + item.Summary})
		}
		if page.HasMore && page.HistoryCursor != nil {
			choices = append(choices, Choice{ID: "\x00earlier", Label: "Earlier activity"})
		}
		if page.Scope == "history" || len(page.Items) == 0 && page.Total > 0 {
			label := "Latest activity"
			if page.Scope != "history" {
				label = "Read activity"
			}
			choices = append(choices, Choice{ID: "\x00latest", Label: label})
		}
		if len(choices) == 0 {
			return Result{Text: "No activity in the loaded window."}, nil
		}
		question := Question{Kind: ChooseOne, Title: location.Environment + " / " + location.Title + " · Activity", Choices: choices}
		if page.Limited {
			question.Prompt = fmt.Sprintf("%d of %d loaded items", len(page.Items), page.Total)
		}
		answer, err := a.asker.Ask(ctx, question)
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("select one activity item")
		}
		switch answer.ChoiceIDs[0] {
		case "\x00earlier":
			if !page.HasMore || page.HistoryCursor == nil {
				return Result{}, errors.New("activity history is unavailable")
			}
			cursor = page.HistoryCursor
			load = true
		case "\x00latest":
			cursor = nil
			load = true
		default:
			for _, item := range page.Items {
				if item.Key == answer.ChoiceIDs[0] {
					return a.inspectOfferedActivity(ctx, item, false)
				}
			}
			return Result{}, errors.New("select an offered activity item")
		}
	}
}
