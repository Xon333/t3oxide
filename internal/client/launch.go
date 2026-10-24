package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

type launchDraft struct {
	Input           LaunchInput
	Selection       *config.ModelSelection
	SourceProject   string
	SourceWorkspace string
}

func (m *Manager) savedLaunch(env, thread string) (launchDraft, bool) {
	if m.State != nil {
		if saved, ok := m.State.LaunchDraft(env, thread); ok {
			draft := launchDraft{Input: LaunchInput{Title: saved.Title, Message: saved.Message}, SourceProject: saved.ProjectID, SourceWorkspace: saved.Workspace}
			if saved.Selection != nil {
				copy := saved.Selection.Clone()
				draft.Selection = &copy
			}
			m.mu.Lock()
			if m.launchDrafts == nil {
				m.launchDrafts = map[draftIdentity]launchDraft{}
			}
			m.launchDrafts[draftIdentity{env, thread}] = draft
			m.mu.Unlock()
			return draft, true
		}
		m.mu.Lock()
		delete(m.launchDrafts, draftIdentity{env, thread})
		m.mu.Unlock()
		return launchDraft{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	draft, ok := m.launchDrafts[draftIdentity{env, thread}]
	if draft.Selection != nil {
		copy := draft.Selection.Clone()
		draft.Selection = &copy
	}
	return draft, ok
}
func (m *Manager) saveLaunch(env, thread string, input LaunchInput, selection *config.ModelSelection, location Location) error {
	if m.State == nil {
		return errors.New("launch draft state is unavailable")
	}
	durable := config.LaunchDraft{Title: input.Title, Message: input.Message, ProjectID: location.ProjectID, Workspace: location.Workspace}
	if selection != nil {
		copy := selection.Clone()
		durable.Selection = &copy
	}
	if err := m.State.SetLaunchDraft(env, thread, durable); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.launchDrafts == nil {
		m.launchDrafts = map[draftIdentity]launchDraft{}
	}
	draft := launchDraft{Input: input, SourceProject: location.ProjectID, SourceWorkspace: location.Workspace}
	if selection != nil {
		copy := selection.Clone()
		draft.Selection = &copy
	}
	m.launchDrafts[draftIdentity{env, thread}] = draft
	return nil
}

func (a *Session) storeLaunchDraft(environment, sourceThread string, input LaunchInput, selection *config.ModelSelection, location Location) error {
	if len(input.Title) > 512 || len(input.Message) > 64*1024 || !utf8.ValidString(input.Title) || !utf8.ValidString(input.Message) {
		return errors.New("launch title or first message exceeds its byte limit or is not UTF-8")
	}
	if err := a.manager.saveLaunch(environment, sourceThread, input, selection, location); err != nil {
		return fmt.Errorf("preserve launch draft: %w", err)
	}
	return nil
}

func (a *Session) location(s *wire.Snapshot) Location {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.locationLocked(s)
}
func (a *Session) locationLocked(s *wire.Snapshot) Location {
	l := Location{ThreadID: a.thread}
	if a.environment != nil {
		l.ProfileID = a.environment.ID
		l.EnvironmentID = a.environment.EnvironmentID
		l.Environment = a.environment.Label
	}
	if s != nil {
		l.ThreadID = s.Metadata.ThreadID
		l.Title = s.Metadata.Title
		if s.Metadata.ProjectID != nil {
			l.ProjectID = *s.Metadata.ProjectID
		}
		if s.Metadata.Workspace != nil {
			l.Workspace = *s.Metadata.Workspace
		}
	}
	if l.Workspace == "" {
		l.Workspace = "Existing workspace"
	}
	return l
}
func (c *ModelCatalog) validateLaunch(selection config.ModelSelection) error {
	copy := *c
	copy.RequiresNewThreadForModelChange = false
	// Explicit launch settings have no legacy-option exemption. Untouched
	// inheritance omits modelSelection from the launch request entirely.
	copy.Current.Options = nil
	return copy.validate(selection)
}

// A different launch model starts with its advertised defaults, never an
// accidental carry from the source conversation's effort or next-send override.
func (c *ModelCatalog) launchModel(slug string) (config.ModelSelection, error) {
	model := c.model(slug)
	if model == nil {
		return config.ModelSelection{}, errors.New("launch model was not offered")
	}
	next := config.ModelSelection{InstanceID: c.InstanceID, Model: slug}
	for _, d := range model.Options {
		if d.CurrentValue != nil {
			next = next.WithOption(d.ID, d.CurrentValue)
			continue
		}
		if d.Type == "select" {
			for _, o := range d.Options {
				if o.IsDefault {
					next = next.WithOption(d.ID, o.ID)
					break
				}
			}
		}
	}
	return next, c.validateLaunch(next)
}
func launchActions(c *ModelCatalog, selection *config.ModelSelection, enabled bool) []string {
	actions := []string{"create", "cancel"}
	if !enabled || c == nil || selection == nil || !c.Available {
		return actions
	}
	if len(c.Models) > 0 {
		actions = append(actions, "model")
	}
	if m := c.model(selection.Model); m != nil {
		if m.descriptor("reasoningEffort", "effort") != nil {
			actions = append(actions, "effort")
		}
		if m.descriptor("serviceTier", "fastMode") != nil {
			actions = append(actions, "tier")
		}
	}
	return actions
}
func (a *Session) pickLaunchSelection(ctx context.Context, c *ModelCatalog, base config.ModelSelection, action string) (config.ModelSelection, error) {
	if action == "model" {
		choices := []Choice{}
		for _, m := range c.Models {
			choices = append(choices, Choice{ID: m.Slug, Label: m.Name, Description: m.Slug})
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Model · New conversation", Choices: choices, Initial: base.Model})
		if err != nil || answer.Cancelled {
			return base, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return base, errors.New("choose one offered launch model")
		}
		if answer.ChoiceIDs[0] == base.Model {
			return base, nil
		}
		return c.launchModel(answer.ChoiceIDs[0])
	}
	model := c.model(base.Model)
	if model == nil {
		return base, errors.New("launch model is no longer offered")
	}
	ids := []string{"reasoningEffort", "effort"}
	if action == "tier" {
		ids = []string{"serviceTier", "fastMode"}
	}
	d := model.descriptor(ids...)
	if d == nil {
		return base, errors.New("launch control was not offered")
	}
	choices := []Choice{{ID: "default", Label: "Provider default"}}
	values := map[string]any{"default": nil}
	initial := "default"
	if d.Type == "boolean" {
		choices = append(choices, Choice{ID: "on", Label: "On"}, Choice{ID: "off", Label: "Off"})
		values["on"], values["off"] = true, false
		if v, ok := base.Option(d.ID); ok {
			if v == true {
				initial = "on"
			} else {
				initial = "off"
			}
		}
	} else {
		for _, o := range d.Options {
			id := "value:" + o.ID
			choices = append(choices, Choice{ID: id, Label: o.Label, Description: o.Description})
			values[id] = o.ID
		}
		if v, ok := base.Option(d.ID); ok {
			initial = "value:" + fmt.Sprint(v)
		}
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: d.Label + " · New conversation", Choices: choices, Initial: initial})
	if err != nil || answer.Cancelled {
		return base, err
	}
	if len(answer.ChoiceIDs) != 1 {
		return base, errors.New("choose one offered launch option")
	}
	value, ok := values[answer.ChoiceIDs[0]]
	if !ok {
		return base, errors.New("launch option was not offered")
	}
	next := base.WithOption(d.ID, value)
	if d.ID == "serviceTier" && model.descriptor("fastMode") == nil {
		next = next.WithOption("fastMode", nil)
	}
	return next, c.validateLaunch(next)
}
func (a *Session) newConversation(ctx context.Context, title string) (Result, error) {
	s := a.snapshot()
	if s == nil || !s.Metadata.Live || s.Metadata.ProjectID == nil {
		return Result{}, errors.New("select a live conversation in the existing workspace first")
	}
	a.mu.Lock()
	enabled := a.features["launchModelSelection"]
	environment := a.environment.EnvironmentID
	writable := a.ready && a.writable
	conn := a.conn
	a.mu.Unlock()
	if !writable {
		return Result{}, errors.New("new conversation requires a connected writable source")
	}
	suppliedTitle := title != ""
	if title == "" {
		title = "New conversation"
	}
	input := LaunchInput{Title: title, Message: a.manager.State.Draft(environment, launchDraftThread(s.Metadata.ThreadID))}
	var selection *config.ModelSelection
	if s.Metadata.ModelSelection != nil {
		copy := s.Metadata.ModelSelection.Clone()
		selection = &copy
	}
	var catalog *ModelCatalog
	unavailable := "This adapter inherits the source model; launch selection is unavailable."
	if enabled {
		var err error
		catalog, err = a.Catalog(ctx)
		if err != nil {
			return Result{}, err
		}
		copy := catalog.Current.Clone()
		selection = &copy
		unavailable = ""
	}
	edited := false
	location := a.location(s)
	if saved, ok := a.manager.savedLaunch(environment, s.Metadata.ThreadID); ok {
		input.Message = saved.Input.Message
		if !suppliedTitle {
			input.Title = saved.Input.Title
		}
		if saved.Selection != nil {
			edited = selection == nil || !selection.Equal(*saved.Selection)
			selection = saved.Selection
		}
		if (saved.SourceProject != "" && saved.SourceProject != location.ProjectID) || (saved.SourceWorkspace != "" && saved.SourceWorkspace != location.Workspace) {
			answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Saved launch workspace changed", Prompt: fmt.Sprintf("This prepared launch belongs to project %q in workspace %q. The current source is project %q in workspace %q. Keep the saved form or explicitly rebind it before reviewing Create.", saved.SourceProject, saved.SourceWorkspace, location.ProjectID, location.Workspace), Choices: []Choice{{ID: "rebind", Label: "Rebind this form to the current workspace"}, {ID: "keep", Label: "Keep saved form and cancel"}}})
			if err != nil {
				return Result{}, err
			}
			if answer.Cancelled {
				return Result{}, nil
			}
			if answer.Action == "keep" || len(answer.ChoiceIDs) == 1 && answer.ChoiceIDs[0] == "keep" {
				return Result{Text: "Saved launch kept unchanged; no conversation created."}, nil
			}
			if answer.Action != "rebind" && (len(answer.ChoiceIDs) != 1 || answer.ChoiceIDs[0] != "rebind") {
				return Result{}, errors.New("choose rebind or keep the saved launch form")
			}
			if err := a.manager.saveLaunch(environment, s.Metadata.ThreadID, input, selection, location); err != nil {
				return Result{}, fmt.Errorf("preserve explicit workspace rebind: %w", err)
			}
		}
	}
	for {
		actions := launchActions(catalog, selection, enabled)
		summary := s.Metadata.Model
		if selection != nil {
			summary = selection.Summary()
		}
		review := &LaunchReview{Input: input, Location: location, Selection: selection, Summary: summary, Actions: actions, Unavailable: unavailable}
		boundSelection := selection
		if selection != nil {
			copy := selection.Clone()
			boundSelection = &copy
		}
		review.StoreDraft = func(input LaunchInput) error {
			return a.storeLaunchDraft(environment, s.Metadata.ThreadID, input, boundSelection, location)
		}
		if selection != nil {
			copy := selection.Clone()
			review.Selection = &copy
		}
		answer, err := a.asker.Ask(ctx, Question{Surface: SurfaceLaunch, Kind: EnterText, Title: "New conversation", LaunchReview: review})
		if err != nil {
			return Result{}, err
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if answer.Launch != nil {
			input = *answer.Launch
		}
		if err = review.StoreDraft(input); err != nil {
			return Result{}, err
		}
		if answer.Cancelled || answer.Action == "cancel" {
			return Result{}, nil
		}
		offered := false
		for _, action := range actions {
			if action == answer.Action {
				offered = true
			}
		}
		if !offered {
			return Result{}, errors.New("launch action was not offered")
		}
		if answer.Action != "create" {
			next, err := a.pickLaunchSelection(ctx, catalog, *selection, answer.Action)
			if err != nil {
				return Result{}, err
			}
			edited = edited || !selection.Equal(next)
			selection = &next
			if err := a.manager.saveLaunch(environment, s.Metadata.ThreadID, input, selection, location); err != nil {
				return Result{}, fmt.Errorf("preserve launch selection: %w", err)
			}
			continue
		}
		if input.Title == "" {
			input.Title = "New conversation"
		}
		if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Message) == "" {
			return Result{}, errors.New("title and first message must contain text")
		}
		current := a.store.Snapshot()
		a.mu.Lock()
		same := a.conn == conn && a.environment != nil && a.environment.EnvironmentID == environment && a.thread == s.Metadata.ThreadID
		a.mu.Unlock()
		if !same || current == nil || current.Metadata.ProjectID == nil || *current.Metadata.ProjectID != *s.Metadata.ProjectID || a.location(current).Workspace != location.Workspace {
			return Result{}, errors.New("source workspace changed while reviewing the launch")
		}
		fields := map[string]any{"threadId": s.Metadata.ThreadID, "title": input.Title, "message": input.Message}
		if enabled && selection != nil {
			fresh, err := a.Catalog(ctx)
			if err != nil {
				return Result{}, err
			}
			// Omit an untouched source selection so T3 inherits its retained
			// provider options. Explicit launch selections use strict validation.
			if edited || !selection.Equal(fresh.Current) {
				if err = fresh.validateLaunch(*selection); err != nil {
					return Result{}, err
				}
				fields["modelSelection"] = selection.Clone()
			}
		} else if edited {
			return Result{}, errors.New("adapter does not support edited launch selection")
		}
		reply, err := a.perform(ctx, "launch", fields, "", false)
		if err != nil {
			return Result{}, err
		}
		a.mu.Lock()
		profile := a.environment.ID
		a.mu.Unlock()
		target, err := a.Target(profile, stringValue(reply["threadId"]))
		if err != nil {
			return Result{}, fmt.Errorf("T3 created conversation %s; select it after saving the current draft: %w", stringValue(reply["threadId"]), err)
		}
		a.manager.mu.Lock()
		delete(a.manager.launchDrafts, draftIdentity{environment, s.Metadata.ThreadID})
		a.manager.mu.Unlock()
		return Result{Next: target}, nil
	}
}
