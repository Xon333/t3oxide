package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This is the previous Go client's JSON shape and read/modify/write behavior:
// unknown promptHistory disappears, while drafts and recovery lanes survive.
func rewriteWithPreviousGoShape(t *testing.T, path string) {
	t.Helper()
	var old struct {
		Drafts         map[string]string         `json:"drafts"`
		Pending        map[string]map[string]any `json:"pending"`
		PendingStops   map[string]map[string]any `json:"pendingStops"`
		ModelOverrides map[string]ModelOverride  `json:"modelOverrides,omitempty"`
		ModelPresets   map[string][]ModelPreset  `json:"modelPresets,omitempty"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err = dec.Decode(&old); err != nil {
		t.Fatal(err)
	}
	old.Drafts["legacy/thread"] = "written by the previous Go client"
	b, err = json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeStateFile(path, append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestPromptHistorySurvivesPreviousGoRewriteWithoutReplayingRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	cleared, sent, attempt := " \tunique cleared �\r\n\n ", "\taccepted\r\n", " rejected\x00paste\n "
	if err = s.SetDraft("env", "thread", cleared); err != nil {
		t.Fatal(err)
	}
	if err = s.RememberPrompt("env", "thread", "cleared", cleared); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("env", "thread", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("env", "thread", sent); err != nil {
		t.Fatal(err)
	}
	record := savedOperation("accepted-id", "send")
	record["message"] = sent
	if err = s.SavePending("env", false, record); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "accepted-id", "thread", sent); err != nil {
		t.Fatal(err)
	}
	if err = s.RememberPrompt("env", "thread", "attempt", attempt); err != nil {
		t.Fatal(err)
	}
	regular, stop := savedOperation("held-send", "send"), savedOperation("independent-stop", "stop")
	regular["message"] = "pending exact\tbytes\r\n"
	stop["runId"], stop["queueHold"] = "run", true
	for _, lane := range []struct {
		stop   bool
		record map[string]any
	}{{false, regular}, {true, stop}} {
		if err = s.SavePending("env", lane.stop, lane.record); err != nil {
			t.Fatal(err)
		}
	}
	want := s.PromptHistory("env", "thread")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	rewriteWithPreviousGoShape(t, path)
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte(`"promptHistory"`)) {
		t.Fatal("previous Go rewrite retained the unknown field")
	}
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.PromptHistory("env", "thread"); !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback lost exact recall: got %#v, want %#v", got, want)
	}
	if s.Draft("env", "thread") != "" || s.Draft("legacy", "thread") == "" {
		t.Fatal("recall loading changed main drafts")
	}
	if !reflect.DeepEqual(s.Pending("env", false), regular) || !reflect.DeepEqual(s.Pending("env", true), stop) {
		t.Fatal("recall loading changed pending/Stop/queue-hold payloads")
	}
	if err = s.SavePending("env", false, regular); err != nil {
		t.Fatal("exact same-ID retry rejected", err)
	}
	changed := cloneRecord(regular)
	changed["message"] = "changed"
	if s.SavePending("env", false, changed) == nil {
		t.Fatal("changed same-ID retry accepted")
	}
	stat, err := os.Stat(path + ".prompts.json")
	if err != nil || stat.Mode().Perm()&0077 != 0 {
		t.Fatal("compatibility copy is missing or not private", err)
	}
}

func TestPromptSidecarFailureBlocksRecallSaveAndAcknowledgment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	text := "\tsole draft\r\n "
	if err = s.SetDraft("env", "thread", text); err != nil {
		t.Fatal(err)
	}
	record := savedOperation("same-id", "send")
	record["message"] = text
	if err = s.SavePending("env", false, record); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", true, savedOperation("stop-id", "stop")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	// A directory at the sidecar destination makes atomic publication fail.
	if err = os.Mkdir(path+".prompts.json", 0700); err != nil {
		t.Fatal(err)
	}
	if s.RememberPrompt("env", "thread", "cleared", text) == nil {
		t.Fatal("failed retention was reported as safe to clear")
	}
	if s.Acknowledge("env", false, "same-id", "thread", text) == nil {
		t.Fatal("ACK cleared state without a compatibility copy")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || s.Draft("env", "thread") != text || s.Pending("env", false)["id"] != "same-id" || s.Pending("env", true)["id"] != "stop-id" || len(s.PromptHistory("env", "thread")) != 0 {
		t.Fatal("failed retention changed original draft, pending/Stop, history or main file")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".t3oxide-state-*"))
	if len(leftovers) != 0 {
		t.Fatalf("failed publication left staging files: %v", leftovers)
	}
}

func TestPromptSidecarMainFailureRetainsFullPriorHistoryAcrossRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPromptEntries; i++ {
		if err = s.RememberPrompt("env", "thread", "cleared", fmt.Sprintf("unique old %d\t\r\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	want := s.PromptHistory("env", "thread")
	text := "\tnew ACK text\r\n "
	if err = s.SetDraft("env", "thread", text); err != nil {
		t.Fatal(err)
	}
	record := savedOperation("pending-id", "send")
	record["message"] = text
	if err = s.SavePending("env", false, record); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", true, savedOperation("stop-id", "stop")); err != nil {
		t.Fatal(err)
	}
	// Keep the real prior file while forcing the main rename to fail after
	// the sidecar prewrite. Restore it to simulate interruption before rename.
	original := path + ".original"
	if err = os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if s.Acknowledge("env", false, "pending-id", "thread", text) == nil {
		t.Fatal("main publication unexpectedly succeeded")
	}
	if s.Draft("env", "thread") != text || s.Pending("env", false)["id"] != "pending-id" || !reflect.DeepEqual(s.PromptHistory("env", "thread"), want) {
		t.Fatal("failed main publication discarded committed state")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(original, path); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	rewriteWithPreviousGoShape(t, path)
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.PromptHistory("env", "thread")
	if len(got) != maxPromptEntries || got[0] != newPromptEntry("env", "thread", "attempt", text) || !reflect.DeepEqual(got[1:], want[:maxPromptEntries-1]) || s.Draft("env", "thread") != text || s.Pending("env", false)["id"] != "pending-id" || s.Pending("env", true)["id"] != "stop-id" {
		t.Fatal("prepared recovery lost priority or changed main recovery")
	}
	if len(s.promptCommitted) != maxPromptEntries+1 || !reflect.DeepEqual(s.promptCommitted[:maxPromptEntries], want) {
		t.Fatal("failed publication discarded prior unique recall from its protected baseline")
	}
	if err = s.Acknowledge("env", false, "pending-id", "thread", text); err != nil {
		t.Fatal("explicit acknowledgment retry failed", err)
	}
	got = s.PromptHistory("env", "thread")
	if len(got) != maxPromptEntries || got[0].Kind != "sent" || got[0].Text != text || s.Pending("env", false) != nil || s.Pending("env", true)["id"] != "stop-id" {
		t.Fatal("successful retry did not publish bounded ACK history and independent Stop")
	}
}

func TestPromptSidecarPreparedPublicationReconciliation(t *testing.T) {
	for _, stage := range []string{"before-main", "after-main", "after-rollback", "main-absent"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s, err := OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			previous := savedOperation("previous-id", "send")
			previous["message"] = "old unique\t\r\n"
			if err = s.SavePending("env", false, previous); err != nil {
				t.Fatal(err)
			}
			if err = s.Acknowledge("env", false, "previous-id", "thread", "old unique\t\r\n"); err != nil {
				t.Fatal(err)
			}
			if err = s.SetDraft("env", "thread", "hello"); err != nil {
				t.Fatal(err)
			}
			if err = s.SavePending("env", false, savedOperation("ack-id", "send")); err != nil {
				t.Fatal(err)
			}
			next := cloneState(s.saved)
			rememberPrompt(&next, "env", "thread", "sent", "hello")
			delete(next.Drafts, "env/thread")
			delete(next.Pending, "env")
			main, err := json.MarshalIndent(next, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			main = append(main, '\n')
			if err = s.writePromptSidecar(promptSidecar{Version: 1, Committed: s.saved.PromptHistory, Prepared: next.PromptHistory, StateSHA256: promptStateDigest(main)}); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if stage == "after-main" || stage == "after-rollback" {
				if err = writeStateFile(path, main); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "after-rollback" {
				rewriteWithPreviousGoShape(t, path)
			}
			if stage == "main-absent" {
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			s, err = OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			entries := s.PromptHistory("env", "thread")
			if stage == "after-main" {
				if !reflect.DeepEqual(entries, next.PromptHistory) {
					t.Fatal("main digest did not confirm committed sent history")
				}
			} else {
				attempt, sent := 0, 1
				if stage == "before-main" {
					// Existing main history remains authoritative.
					attempt, sent = 1, 0
				}
				if len(entries) != 2 || entries[sent].Kind != "sent" || entries[sent].Text != "old unique\t\r\n" || entries[attempt].Kind != "attempt" || entries[attempt].Text != "hello" {
					t.Fatalf("uncertain publication lost text or asserted sent: %#v", entries)
				}
			}
			if stage == "before-main" {
				if s.Draft("env", "thread") != "hello" || s.Pending("env", false)["id"] != "ack-id" {
					t.Fatal("prepared recall cleared uncommitted main recovery")
				}
			} else if s.Draft("env", "thread") != "" || s.Pending("env", false) != nil {
				t.Fatal("sidecar inferred or replayed a main operation")
			}
		})
	}
}

func TestPromptSidecarProtectsExistingMainHistoryBeforeAnyMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RememberPrompt("env", "thread", "cleared", "sole cleared copy\t\r\n� "); err != nil {
		t.Fatal(err)
	}
	want := s.PromptHistory("env", "thread")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path + ".prompts.json"); err != nil {
		t.Fatal(err)
	}
	// Simulate a state file created by the earlier history implementation.
	before, _ := os.ReadFile(path)
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("sidecar migration changed the main transaction")
	}
	rewriteWithPreviousGoShape(t, path)
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !reflect.DeepEqual(s.PromptHistory("env", "thread"), want) {
		t.Fatal("opening existing history did not protect immediate rollback")
	}
}

func TestRecoveredPreparedPromptSurvivesAnotherFailedCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPromptEntries-1; i++ {
		if err = s.RememberPrompt("env", "thread", "cleared", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	// ACK A published, but finalization was interrupted before an old-client
	// rewrite removed the main history and digest evidence.
	next := cloneState(s.saved)
	rememberPrompt(&next, "env", "thread", "sent", "unique A\t\r\n")
	main, _ := json.MarshalIndent(next, "", "  ")
	main = append(main, '\n')
	if err = s.writePromptSidecar(promptSidecar{Version: 1, Committed: s.saved.PromptHistory, Prepared: next.PromptHistory, StateSHA256: promptStateDigest(main)}); err != nil {
		t.Fatal(err)
	}
	if err = writeStateFile(path, main); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	rewriteWithPreviousGoShape(t, path)
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	want := s.PromptHistory("env", "thread")
	if len(want) != maxPromptEntries || want[0].Text != "unique A\t\r\n" || want[0].Kind != "attempt" {
		t.Fatal("did not recover A as the newest bounded attempt")
	}
	original := path + ".original"
	if err = os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	// B's failed main publication must retain the entire prior checkpoint,
	// including A's only compatibility copy.
	if s.RememberPrompt("env", "thread", "attempt", "new B") == nil {
		t.Fatal("main publication unexpectedly succeeded")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(original, path); err != nil {
		t.Fatal(err)
	}
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := s.PromptHistory("env", "thread")
	if len(got) != maxPromptEntries || got[0] != newPromptEntry("env", "thread", "attempt", "new B") || !reflect.DeepEqual(got[1:], want[:maxPromptEntries-1]) {
		t.Fatal("failed second checkpoint lost unique recovered A or new B")
	}
	protected := map[string]PromptEntry{}
	for _, entry := range s.promptCommitted {
		protected[entry.ID] = entry
	}
	for _, entry := range want {
		if protected[entry.ID] != entry {
			t.Fatal("failed second checkpoint discarded the prior protected baseline")
		}
	}
	if len(protected) != maxPromptEntries+1 {
		t.Fatal("failed second checkpoint retained an unexpected number of entries")
	}
}

func TestFullPromptHistoryRetainsPreparedACKAcrossOrdinarySave(t *testing.T) {
	for _, bound := range []string{"entries", "bytes"} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rollback=%t", bound, rollback), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "state.json")
				s, err := OpenState(path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { s.Close() }()
				count := maxPromptEntries
				if bound == "bytes" {
					count = maxPromptHistoryBytes / (64 * 1024)
				}
				for i := 0; i < count; i++ {
					text := fmt.Sprintf("old %02d", i)
					if bound == "bytes" {
						text += strings.Repeat("x", 64*1024-len(text))
					}
					if err = s.RememberPrompt("env", "thread", "cleared", text); err != nil {
						t.Fatal(err)
					}
				}
				oldest := s.saved.PromptHistory[count-1]
				text := " \tunique published A�\r\n\n "
				ack := savedOperation("ack-A", "send")
				ack["message"] = text
				if err = s.SetDraft("env", "thread", text); err != nil {
					t.Fatal(err)
				}
				if err = s.SavePending("env", false, ack); err != nil {
					t.Fatal(err)
				}
				stop := savedOperation("stop-id", "stop")
				stop["runId"], stop["queueHold"] = "run", true
				if err = s.SavePending("env", true, stop); err != nil {
					t.Fatal(err)
				}
				// Publish the correlated ACK main transaction, leaving the
				// prewrite envelope in place as an interrupted finalization.
				next := cloneState(s.saved)
				rememberPrompt(&next, "env", "thread", "sent", text)
				delete(next.Drafts, "env/thread")
				delete(next.Pending, "env")
				main, err := json.MarshalIndent(next, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				main = append(main, '\n')
				if err = s.writePromptSidecar(promptSidecar{Version: 1, Committed: s.saved.PromptHistory, Prepared: next.PromptHistory, StateSHA256: promptStateDigest(main)}); err != nil {
					t.Fatal(err)
				}
				if err = writeStateFile(path, main); err != nil {
					t.Fatal(err)
				}
				if err = s.Close(); err != nil {
					t.Fatal(err)
				}
				if rollback {
					rewriteWithPreviousGoShape(t, path)
				} else {
					// A main-only draft change invalidates the whole-file digest
					// while preserving the newer main history and its compaction.
					next.Drafts["legacy/thread"] = "main-only edit"
					main, err = json.MarshalIndent(next, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if err = writeStateFile(path, append(main, '\n')); err != nil {
						t.Fatal(err)
					}
				}
				s, err = OpenState(path)
				if err != nil {
					t.Fatal(err)
				}
				want := append([]PromptEntry(nil), next.PromptHistory...)
				if rollback {
					want[0] = newPromptEntry("env", "thread", "attempt", text)
				}
				assertRecall := func() {
					t.Helper()
					got := s.PromptHistory("env", "thread")
					if !reflect.DeepEqual(got, want) {
						t.Fatal("prepared ACK lost priority or main compaction was reversed")
					}
					if err := validatePromptHistory(got); err != nil {
						t.Fatal("recovery exceeded normal recall bounds", err)
					}
					for _, entry := range got {
						if entry.ID == oldest.ID {
							t.Fatal("normal oldest entry displaced recovered ACK")
						}
					}
					if s.Pending("env", false) != nil || !reflect.DeepEqual(s.Pending("env", true), stop) || s.Draft("legacy", "thread") == "" {
						t.Fatal("recall changed main drafts, ACK operation ID or independent Stop")
					}
				}
				assertRecall()
				if s.Draft("env", "thread") != "" {
					t.Fatal("recall replayed the acknowledged draft")
				}
				if err = s.SetDraft("env", "thread", "ordinary draft\t\r\n"); err != nil {
					t.Fatal(err)
				}
				if err = s.Close(); err != nil {
					t.Fatal(err)
				}
				s, err = OpenState(path)
				if err != nil {
					t.Fatal(err)
				}
				assertRecall()
				if s.Draft("env", "thread") != "ordinary draft\t\r\n" {
					t.Fatal("ordinary draft did not survive finalization")
				}
			})
		}
	}
}

func TestUnresolvedPromptRetentionLimitBlocksOverwrite(t *testing.T) {
	for _, bound := range []string{"entries", "bytes"} {
		t.Run(bound, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s, err := OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetDraft("env", "thread", "original draft"); err != nil {
				t.Fatal(err)
			}
			staged := promptSidecar{Version: 1, StateSHA256: strings.Repeat("0", 64)}
			count := maxPromptEntries
			if bound == "bytes" {
				count = maxPromptHistoryBytes / (64 * 1024)
			}
			for i := 0; i < count; i++ {
				committed, prepared := fmt.Sprintf("committed %d", i), fmt.Sprintf("prepared %d", i)
				if bound == "bytes" {
					committed += strings.Repeat("x", 64*1024-len(committed))
					prepared += strings.Repeat("x", 64*1024-len(prepared))
				}
				staged.Committed = append(staged.Committed, newPromptEntry("env", "thread", "cleared", committed))
				staged.Prepared = append(staged.Prepared, newPromptEntry("env", "thread", "attempt", prepared))
			}
			if err = s.writePromptSidecar(staged); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			beforeMain, _ := os.ReadFile(path)
			beforeSidecar, _ := os.ReadFile(path + ".prompts.json")
			if s.RememberPrompt("env", "thread", "cleared", "one more unique prompt") == nil {
				t.Fatal("overwrote unresolved unique recall beyond its retention bound")
			}
			afterMain, _ := os.ReadFile(path)
			afterSidecar, _ := os.ReadFile(path + ".prompts.json")
			if !bytes.Equal(beforeMain, afterMain) || !bytes.Equal(beforeSidecar, afterSidecar) || s.Draft("env", "thread") != "original draft" {
				t.Fatal("retention limit failure changed original state or recall")
			}
		})
	}
}

func TestInvalidPromptSidecarsFailClosedWithoutChangingRecovery(t *testing.T) {
	valid := promptSidecar{Version: 1, Committed: []PromptEntry{newPromptEntry("env", "thread", "cleared", "sole recall\t\r\n")}}
	encoded, _ := json.Marshal(valid)
	badID := valid
	badID.Committed = append([]PromptEntry(nil), valid.Committed...)
	badID.Committed[0].Text = "changed without updating ID"
	badEncoded, _ := json.Marshal(badID)
	tooMany := valid
	tooMany.Committed = nil
	for i := 0; i <= maxPromptEntries; i++ {
		tooMany.Committed = append(tooMany.Committed, newPromptEntry("env", "thread", "cleared", fmt.Sprint(i)))
	}
	tooMany.Prepared, tooMany.Committed = tooMany.Committed, nil
	tooMany.StateSHA256 = strings.Repeat("0", 64)
	tooManyPreparedEncoded, _ := json.Marshal(tooMany)
	tooMany.Prepared = tooMany.Prepared[:maxPromptEntries]
	for i := maxPromptEntries; i <= 2*maxPromptEntries; i++ {
		tooMany.Committed = append(tooMany.Committed, newPromptEntry("env", "thread", "cleared", fmt.Sprint(i)))
	}
	// Every individual snapshot fits; their unique union exceeds the bound.
	tooManyUnionEncoded, _ := json.Marshal(tooMany)
	tooLarge := promptSidecar{Version: 1}
	for i := 0; i < 9; i++ {
		tooLarge.Prepared = append(tooLarge.Prepared, newPromptEntry("env", "thread", "attempt", fmt.Sprint(i)+strings.Repeat("x", 64*1024-1)))
	}
	tooLarge.StateSHA256 = strings.Repeat("0", 64)
	tooLargeEncoded, _ := json.Marshal(tooLarge)
	missingDigest, _ := json.Marshal(promptSidecar{Version: 1, Prepared: valid.Committed})
	for name, bad := range map[string][]byte{
		"null": []byte("null"), "damaged": []byte("{"), "version": []byte(`{"version":2}`),
		"trailing": append(append([]byte(nil), encoded...), []byte(" {}")...),
		"unknown":  []byte(`{"version":1,"unexpected":"unique text"}`),
		"bad-ID":   badEncoded, "too-many-prepared": tooManyPreparedEncoded, "too-many-union": tooManyUnionEncoded,
		"history-too-large": tooLargeEncoded,
		"missing-digest":    missingDigest,
		"invalid-digest":    []byte(`{"version":1,"stateSHA256":"invalid"}`),
		"oversized":         []byte(strings.Repeat(" ", maxPromptSidecarBytes+1)),
		"bad-UTF8":          append(append([]byte(nil), encoded...), byte(255)),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s, err := OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetDraft("env", "thread", "original draft\t\r\n"); err != nil {
				t.Fatal(err)
			}
			if err = s.SavePending("env", false, savedOperation("same-id", "send")); err != nil {
				t.Fatal(err)
			}
			if err = s.SavePending("env", true, savedOperation("stop-id", "stop")); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path+".prompts.json", bad, 0600); err != nil {
				t.Fatal(err)
			}
			if opened, err := OpenState(path); err == nil {
				opened.Close()
				t.Fatal("invalid sidecar accepted")
			}
			after, _ := os.ReadFile(path)
			retained, _ := os.ReadFile(path + ".prompts.json")
			if !bytes.Equal(before, after) || !bytes.Equal(bad, retained) {
				t.Fatal("failed open modified main recovery or invalid sidecar")
			}
			// Repair only the invalid fixture and prove failed open released its lock.
			if err = os.Remove(path + ".prompts.json"); err != nil {
				t.Fatal(err)
			}
			s, err = OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Draft("env", "thread") != "original draft\t\r\n" || s.Pending("env", false)["id"] != "same-id" || s.Pending("env", true)["id"] != "stop-id" {
				t.Fatal("main recovery was not intact after sidecar repair")
			}
		})
	}
}
