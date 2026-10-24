package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
)

func TestDraftAutosaveIsIdleUntilEditedAndDoesNotDebounceForever(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		wake := make(chan struct{}, 1)
		checks, saves, reports := 0, 0, 0
		dirty := false
		go runDraftSaver(ctx, wake, func() bool { checks++; return dirty },
			func() error { saves++; dirty = false; return nil }, func(error) { reports++ })
		synctest.Wait()
		time.Sleep(time.Hour)
		synctest.Wait()
		if checks != 0 || saves != 0 || reports != 0 {
			t.Fatalf("idle saver did work: checks=%d saves=%d reports=%d", checks, saves, reports)
		}
		for i := 0; i < 7; i++ {
			dirty = true
			wake <- struct{}{}
			synctest.Wait()
			time.Sleep(50 * time.Millisecond)
			synctest.Wait()
			if i < 6 && saves != 0 {
				t.Fatal("save occurred before its bounded coalescing deadline")
			}
		}
		if saves != 1 || dirty {
			t.Fatalf("continuous edits postponed the 350 ms save: saves=%d dirty=%v", saves, dirty)
		}
		oldChecks := checks
		time.Sleep(time.Hour)
		synctest.Wait()
		if saves != 1 || checks != oldChecks {
			t.Fatal("idle session kept a recurring save or dirty-check timer")
		}
		cancel()
		synctest.Wait()
	})
}

func TestDraftAutosaveFailureWaitsForAnEditAndCancellationStopsPendingWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		wake := make(chan struct{}, 1)
		saves, reports := 0, 0
		go runDraftSaver(ctx, wake, func() bool { return true },
			func() error { saves++; return errors.New("fixture storage unavailable") }, func(error) { reports++ })
		wake <- struct{}{}
		synctest.Wait()
		time.Sleep(draftSaveDelay)
		synctest.Wait()
		if saves != 1 || reports != 1 {
			t.Fatal("storage failure was not reported exactly once")
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if saves != 1 || reports != 1 {
			t.Fatal("storage failure caused repeated idle work")
		}
		wake <- struct{}{}
		synctest.Wait()
		cancel()
		synctest.Wait()
		time.Sleep(time.Hour)
		if saves != 1 {
			t.Fatal("retired saver wrote after cancellation")
		}
	})
}

func TestDraftAutosavePersistsWithoutTransportAndExplicitReplacementSurvives(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, state, env := recoveryManager(t)
		a, err := m.NewSession(env.ID, "thread")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go a.autosaveDrafts(ctx)
		if err := a.SetDraft("offline draft\n\tkept exactly"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(draftSaveDelay)
		synctest.Wait()
		if got := state.Draft(env.EnvironmentID, "thread"); got != "offline draft\n\tkept exactly" {
			t.Fatalf("offline draft was not persisted: %q", got)
		}
		if err := a.SetDraft("queued for autosave"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := a.StoreDraft("explicit replacement"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(draftSaveDelay)
		synctest.Wait()
		if got := state.Draft(env.EnvironmentID, "thread"); got != "explicit replacement" {
			t.Fatalf("timer resurrected a superseded draft: %q", got)
		}
		cancel()
		synctest.Wait()
	})
}

func TestDraftAutosavePublishesStorageRecoveryWithoutWaitingForAStreamEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		state, err := config.OpenState(path)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		env := config.Environment{ID: "fixture", EnvironmentID: "env"}
		m := NewManager([]config.Environment{env}, state)
		defer m.Close()
		a, err := m.NewSession(env.ID, "thread")
		if err != nil {
			t.Fatal(err)
		}
		// A directory at the atomic-replace destination produces a deterministic
		// storage failure regardless of filesystem permissions or the caller UID.
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go a.autosaveDrafts(ctx)
		_ = a.SetDraft("first edit")
		synctest.Wait()
		time.Sleep(draftSaveDelay)
		synctest.Wait()
		if !a.Status().DraftSaveFailed {
			t.Fatal("failed local save was not exposed")
		}
		select {
		case <-a.wake:
		default:
			t.Fatal("failed save did not notify the UI")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_ = a.SetDraft("recovered edit") // The previous error remains until save succeeds.
		synctest.Wait()
		time.Sleep(draftSaveDelay)
		synctest.Wait()
		if a.Status().DraftSaveFailed || state.Draft("env", "thread") != "recovered edit" {
			t.Fatal("storage recovery did not publish the saved draft")
		}
		select {
		case <-a.wake:
		default:
			t.Fatal("recovery waited for an unrelated server event to clear the UI failure")
		}
		cancel()
		synctest.Wait()
	})
}
