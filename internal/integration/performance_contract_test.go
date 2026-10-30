package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// These are real-helper boundary observations and deterministic admission
// gates, not terminal-paint benchmarks or production latency percentiles.
func TestPerformanceActualMetadataHostsAndPartialOutcomes(t *testing.T) {
	for _, hosts := range []int{1, 2, 4} {
		for cycle := range 3 {
			t.Run(fmt.Sprintf("hosts-%d-cycle-%d", hosts, cycle), func(t *testing.T) {
				fixtures := make([]*fixture, hosts)
				profiles := make([]config.Environment, hosts)
				for i := range fixtures {
					fixtures[i] = newEnvironmentFixture(t, fmt.Sprintf("synthetic-env-%d", i), fmt.Sprintf("source-%d", i), "Synthetic source")
					profiles[i] = fixtures[i].profile
				}
				delayed := -1
				release := func() {}
				if hosts > 1 {
					delayed = hosts - 1
					if hosts == 4 {
						delayed = 2
					}
					release = fixtures[delayed].holdNativeShell()
				}
				if hosts == 4 {
					fixtures[3].server.Close()
				}
				state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
				if err != nil {
					t.Fatal(err)
				}
				defer state.Close()
				manager := client.NewManager(profiles, state)
				defer manager.Close()
				a, err := manager.NewSession(profiles[0].ID, "")
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				type observation struct {
					source  client.InboxSource
					elapsed time.Duration
				}
				progress := make(chan observation, hosts)
				done := make(chan error, 1)
				start := time.Now()
				go func() {
					_, err := a.InboxReadProgress(ctx, "", nil, func(s client.InboxSource) { progress <- observation{s, time.Since(start)} })
					done <- err
				}()
				observed := make([]object, 0, hosts)
				useful, failed := 0, 0
				var firstUseful time.Duration
				released := false
				for len(observed) < hosts {
					select {
					case item := <-progress:
						status := "ready"
						if item.source.Error != "" {
							status = "error"
							failed++
						} else {
							useful++
							if firstUseful == 0 {
								firstUseful = item.elapsed
							}
							if !released && delayed >= 0 {
								if item.source.ProfileID == profiles[delayed].ID {
									t.Fatal("delayed source appeared before explicit release")
								}
								// A healthy source is already actionable while the peer
								// remains held. The fixed delay is fixture load, not an SLO.
								released = true
								time.AfterFunc(120*time.Millisecond, release)
							}
						}
						observed = append(observed, object{"source": item.source.ProfileID, "outcome": status, "elapsedMs": float64(item.elapsed) / float64(time.Millisecond), "rows": len(item.source.Page.Threads)})
					case <-ctx.Done():
						t.Fatal("metadata sources did not complete within fixture deadline")
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if useful == 0 || hosts == 4 && failed != 1 || hosts < 4 && failed != 0 {
					t.Fatalf("lost partial outcomes: useful=%d failed=%d", useful, failed)
				}
				counts := make([]object, 0, hosts)
				for i, f := range fixtures {
					c := f.requestCounts()
					if c["/ws"] != 0 || c["rpc:orchestration.subscribeThread"] != 0 {
						t.Fatal("metadata read acquired transcript transport")
					}
					if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
						t.Fatal("metadata read mutated")
					}
					counts = append(counts, object{"source": profiles[i].ID, "descriptor": c["/.well-known/t3/environment"], "shell": c["/api/orchestration/shell"], "sockets": c["/ws"], "effects": 0})
				}
				record, _ := json.Marshal(object{"schema": 1, "boundary": "InboxReadProgress call to per-source callback; actual fresh Node metadata helpers; no transcript or terminal", "hosts": hosts, "cycle": cycle, "firstUsefulMs": float64(firstUseful) / float64(time.Millisecond), "allOutcomesMs": float64(time.Since(start)) / float64(time.Millisecond), "usefulSources": useful, "failedSources": failed, "outcomes": observed, "counts": counts})
				t.Log(string(record))
			})
		}
	}
}

func TestPerformanceActualStopAndRequestBypassHeldMetadataRead(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	request(t, c, "hello", nil, "")
	var store wire.Store
	store.Select("thread-a")
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live })
	release := f.holdNativeShell()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	readDone := make(chan error, 1)
	go func() { _, err := c.Request(ctx, "list", nil, "held-read"); readDone <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for f.requestCounts()["/api/orchestration/shell"] == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.requestCounts()["/api/orchestration/shell"] == 0 {
		t.Fatal("normal read was not held")
	}
	f.addRequests()
	requestStart := time.Now()
	f.broadcast("thread-a", "runtime-request.updated", object{"id": "approval-a"}, 0)
	type stopObservation struct {
		err     error
		elapsed time.Duration
	}
	stopDone := make(chan stopObservation, 1)
	stopStart := time.Now()
	go func() {
		_, err := c.Request(ctx, "stop", object{"threadId": "thread-a", "runId": "run-thread-a"}, "stop-during-held-read")
		stopDone <- stopObservation{err, time.Since(stopStart)}
	}()
	got := untilView(t, c, &store, func(s *wire.Snapshot) bool { return len(s.Metadata.Requests) == 2 })
	requestElapsed := time.Since(requestStart)
	if got.Metadata.Requests[0]["revision"] == "" {
		t.Fatal("request offer lost exact revision")
	}
	stop := <-stopDone
	if stop.err != nil {
		t.Fatal(stop.err)
	}
	select {
	case err := <-readDone:
		t.Fatalf("normal read unexpectedly completed while held: %v", err)
	default:
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("Stop duplicate/lost: %d/%d", effects, dispatches)
	}
	release()
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(object{"schema": 2, "boundary": "actual helper held normal list; request native event to decoded Go offer and Stop input to receipt (overlapping observations, not exclusive CPU/socket)", "requestApplyMs": float64(requestElapsed) / float64(time.Millisecond), "stopReceiptMs": float64(stop.elapsed) / float64(time.Millisecond), "normalReadHeldThroughControl": true, "pendingRequests": len(got.Metadata.Requests), "effects": 1, "dispatches": 1, "boundedReads": f.requestCounts()["/api/orchestration/threads/thread-a/bounded"]})
	t.Log(string(record))
}
