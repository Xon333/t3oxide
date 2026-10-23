package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptHistoryAcknowledgmentAndReopenPreserveExactBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	text := " \tGo �\r\nnext\n\n "
	if err = s.SetDraft("env", "thread", text); err != nil {
		t.Fatal(err)
	}
	record := savedOperation("exact", "send")
	record["message"] = text
	if err = s.SavePending("env", false, record); err != nil {
		t.Fatal(err)
	}
	if len(s.PromptHistory("env", "thread")) != 0 {
		t.Fatal("unacknowledged prompt entered recall")
	}
	if err = s.Acknowledge("env", false, "other", "thread", text); err == nil {
		t.Fatal("wrong operation acknowledged")
	}
	if err = s.Acknowledge("env", false, "exact", "thread", text); err != nil {
		t.Fatal(err)
	}
	if err = s.RememberPrompt("env", "thread", "cleared", "\tcleared\r\n�"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := s.PromptHistory("env", "thread")
	if len(entries) != 2 || entries[1].Kind != "sent" || entries[1].Text != text || entries[0].Text != "\tcleared\r\n�" {
		t.Fatalf("exact history lost: %#v", entries)
	}
	if s.Draft("env", "thread") != "" || s.Pending("env", false) != nil {
		t.Fatal("acknowledgment did not clear original state")
	}
	if len(s.PromptHistory("other", "thread")) != 0 {
		t.Fatal("recall crossed environments")
	}
}

func TestPromptHistoryBoundsAndRejectedMutation(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 40; i++ {
		if err := s.RememberPrompt("env", "thread", "attempt", fmt.Sprint(i)+strings.Repeat("x", 64000)); err != nil {
			t.Fatal(err)
		}
	}
	entries := s.PromptHistory("env", "thread")
	total := 0
	for _, e := range entries {
		total += len(e.Text)
	}
	if total > maxPromptHistoryBytes || len(entries) > maxPromptEntries || !strings.HasPrefix(entries[0].Text, "39") {
		t.Fatal("history bounds/order lost")
	}
	if s.RememberPrompt("env", "thread", "sent", "forged") == nil || s.RememberPrompt("env", "thread", "attempt", string([]byte{255})) == nil {
		t.Fatal("invalid entry accepted")
	}
}
