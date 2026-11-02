package client

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Xon333/t3oxide/internal/wire"
)

// ActivitySource reads structured source bytes for an explicitly selected loaded
// activity. It shares the inspector's bounded decoder, never its Markdown view.
func (a *Session) ActivitySource(ctx context.Context, offered wire.ActivityItem) (wire.ActivityDetail, error) {
	conn, location, err := a.activityConnection()
	if err != nil {
		return wire.ActivityDetail{}, err
	}
	current := func() bool {
		if ctx.Err() != nil {
			return false
		}
		a.mu.Lock()
		valid := a.conn == conn && a.ready && a.helloVerified && a.environment != nil && a.environment.EnvironmentID == location.EnvironmentID && a.thread == location.ThreadID
		a.mu.Unlock()
		page := a.ActivityItems()
		if valid && page != nil {
			for _, item := range page.Items {
				if item == offered {
					return true
				}
			}
		}
		return false
	}
	if !current() {
		return wire.ActivityDetail{}, errors.New("activity source changed; select it again")
	}
	reply, err := conn.Request(ctx, "inspect-item", map[string]any{"threadId": offered.SourceThreadID, "itemId": offered.ItemID, "revision": offered.UpdatedAt}, "")
	if err != nil {
		return wire.ActivityDetail{}, err
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return wire.ActivityDetail{}, err
	}
	detail, err := wire.DecodeActivityDetail(raw)
	if err != nil {
		return wire.ActivityDetail{}, err
	}
	if !current() || detail.Key != offered.Key || detail.SourceThreadID != offered.SourceThreadID || detail.ItemID != offered.ItemID || detail.RequestedRevision != offered.UpdatedAt || detail.Changed {
		return wire.ActivityDetail{}, errors.New("activity source changed; select it again")
	}
	return detail, nil
}
