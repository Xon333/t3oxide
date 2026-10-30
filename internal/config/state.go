package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

const maxStateBytes = 2 * 1024 * 1024

// State contains drafts, bounded local prompt recall and exact, unacknowledged
// operation records. It is not a conversation database. Every mutation is
// written before publication.
// A process lock prevents two Go clients from overwriting each other's recovery
// records. Use go-drafts.json beside Swift's drafts.json to keep rollback intact.
type State struct {
	mu     sync.Mutex
	path   string
	lock   *os.File
	closed bool
	saved  savedState
	// View reads never wait behind draft/journal fsync. This immutable identity
	// summary is published only after the existing state owner commits.
	presentation atomic.Pointer[statePresentation]
	// Retain the compatibility baseline (including recovered attempts) until
	// the main transaction succeeds, even if a prepared sidecar write succeeded.
	promptCommitted    []PromptEntry
	promptSidecarDirty bool
}

type statePresentation struct {
	hasPending  bool
	destination *CreationDestination
}

func (s *State) publishPresentation() {
	p := &statePresentation{hasPending: len(s.saved.Pending) != 0 || len(s.saved.PendingStops) != 0}
	if s.saved.CreationDestination != nil {
		copy := *s.saved.CreationDestination
		p.destination = &copy
	}
	s.presentation.Store(p)
}

type savedState struct {
	PromptHistory       []PromptEntry             `json:"promptHistory,omitempty"`
	Drafts              map[string]string         `json:"drafts"`
	Pending             map[string]map[string]any `json:"pending"`
	PendingStops        map[string]map[string]any `json:"pendingStops"`
	ModelOverrides      map[string]ModelOverride  `json:"modelOverrides,omitempty"`
	ModelPresets        map[string][]ModelPreset  `json:"modelPresets,omitempty"`
	BrowserPrefs        *BrowserPreferences       `json:"browserPreferences,omitempty"`
	LaunchDrafts        map[string]LaunchDraft    `json:"launchDrafts,omitempty"`
	CreationDestination *CreationDestination      `json:"creationDestination,omitempty"`
	BatchReceipts       map[string]BatchReceipt   `json:"batchReceipts,omitempty"`
	ReviewPositions     []ReviewPosition          `json:"reviewPositions,omitempty"`
}

func DefaultStatePath() string { return filepath.Join(DefaultDirectory(), "go-drafts.json") }

func OpenState(path string) (*State, error) {
	if path == "" {
		path = DefaultStatePath()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another Go client owns this drafts/recovery file")
	}
	s := &State{path: path, lock: lock, saved: savedState{Drafts: map[string]string{}, Pending: map[string]map[string]any{}, PendingStops: map[string]map[string]any{}, ModelOverrides: map[string]ModelOverride{}, ModelPresets: map[string][]ModelPreset{}, LaunchDrafts: map[string]LaunchDraft{}, BatchReceipts: map[string]BatchReceipt{}}}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = s.loadPromptSidecar(nil); err != nil {
			s.Close()
			return nil, err
		}
		s.publishPresentation()
		return s, nil
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err == nil && len(b) > maxStateBytes {
		err = errors.New("saved state exceeds 2 MiB")
	}
	if err == nil {
		s.saved = savedState{}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		err = dec.Decode(&s.saved)
		if err == nil {
			var extra any
			if dec.Decode(&extra) != io.EOF {
				err = errors.New("saved state has trailing content")
			}
		}
	}
	if err == nil {
		err = validateState(s.saved)
	}
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("saved drafts/actions could not be loaded; preserve and repair %s before sending new work: %w", path, err)
	}
	if s.saved.PendingStops == nil {
		s.saved.PendingStops = map[string]map[string]any{}
	}
	if s.saved.LaunchDrafts == nil {
		s.saved.LaunchDrafts = map[string]LaunchDraft{}
	}
	if err = s.loadPromptSidecar(b); err != nil {
		s.Close()
		return nil, err
	}
	s.publishPresentation()
	return s, nil
}

func (s *State) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock == nil {
		return nil
	}
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	return s.lock.Close()
}

