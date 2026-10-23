package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const maxPromptEntries = 24
const maxPromptHistoryBytes = 512 * 1024

// The protected baseline can retain two histories after an interrupted write;
// the next prepared history stays bounded to one. Their union is also bounded
// to two histories, failing closed before overwriting unresolved unique text.
// This file contains no executable operations. Its encoded bound also covers
// JSON escaping and entry metadata.
const maxPromptSidecarBytes = 3 * maxStateBytes

type promptSidecar struct {
	Version     int           `json:"version"`
	Committed   []PromptEntry `json:"committed"`
	Prepared    []PromptEntry `json:"prepared,omitempty"`
	StateSHA256 string        `json:"stateSHA256,omitempty"`
}

func promptStateDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// loadPromptSidecar runs under the process lock acquired by OpenState. A bad
// compatibility copy blocks opening without changing either file. Main drafts
// and both operation lanes always come exclusively from the main state file.
func (s *State) loadPromptSidecar(main []byte) error {
	path := s.path + ".prompts.json"
	err := s.readPromptSidecar(path, main)
	if err != nil {
		return fmt.Errorf("saved prompt recall could not be loaded; preserve and repair %s before sending new work: %w", path, err)
	}
	return nil
}

func (s *State) readPromptSidecar(path string, main []byte) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		s.promptCommitted = append([]PromptEntry(nil), s.saved.PromptHistory...)
		// Protect history written by a pre-sidecar development build, even if
		// this open is followed immediately by running an older Go client.
		if len(s.promptCommitted) != 0 {
			return s.writePromptSidecar(promptSidecar{Version: 1, Committed: s.promptCommitted})
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPromptSidecarBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxPromptSidecarBytes || !utf8.Valid(b) {
		return errors.New("prompt compatibility copy exceeds its encoded limit or contains invalid UTF-8")
	}
	var saved promptSidecar
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&saved); err != nil {
		return err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("prompt compatibility copy has trailing content")
	}
	if err = validatePromptSidecar(saved); err != nil {
		return err
	}
	s.promptCommitted = append([]PromptEntry(nil), saved.Committed...)
	s.promptSidecarDirty = saved.StateSHA256 != ""
	if saved.StateSHA256 != "" && saved.StateSHA256 == promptStateDigest(main) {
		if !equalPromptHistory(saved.Prepared, s.saved.PromptHistory) {
			return errors.New("prepared prompt history does not match its main transaction")
		}
		// The exact main publication confirms the prepared ACK transaction.
		s.promptCommitted = append([]PromptEntry(nil), saved.Prepared...)
		return nil
	}
	// A crash or an older client's rewrite may remove publication evidence.
	// Keep its text as recall without asserting that a prepared send committed.
	confirmed := map[string]bool{}
	for _, entries := range [][]PromptEntry{s.saved.PromptHistory, saved.Committed} {
		for _, entry := range entries {
			confirmed[entry.ID] = true
		}
	}
	uncertain := append([]PromptEntry(nil), saved.Prepared...)
	for i, entry := range uncertain {
		if entry.Kind == "sent" && !confirmed[entry.ID] {
			uncertain[i] = newPromptEntry(entry.Environment, entry.Thread, "attempt", entry.Text)
		}
	}
	// Protect every recovered entry on the next prewrite, including entries
	// outside the bounded UI view. Only a successful main publication may prune
	// this baseline back to the normal recall bound.
	retained, err := retainPromptHistory(saved.Committed, s.saved.PromptHistory, uncertain)
	if err != nil {
		return err
	}
	// Main history preserves an already published compaction. When an older
	// client has removed it, the prepared snapshot is newer than the committed
	// baseline: recover it first so an ordinary save cannot finalize away its
	// unique text merely because older entries filled the normal recall bound.
	s.saved.PromptHistory = mergePromptHistory(s.saved.PromptHistory, uncertain, saved.Committed)
	s.promptCommitted = retained
	s.promptSidecarDirty = s.promptSidecarDirty || !equalPromptHistory(retained, saved.Committed)
	return nil
}

func validatePromptSidecar(saved promptSidecar) error {
	if saved.Version != 1 || (saved.StateSHA256 == "" && len(saved.Prepared) != 0) {
		return errors.New("unsupported prompt compatibility copy")
	}
	if saved.StateSHA256 != "" {
		if b, err := hex.DecodeString(saved.StateSHA256); err != nil || len(b) != sha256.Size {
			return errors.New("invalid prepared state digest")
		}
	}
	for i, entries := range [][]PromptEntry{saved.Committed, saved.Prepared} {
		limit := 1
		if i == 0 {
			limit = 2
		}
		if err := validatePromptHistoryLimits(entries, limit*maxPromptEntries, limit*maxPromptHistoryBytes); err != nil {
			return err
		}
		for _, entry := range entries {
			if !utf8.ValidString(entry.Environment) || !utf8.ValidString(entry.Thread) || entry != newPromptEntry(entry.Environment, entry.Thread, entry.Kind, entry.Text) {
				return errors.New("prompt compatibility entry does not match its ID")
			}
		}
	}
	if _, err := retainPromptHistory(saved.Committed, saved.Prepared); err != nil {
		return err
	}
	return nil
}

