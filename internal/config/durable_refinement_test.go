package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPresentationIdentityReadsDoNotWaitForStateWriter(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pin := CreationDestination{ProfileID: "profile", EnvironmentID: "host", ProjectID: "project"}
	if err = s.SetCreationDestination(&pin); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("host", false, map[string]any{"id": "original-op", "environmentId": "host", "threadId": "thread", "op": "send", "message": "exact human draft"}); err != nil {
		t.Fatal(err)
	}
	// Hold the writer lock exactly as an fsync transaction does. View-facing
	// identity reads must finish without waiting for it, not merely be fast
	// on an uncontended fixture or state-less render benchmark.
	s.mu.Lock()
	done := make(chan bool, 1)
	go func() {
		got, ok := s.CreationDestination()
		done <- s.HasPendingRecords() && ok && got == pin
	}()
	select {
	case ok := <-done:
		s.mu.Unlock()
		if !ok {
			t.Fatal("presentation snapshot lost committed identity")
		}
	case <-time.After(time.Second):
		s.mu.Unlock()
		t.Fatal("View-facing read waited behind the state writer")
	}
	path := s.path
	s.path = t.TempDir()
	if err = s.ClearPending("host", false, "original-op"); err == nil {
		t.Fatal("fixture failure did not prevent publication")
	}
	if !s.HasPendingRecords() {
		t.Fatal("failed publication cleared the visible recovery cue")
	}
	s.path = path
	if err = s.ClearPending("host", false, "original-op"); err != nil || s.HasPendingRecords() {
		t.Fatal("committed clear did not publish its new summary", err)
	}
}

func TestCreationDestinationRestartAtomicFailureAndResetPreserveDrafts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("A", "thread", "exact \r\n\t draft"); err != nil {
		t.Fatal(err)
	}
	pin := CreationDestination{ProfileID: "target-profile", EnvironmentID: "target-environment", ProjectID: "project"}
	if err = s.SetCreationDestination(&pin); err != nil {
		t.Fatal(err)
	}
	pin.ProjectID = "changed caller"
	if got, _ := s.CreationDestination(); got.ProjectID != "project" {
		t.Fatal("caller mutated committed pin")
	}
	oldPath := s.path
	s.path = t.TempDir()
	if err = s.SetCreationDestination(&CreationDestination{ProfileID: "other", EnvironmentID: "other", ProjectID: "other"}); err == nil {
		t.Fatal("publication failure became success")
	}
	s.path = oldPath
	if got, _ := s.CreationDestination(); got.ProjectID != "project" {
		t.Fatal("failed pin replaced previous state")
	}
	s.Close()
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, ok := s.CreationDestination()
	if !ok || got.ProfileID != "target-profile" || got.EnvironmentID != "target-environment" || got.ProjectID != "project" {
		t.Fatal(got)
	}
	if s.Draft("A", "thread") != "exact \r\n\t draft" {
		t.Fatal("pin lost draft")
	}
	if err = s.SetCreationDestination(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok = s.CreationDestination(); ok {
		t.Fatal("reset retained pin")
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "modelSelection") || strings.Contains(string(data), "workspaceStrategy") {
		t.Fatal("destination snapshotted native defaults")
	}
}

func receiptFixture(t *testing.T, s *State) (BatchReceipt, func(int) map[string]any) {
	t.Helper()
	const original = "  A\r\n\tB\n界 C\n"
	r := BatchReceipt{ID: "batch", EnvironmentID: "draft-env", Source: "\x00file-many/source", Original: original, CreatedAt: "2026-10-30T08:00:00Z", Items: []BatchReceiptItem{{Start: 0, End: 5, State: BatchRemainingDraft}, {Start: 5, End: 8, State: BatchRemainingDraft}, {Start: 8, End: 14, State: BatchRemainingDraft}}}
	if err := s.SetDraft(r.EnvironmentID, r.Source, r.Original); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginBatchReceipt(r); err != nil {
		t.Fatal(err)
	}
	return r, func(i int) map[string]any {
		return map[string]any{"id": []string{"operation-A", "operation-B", "operation-C"}[i], "threadId": []string{"thread-A", "thread-B", "thread-C"}[i], "newThreadId": []string{"thread-A", "thread-B", "thread-C"}[i], "environmentId": "target-env", "projectId": "project", "op": "launch-project", "message": r.ItemText(i), "unknown": map[string]any{"exact": json.Number("9007199254740991")}, "batchDraft": map[string]any{"environment": r.EnvironmentID, "source": r.Source, "before": r.Original[r.Items[i].Start:], "consumed": r.ItemText(i), "receiptId": r.ID, "itemIndex": i}}
	}
}