// ImportLegacy copies Swift's bounded drafts/recovery state exactly once, while
// this client's process lock is held. The legacy file is never changed. An
// existing Go state file always wins, including an intentionally empty one.
func (s *State) ImportLegacy(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("draft/recovery state is closed")
	}
	if _, err := os.Stat(s.path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxStateBytes {
		return errors.New("legacy drafts/actions exceed 2 MiB; original file was preserved")
	}
	var next savedState
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err = dec.Decode(&next); err == nil {
		var extra any
		if dec.Decode(&extra) != io.EOF {
			err = errors.New("legacy state has trailing content")
		}
	}
	if err == nil {
		err = validateState(next)
	}
	if err != nil {
		return fmt.Errorf("legacy drafts/actions could not be imported; preserve and repair %s: %w", path, err)
	}
	if next.PendingStops == nil {
		next.PendingStops = map[string]map[string]any{}
	}
	if next.LaunchDrafts == nil {
		next.LaunchDrafts = map[string]LaunchDraft{}
	}
	next.PromptHistory = mergePromptHistory(s.saved.PromptHistory, next.PromptHistory)
	return s.commit(next)
}

func validateState(v savedState) error {
	if v.CreationDestination != nil {
		if err := v.CreationDestination.validate(); err != nil {
			return err
		}
	}
	if len(v.BatchReceipts) > maxBatchReceipts {
		return errors.New("batch receipt limit exceeded")
	}
	for id, receipt := range v.BatchReceipts {
		if id != receipt.ID {
			return errors.New("batch receipt key mismatch")
		}
		if err := validateBatchReceipt(receipt); err != nil {
			return err
		}
	}
	if err := validateReviewPositions(v.ReviewPositions); err != nil {
		return err
	}
	if err := validatePromptHistory(v.PromptHistory); err != nil {
		return err
	}
	if err := validateModelPreferences(v); err != nil {
		return err
	}
	if err := validateClientPreferences(v); err != nil {
		return err
	}
	if v.Drafts == nil || v.Pending == nil {
		return errors.New("unsupported saved state")
	}
	if len(v.Drafts) > 40 || len(v.Pending) > 8 || len(v.PendingStops) > 8 {
		return errors.New("saved state exceeds draft or operation limits")
	}
	for _, text := range v.Drafts {
		if len(text) > 64*1024 {
			return errors.New("draft exceeds 64 KiB")
		}
	}
	for _, lane := range []map[string]map[string]any{v.Pending, v.PendingStops} {
		for env, record := range lane {
			if err := validateRecord(env, record); err != nil {
				return err
			}
		}
	}
	for _, record := range v.PendingStops {
		if record["op"] != "stop" {
			return errors.New("non-Stop operation in Stop recovery lane")
		}
	}
	return nil
}

func validateRecord(env string, record map[string]any) error {
	if env == "" || record == nil || record["environmentId"] != env {
		return errors.New("saved operation environment does not match its key")
	}
	for _, key := range []string{"id", "threadId", "op"} {
		v, ok := record[key].(string)
		if !ok || v == "" || len(v) > 4096 {
			return fmt.Errorf("saved operation has invalid %s", key)
		}
	}
	switch record["op"] {
	case "send", "stop", "answer", "launch", "select-model", "launch-project", "thread-action":
	default:
		return errors.New("unsupported saved operation")
	}
	b, err := json.Marshal(record)
	if err != nil || len(b) > 96*1024 {
		return errors.New("saved operation is invalid or exceeds 96 KiB")
	}
	return nil
}

func draftKey(env, thread string) string { return env + "/" + thread }

func (s *State) Draft(env, thread string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved.Drafts[draftKey(env, thread)]
}

