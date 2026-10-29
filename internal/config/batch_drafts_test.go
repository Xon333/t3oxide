package config

import (
	"path/filepath"
	"testing"
)

func TestBatchDraftAndNativeACKAdvanceAtomicallyAndOnlyMatchingSource(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := "\x00file-many/source-thread"
	const text = "\n  task one  \n\nsecond task\n"
	if err = s.SetDraft("draft-environment", source, text); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("draft-environment", "source-thread", "original composer"); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"id": "operation", "threadId": "native-thread", "newThreadId": "native-thread", "op": "launch-project", "environmentId": "launch-environment", "batchDraft": map[string]any{"environment": "draft-environment", "source": source, "before": text, "consumed": "\n  task one  \n"}}
	if err = s.SavePending("launch-environment", false, record); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("launch-environment", false, "wrong-operation", "native-thread", ""); err == nil {
		t.Fatal("unrelated receipt consumed draft")
	}
	if s.Draft("draft-environment", source) != text {
		t.Fatal("unconfirmed task disappeared")
	}
	if err = s.Acknowledge("launch-environment", false, "operation", "native-thread", ""); err != nil {
		t.Fatal(err)
	}
	if s.Draft("draft-environment", source) != "\nsecond task\n" || s.Pending("launch-environment", false) != nil || s.Draft("draft-environment", "source-thread") != "original composer" {
		t.Fatal("native ACK did not atomically preserve exact remaining/source drafts")
	}
	record["id"] = "operation-two"
	if err = s.SavePending("launch-environment", false, record); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("draft-environment", source, "new human edit"); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("launch-environment", false, "operation-two", "native-thread", ""); err != nil {
		t.Fatal(err)
	}
	if s.Draft("draft-environment", source) != "new human edit" {
		t.Fatal("old receipt overwrote a newer list")
	}
}
