// Package client attaches the terminal to T3oxide's existing NDJSON adapter.
// T3 owns conversations, queues, execution and durable operation receipts.
package client

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/Xon333/t3oxide/internal/config"
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
	ProfileIDs                                                                     []string
	Location                                                                       Location
	ConnectionState, RunState, ActiveRunID                                         string
	PendingAction, PendingStop, DraftSaveFailed, Historical                        bool
	Receipt                                                                        *Receipt
}

type Location struct {
	ProfileID, EnvironmentID, Environment, ProjectID, ThreadID, Title, Workspace string
}

// Receipt is attachment-local send evidence, independent of run/request state.
type Receipt struct {
	OperationID, MessageID, ThreadID, Phase, RunID string
	Sequence, ObservedSequence                     int64
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
type BrowserGroup struct {
	ID, Label string
	ChoiceIDs []string
}
type Surface string

const (
	SurfaceDialog  Surface = "dialog"
	SurfaceBrowser Surface = "browser"
	SurfaceLaunch  Surface = "launch"
)

type BrowserPage struct {
	ProfileIDs []string
	ProfileID  string
	Location   string
	Profiles   []Choice
	Groups     []BrowserGroup
	Actions    []string
	Query      string
	More       bool
	Error      string
	ObservedAt string
	Lifecycle  string
	Cursor     int
	NextCursor *int
}

// ModelSettingsReview is a complete, locally staged view of the current T3
// model catalog. The client validates and applies the returned selection once.
type ModelSettingsReview struct {
	Catalog   ModelCatalog
	Selection config.ModelSelection
	Location  Location
	Scope     string
	Actions   []string
}

// RequestOption and RequestQuestion are the exact controls advertised by one
// revision-bound T3 user-input request. InitialAnswer is preserved verbatim.
type RequestOption struct{ Label, Value, Description string }
type RequestQuestion struct {
	ID, Header, Question, InitialAnswer      string
	Required, MultiSelect, AllowCustomAnswer bool
	Options                                  []RequestOption
}

// RequestProgress is transient UI state for a single verified request. Clone
// it before retaining or editing the answers map.
type RequestProgress struct {
	Index   int
	Answers map[string]any
}

func (p RequestProgress) Clone() RequestProgress {
	copy := RequestProgress{Index: p.Index, Answers: make(map[string]any, len(p.Answers))}
	for id, value := range p.Answers {
		copy.Answers[id] = cloneRequestAnswer(value)
	}
	return copy
}

func cloneRequestAnswer(value any) any {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		return append([]any(nil), typed...)
	default:
		return value
	}
}

func cloneModelCatalog(c ModelCatalog) ModelCatalog {
	c.Current = c.Current.Clone()
	c.Models = append([]CatalogModel(nil), c.Models...)
	for i := range c.Models {
		c.Models[i].Options = append([]OptionDescriptor(nil), c.Models[i].Options...)
		for j := range c.Models[i].Options {
			c.Models[i].Options[j].Options = append([]OptionChoice(nil), c.Models[i].Options[j].Options...)
		}
	}
	return c
}

func cloneQuestion(q Question) Question {
	q.Choices = append([]Choice(nil), q.Choices...)
	if q.ModelSettings != nil {
		copy := *q.ModelSettings
		copy.Catalog = cloneModelCatalog(copy.Catalog)
		copy.Selection = copy.Selection.Clone()
		copy.Actions = append([]string(nil), copy.Actions...)
		q.ModelSettings = &copy
	}
	if q.RequestQuestions != nil {
		q.RequestQuestions = append([]RequestQuestion(nil), q.RequestQuestions...)
		for i := range q.RequestQuestions {
			q.RequestQuestions[i].Options = append([]RequestOption(nil), q.RequestQuestions[i].Options...)
		}
	}
	if q.RequestProgress != nil {
		copy := q.RequestProgress.Clone()
		q.RequestProgress = &copy
	}
	return q
}

type LaunchInput struct{ Title, Message string }
type LaunchReview struct {
	Input       LaunchInput
	Location    Location
	Selection   *config.ModelSelection
	Summary     string
	Actions     []string
	Unavailable string
	// StoreDraft synchronously preserves this exact source's separate launch
	// draft. It remains valid while the UI retires the prompt during shutdown.
	StoreDraft func(LaunchInput) error
}
type Question struct {
	Surface                             Surface
	BrowserPage                         *BrowserPage
	LaunchReview                        *LaunchReview
	ModelSettings                       *ModelSettingsReview
	RequestQuestions                    []RequestQuestion
	RequestProgress                     *RequestProgress
	SingleLine                          bool
	Navigation                          bool
	Kind                                Kind
	Title, Prompt, Placeholder, Initial string
	Choices                             []Choice
}
type Answer struct {
	Launch          *LaunchInput
	ModelSettings   *config.ModelSelection
	RequestAction   string
	RequestProgress *RequestProgress
	Action          string
	Cancelled       bool
	ChoiceIDs       []string
	Text            string
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
	if answer.Launch != nil {
		copy := *answer.Launch
		answer.Launch = &copy
	}
	if answer.ModelSettings != nil {
		copy := answer.ModelSettings.Clone()
		answer.ModelSettings = &copy
	}
	if answer.RequestProgress != nil {
		copy := answer.RequestProgress.Clone()
		answer.RequestProgress = &copy
	}
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
	p := &Prompt{ID: b.next.Add(1), Question: cloneQuestion(q), Done: done, response: make(chan Answer, 1)}
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
