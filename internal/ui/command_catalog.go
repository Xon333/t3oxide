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
		{"requests", "T3 requests", "", "", k.Requests},
		{"help", "Keyboard help", "", "", k.Help},
		{"edit", "Edit draft", "", "External editor", k.Edit},
		{"recall", "Prompt history", "", "", k.Recall},
		{"clear", "Clear draft", "", "Recoverable in prompt history", k.Clear},
		{"env", "Environments", "", "", k.Environments},
		{"threads", "Find conversations", "[words]", "Titles and projects", key.Binding{}},
		{"new", "New conversation", "[title]", "Uses the current workspace", key.Binding{}},
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
		{"error", "Error details", "", "", k.Detail},
		{"theme", "Theme", "[terminal|violet|cyan|lime]", "", key.Binding{}},
		{"mouse", "Mouse capture", "[on|off]", "Off enables native selection", key.Binding{}},
		{"sidebar", "Sidebar", "[on|off]", "", k.Sidebar},
		{"copy", "Copy", "[page|draft|answer|error|detail]", "Last assistant message by default", k.Copy},
		{"reconnect", "Reconnect", "", "Never replays writes", key.Binding{}},
		{"web", "Full T3", "", "Configured address", key.Binding{}},
		{"quit", "Exit", "", "Detaches locally", k.Quit},
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
	case "model", "effort", "speed", "override":
		if !m.status.ModelControls {
			return "Unavailable: connect a writable thread with a model-capable adapter"
		}
	case "presets":
		if !m.status.ModelCatalog {
			return "Unavailable: this adapter has no model catalog"
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
		description := spec.arguments
		if spec.binding.Enabled() {
			if description != "" {
				description += " · "
			}
			description += spec.binding.Help().Key
		}
		row := choiceItem{title: spec.title, description: description, command: spec.name}
		if reason := m.commandUnavailable(spec.name); reason != "" {
			row.disabled, row.description = true, reason
		}
		items = append(items, row)
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
	case "help", "keys", "env", "earlier", "latest", "stop", "requests", "parent", "runtime", "pending", "reconnect", "web", "quit", "edit", "clear", "error":
		valid = args == ""
	case "theme":
		valid = args == "" || ValidTheme(args)
	case "mouse", "sidebar":
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
