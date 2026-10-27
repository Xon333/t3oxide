package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"github.com/Xon333/t3oxide/internal/client"
)

// This is presentation metadata for existing commands, not another command
// runtime. Dispatch remains the small UI switch and client.Session.Command.
// Palette, slash completion and the command reference consume these same rows.
type commandSpec struct {
	name, title, arguments, description string
	binding                             key.Binding
}

func (m *Model) commandCatalog() []commandSpec {
	k := m.keyMap()
	rows := []commandSpec{
		{"switch", "Conversations", "[words]", "", k.Threads},
		{"search", "Find in current page", "[text]", "", k.Search},
		{"earlier", "Earlier history", "", "Pauses live detail", k.Earlier},
		{"latest", "Latest", "", "Resumes live detail", k.Latest},
		{"requests", "Requests", "", "Review offered questions and approvals", k.Requests},
		{"activity", "Activity and results", "[item-key]", "Tools and results; separate activity history", key.Binding{}},
		{"help", "Keyboard help", "", "", k.Help},
		{"edit", "Edit draft", "", "External editor", k.Edit},
		{"recall", "Prompt history", "", "", k.Recall},
		{"clear", "Clear draft", "", "Recoverable in prompt history", k.Clear},
		{"env", "Environments", "", "", k.Environments},
		{"threads", "Search conversations", "[words]", "Host titles and projects, including unloaded rows", key.Binding{}},
		{"new", "New conversation", "[title]", "Uses the current workspace", key.Binding{}},
		{"settings", "Settings", "", "Model, effort, tier and appearance", key.Binding{}},
		{"details", "Details", "", "", k.Detail},
		{"stop", "Stop + hold queue", "", "Reviewed active turn", k.Stop},
		{"filter", "Conversation types", "[all|main|subagents|forks]", "", key.Binding{}},
		{"parent", "Parent conversation", "", "", k.Parent},
		{"model", "Thread model", "[next] [model]", "", k.Model},
		{"effort", "Reasoning effort", "[next] [value]", "", k.Effort},
		{"speed", "Speed", "[next] [value]", "", k.Speed},
		{"presets", "Model presets", "[next] [save|remove|apply] [name]", "", key.Binding{}},
		{"override", "Next-message override", "[model|effort|speed|presets|clear]", "T3 may adopt the dispatched selection", key.Binding{}},
		{"runtime", "Runtime details", "", "Saved, requested and reported settings", key.Binding{}},
		{"delivery", "Delivery", "auto|queue|steer", "", key.Binding{}},
		{"pending", "Saved operations", "", "Original IDs and payloads", key.Binding{}},
		{"retry", "Retry saved action", "[stop]", "Reuses the original operation ID", key.Binding{}},
		{"resolve", "Resolve reviewed action", "[stop]", "Confirms local record clearance", key.Binding{}},
		{"error", "Error details", "", "", k.Error},
		{"theme", "Theme", "[terminal|violet|cyan|lime]", "", key.Binding{}},
		{"mouse", "Mouse", "[on|off]", "Click and scroll; off enables native selection", key.Binding{}},
		{"sidebar", "Context pane", "[on|off]", "", k.Sidebar},
		{"context", "Conversation context", "", "", key.Binding{}},
		{"navigator", "Conversation sidebar", "[on|off]", "", key.Binding{}},
		{"copy", "Copy", "[page|draft|answer|error|detail]", "Last agent message by default", k.Copy},
		{"reconnect", "Reconnect", "", "Never replays writes", key.Binding{}},
		{"web", "Show T3 address", "", "Configured address for this environment", key.Binding{}},
		{"keyboard", "Keyboard shortcuts", "modern|legacy", "Modern Ctrl+P Commands; legacy editor Ctrl+P", key.Binding{}},
		{"quit", "Exit", "", "Detaches locally", k.Quit},
	}
	// Reuse semantic task descriptions from the client contract, including
	// rows whose concise UI label deliberately differs from the command ID.
	for i := range rows {
		for _, remote := range client.CommandCatalog() {
			if rows[i].name == remote.Name && rows[i].description == "" {
				rows[i].description = remote.Description
				break
			}
		}
	}
	// The session is the authority for supported remote command names. Retain
	// any additive session command rather than maintaining another hidden list.
	known := make(map[string]bool, len(rows))
	for _, row := range rows {
		known[row.name] = true
	}
	for _, row := range client.CommandCatalog() {
		if !known[row.Name] {
			rows = append(rows, commandSpec{name: row.Name, title: row.Name, description: row.Description})
		}
	}
	return rows
}

func (m *Model) commandSpec(name string) (commandSpec, bool) {
	if name == "keys" {
		name = "help"
	}
	for _, row := range m.commandCatalog() {
		if row.name == name {
			return row, true
		}
	}
	return commandSpec{}, false
}