func TestBatchReceiptRestartKeepsAcknowledgedRecoveryAndDraftWithSameID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	r, record := receiptFixture(t, s)
	if err = s.SavePending("target-env", false, record(2)); err == nil {
		t.Fatal("unacknowledged prefix admitted later item")
	}
	if err = s.SavePending("target-env", false, record(0)); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("target-env", false, "operation-A", "thread-A", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("target-env", false, record(1)); err != nil {
		t.Fatal(err)
	}
	if err = s.PauseBatchReceipt(r.ID, "May already be sent — Review saved action"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	receipts := s.BatchReceipts()
	if len(receipts) != 1 || receipts[0].Original != r.Original || receipts[0].Items[0].State != BatchAcknowledged || receipts[0].Items[1].State != BatchNeedsRecovery || receipts[0].Items[2].State != BatchRemainingDraft {
		t.Fatalf("incorrect restarted receipt: %+v", receipts)
	}
	pending := s.Pending("target-env", false)
	if pending["id"] != "operation-B" || pending["message"] != r.ItemText(1) || pending["unknown"].(map[string]any)["exact"] != json.Number("9007199254740991") {
		t.Fatal("original operation changed on restart")
	}
	changed := record(1)
	changed["id"] = "replacement"
	if err = s.SavePending("target-env", false, changed); err == nil {
		t.Fatal("new operation replaced recovery")
	}
	if err = s.SetDraft(r.EnvironmentID, r.Source, "new human list\t\r\n"); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("target-env", false, "operation-B", "thread-B", ""); err != nil {
		t.Fatal(err)
	}
	if s.Draft(r.EnvironmentID, r.Source) != "new human list\t\r\n" {
		t.Fatal("late receipt overwrote edited list")
	}
	got := s.BatchReceipts()[0]
	if got.Items[0].ThreadID != "thread-A" || got.Items[1].State != BatchAcknowledged || got.Items[1].ThreadID != "thread-B" || got.Items[2].OperationID != "" || s.Pending("target-env", false) != nil {
		t.Fatal("receipt replayed or retargeted remaining draft")
	}
	got.Items[0].ThreadID = "caller mutation"
	if s.BatchReceipts()[0].Items[0].ThreadID != "thread-A" {
		t.Fatal("mutable receipt escaped")
	}
}

func TestBatchReceiptDurabilityFailureRetainsOriginalAndNoPendingPublication(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, record := receiptFixture(t, s)
	path := s.path
	s.path = t.TempDir()
	if err = s.SavePending("target-env", false, record(0)); err == nil {
		t.Fatal("failed receipt publication admitted dispatch")
	}
	s.path = path
	if s.Pending("target-env", false) != nil || s.BatchReceipts()[0].Items[0].State != BatchRemainingDraft || s.Draft(r.EnvironmentID, r.Source) != r.Original {
		t.Fatal("failed transaction changed recoverable original")
	}
	bad := record(0)
	bad["batchDraft"].(map[string]any)["before"] = "wrong input"
	if err = s.SavePending("target-env", false, bad); err == nil {
		t.Fatal("mismatched human input binding accepted")
	}
}

func TestReviewPositionIsBoundedAndSourceQualified(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := ReviewPosition{ProfileID: "profile", EnvironmentID: "environment", ProjectID: "project", ThreadID: "thread", Digest: strings.Repeat("a", 64), Offset: 17}
	if err = s.SaveReviewPosition(p); err != nil {
		t.Fatal(err)
	}
	other := p
	other.EnvironmentID = "other"
	if _, ok := s.ReviewPosition(other); ok {
		t.Fatal("cross-source identical hash restored position")
	}
	got, ok := s.ReviewPosition(p)
	if !ok || got.Offset != 17 {
		t.Fatal(got)
	}
	p.Digest = ""
	if err = s.SaveReviewPosition(p); err != nil {
		t.Fatal(err)
	}
	if _, ok = s.ReviewPosition(p); ok {
		t.Fatal("invalidated review retained position")
	}
}
