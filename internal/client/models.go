package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

type ModelCatalog struct {
	ThreadID                        string                `json:"threadId"`
	Current                         config.ModelSelection `json:"current"`
	InstanceID                      string                `json:"instanceId"`
	ProviderLabel                   string                `json:"providerLabel"`
	Available                       bool                  `json:"available"`
	Models                          []CatalogModel        `json:"models"`
	More                            bool                  `json:"more"`
	RequiresNewThreadForModelChange bool                  `json:"requiresNewThreadForModelChange"`
}
type CatalogModel struct {
	Slug      string             `json:"slug"`
	Name      string             `json:"name"`
	IsDefault bool               `json:"isDefault"`
	IsCustom  bool               `json:"isCustom"`
	Options   []OptionDescriptor `json:"options"`
}
type OptionDescriptor struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Label        string         `json:"label"`
	Description  string         `json:"description"`
	CurrentValue any            `json:"currentValue,omitempty"`
	Options      []OptionChoice `json:"options,omitempty"`
}
type OptionChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	IsDefault   bool   `json:"isDefault"`
}

func DecodeCatalog(result map[string]any) (*ModelCatalog, error) {
	b, err := json.Marshal(result)
	if err != nil || len(b) > 256*1024 {
		return nil, errors.New("model catalog exceeds its byte limit")
	}
	var c ModelCatalog
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if err = c.Current.Validate(); err != nil {
		return nil, err
	}
	if c.ThreadID == "" || len(c.ThreadID) > 4096 || c.InstanceID != c.Current.InstanceID || len(c.ProviderLabel) > 256 || len(c.Models) > 200 {
		return nil, errors.New("invalid model catalog identity or limits")
	}
	if _, ok := result["available"].(bool); !ok {
		return nil, errors.New("missing catalog availability")
	}
	seen := map[string]bool{}
	for _, m := range c.Models {
		if err = (config.ModelSelection{InstanceID: c.InstanceID, Model: m.Slug}).Validate(); err != nil {
			return nil, err
		}
		if seen[m.Slug] || len(m.Name) > 256 || len(m.Options) > 64 {
			return nil, errors.New("invalid or duplicate catalog model")
		}
		seen[m.Slug] = true
		ids := map[string]bool{}
		for _, d := range m.Options {
			if d.ID == "" || len(d.ID) > 128 || ids[d.ID] || len(d.Label) > 128 || len(d.Description) > 512 {
				return nil, errors.New("invalid catalog descriptor")
			}
			ids[d.ID] = true
			switch d.Type {
			case "boolean":
				if d.CurrentValue != nil {
					if _, ok := d.CurrentValue.(bool); !ok {
						return nil, errors.New("invalid boolean default")
					}
				}
			case "select":
				if len(d.Options) == 0 || len(d.Options) > 32 {
					return nil, errors.New("invalid catalog choices")
				}
				choices := map[string]bool{}
				for _, o := range d.Options {
					if o.ID == "" || len(o.ID) > 1024 || choices[o.ID] || len(o.Label) > 128 || len(o.Description) > 512 {
						return nil, errors.New("invalid catalog choice")
					}
					choices[o.ID] = true
				}
				if d.CurrentValue != nil {
					v, ok := d.CurrentValue.(string)
					if !ok || !choices[v] {
						return nil, errors.New("invalid catalog default")
					}
				}
			default:
				return nil, errors.New("unknown catalog control")
			}
		}
	}
	return &c, nil
}
func (c *ModelCatalog) model(slug string) *CatalogModel {
	for i := range c.Models {
		if c.Models[i].Slug == slug {
			return &c.Models[i]
		}
	}
	return nil
}
func (d OptionDescriptor) accepts(value any) bool {
	if d.Type == "boolean" {
		_, ok := value.(bool)
		return ok
	}
	valueString, ok := value.(string)
	if !ok {
		return false
	}
	for _, o := range d.Options {
		if o.ID == valueString {
			return true
		}
	}
	return false
}
func (m *CatalogModel) descriptor(ids ...string) *OptionDescriptor {
	for _, id := range ids {
		for i := range m.Options {
			if m.Options[i].ID == id {
				return &m.Options[i]
			}
		}
	}
	return nil
}
func (c *ModelCatalog) validate(next config.ModelSelection) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if !c.Available {
		return errors.New("this provider is unavailable in T3")
	}
	if next.InstanceID != c.InstanceID {
		return errors.New("preset uses another provider; provider switching remains in full T3")
	}
	if c.RequiresNewThreadForModelChange && next.Model != c.Current.Model {
		return errors.New("this provider requires a new conversation to change models; use full T3")
	}
	model := c.model(next.Model)
	if model == nil {
		return errors.New("model is no longer in this environment's T3 catalog")
	}
	for _, o := range next.Options {
		if next.Model == c.Current.Model {
			if old, ok := c.Current.Option(o.ID); ok && old == o.Value {
				continue
			}
		}
		d := model.descriptor(o.ID)
		if d == nil || !d.accepts(o.Value) {
			return fmt.Errorf("T3 does not offer %s=%v for %s", o.ID, o.Value, next.Model)
		}
	}
	return nil
}
func (c *ModelCatalog) withModel(base config.ModelSelection, slug string) (config.ModelSelection, error) {
	m := c.model(slug)
	if m == nil {
		return base, errors.New("model was not offered by T3")
	}
	next := base.Clone()
	next.Model = slug
	if slug != base.Model {
		next.Options = nil
		for _, o := range base.Options {
			if d := m.descriptor(o.ID); d != nil && d.accepts(o.Value) {
				next.Options = append(next.Options, o)
			}
		}
	}
	return next, c.validate(next)
}

