package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ReviewRequests presents client prompts with exact T3 request IDs,
// revisions and offered decisions retained. Dismissal is strictly local.
func (a *Session) ReviewRequests(ctx context.Context) error {
	s := a.snapshot()
	if s == nil || !s.Metadata.Live || len(s.Metadata.Requests) == 0 {
		return errors.New("no actionable pending requests in the live view")
	}
	requests := s.Metadata.Requests
	selected := requests[0]
	if len(requests) > 1 {
		choices := make([]Choice, 0, len(requests))
		for i, r := range requests {
			choices = append(choices, Choice{ID: strconv.Itoa(i), Label: stringValue(r["prompt"]), Description: stringValue(r["kind"])})
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Pending T3 requests", Prompt: "Choose the request to review.", Choices: choices})
		if err != nil {
			return err
		}
		if answer.Cancelled || len(answer.ChoiceIDs) != 1 {
			return nil
		}
		i, err := strconv.Atoi(answer.ChoiceIDs[0])
		if err != nil || i < 0 || i >= len(requests) {
			return errors.New("invalid request selection")
		}
		selected = requests[i]
	}
	if !boolValue(selected["supported"]) {
		return fmt.Errorf("request unavailable here: %s", stringValue(selected["limitation"]))
	}
	id, revision := stringValue(selected["id"]), stringValue(selected["revision"])
	if id == "" || revision == "" {
		return errors.New("request has no authoritative identity or revision")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.mu.Lock()
	if a.requestCancel != nil {
		a.mu.Unlock()
		return errors.New("a T3 request is already being reviewed")
	}
	a.requestCancel, a.requestID, a.requestRevision = cancel, id, revision
	a.requestGeneration++
	generation := a.requestGeneration
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.requestGeneration == generation && a.requestID == id && a.requestRevision == revision {
			a.requestCancel = nil
			a.requestID = ""
			a.requestRevision = ""
		}
		a.mu.Unlock()
	}()
	if !containsRequest(a.snapshot(), id, revision) {
		return errors.New("request changed; reopen /requests")
	}
	fields := map[string]any{"threadId": s.Metadata.ThreadID, "requestId": id, "revision": revision}
	switch stringValue(selected["kind"]) {
	case "approval_request":
		options, err := objects(selected["options"])
		if err != nil || len(options) == 0 || len(options) > 5 {
			return errors.New("review this approval in full T3")
		}
		choices := make([]Choice, 0, len(options))
		allowed := map[string]bool{}
		for _, o := range options {
			decision := stringValue(o["decision"])
			switch decision {
			case "accept", "acceptForSession", "acceptAlways", "decline", "cancel":
			default:
				return errors.New("unsupported approval decision; use full T3")
			}
			if allowed[decision] {
				return errors.New("ambiguous approval options; use full T3")
			}
			allowed[decision] = true
			choices = append(choices, Choice{ID: decision, Label: stringValue(o["label"]), Description: stringValue(o["warning"])})
		}
		answer, err := a.asker.Ask(requestCtx, Question{Kind: ChooseOne, Title: "T3 approval", Prompt: stringValue(selected["prompt"]), Choices: choices})
		if err != nil {
			return err
		}
		if answer.Cancelled {
			return nil
		}
		if len(answer.ChoiceIDs) != 1 || !allowed[answer.ChoiceIDs[0]] {
			return errors.New("approval choice was not offered by T3")
		}
		fields["decision"] = answer.ChoiceIDs[0]
	case "user_input_request":
		questions, err := objects(selected["questions"])
		if err != nil || len(questions) > 8 {
			return errors.New("review this question set in full T3")
		}
		answers := make(map[string]any, len(questions))
		seen := map[string]bool{}
		for _, question := range questions {
			qid := stringValue(question["id"])
			if qid == "" || seen[qid] {
				return errors.New("question identities are missing or duplicated")
			}
			seen[qid] = true
			value, answered, err := a.askQuestion(requestCtx, stringValue(selected["prompt"]), question)
			if err != nil {
				return err
			}
			if !answered {
				return nil
			}
			if value != nil {
				answers[qid] = value
			}
		}
		fields["answers"] = answers
	default:
		return errors.New("request type is not supported by this adapter; use full T3")
	}
	if requestCtx.Err() != nil {
		return requestCtx.Err()
	}
	if !containsRequest(a.snapshot(), id, revision) {
		return errors.New("request changed while you reviewed it; no answer sent")
	}
	_, err := a.perform(requestCtx, "answer", fields, "", false)
	return err
}

func objects(value any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if typed, ok := value.([]map[string]any); ok {
		return typed, nil
	}
	array, ok := value.([]any)
	if !ok {
		return nil, errors.New("expected array")
	}
	result := make([]map[string]any, 0, len(array))
	for _, item := range array {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("expected object")
		}
		result = append(result, m)
	}
	return result, nil
}

