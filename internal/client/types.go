// Package client attaches the terminal to T3oxide's existing NDJSON adapter.
// T3 owns conversations, queues, execution and durable operation receipts.
package client

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/Xon333/t3oxide/internal/wire"
)

// Event is a complete bounded view. Revision is attachment-local, while the
// Snapshot retains the adapter's authoritative revision and stable row keys.
// Consumers must ignore events from a previously detached session.
type Event struct {
	Revision             uint64
	Snapshot             *wire.Snapshot
	Status               Status
	Draft, DraftAccepted *string
	ResetScroll, Follow  bool
	Error                string
}

type Status struct {
	Context, ModelName, State                                                      string
	NextModel                                                                      string
	ProfileID, EnvironmentID, ThreadID, Delivery                                   string
	Connected, Writable                                                            bool
	ModelCatalog, ModelControls, ThreadNavigation, RuntimeDetails, ParentAvailable bool
	Requests                                                                       int
}

type CommandSpec struct{ Name, Description string }

// Result.Next is lazy. Starting Events attaches it; merely opening or dismissing
// a picker must never create a helper process.
type Result struct {
	Next                      *Session
	Text, DetailTitle, Detail string
}

type Kind uint8

const (
	ChooseOne Kind = iota
	ChooseMany
	EnterText
)

type Choice struct{ ID, Label, Description string }
type Question struct {
	Navigation                          bool
	Kind                                Kind
	Title, Prompt, Placeholder, Initial string
	Choices                             []Choice
}
type Answer struct {
	Action    string
	Cancelled bool
	ChoiceIDs []string
	Text      string
}

// Prompt is one exact client interaction. Done closes on reply, host request
// supersession, detach or shutdown. Closing a UI surface does not answer T3;
// cancellation is represented explicitly in Answer.
type Prompt struct {
	ID        uint64
	Question  Question
	Done      <-chan struct{}
	response  chan Answer
	responded atomic.Bool
}

// Respond is nonblocking and accepts at most one reply to this prompt.
func (p *Prompt) Respond(answer Answer) bool {
	select {
	case <-p.Done:
		return false
	default:
	}
	if !p.responded.CompareAndSwap(false, true) {
		return false
	}
	answer.ChoiceIDs = append([]string(nil), answer.ChoiceIDs...)
	p.response <- answer
	return true
}

type prompter interface {
	Ask(context.Context, Question) (Answer, error)
}
type promptBroker struct {
	requests chan *Prompt
	next     atomic.Uint64
	busy     atomic.Bool
}

func newPromptBroker() *promptBroker { return &promptBroker{requests: make(chan *Prompt, 1)} }

func (b *promptBroker) Ask(ctx context.Context, q Question) (Answer, error) {
	if ctx == nil {
		return Answer{Cancelled: true}, errors.New("interaction has no active context")
	}
	if !b.busy.CompareAndSwap(false, true) {
		return Answer{Cancelled: true}, errors.New("another interaction is already open")
	}
	defer b.busy.Store(false)
	done := make(chan struct{})
	defer close(done)
	p := &Prompt{ID: b.next.Add(1), Question: q, Done: done, response: make(chan Answer, 1)}
	select {
	case b.requests <- p:
	case <-ctx.Done():
		return Answer{Cancelled: true}, ctx.Err()
	}
	select {
	case answer := <-p.response:
		if ctx.Err() != nil {
			return Answer{Cancelled: true}, ctx.Err()
		}
		return answer, nil
	case <-ctx.Done():
		return Answer{Cancelled: true}, ctx.Err()
	}
}
