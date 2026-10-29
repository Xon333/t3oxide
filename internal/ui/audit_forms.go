package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/charmbracelet/x/ansi"
)

// Forms stage values locally. The original prompt remains the only authority
// that can accept them; viewing, cycling and Back never dispatch a mutation.
type modelSettingsForm struct {
	review    *client.ModelSettingsReview
	selection config.ModelSelection
	focus     int
}

func (m *Model) initModelSettings(review *client.ModelSettingsReview) {
	m.dialog.settingsForm = &modelSettingsForm{review: review, selection: review.Selection.Clone()}
	location := review.Location
	env := location.Environment
	if env == "" {
		env = location.ProfileID
	}
	if env == "" {
		env = location.EnvironmentID
	}
	title := location.Title
	if title == "" {
		title = location.ThreadID
	}
	m.dialog.title = "Settings · " + env + " / " + title
	m.dialog.description = review.Scope + " · source thread: " + location.ThreadID
}
func (f *modelSettingsForm) model() *client.CatalogModel {
	for i := range f.review.Catalog.Models {
		if f.review.Catalog.Models[i].Slug == f.selection.Model {
			return &f.review.Catalog.Models[i]
		}
	}
	return nil
}
func (f *modelSettingsForm) optionCount() int {
	if model := f.model(); model != nil {
		return len(model.Options)
	}
	return 0
}
func descriptorDefault(d client.OptionDescriptor) any {
	if d.CurrentValue != nil {
		return d.CurrentValue
	}
	for _, option := range d.Options {
		if option.IsDefault {
			return option.ID
		}
	}
	return nil
}
func (m *Model) cycleModelSetting(delta int) {
	f := m.dialog.settingsForm
	if f.focus == 0 {
		models := f.review.Catalog.Models
		if len(models) == 0 || f.review.Catalog.RequiresNewThreadForModelChange {
			return
		}
		index := 0
		for i := range models {
			if models[i].Slug == f.selection.Model {
				index = i
				break
			}
		}
		next := models[(index+delta+len(models))%len(models)]
		if next.Slug == f.selection.Model {
			return
		}
		f.selection.Model, f.selection.Options = next.Slug, nil
		for _, descriptor := range next.Options {
			if value := descriptorDefault(descriptor); value != nil {
				f.selection = f.selection.WithOption(descriptor.ID, value)
			}
		}
	} else if model := f.model(); model != nil && f.focus <= len(model.Options) {
		descriptor := model.Options[f.focus-1]
		value, found := f.selection.Option(descriptor.ID)
		if !found {
			value = descriptorDefault(descriptor)
		}
		if descriptor.Type == "boolean" {
			f.selection = f.selection.WithOption(descriptor.ID, value != true)
			return
		}
		// Include an explicit provider-default choice instead of inventing a value.
		values := []any{nil}
		for _, option := range descriptor.Options {
			values = append(values, option.ID)
		}
		index := 0
		for i, candidate := range values {
			if candidate == value {
				index = i
				break
			}
		}
		f.selection = f.selection.WithOption(descriptor.ID, values[(index+delta+len(values))%len(values)])
	}
}
func (m *Model) modelSettingsKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.dialog.settingsForm
	count := f.optionCount() + 3 // model, options, Apply, Cancel
	if key.Matches(msg, m.actionBinding("info")) {
		return m.dispatch("info", "")
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		return m.cancelForm()
	case "tab", "down":
		f.focus = (f.focus + 1) % count
	case "shift+tab", "up":
		f.focus = (f.focus + count - 1) % count
	case "left":
		m.cycleModelSetting(-1)
	case "right", "space", " ":
		m.cycleModelSetting(1)
	case "enter":
		if f.focus == count-2 {
			return m.applyModelSettings()
		}
		if f.focus == count-1 {
			return m.cancelForm()
		}
		return m.openModelSettingChooser()
	}
	m.resize()
	return nil
}
func (m *Model) applyModelSettings() tea.Cmd {
	d := m.dialog
	if d == nil || d.settingsForm == nil || d.prompt == nil {
		return nil
	}
	f := d.settingsForm
	if !slices.Contains(f.review.Actions, "apply") {
		m.notice = "Apply is not offered"
		return nil
	}
	selection := f.selection.Clone()
	if err := selection.Validate(); err != nil {
		m.problem = err.Error()
		return nil
	}
	if !d.prompt.Respond(client.Answer{Action: "apply", ModelSettings: &selection}) {
		m.notice = "This settings request was retired; no change sent"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}
func (m *Model) modelSettingsView(width, height int) string {
	f := m.dialog.settingsForm
	state := "No changes"
	if !f.selection.Equal(f.review.Selection) {
		state = "Changes staged"
	}
	scope := f.review.Scope
	if scope == "" {
		scope = "Current thread"
	}
	heading := fitLine(scope+" · "+state, width)
	rows := []string{"Model  " + f.selection.Model}
	if f.model() == nil {
		rows[0] += " · unavailable"
	}
	if model := f.model(); model != nil {
		for _, descriptor := range model.Options {
			value, exists := f.selection.Option(descriptor.ID)
			label := "Provider default"
			if exists {
				label = fmt.Sprint(value)
				for _, option := range descriptor.Options {
					if option.ID == value {
						label = option.Label
						if label == "" {
							label = option.ID
						}
						break
					}
				}
			}
			rows = append(rows, descriptor.Label+"  "+label)
		}
	}
	rows = append(rows, "Apply changes", "Cancel")
	return colors(m.theme).subtle().Render(heading) + "\n" + m.focusedFormRows(rows, f.focus, width, max(1, height-1), "")
}

// The form sizes to its content; large provider catalogs keep the focused
// field in the native viewport instead of leaving a permanent empty panel.
func (m *Model) modelSettingsPreferredHeight() int {
	return m.dialog.settingsForm.optionCount() + 5 // scope, model, options, Apply, Cancel, help
}

// Full details use the source values, never the ellipsized form rendering.
func (m *Model) modelSettingsDetails() string {
	f := m.dialog.settingsForm
	location := f.review.Location
	rows := []string{
		"Scope: " + f.review.Scope,
		"Environment: " + location.Environment + " (" + location.EnvironmentID + ")",
		"Profile: " + location.ProfileID,
		"Source thread: " + location.ThreadID,
		"Conversation: " + location.Title,
		"Workspace: " + location.Workspace,
		"Provider instance: " + f.selection.InstanceID,
		"Saved: " + f.review.Selection.Summary(),
		"Staged: " + f.selection.Summary(),
	}
	if model := f.model(); model != nil {
		rows = append(rows, "", "Model: "+model.Name+" ("+model.Slug+")")
		for _, descriptor := range model.Options {
			value, exists := f.selection.Option(descriptor.ID)
			label := "Provider default"
			if exists {
				label = fmt.Sprint(value)
			}
			rows = append(rows, "", descriptor.Label+" ("+descriptor.ID+"): "+label, descriptor.Description)
			for _, choice := range descriptor.Options {
				rows = append(rows, choice.Label+" · "+choice.ID+" · "+choice.Description)
			}
		}
	} else {
		rows = append(rows, "The staged model is no longer offered. Choose an advertised model before applying.")
	}
	if f.review.Catalog.RequiresNewThreadForModelChange {
		rows = append(rows, "This provider requires a new conversation to change models.")
	}
	rows = append(rows, "", "Edits stay local until Apply changes. T3 validates the source and current catalog before updating it.")
	return safeText(strings.Join(rows, "\n"))
}

// One context-sensitive help line owns each form's controls. The outer modal
// renders it, keeping compact forms from paying for a second shortcut row.
func (m *Model) formDialogHelpPairs() []string {
	d := m.dialog
	info, dismiss := m.actionBinding("info").Help().Key, m.actionBinding("dismiss").Help().Key
	if d.settingsForm != nil {
		f := d.settingsForm
		action := "choose"
		if f.focus == f.optionCount()+1 {
			action = "apply"
		} else if f.focus == f.optionCount()+2 {
			action = "cancel"
		}
		return []string{"Enter", action, "Tab/↑↓", "field", info, "Info", dismiss, "back"}
	}
	if d.requestForm != nil {
		f := d.requestForm
		action := "select"
		switch f.focus {
		case 1:
			submit := "next"
			if f.index == len(f.questions)-1 {
				submit = "submit"
			}
			pairs := []string{"Tab", "controls", "Enter", "newline"}
			if binding := m.actionBinding("request-submit"); binding.Enabled() {
				pairs = append(pairs, binding.Help().Key, submit)
			}
			return append(pairs, info, "Info", dismiss, "back")
		case 2:
			action = "back"
		case 3:
			action = "next"
			if f.index == len(f.questions)-1 {
				action = "submit"
			}
		case 4:
			action = "cancel"
		}
		return []string{"Enter", action, "Tab", "controls", info, "Info", dismiss, "back"}
	}
	return nil
}
func (m *Model) focusedFormRows(rows []string, focus, width, height int, help string) string {
	for i := range rows {
		rows[i] = ansi.Truncate(safeText(rows[i]), max(1, width-2), "…")
		if i == focus {
			rows[i] = colors(m.theme).selection().Render("› " + rows[i])
		} else {
			rows[i] = "  " + rows[i]
		}
	}
	available := height
	if help != "" {
		available--
	}
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(max(1, available)))
	v.SetContent(strings.Join(rows, "\n"))
	if focus >= v.Height() {
		v.SetYOffset(focus - v.Height() + 1)
	}
	if help != "" {
		return v.View() + "\n" + ansi.Truncate(help, width, "…")
	}
	return v.View()
}