func (s *State) writePromptSidecar(saved promptSidecar) error {
	if err := validatePromptSidecar(saved); err != nil {
		return err
	}
	b, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if len(b)+1 > maxPromptSidecarBytes {
		return errors.New("prompt compatibility copy exceeds its encoded limit")
	}
	if err = writeStateFile(s.path+".prompts.json", append(b, '\n')); err != nil {
		return fmt.Errorf("prompt compatibility copy could not be saved; original draft/actions retained: %w", err)
	}
	return nil
}

func equalPromptHistory(a, b []PromptEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func retainPromptHistory(lists ...[]PromptEntry) ([]PromptEntry, error) {
	var retained []PromptEntry
	ids := map[string]bool{}
	size := 0
	for _, entries := range lists {
		for _, entry := range entries {
			if ids[entry.ID] {
				continue
			}
			if len(retained) == 2*maxPromptEntries || size+len(entry.Text) > 2*maxPromptHistoryBytes {
				return nil, errors.New("unresolved prompt checkpoints exceed retention limits; preserve and repair the prompt compatibility copy before saving more recall")
			}
			retained = append(retained, entry)
			ids[entry.ID] = true
			size += len(entry.Text)
		}
	}
	return retained, nil
}

// Earlier lists have priority; only whole, exact entries enter bounded recall.
func mergePromptHistory(lists ...[]PromptEntry) []PromptEntry {
	var merged []PromptEntry
	ids := map[string]bool{}
	size := 0
	for _, entries := range lists {
		for _, entry := range entries {
			if ids[entry.ID] {
				continue
			}
			if len(merged) == maxPromptEntries || size+len(entry.Text) > maxPromptHistoryBytes {
				return merged
			}
			merged = append(merged, entry)
			ids[entry.ID] = true
			size += len(entry.Text)
		}
	}
	return merged
}

// PromptEntry is bounded local recall, separate from the remote transcript.
// Sent entries are created only by the correlated acknowledgment transaction.
type PromptEntry struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	Thread      string `json:"thread"`
	Kind        string `json:"kind"`
	Text        string `json:"text"`
}

func (s *State) PromptHistory(env, thread string) []PromptEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entries []PromptEntry
	for _, e := range s.saved.PromptHistory {
		if e.Environment == env && e.Thread == thread {
			entries = append(entries, e)
		}
	}
	return entries
}

func (s *State) RememberPrompt(env, thread, kind, text string) error {
	if kind != "cleared" && kind != "attempt" {
		return errors.New("unsupported prompt recovery kind")
	}
	if !utf8.ValidString(text) || len(text) > 64*1024 {
		return errors.New("recovery text must be valid UTF-8 and at most 64 KiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	rememberPrompt(&next, env, thread, kind, text)
	return s.commit(next)
}

func rememberPrompt(next *savedState, env, thread, kind, text string) {
	if text == "" {
		return
	}
	entry := newPromptEntry(env, thread, kind, text)
	entries := []PromptEntry{entry}
	size := len(text)
	for _, e := range next.PromptHistory {
		if e.ID == entry.ID {
			continue
		}
		if len(entries) == maxPromptEntries || size+len(e.Text) > maxPromptHistoryBytes {
			break
		}
		entries = append(entries, e)
		size += len(e.Text)
	}
	next.PromptHistory = entries
}

func newPromptEntry(env, thread, kind, text string) PromptEntry {
	sum := sha256.Sum256([]byte(env + "\x00" + thread + "\x00" + kind + "\x00" + text))
	return PromptEntry{hex.EncodeToString(sum[:]), env, thread, kind, text}
}

func validatePromptHistory(entries []PromptEntry) error {
	return validatePromptHistoryLimits(entries, maxPromptEntries, maxPromptHistoryBytes)
}

func validatePromptHistoryLimits(entries []PromptEntry, maxEntries, maxBytes int) error {
	if len(entries) > maxEntries {
		return errors.New("prompt history exceeds entry limit")
	}
	size := 0
	ids := map[string]bool{}
	for _, e := range entries {
		if len(e.ID) != 64 || ids[e.ID] || e.Environment == "" || e.Thread == "" || len(e.Text) > 64*1024 || !utf8.ValidString(e.Text) {
			return errors.New("invalid prompt history entry")
		}
		if _, err := hex.DecodeString(e.ID); err != nil {
			return errors.New("invalid prompt history ID")
		}
		if e.Kind != "sent" && e.Kind != "cleared" && e.Kind != "attempt" {
			return errors.New("invalid prompt history kind")
		}
		ids[e.ID] = true
		size += len(e.Text)
	}
	if size > maxBytes {
		return errors.New("prompt history exceeds byte limit")
	}
	return nil
}