func (s *State) SetDraft(env, thread, value string) error {
	if len(value) > 64*1024 {
		return errors.New("draft exceeds 64 KiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	key := draftKey(env, thread)
	if value == "" {
		delete(next.Drafts, key)
	} else {
		next.Drafts[key] = value
	}
	return s.commit(next)
}

func (s *State) ClearDraftIfEqual(env, thread, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := draftKey(env, thread)
	if s.saved.Drafts[key] != value {
		return nil
	}
	next := cloneState(s.saved)
	delete(next.Drafts, key)
	return s.commit(next)
}

func (s *State) Pending(env string, stop bool) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	lane := s.saved.Pending
	if stop {
		lane = s.saved.PendingStops
	}
	return cloneRecord(lane[env])
}

// SavePending refuses to replace unresolved work. Explicit retry may save the
// identical ID and payload; changing either is always rejected.
func (s *State) SavePending(env string, stop bool, record map[string]any) error {
	if err := validateRecord(env, record); err != nil {
		return err
	}
	if stop && record["op"] != "stop" {
		return errors.New("only Stop may use the reserved recovery lane")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	lane := next.Pending
	if stop {
		lane = next.PendingStops
	}
	if old := lane[env]; old != nil {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(record)
		if !bytes.Equal(a, b) {
			return errors.New("an unresolved operation already occupies this lane; retry its exact saved ID or explicitly resolve it")
		}
	}
	lane[env] = cloneRecord(record)
	if err := updateBatchReceipt(&next, record, BatchNeedsRecovery); err != nil {
		return err
	}
	return s.commit(next)
}

func (s *State) ClearPending(env string, stop bool, id string) error {
	return s.clearPending(env, stop, id, false)
}

// RejectPending retires only a definitely rejected first attempt. It is distinct
// from a human clearing local recovery without a native outcome receipt.
func (s *State) RejectPending(env string, stop bool, id string) error {
	return s.clearPending(env, stop, id, true)
}
func (s *State) clearPending(env string, stop bool, id string, rejected bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	lane := next.Pending
	if stop {
		lane = next.PendingStops
	}
	if old := lane[env]; old != nil && old["id"] != id {
		return errors.New("saved operation changed; refusing to clear another action")
	}
	if old := lane[env]; old != nil && rejected {
		if err := updateBatchReceipt(&next, old, BatchRemainingDraft); err != nil {
			return err
		}
	} else if old != nil {
		if b, ok := old["batchDraft"].(map[string]any); ok {
			if id, ok := b["receiptId"].(string); ok {
				if receipt, exists := next.BatchReceipts[id]; exists {
					receipt.PauseReason = "Local retry record was explicitly cleared. Native outcome remains unreported; inspect the original operation in T3."
					next.BatchReceipts[id] = receipt
				}
			}
		}
	}
	delete(lane, env)

	return s.commit(next)
}

// Acknowledge atomically clears the exact operation and its unchanged submitted
// draft. Failure retains both, keeping retry attached to the same stable ID.
// Send and launch clear only their own unchanged submitted draft.
func (s *State) Acknowledge(env string, stop bool, id, thread, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	lane := next.Pending
	if stop {
		lane = next.PendingStops
	}
	old := lane[env]
	if old == nil || old["id"] != id {
		return errors.New("saved operation changed; refusing to acknowledge another action")
	}
	if thread != "" && old["threadId"] != thread {
		return errors.New("saved operation belongs to another thread")
	}
	if old["op"] == "launch-project" {
		if err := advanceBatchDraft(&next, old); err != nil {
			return err
		}
		if err := updateBatchReceipt(&next, old, BatchAcknowledged); err != nil {
			return err
		}
	}
	delete(lane, env)
	if old["op"] == "launch-project" {
		form, _ := old["formSource"].(string)
		key := draftKey(env, form)
		if saved, ok := next.LaunchDrafts[key]; ok && saved.Title == old["title"] && saved.Message == old["message"] && saved.ProjectID == old["projectId"] && saved.NativeDefaultsRevision == old["defaultsRevision"] && launchSelectionMatches(saved.Selection, old["modelSelection"]) && launchWorkspaceMatches(saved.WorkspaceStrategy, old["workspaceStrategy"]) {
			delete(next.LaunchDrafts, key)
			messageKey := draftKey(env, "\x00new/"+form)
			if next.Drafts[messageKey] == saved.Message {
				delete(next.Drafts, messageKey)
			}
		}
	}
	if old["op"] == "send" && message != "" && old["message"] == message {
		rememberPrompt(&next, env, thread, "sent", message)
	}
	if old["op"] == "send" {
		key := draftKey(env, thread)
		if override, ok := next.ModelOverrides[key]; ok && old["modelOverrideId"] == override.ID {
			delete(next.ModelOverrides, key)
		}
	}
	if old["op"] == "launch" {
		key := draftKey(env, thread)
		message, messageOK := old["message"].(string)
		title, titleOK := old["title"].(string)
		if saved, ok := next.LaunchDrafts[key]; ok && messageOK && titleOK && saved.Message == message && saved.Title == title {
			selection, hasSelection := old["modelSelection"]
			if (hasSelection && launchSelectionMatches(saved.Selection, selection)) || (!hasSelection && saved.Selection == nil) {
				delete(next.LaunchDrafts, key)
			}
		}
	}
	draftThread := thread
	if old["op"] == "launch" {
		// Launch is correlated against the source thread, but its draft is
		// isolated from that thread's ordinary composer.
		draftThread = "\x00new/" + thread
	}
	if message != "" && (old["op"] == "send" || old["op"] == "launch") && next.Drafts[draftKey(env, draftThread)] == message {
		delete(next.Drafts, draftKey(env, draftThread))
	}
	return s.commit(next)
}

func (s *State) commit(next savedState) error {
	if s.closed {
		return errors.New("draft/recovery state is closed")
	}
	if err := validateState(next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > maxStateBytes {
		return errors.New("saved drafts/actions exceed 2 MiB")
	}
	// Older Go clients drop unknown main-file fields. Publish a compatibility
	// copy before clearing any draft/pending record, retaining the previous
	// committed history if the main publication fails.
	checkpoint := s.promptSidecarDirty || !equalPromptHistory(next.PromptHistory, s.promptCommitted)
	if checkpoint {
		staged := promptSidecar{Version: 1, Committed: s.promptCommitted, Prepared: next.PromptHistory, StateSHA256: promptStateDigest(b)}
		if err = s.writePromptSidecar(staged); err != nil {
			return err
		}
		s.promptSidecarDirty = true
	}
	if err = writeStateFile(s.path, b); err != nil {
		return err
	}
	s.saved = next
	s.publishPresentation()
	if checkpoint {
		s.promptCommitted = append([]PromptEntry(nil), next.PromptHistory...)
		// Main publication has succeeded. Finalization is optional: the durable
		// prepared envelope can be reconciled by its digest after interruption.
		// Do not turn a successful ACK into an apparent failed transaction.
		s.promptSidecarDirty = s.writePromptSidecar(promptSidecar{Version: 1, Committed: s.promptCommitted}) != nil
	}
	return nil
}

// writeStateFile is the existing private, fsynced atomic state publication path,
// shared by the main transaction and its prompt-only compatibility copy.
func writeStateFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".t3oxide-state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return err
	}
	return nil
}

func cloneRecord(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	var copied map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&copied)
	return copied
}
func cloneState(v savedState) savedState {
	n := savedState{Drafts: map[string]string{}, Pending: map[string]map[string]any{}, PendingStops: map[string]map[string]any{}, ModelOverrides: map[string]ModelOverride{}, ModelPresets: map[string][]ModelPreset{}, LaunchDrafts: map[string]LaunchDraft{}, BatchReceipts: map[string]BatchReceipt{}}
	n.PromptHistory = append([]PromptEntry(nil), v.PromptHistory...)
	if v.CreationDestination != nil {
		copy := *v.CreationDestination
		n.CreationDestination = &copy
	}
	for id, receipt := range v.BatchReceipts {
		n.BatchReceipts[id] = receipt.Clone()
	}
	n.ReviewPositions = append([]ReviewPosition(nil), v.ReviewPositions...)
	for k, x := range v.Drafts {
		n.Drafts[k] = x
	}
	for k, x := range v.Pending {
		n.Pending[k] = cloneRecord(x)
	}
	for k, x := range v.PendingStops {
		n.PendingStops[k] = cloneRecord(x)
	}
	for key, o := range v.ModelOverrides {
		o.Selection = o.Selection.Clone()
		n.ModelOverrides[key] = o
	}
	for key, list := range v.ModelPresets {
		for _, p := range list {
			p.Selection = p.Selection.Clone()
			n.ModelPresets[key] = append(n.ModelPresets[key], p)
		}
	}
	if v.BrowserPrefs != nil {
		prefs := cloneBrowserPreferences(*v.BrowserPrefs)
		n.BrowserPrefs = &prefs
	}
	for key, draft := range v.LaunchDrafts {
		n.LaunchDrafts[key] = cloneLaunchDraft(draft)
	}
	return n
}