// Local return frames are bounded and contain no remote prompt. Native editor
// values retain caret and selection; remote prompt liveness is never restored.
type localSurface struct {
	generation uint64
	dialog     *dialog
}

func (m *Model) pushLocalSurface(d *dialog) {
	if d == nil || d.prompt != nil {
		return
	}
	if len(m.localBack) == 8 {
		m.localBack = m.localBack[1:]
	}
	m.localBack = append(m.localBack, localSurface{m.generation, d})
}
func (m *Model) popLocalSurface() tea.Cmd {
	for len(m.localBack) > 0 {
		last := m.localBack[len(m.localBack)-1]
		m.localBack = m.localBack[:len(m.localBack)-1]
		if last.generation != m.generation || last.dialog.prompt != nil {
			continue
		}
		m.dialog = last.dialog
		m.resize()
		return m.restoreFocus()
	}
	return m.restoreFocus()
}
func (m *Model) cancelForm() tea.Cmd {
	d := m.dialog
	if d != nil && d.prompt != nil {
		answer := client.Answer{Action: "cancel", Cancelled: true}
		if d.requestForm != nil {
			if !m.saveRequestAnswer() {
				return nil
			}
			answer.RequestAction = "cancel"
			answer.RequestProgress = d.requestForm.progress()
		}
		d.prompt.Respond(answer)
	}
	m.dialog = nil
	if len(m.localBack) > 0 {
		return m.popLocalSurface()
	}
	m.resize()
	return m.restoreFocus()
}
func (m *Model) attachedTargetStatus() string {
	if m.status.ThreadID == "" {
		return m.authoritativeStatus()
	}
	env := m.status.Location.Environment
	if env == "" {
		env = m.status.ProfileID
	}
	if env == "" {
		env = m.status.EnvironmentID
	}
	title := m.status.Location.Title
	if title == "" {
		title = m.status.ThreadID
	}
	return "Attached: " + env + " / " + title + " · " + m.authoritativeStatus() + " · Ctrl+S Stop attached turn"
}