// Catalog is read-only and off-loop. Only sanitized model/option descriptors
// cross the adapter boundary; provider credentials and settings stay on T3.
func (a *Session) Catalog(ctx context.Context) (*ModelCatalog, error) {
	a.mu.Lock()
	conn, thread, ready := a.conn, a.thread, a.ready
	enabled := a.features["modelCatalog"]
	a.mu.Unlock()
	if conn == nil || !ready || thread == "" {
		return nil, errors.New("select a connected conversation first")
	}
	if !enabled {
		return nil, errors.New("this host adapter does not expose model controls; adopt the bundled adapter")
	}
	result, err := conn.Request(ctx, "models", map[string]any{"threadId": thread}, "")
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	current := a.conn == conn && a.thread == thread
	a.mu.Unlock()
	if !current || ctx.Err() != nil {
		return nil, context.Canceled
	}
	c, err := DecodeCatalog(result)
	if err != nil {
		return nil, err
	}
	if c.ThreadID != thread {
		return nil, errors.New("model catalog belongs to another conversation")
	}
	return c, nil
}
func (a *Session) modelBase(c *ModelCatalog, next bool) config.ModelSelection {
	if next {
		a.mu.Lock()
		env := a.environment.EnvironmentID
		thread := a.thread
		a.mu.Unlock()
		if o := a.manager.State.Override(env, thread); o != nil {
			return o.Selection
		}
	}
	return c.Current.Clone()
}
func modelScope(next bool) string {
	if next {
		return "Next send · composer override"
	}
	return "Current thread"
}
func (a *Session) applyModel(ctx context.Context, c *ModelCatalog, selected config.ModelSelection, next bool) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := c.validate(selected); err != nil {
		return result, err
	}
	a.mu.Lock()
	thread := a.thread
	env := a.environment
	enabled := a.features["modelSelection"]
	overrideEnabled := a.features["messageModelOverride"]
	a.mu.Unlock()
	if env == nil || thread != c.ThreadID {
		return result, errors.New("conversation changed while choosing a model")
	}
	if next {
		if !overrideEnabled {
			return result, errors.New("this host does not support message model overrides")
		}
		if err := a.manager.State.SetOverride(env.EnvironmentID, thread, &config.ModelOverride{ID: wire.NewID(), Selection: selected.Clone()}); err != nil {
			return result, err
		}
		a.signal()
		result.Text = "Next send staged: " + selected.Summary() + ". T3 may adopt it as the thread setting; no hidden restore."
		return result, nil
	}
	if !enabled {
		return result, errors.New("model writes are unsupported by this host")
	}
	if c.Current.Equal(selected) {
		result.Text = "Thread already has this saved selection."
		return result, nil
	}
	_, err := a.perform(ctx, "select-model", map[string]any{"threadId": thread, "modelSelection": selected, "expectedModelSelection": c.Current}, "", false)
	if err != nil {
		return result, err
	}
	result.Text = "Thread selection acknowledged; displayed settings update from T3, not optimistically."
	a.signal()
	return result, nil
}
func (a *Session) pickModel(ctx context.Context, args string) (Result, error) {
	next, args := nextArgument(args)
	c, err := a.Catalog(ctx)
	if err != nil {
		return Result{}, err
	}
	base := a.modelBase(c, next)
	if args != "" {
		selected, err := c.withModel(base, args)
		if err != nil {
			return Result{}, err
		}
		return a.applyModel(ctx, c, selected, next)
	}
	if c.RequiresNewThreadForModelChange {
		return Result{}, errors.New("this provider requires a new conversation to change models; use full T3")
	}
	choices := make([]Choice, 0, len(c.Models))
	a.mu.Lock()
	env := a.environment.EnvironmentID
	a.mu.Unlock()
	presets := a.manager.State.Presets(env)
	for _, model := range c.Models {
		prefix := ""
		for _, p := range presets {
			if p.Selection.InstanceID == c.InstanceID && p.Selection.Model == model.Slug {
				prefix = "★ "
				break
			}
		}
		note := model.Slug
		if model.Slug == base.Model {
			note += " · selected"
		}
		if model.IsDefault {
			note += " · T3 default"
		}
		choices = append(choices, Choice{ID: model.Slug, Label: prefix + model.Name, Description: note})
	}
	if len(choices) == 0 {
		return Result{}, errors.New("T3 advertised no selectable models for this provider")
	}
	prompt := c.ProviderLabel + " · " + modelScope(next) + ". Model changes reset options unsupported by the new model."
	if c.More {
		prompt += " Catalog is bounded; some entries were omitted."
	}
	if next {
		prompt += " One dispatch override; T3 owns any resulting thread preference. No automatic restore."
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Model · " + modelScope(next), Prompt: prompt, Choices: choices, Initial: base.Model})
	if err != nil || answer.Cancelled {
		return Result{}, err
	}
	if len(answer.ChoiceIDs) != 1 {
		return Result{}, errors.New("choose one model")
	}
	selected, err := c.withModel(base, answer.ChoiceIDs[0])
	if err != nil {
		return Result{}, err
	}
	return a.applyModel(ctx, c, selected, next)
}
func nextArgument(args string) (bool, string) {
	args = strings.TrimSpace(args)
	if args == "next" {
		return true, ""
	}
	if strings.HasPrefix(args, "next ") {
		return true, strings.TrimSpace(strings.TrimPrefix(args, "next "))
	}
	return false, args
}
func (a *Session) pickOption(ctx context.Context, kind, args string) (Result, error) {
	next, arg := nextArgument(args)
	c, err := a.Catalog(ctx)
	if err != nil {
		return Result{}, err
	}
	base := a.modelBase(c, next)
	model := c.model(base.Model)
	if model == nil {
		return Result{}, errors.New("selected model is unavailable in T3's current catalog")
	}
	ids := []string{"reasoningEffort", "effort"}
	if kind == "speed" {
		ids = []string{"serviceTier", "fastMode"}
	}
	d := model.descriptor(ids...)
	if d == nil {
		return Result{}, fmt.Errorf("%s has no advertised %s control", model.Name, kind)
	}
	values := map[string]any{"default": nil}
	choices := []Choice{{ID: "default", Label: "Provider default", Description: "Remove the explicit override; does not claim an observed runtime value"}}
	if d.Type == "boolean" {
		values["on"] = true
		values["off"] = false
		choices = append(choices, Choice{ID: "on", Label: d.Label + " · On"}, Choice{ID: "off", Label: d.Label + " · Off"})
	} else {
		for _, o := range d.Options {
			id := "value:" + o.ID
			values[id] = o.ID
			note := o.Description
			if o.IsDefault {
				note += " · advertised default"
			}
			choices = append(choices, Choice{ID: id, Label: o.Label, Description: note})
		}
	}
	chosen := arg
	if chosen == "" {
		initial := "default"
		if v, ok := base.Option(d.ID); ok {
			if d.Type == "boolean" {
				if v == true {
					initial = "on"
				} else {
					initial = "off"
				}
			} else {
				initial = "value:" + fmt.Sprint(v)
			}
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: d.Label + " · " + modelScope(next), Prompt: optionPrompt(model.Name, d.Description, next), Choices: choices, Initial: initial})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one option")
		}
		chosen = answer.ChoiceIDs[0]
	} else if d.Type == "select" && chosen != "default" {
		chosen = "value:" + chosen
	}
	value, ok := values[chosen]
	if !ok {
		return Result{}, errors.New("option was not offered by this model")
	}
	selected := base.WithOption(d.ID, value)
	// The existing T3 service-tier adapter falls back to legacy fastMode when
	// serviceTier is absent. Resetting/changing tier must not retain a hidden
	// legacy speed override. An independently advertised fastMode is retained.
	if d.ID == "serviceTier" && model.descriptor("fastMode") == nil {
		selected = selected.WithOption("fastMode", nil)
	}
	return a.applyModel(ctx, c, selected, next)
}
func (a *Session) presets(ctx context.Context, args string) (Result, error) {
	next, action := nextArgument(args)
	c, err := a.Catalog(ctx)
	if err != nil {
		return Result{}, err
	}
	a.mu.Lock()
	env := a.environment.EnvironmentID
	a.mu.Unlock()
	presets := a.manager.State.Presets(env)
	if action == "" {
		choices := []Choice{{ID: "save", Label: "Save current selection as a favorite preset", Description: modelScope(next)}, {ID: "remove", Label: "Remove a saved preset", Description: "Local preference only"}}
		for _, p := range presets {
			note := p.Selection.Summary()
			if err := c.validate(p.Selection); err != nil {
				note += " · unavailable: " + err.Error()
			}
			choices = append(choices, Choice{ID: "apply:" + p.Name, Label: "★ " + p.Name, Description: note})
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Presets · " + modelScope(next), Prompt: "Saved only for this T3 environment. Every application is revalidated against its current catalog.", Choices: choices})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one preset action")
		}
		action = answer.ChoiceIDs[0]
	}
	if action == "save" || strings.HasPrefix(action, "save ") {
		name := strings.TrimSpace(strings.TrimPrefix(action, "save"))
		if name == "" {
			answer, err := a.asker.Ask(ctx, Question{Kind: EnterText, Title: "Save model preset", Prompt: "Name this model + effort + speed combination. Names are unique per environment."})
			if err != nil || answer.Cancelled {
				return Result{}, err
			}
			name = answer.Text
		}
		if err := a.manager.State.SavePreset(env, name, a.modelBase(c, next)); err != nil {
			return Result{}, err
		}
		return Result{Text: "Model preset saved locally."}, nil
	}
	if action == "remove" {
		var choices []Choice
		for _, p := range presets {
			choices = append(choices, Choice{ID: p.Name, Label: p.Name, Description: p.Selection.Summary()})
		}
		if len(choices) == 0 {
			return Result{}, errors.New("no model presets saved")
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Remove local preset", Prompt: "Only the saved favorite is removed; no T3 settings are changed.", Choices: choices})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one preset")
		}
		return Result{Text: "Preset removed."}, a.manager.State.DeletePreset(env, answer.ChoiceIDs[0])
	}
	name := strings.TrimPrefix(action, "apply:")
	if strings.HasPrefix(action, "apply ") {
		name = strings.TrimSpace(strings.TrimPrefix(action, "apply "))
	}
	for _, p := range presets {
		if p.Name == name {
			return a.applyModel(ctx, c, p.Selection, next)
		}
	}
	return Result{}, errors.New("preset was not offered")
}
func (a *Session) override(ctx context.Context, args string) (Result, error) {
	if args == "clear" {
		a.mu.Lock()
		env, thread := a.environment, a.thread
		a.mu.Unlock()
		if env == nil || thread == "" {
			return Result{}, errors.New("select a conversation")
		}
		err := a.manager.State.SetOverride(env.EnvironmentID, thread, nil)
		a.signal()
		return Result{Text: "Next-message override cleared; saved uncertain operations are unchanged."}, err
	}
	if args == "" {
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Next send · composer override", Prompt: "Stages one dispatch's selection with the draft. T3 owns its resulting thread preference; the client never performs a hidden restore.", Choices: []Choice{{ID: "model", Label: "Model"}, {ID: "effort", Label: "Reasoning effort"}, {ID: "speed", Label: "Speed / service tier"}, {ID: "presets", Label: "Favorite preset"}, {ID: "clear", Label: "Clear this draft's override"}}})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose an override action")
		}
		args = answer.ChoiceIDs[0]
	}
	switch args {
	case "model":
		return a.pickModel(ctx, "next")
	case "effort", "speed":
		return a.pickOption(ctx, args, "next")
	case "presets":
		return a.presets(ctx, "next")
	case "clear":
		return a.override(ctx, "clear")
	default:
		return Result{}, errors.New("use /override, /override clear, or /model next")
	}
}
func (a *Session) runtimeResult() (Result, error) {
	s := a.snapshot()
	if s == nil {
		return Result{}, errors.New("select a conversation")
	}
	data := map[string]any{"threadSelection": s.Metadata.ModelSelection, "runtime": s.Metadata.RuntimeDetails, "lineage": s.Metadata.Lineage, "observedAt": s.Metadata.ObservedAt, "live": s.Metadata.Live, "historical": s.Metadata.Historical}
	raw, _ := json.MarshalIndent(data, "", "  ")
	detail := "Saved thread preferences, T3's run request and provider-reported selection are separate. A missing field is unreported, not a guessed effective value.\n\n"
	if !s.Metadata.Live {
		detail += "This view is cached or historical. Use /latest for current details.\n\n"
	}
	detail += "Provider-reported settings are the strongest telemetry exposed here, not proof of the provider's internal computation. Last-run details are not a currently running turn.\n\n```json\n" + string(raw) + "\n```"
	return Result{DetailTitle: "Effective runtime details · T3 evidence", Detail: detail}, nil
}

func optionPrompt(model, description string, next bool) string {
	text := model + " · " + description
	if next {
		text += " One dispatch override; T3 may adopt it as the thread setting. No automatic restore."
	}
	return text
}
