package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Exercise the entire Go -> stdio -> Node -> HTTP cancellation path. Unit
// cancellation acknowledgements alone cannot prove the held handler retired.
func TestActualAdapterCancelledMetadataReadReleasesHTTPAndRegularLane(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	request(t, c, "hello", nil, "")
	release := f.holdNativeShell()
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Request(ctx, "list", nil, "cancel-held-metadata")
		done <- err
	}()
	waitHolds := func(want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			f.mu.Lock()
			active := f.activeShellHolds
			f.mu.Unlock()
			if active == want {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("held HTTP handlers did not reach %d", want)
	}
	waitHolds(1)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled read: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled Go waiter did not retire")
	}
	// Do not release the fixture gate: HTTP must terminate from cancellation.
	waitHolds(0)
	request(t, c, "hello", nil, "after-cancel")
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("read cancellation mutated native state: %d/%d", effects, dispatches)
	}
	if counts := f.requestCounts(); counts["/ws"] != 0 || counts["/api/orchestration/shell"] != 1 {
		t.Fatalf("cancellation replayed metadata or attached detail: %+v", counts)
	}
}