func (m *Model) commandUnavailable(name string) string {
	switch name {
	case "requests":
		if m.status.Requests == 0 {
			return "Unavailable: no offered questions or approvals for the attached conversation"
		}
	case "stop":
		if m.status.ActiveRunID == "" {
			return "Unavailable: no reviewed active turn; use Latest to refresh"
		}
	case "earlier":
		if m.snapshot == nil || !m.snapshot.Metadata.HasMore {
			return "Unavailable: no earlier message page is offered"
		}
	case "activity":
		if !m.status.Activity {
			return "Unavailable: this attachment has no activity-capable adapter"
		}
	case "pending":
		if !m.status.PendingAction && !m.status.PendingStop {
			return "Unavailable: no saved operations for the attached conversation"
		}
	case "model", "effort", "speed", "override":
		if !m.status.ModelControls {
			return "Unavailable: connect a writable thread with a model-capable adapter"
		}
	case "filter":
		if !m.status.ThreadNavigation {
			return "Unavailable: this adapter has no lineage navigation"
		}
	case "parent":
		if !m.status.ParentAvailable {
			return "Unavailable: this conversation has no reported parent"
		}
	case "runtime":
		if !m.status.RuntimeDetails {
			return "Unavailable: no runtime details reported"
		}
	}
	return ""
}

func (m *Model) paletteItems() []list.Item {
	var items []list.Item
	for _, spec := range m.commandCatalog() {
		if spec.name == "delivery" {
			for _, mode := range []string{"auto", "queue", "steer"} {
				items = append(items, choiceItem{title: "Delivery · " + mode, command: spec.name, args: mode})
			}
			continue
		}
		if spec.name == "copy" || spec.name == "retry" || spec.name == "resolve" || spec.name == "keyboard" {
			variants := []struct{ args, label string }{}
			switch spec.name {
			case "keyboard":
				variants = []struct{ args, label string }{{"modern", "Ctrl+P Commands (default)"}, {"legacy", "Editor Ctrl+P"}}
			case "copy":
				variants = []struct{ args, label string }{{"", "Last agent message (loaded page)"}, {"page", "Loaded page (bounded)"}, {"draft", "Exact draft"}, {"answer", "Current answer"}, {"error", "Error details"}, {"detail", "Open details"}}
			default:
				variants = []struct{ args, label string }{{"", "ordinary action"}, {"stop", "Stop operation"}}
			}
			for _, variant := range variants {
				row := choiceItem{title: spec.title + " · " + variant.label, description: spec.description, command: spec.name, args: variant.args}
				if spec.name == "retry" || spec.name == "resolve" {
					available := m.status.PendingAction
					if variant.args == "stop" {
						available = m.status.PendingStop
					}
					if !available {
						row.disabled = true
						row.description += " · Unavailable: no saved " + variant.label + " for this attachment"
					}
				}
				items = append(items, row)
			}
			continue
		}
		description := strings.TrimSpace(spec.description + " · " + spec.arguments)
		description = strings.Trim(description, " ·")
		if spec.binding.Enabled() {
			if description != "" {
				description += " · "
			}
			description += spec.binding.Help().Key
		}
		row := choiceItem{title: spec.title, description: description, command: spec.name}
		if reason := m.commandUnavailable(spec.name); reason != "" {
			row.disabled, row.description = true, description+" · "+reason
		}
		items = append(items, row)
	}
	if m.exactRejectedInputAvailable() {
		items = append(items, choiceItem{title: m.exactRejectedInputLabel(), description: "Replace draft locally; keeps previous draft in recall", command: "use-exact"})
	}
	return items
}

func (m *Model) validateCommand(name, args string) error {
	spec, ok := m.commandSpec(name)
	if !ok {
		return fmt.Errorf("unknown command /%s", name)
	}
	valid := true
	switch name {
	case "help", "keys", "env", "earlier", "latest", "stop", "requests", "parent", "runtime", "pending", "reconnect", "web", "quit", "edit", "clear", "error", "settings", "details", "context":
		valid = args == ""
	case "keyboard":
		valid = args == "modern" || args == "legacy"
	case "theme":
		valid = args == "" || ValidTheme(args) || args == "default"
	case "mouse", "sidebar", "navigator":
		valid = args == "" || args == "on" || args == "off"
	case "delivery":
		valid = args == "auto" || args == "queue" || args == "steer"
	case "retry", "resolve":
		valid = args == "" || args == "stop"
	case "copy":
		valid = args == "" || args == "page" || args == "draft" || args == "answer" || args == "error" || args == "detail"
	case "filter":
		valid = args == "" || args == "all" || args == "main" || args == "subagents" || args == "forks"
	case "override":
		valid = args == "" || args == "model" || args == "effort" || args == "speed" || args == "presets" || args == "clear"
	case "new":
		valid = len(args) <= 512
	}
	if !valid {
		return fmt.Errorf("use /%s %s", name, spec.arguments)
	}
	return nil
}

func (m *Model) isSlashCommand(text string) bool {
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, "\r\n") {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	_, known := m.commandSpec(name)
	return known
}