func (a *Session) askQuestion(ctx context.Context, prompt string, q map[string]any) (any, bool, error) {
	options, err := objects(q["options"])
	if err != nil || len(options) > 24 {
		return nil, false, errors.New("question options require full T3")
	}
	required := true
	if v, exists := q["required"]; exists {
		b, ok := v.(bool)
		if !ok {
			return nil, false, errors.New("unsupported required flag")
		}
		required = b
	}
	question := stringValue(q["question"])
	if question == "" {
		question = stringValue(q["id"])
	}
	title := stringValue(q["header"])
	if title == "" {
		title = "T3 question"
	}
	if prompt != "" {
		question = prompt + "\n\n" + question
	}
	if len(options) == 0 {
		answer, err := a.asker.Ask(ctx, Question{Kind: EnterText, Title: title, Prompt: question, Initial: stringValue(q["initialAnswer"]), Placeholder: "Your answer"})
		if err != nil {
			return nil, false, err
		}
		if answer.Cancelled {
			return nil, false, nil
		}
		if answer.Text == "" {
			if required {
				return nil, false, errors.New("answer required; reopen /requests")
			}
			return nil, true, nil
		}
		return answer.Text, true, nil
	}
	multi, custom := boolValue(q["multiSelect"]), boolValue(q["allowCustomAnswer"])
	choices := make([]Choice, 0, len(options)+2)
	values := make(map[string]string, len(options))
	for i, o := range options {
		label := stringValue(o["label"])
		value := stringValue(o["value"])
		if value == "" {
			value = label
		}
		if label == "" || value == "" {
			return nil, false, errors.New("unsupported question option; use full T3")
		}
		id := strconv.Itoa(i)
		values[id] = value
		choices = append(choices, Choice{ID: id, Label: label, Description: stringValue(o["description"])})
	}
	if custom {
		choices = append(choices, Choice{ID: "custom", Label: "Write a custom answer…"})
	}
	if !required && !multi {
		choices = append(choices, Choice{ID: "skip", Label: "Leave unanswered (optional)"})
	}
	kind := ChooseOne
	if multi {
		kind = ChooseMany
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: kind, Title: title, Prompt: question, Choices: choices})
	if err != nil {
		return nil, false, err
	}
	if answer.Cancelled {
		return nil, false, nil
	}
	if !multi && len(answer.ChoiceIDs) != 1 {
		return nil, false, errors.New("choose one answer")
	}
	selected := make([]string, 0, len(answer.ChoiceIDs))
	for _, id := range answer.ChoiceIDs {
		if id == "skip" && !required && !multi {
			return nil, true, nil
		}
		if id == "custom" && custom {
			text, err := a.asker.Ask(ctx, Question{Kind: EnterText, Title: title, Prompt: "Custom answer", Initial: stringValue(q["initialAnswer"])})
			if err != nil {
				return nil, false, err
			}
			if text.Cancelled {
				return nil, false, nil
			}
			if strings.TrimSpace(text.Text) != "" {
				selected = append(selected, text.Text)
			}
			continue
		}
		value, ok := values[id]
		if !ok {
			return nil, false, errors.New("answer option was not offered by T3")
		}
		selected = append(selected, value)
	}
	if len(selected) == 0 {
		if required {
			return nil, false, errors.New("answer required; reopen /requests")
		}
		return nil, true, nil
	}
	if multi {
		return selected, true, nil
	}
	return selected[0], true, nil
}