func (m *Model) openModelSettingChooser() tea.Cmd {
	source := m.dialog
	f := source.settingsForm
	var items []list.Item
	local := "settings-model"
	title, current := "Model", f.selection.Model
	if f.focus == 0 {
		if f.review.Catalog.RequiresNewThreadForModelChange {
			m.notice = "This provider requires a new conversation to change models"
			return nil
		}
		for _, model := range f.review.Catalog.Models {
			label := model.Name
			if label == "" {
				label = model.Slug
			}
			items = append(items, choiceItem{id: model.Slug, title: label, description: model.Slug})
		}
	} else if model := f.model(); model != nil && f.focus <= len(model.Options) {
		local = "settings-option"
		descriptor := model.Options[f.focus-1]
		title, current = descriptor.Label, "default"
		if value, exists := f.selection.Option(descriptor.ID); exists {
			if descriptor.Type == "boolean" {
				current = fmt.Sprint(value)
			} else {
				current = "value:" + fmt.Sprint(value)
			}
		}
		items = append(items, choiceItem{id: "default", title: "Provider default"})
		if descriptor.Type == "boolean" {
			items = append(items, choiceItem{id: "true", title: "On"}, choiceItem{id: "false", title: "Off"})
		} else {
			for _, option := range descriptor.Options {
				label := option.Label
				if label == "" {
					label = option.ID
				}
				items = append(items, choiceItem{id: "value:" + option.ID, title: label, description: option.Description})
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	m.dialogSerial++
	d := &dialog{serial: m.dialogSerial, title: title + " · staged choice", local: local, formSource: source}
	d.list = m.newList(d.title, items)
	d.list.SetFilterText("")
	d.list.SetFilterState(list.Filtering)
	for i, raw := range items {
		item := raw.(choiceItem)
		if item.id == current {
			item.description = strings.TrimSuffix("Selected · "+item.description, " · ")
			d.list.SetItem(i, item)
			d.refilter(false)
			d.list.Select(i)
			break
		}
	}
	m.dialog = d
	m.refreshDialogDetails()
	m.resize()
	return m.restoreFocus()
}
func (m *Model) restoreSettingsForm() tea.Cmd {
	source := m.dialog.formSource
	m.dialog = nil
	if source != nil && source.prompt != nil {
		select {
		case <-source.prompt.Done:
			m.notice = "This settings request was retired"
			source = nil
		default:
		}
	}
	m.dialog = source
	m.resize()
	return m.restoreFocus()
}
func (m *Model) chooseModelSetting(selected choiceItem) tea.Cmd {
	d := m.dialog
	source := d.formSource
	m.restoreSettingsForm()
	if m.dialog != source || source == nil {
		return nil
	}
	f := source.settingsForm
	if d.local == "settings-model" {
		for _, model := range f.review.Catalog.Models {
			if model.Slug == selected.id {
				if model.Slug == f.selection.Model {
					return m.restoreFocus()
				}
				f.selection.Model, f.selection.Options = model.Slug, nil
				for _, descriptor := range model.Options {
					if value := descriptorDefault(descriptor); value != nil {
						f.selection = f.selection.WithOption(descriptor.ID, value)
					}
				}
				break
			}
		}
	} else if model := f.model(); model != nil && f.focus > 0 && f.focus <= len(model.Options) {
		descriptor := model.Options[f.focus-1]
		var value any
		if descriptor.Type == "boolean" && selected.id != "default" {
			value = selected.id == "true"
		} else if v, ok := strings.CutPrefix(selected.id, "value:"); ok {
			value = v
		}
		f.selection = f.selection.WithOption(descriptor.ID, value)
	}
	m.resize()
	return m.restoreFocus()
}
