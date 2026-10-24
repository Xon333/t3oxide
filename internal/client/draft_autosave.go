package client

import (
	"context"
	"time"
)

const draftSaveDelay = 350 * time.Millisecond

// autosaveDrafts belongs to the attachment, not its current transport. It also
// saves while reconnecting; disk writes do not delay incoming transcript frames.
// The existing persistence lock still serializes saves with exact acknowledgments.
func (a *Session) autosaveDrafts(ctx context.Context) {
	runDraftSaver(ctx, a.draftWake, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.draftDirty
	}, a.flushDraft, func(err error) {
		a.notice("Draft save failed; sending is blocked: " + err.Error())
		a.signal()
	})
}

// A dirty edit starts one timer. Later keystrokes do not reset its deadline, so
// continuous typing is saved within 350 ms instead of waiting for a pause. After
// the save, an idle session owns no timer. Errors remain in the normal draft state;
// the next edit, explicit send or final close retries persistence, never a write
// to T3. The lifecycle owner cancels this worker before its final synchronous save.
func runDraftSaver(ctx context.Context, wake <-chan struct{}, dirty func() bool, save func() error, report func(error)) {
	var timer *time.Timer
	var due <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			if due == nil && dirty() {
				timer = time.NewTimer(draftSaveDelay)
				due = timer.C
			}
		case <-due:
			timer, due = nil, nil
			if err := save(); err != nil {
				report(err)
			}
		}
	}
}
