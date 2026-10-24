package client

import (
	"context"
	"reflect"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func userInputRequest(revision string) map[string]any {
	return map[string]any{
		"id": "question-request", "revision": revision, "kind": "user_input_request", "supported": true,
		"prompt": "Complete this form", "questions": []map[string]any{
			{"id": "text", "header": "Exact text", "question": "Keep the provided initial text exactly", "initialAnswer": " \tseed\r\n ", "required": true, "options": []any{}},
			{"id": "choice", "header": "Choice", "question": "Choose an exact value", "required": true, "options": []map[string]any{{"label": "Friendly", "value": "canonical"}}},
		},
	}
}

func TestQuestionFormRetainsExactInitialAndAnswersAndSubmitsOnce(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, []map[string]any{userInputRequest("revision-one")})}); err != nil {
		t.Fatal(err)
	}
	exact := " \tseed\r\n "
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if len(q.RequestQuestions) != 2 || q.RequestQuestions[0].InitialAnswer != exact || q.RequestProgress == nil || q.RequestProgress.Index != 0 {
			t.Fatalf("question form changed its exact initial value or omitted progress: %#v", q)
		}
		answers := map[string]any{"text": exact, "choice": "canonical"}
		progress := (&RequestProgress{Index: 1, Answers: answers}).Clone()
		answers["text"] = "caller mutation"
		return Answer{RequestAction: "submit", RequestProgress: &progress}, nil
	})
	if err := a.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 1 || packets[0]["op"] != "answer" || packets[0]["requestId"] != "question-request" || packets[0]["revision"] != "revision-one" {
		t.Fatalf("form did not submit one revision-bound answer: %#v", packets)
	}
	answers := packets[0]["answers"].(map[string]any)
	if answers["text"] != exact || answers["choice"] != "canonical" || state.Pending("fixture-env", false) != nil {
		t.Fatalf("answer payload changed or remained pending: %#v", answers)
	}
}

func TestQuestionFormRejectsMissingRequiredAndStaleRevision(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "stale"}[stale], func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
			if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, []map[string]any{userInputRequest("revision-one")})}); err != nil {
				t.Fatal(err)
			}
			a.asker = recoveryAsker(func(_ context.Context, _ Question) (Answer, error) {
				if stale {
					if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(3, false, []map[string]any{userInputRequest("revision-two")})}); err != nil {
						t.Fatal(err)
					}
					return Answer{RequestAction: "submit", RequestProgress: &RequestProgress{Answers: map[string]any{"text": "answer", "choice": "canonical"}}}, nil
				}
				return Answer{RequestAction: "submit", RequestProgress: &RequestProgress{Answers: map[string]any{"text": ""}}}, nil
			})
			if err := a.ReviewRequests(context.Background()); err == nil {
				t.Fatal("invalid or superseded question form was accepted")
			}
			if len(requestsFromLog(t, path)) != 0 || state.Pending("fixture-env", false) != nil {
				t.Fatal("invalid or stale form sent a response")
			}
		})
	}
}

func TestRequestProgressCloneRetainsIndependentAnswerValues(t *testing.T) {
	original := RequestProgress{Index: 1, Answers: map[string]any{"text": "exact", "multi": []string{"a", "b"}}}
	copy := original.Clone()
	copy.Answers["text"] = "changed"
	copy.Answers["multi"].([]string)[0] = "changed"
	if original.Answers["text"] != "exact" || !reflect.DeepEqual(original.Answers["multi"], []string{"a", "b"}) {
		t.Fatal("request progress clone aliases caller answers")
	}
}
