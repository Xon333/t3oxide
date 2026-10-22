package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func savedOperation(id, op string) map[string]any {
	return map[string]any{"id": id, "op": op, "environmentId": "env", "threadId": "thread", "message": "hello"}
}

func TestDraftsIsolateEnvironmentAndThreadAndPersistPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-drafts.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ env, thread, text string }{{"a", "t", "one"}, {"b", "t", "two"}, {"a", "u", "three"}} {
		if err = s.SetDraft(v.env, v.thread, v.text); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.ClearDraftIfEqual("a", "t", "different"); err != nil {
		t.Fatal(err)
	}
	if s.Draft("a", "t") != "one" || s.Draft("b", "t") != "two" {
		t.Fatal("draft isolation lost")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Draft("a", "u") != "three" {
		t.Fatal("draft not persisted")
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm()&0077 != 0 {
		t.Fatal("state is not private")
	}
	if err = s.ClearDraftIfEqual("a", "t", "one"); err != nil {
		t.Fatal(err)
	}
	if s.Draft("a", "t") != "" {
		t.Fatal("accepted draft not cleared")
	}
}
func TestPendingOperationsKeepExactPayloadAcrossRestartsAndStopLane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-drafts.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	regular, stop := savedOperation("send-id", "send"), savedOperation("stop-id", "stop")
	if err = s.SavePending("env", false, regular); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", true, stop); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", false, savedOperation("new-id", "send")); err == nil {
		t.Fatal("replaced unresolved operation")
	}
	changed := savedOperation("send-id", "send")
	changed["message"] = "different"
	if s.SavePending("env", false, changed) == nil {
		t.Fatal("reused ID with different payload")
	}
	if err = s.SavePending("env", false, regular); err != nil {
		t.Fatal("exact retry rejected", err)
	}
	copy := s.Pending("env", false)
	copy["message"] = "mutated"
	if s.Pending("env", false)["message"] != "hello" {
		t.Fatal("caller changed stored payload")
	}
	s.Close()
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Pending("env", true)["id"] != "stop-id" || s.Pending("env", false)["id"] != "send-id" {
		t.Fatal("operation ID did not survive restart")
	}
	if s.ClearPending("env", true, "wrong-id") == nil {
		t.Fatal("cleared a different operation")
	}
	if err = s.ClearPending("env", true, "stop-id"); err != nil {
		t.Fatal(err)
	}
	if s.Pending("env", false) == nil {
		t.Fatal("clearing Stop removed normal lane")
	}
}
func TestStateLockAndDamagedStateFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-drafts.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := OpenState(path); err == nil {
		second.Close()
		t.Fatal("two clients acquired same state")
	}
	s.Close()
	for _, bad := range []string{"{}", `{"drafts":{},"pending":null}`, `{"drafts":{},"pending":{}} trailing`, `{"drafts":{},"pending":{"env":{"id":"a"}}}`} {
		if err = os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if opened, err := OpenState(path); err == nil {
			opened.Close()
			t.Fatalf("accepted damaged state %s", bad)
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != bad {
			t.Fatal("damaged state overwritten")
		}
	}
}
func TestFailedStateCommitDoesNotPublishOrDiscardDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetDraft("a", "b", "retained"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("a", "b", strings.Repeat("x", 64*1024+1)); err == nil {
		t.Fatal("oversized draft accepted")
	}
	if s.Draft("a", "b") != "retained" {
		t.Fatal("failed update changed draft")
	}
	s.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if s.SavePending("env", false, savedOperation("id", "send")) == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if s.Pending("env", false) != nil {
		t.Fatal("failed save published operation")
	}
}

func TestImportLegacyIsOneTimeAndPreservesOriginalRecovery(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "drafts.json")
	current := filepath.Join(dir, "go-drafts.json")
	original := `{"drafts":{"env/thread":"draft"},"pending":{"env":{"id":"original-id","op":"send","environmentId":"env","threadId":"thread","message":"draft"}},"pendingStops":{"env":{"id":"stop-id","op":"stop","environmentId":"env","threadId":"thread","runId":"run"}}}`
	if err := os.WriteFile(legacy, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenState(current)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ImportLegacy(legacy); err != nil {
		t.Fatal(err)
	}
	if s.Draft("env", "thread") != "draft" || s.Pending("env", false)["id"] != "original-id" || s.Pending("env", true)["id"] != "stop-id" {
		t.Fatal("legacy recovery or draft not imported")
	}
	if err = s.ClearPending("env", false, "original-id"); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportLegacy(legacy); err != nil {
		t.Fatal(err)
	}
	if s.Pending("env", false) != nil {
		t.Fatal("reimported previously resolved legacy operation")
	}
	b, _ := os.ReadFile(legacy)
	if string(b) != original {
		t.Fatal("legacy file changed")
	}
}

func TestImportLegacyDamageFailsClosedAndAbsenceIsAllowed(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenState(filepath.Join(dir, "go-drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	legacy := filepath.Join(dir, "drafts.json")
	if err = s.ImportLegacy(legacy); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(legacy, []byte(`{"drafts":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportLegacy(legacy); err == nil {
		t.Fatal("invalid legacy state imported")
	}
	if _, err = os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("failed import created current state")
	}
}

func TestAcknowledgeAtomicallyClearsOperationAndOnlyUnchangedDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetDraft("env", "thread", "hello"); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", false, savedOperation("send-id", "send")); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if err = s.Acknowledge("env", false, "send-id", "thread", "hello"); err == nil {
		t.Fatal("failed ack save unexpectedly succeeded")
	}
	if s.Pending("env", false) == nil || s.Draft("env", "thread") != "hello" {
		t.Fatal("failed ack discarded recovery or draft")
	}
	s.path = path
	if err = s.Acknowledge("env", false, "send-id", "thread", "hello"); err != nil {
		t.Fatal(err)
	}
	if s.Pending("env", false) != nil || s.Draft("env", "thread") != "" {
		t.Fatal("ack failed to atomically clear state")
	}
	if err = s.SavePending("env", false, savedOperation("next-id", "send")); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("env", "thread", "new draft written while sending"); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "next-id", "thread", "hello"); err != nil {
		t.Fatal(err)
	}
	if s.Draft("env", "thread") != "new draft written while sending" {
		t.Fatal("ack cleared subsequently edited draft")
	}
}
