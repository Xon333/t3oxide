package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type SavedAction struct {
	ProfileID, EnvironmentID, ThreadID, OperationID, Kind string
	Stop                                                  bool
	Record                                                map[string]any
}

// HasSavedActions is safe for persistent UI cues; it does not clone private
// payloads or enumerate configured routes on each render.
func (a *Session) HasSavedActions() bool {
	return a != nil && a.manager != nil && a.manager.State != nil && a.manager.State.HasPendingRecords()
}

func (a *Session) SavedActions() []SavedAction {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return nil
	}
	var result []SavedAction
	for _, saved := range a.manager.State.PendingRecords() {
		profile := ""
		for _, env := range a.manager.Environments {
			if env.EnvironmentID == saved.EnvironmentID {
				profile = env.ID
				break
			}
		}
		r := saved.Record
		result = append(result, SavedAction{ProfileID: profile, EnvironmentID: saved.EnvironmentID, ThreadID: stringValue(r["threadId"]), OperationID: stringValue(r["id"]), Kind: stringValue(r["op"]), Stop: saved.Stop, Record: r})
	}
	return result
}

// ReviewSavedAction exposes the exact saved intent before any explicit
// same-ID reconciliation. Closing this presentation never clears the journal.
func (a *Session) ReviewSavedAction(ctx context.Context) (Result, error) {
	actions := a.SavedActions()
	if len(actions) == 0 {
		return Result{Text: "No saved action needs recovery."}, nil
	}
	chosen := actions[0]
	if len(actions) > 1 {
		choices := make([]Choice, 0, len(actions))
		for _, item := range actions {
			choices = append(choices, Choice{ID: item.OperationID, Label: item.Kind + " · " + item.EnvironmentID, Description: item.ThreadID})
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Review saved action", Choices: choices})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one saved action")
		}
		found := false
		for _, item := range actions {
			if item.OperationID == answer.ChoiceIDs[0] {
				chosen = item
				found = true
			}
		}
		if !found {
			return Result{}, errors.New("saved action was not offered")
		}
	}
	raw, _ := json.MarshalIndent(chosen.Record, "", "  ")
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "May already be sent — Review saved action", Prompt: fmt.Sprintf("Original target: %s · %s\nType: %s\nOperation: %s\n\n%s\n\nReconcile uses this exact operation ID, payload and native revision. It does not create a replacement action. Closing keeps everything saved.", chosen.EnvironmentID, chosen.ThreadID, chosen.Kind, chosen.OperationID, raw), Choices: []Choice{{ID: "keep", Label: "Keep saved action"}, {ID: "reconcile", Label: "Reconcile original operation"}}})
	if err != nil || answer.Cancelled {
		return Result{}, err
	}
	if len(answer.ChoiceIDs) != 1 {
		return Result{}, errors.New("choose one recovery action")
	}
	if answer.ChoiceIDs[0] == "keep" {
		return Result{}, nil
	}
	if answer.ChoiceIDs[0] != "reconcile" {
		return Result{}, errors.New("recovery action was not offered")
	}
	current := a.manager.State.Pending(chosen.EnvironmentID, chosen.Stop)
	if current == nil || stringValue(current["id"]) != chosen.OperationID {
		return Result{}, errors.New("saved action changed; review its current record")
	}
	if chosen.ProfileID == "" {
		return Result{}, errors.New("original environment is no longer configured; restore its exact source route in native T3 before reconciling; saved action retained")
	}
	if chosen.Kind == "launch-project" {
		env, err := a.navigatorEnvironment(chosen.ProfileID)
		if err != nil {
			return Result{}, err
		}
		conn, err := nativeConnection(ctx, env)
		if err != nil {
			return Result{}, err
		}
		defer closeNativeConnection(conn)
		delete(current, "id")
		delete(current, "op")
		delete(current, "environmentId")
		reply, err := a.performProjectLaunch(ctx, conn, env, current, chosen.OperationID, true)
		if err != nil {
			return Result{}, err
		}
		next, err := a.Target(chosen.ProfileID, stringValue(reply["threadId"]))
		return Result{Next: next, Text: "Original native launch reconciled."}, err
	}
	status := a.Status()
	if status.EnvironmentID != chosen.EnvironmentID || status.ThreadID != chosen.ThreadID {
		next, err := a.Target(chosen.ProfileID, chosen.ThreadID)
		return Result{Next: next, Text: "Opened the original action target. Review saved action again to reconcile its same operation ID."}, err
	}
	_, err = a.Retry(ctx, chosen.Stop)
	return Result{Text: "Original operation acknowledged by T3."}, err
}
