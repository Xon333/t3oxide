package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"github.com/Xon333/t3oxide/internal/client"
)

// Application actions use the same resolved Bubbles bindings in dispatch,
// help and command hints. Component editing/navigation keeps its native owner.
// Context-specific keys augment the global defaults. A configured context
// replaces the keys for that action in that context, without changing others.
type shortcutAction struct {
	id, command, label string
	keys               map[string][]string
}

var shortcutContexts = []string{"global", "composer", "reading", "navigator", "activity", "context", "picker", "form", "search", "detail", "agentbox"}

func workspaceKeys(reading, composer []string) map[string][]string {
	return map[string][]string{"composer": composer, "reading": reading, "navigator": reading, "activity": reading, "context": reading}
}

func workspaceNavigatorKeys(reading, composer, navigator []string) map[string][]string {
	keys := workspaceKeys(reading, composer)
	keys["navigator"] = navigator
	return keys
}

var shortcutActions = []shortcutAction{
	{"stop", "stop", "Stop/hold", map[string][]string{"global": {"ctrl+s"}}},
	{"quit", "quit", "exit", map[string][]string{"global": {"ctrl+q"}}},
	{"palette", "", "commands", map[string][]string{"global": {"ctrl+space", "ctrl+@", "f3", "super+p"}, "composer": {"ctrl+p"}, "reading": {"ctrl+p"}, "navigator": {"ctrl+p"}, "activity": {"ctrl+p"}, "context": {"ctrl+p"}, "form": {"ctrl+p"}, "search": {"ctrl+p"}, "detail": {"ctrl+p"}, "agentbox": {"ctrl+p"}}},
	{"help", "help", "help", map[string][]string{"global": {"ctrl+alt+h", "f1", "super+h"}, "reading": {"?"}, "activity": {"?"}, "context": {"?"}, "agentbox": {"?"}}},
	{"info", "info", "Info", map[string][]string{"global": {"ctrl+l", "f2", "super+i"}}},
	{"error", "error", "error", map[string][]string{"global": {"f8"}}},
	{"mode", "mode", "mode", map[string][]string{"global": {"ctrl+alt+b", "shift+super+b"}}},
	{"switch", "switch", "conversations", map[string][]string{"composer": {"ctrl+o", "f6", "super+o"}, "reading": {"ctrl+o", "f6", "super+o"}, "navigator": {"ctrl+o", "super+o"}, "activity": {"ctrl+o", "f6", "super+o"}, "context": {"ctrl+o", "f6", "super+o"}}},
	{"environments", "env", "environments", workspaceNavigatorKeys([]string{"ctrl+e"}, nil, []string{"ctrl+alt+e"})},
	{"requests", "requests", "requests", workspaceKeys([]string{"ctrl+r"}, []string{"ctrl+r"})},
	{"search", "search", "find in page", map[string][]string{"reading": {"ctrl+f", "/", "super+f"}, "navigator": {"ctrl+alt+f", "super+f"}, "activity": {"ctrl+f", "/", "super+f"}, "context": {"ctrl+f", "/", "super+f"}}},
	{"sidebar", "sidebar", "activity pane", workspaceNavigatorKeys([]string{"ctrl+b", "ctrl+alt+a", "super+b"}, []string{"ctrl+alt+a", "super+b"}, []string{"ctrl+alt+a", "super+b"})},
	{"model", "model", "model", workspaceKeys([]string{"alt+m", "ctrl+alt+m", "super+m"}, []string{"ctrl+alt+m", "super+m"})},
	{"effort", "effort", "effort", workspaceKeys([]string{"alt+e", "ctrl+alt+d", "super+e"}, []string{"ctrl+alt+d", "super+e"})},
	{"speed", "speed", "speed", workspaceKeys([]string{"alt+s"}, nil)},
	{"parent", "parent", "parent", workspaceKeys([]string{"alt+up"}, nil)},
	{"earlier", "earlier", "earlier", workspaceNavigatorKeys([]string{"alt+left"}, nil, nil)},
	{"latest", "latest", "latest", workspaceNavigatorKeys([]string{"alt+right"}, nil, nil)},
	{"copy", "copy", "copy", map[string][]string{"composer": {"ctrl+y"}, "reading": {"ctrl+y"}, "detail": {"ctrl+y"}, "form": {"ctrl+y"}, "agentbox": {"ctrl+y"}}},
	{"focus-next", "", "focus", map[string][]string{"composer": {"tab"}, "reading": {"tab"}, "navigator": {"tab"}, "activity": {"tab"}, "context": {"tab"}}},
	{"focus-previous", "", "focus back", map[string][]string{"composer": {"shift+tab"}, "reading": {"shift+tab"}, "navigator": {"shift+tab"}, "activity": {"shift+tab"}, "context": {"shift+tab"}}},
	// CSI arrow modifiers use Meta for the Command bit; Kitty u uses Super.
	{"focus-navigation", "", "conversations", workspaceKeys([]string{"ctrl+alt+left", "super+left", "meta+left"}, []string{"ctrl+alt+left", "super+left", "meta+left"})},
	{"focus-composer", "", "compose", workspaceKeys([]string{"ctrl+alt+right", "super+right", "meta+right"}, []string{"ctrl+alt+right", "super+right", "meta+right"})},
	{"clear", "clear", "clear draft", map[string][]string{"composer": {"ctrl+x"}}},
	{"edit", "edit", "editor", map[string][]string{"composer": {"f4"}}},
	{"recall", "recall", "prompts", map[string][]string{"composer": {"f5"}, "reading": {"f5"}}},
	{"send", "send", "send", map[string][]string{"composer": {"ctrl+]", "super+enter"}}},
	{"send-literal", "send-literal", "send text", map[string][]string{"composer": {"ctrl+\\", "ctrl+enter"}}},
	{"steer", "steer", "steer", map[string][]string{"composer": {"ctrl+alt+enter", "shift+super+enter"}}},
	{"request-submit", "", "next/submit", map[string][]string{"form": {"ctrl+enter"}}},
	{"dismiss", "", "back", map[string][]string{"global": {"esc"}}},
	{"new", "new", "New", workspaceKeys([]string{"ctrl+n", "super+n"}, []string{"ctrl+alt+n", "super+n"})},
	{"new-same", "new-same", "New same project", workspaceKeys([]string{"ctrl+alt+n", "ctrl+alt+t", "shift+super+n", "ctrl+shift+n"}, []string{"ctrl+alt+t", "shift+super+n"})},
	{"settings", "settings", "settings", workspaceKeys([]string{"ctrl+g", "ctrl+alt+g", "super+,"}, []string{"ctrl+alt+g", "super+,"})},
	{"refresh", "refresh", "refresh", workspaceKeys([]string{"alt+r", "ctrl+alt+r", "super+r"}, []string{"ctrl+alt+r", "super+r"})},
	{"filter", "filter", "types", workspaceKeys([]string{"ctrl+/", "ctrl+_"}, nil)},
	{"navigator.more", "", "more", map[string][]string{"navigator": {"alt+a", "f6"}}},
	{"browser.search", "", "search conversations", map[string][]string{"picker": {"ctrl+alt+f", "super+f"}}},
	{"browser.filter", "", "thread types", map[string][]string{"picker": {"f6"}}},
	{"thread.1", "", "thread 1", map[string][]string{"composer": {"ctrl+alt+1", "super+1"}, "reading": {"ctrl+alt+1", "super+1"}, "navigator": {"ctrl+alt+1", "super+1"}}},
	{"thread.2", "", "thread 2", map[string][]string{"composer": {"ctrl+alt+2", "super+2"}, "reading": {"ctrl+alt+2", "super+2"}, "navigator": {"ctrl+alt+2", "super+2"}}},
	{"thread.3", "", "thread 3", map[string][]string{"composer": {"ctrl+alt+3", "super+3"}, "reading": {"ctrl+alt+3", "super+3"}, "navigator": {"ctrl+alt+3", "super+3"}}},
	{"thread.4", "", "thread 4", map[string][]string{"composer": {"ctrl+alt+4", "super+4"}, "reading": {"ctrl+alt+4", "super+4"}, "navigator": {"ctrl+alt+4", "super+4"}}},
	{"thread.5", "", "thread 5", map[string][]string{"composer": {"ctrl+alt+5", "super+5"}, "reading": {"ctrl+alt+5", "super+5"}, "navigator": {"ctrl+alt+5", "super+5"}}},
	{"thread.6", "", "thread 6", map[string][]string{"composer": {"ctrl+alt+6", "super+6"}, "reading": {"ctrl+alt+6", "super+6"}, "navigator": {"ctrl+alt+6", "super+6"}}},
	{"thread.7", "", "thread 7", map[string][]string{"composer": {"ctrl+alt+7", "super+7"}, "reading": {"ctrl+alt+7", "super+7"}, "navigator": {"ctrl+alt+7", "super+7"}}},
	{"thread.8", "", "thread 8", map[string][]string{"composer": {"ctrl+alt+8", "super+8"}, "reading": {"ctrl+alt+8", "super+8"}, "navigator": {"ctrl+alt+8", "super+8"}}},
	{"thread.9", "", "thread 9", map[string][]string{"composer": {"ctrl+alt+9", "super+9"}, "reading": {"ctrl+alt+9", "super+9"}, "navigator": {"ctrl+alt+9", "super+9"}}},
	{"agentbox.previous", "", "previous", map[string][]string{"agentbox": {"up", "k"}}},
	{"agentbox.next", "", "next", map[string][]string{"agentbox": {"down", "j"}}},
	{"agentbox.open", "", "open", map[string][]string{"agentbox": {"enter"}}},
	{"agentbox.reply", "", "reply", map[string][]string{"agentbox": {"r"}}},
	{"agentbox.file", "file", "file", map[string][]string{"agentbox": {"n"}}},
	{"agentbox.review", "", "mark reviewed", map[string][]string{"agentbox": {"e"}}},
	{"agentbox.later", "", "Later", map[string][]string{"agentbox": {"l"}}},
	{"agentbox.summary", "", "summary", map[string][]string{"agentbox": {"s"}}},
	{"agentbox.undo", "", "undo local", map[string][]string{"agentbox": {"z"}}},
	{"agentbox.inbox", "inbox", "inbox", map[string][]string{"agentbox": {"esc"}}},
	{"agentbox.view", "", "view", map[string][]string{"agentbox": {"v"}}},
	{"agentbox.priority", "", "priority", map[string][]string{"agentbox": {"p"}}},
	{"agentbox.diff", "review", "changes", map[string][]string{"agentbox": {"d"}}},
	{"agentbox.developer", "", "Developer", map[string][]string{"agentbox": {"o"}}},
	{"agentbox.choice-1", "", "choice 1", map[string][]string{"agentbox": {"1"}}},
	{"agentbox.choice-2", "", "choice 2", map[string][]string{"agentbox": {"2"}}},
	{"agentbox.choice-3", "", "choice 3", map[string][]string{"agentbox": {"3"}}},
	{"agentbox.search", "", "search", map[string][]string{"agentbox": {"/"}}},
	{"agentbox.refresh", "refresh", "refresh", map[string][]string{"agentbox": {"ctrl+r"}}},
	{"agentbox.focus-next", "", "focus", map[string][]string{"agentbox": {"tab"}}},
	{"agentbox.focus-previous", "", "focus back", map[string][]string{"agentbox": {"shift+tab"}}},
	{"agentbox.page-next", "", "next page", map[string][]string{"agentbox": {"pgdown"}}},
	{"agentbox.page-previous", "", "previous page", map[string][]string{"agentbox": {"pgup"}}},
}

type ShortcutConfig struct {
	Version  int                            `json:"version"`
	Bindings map[string]map[string][]string `json:"bindings"`
	path     string
}

// LoadShortcuts is a bounded, startup-only read. A missing optional file uses
// the shipped defaults; malformed configuration fails before opening the TUI.
func LoadShortcuts(path string) (*ShortcutConfig, error) {
	c := &ShortcutConfig{Version: 1, path: path}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shortcuts %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return nil, fmt.Errorf("shortcuts %s: %w", path, err)
	}
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("shortcuts %s exceeds 64 KiB", path)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	c.Version = 0
	if err = decoder.Decode(c); err != nil {
		return nil, fmt.Errorf("shortcuts %s: %w", path, err)
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("shortcuts %s: expected one JSON object", path)
	}
	if err = c.validate(); err != nil {
		return nil, fmt.Errorf("shortcuts %s: %w; remove this file to restore defaults", path, err)
	}
	return c, nil
}

func shortcutDefinition(id string) (shortcutAction, bool) {
	for _, a := range shortcutActions {
		if a.id == id {
			return a, true
		}
	}
	return shortcutAction{}, false
}

func (c *ShortcutConfig) keysFor(a shortcutAction, context string) []string {
	global, active := a.keys["global"], a.keys[context]
	if c != nil {
		if override, ok := c.Bindings["global"][a.id]; ok {
			global = override
		}
		if context != "global" {
			if override, ok := c.Bindings[context][a.id]; ok {
				return slices.Clone(override)
			}
		}
	}
	keys := slices.Clone(active)
	if context == "global" {
		keys = nil
	}
	keys = append(keys, global...)
	return keys
}

func (c *ShortcutConfig) validate() error {
	if c.Version != 1 {
		return errors.New("version must be 1")
	}
	for context, bindings := range c.Bindings {
		if !slices.Contains(shortcutContexts, context) {
			return fmt.Errorf("unknown context %q", context)
		}
		for id, keys := range bindings {
			a, ok := shortcutDefinition(id)
			if !ok {
				return fmt.Errorf("unknown action %q in %s", id, context)
			}
			if _, ok := a.keys[context]; !ok && (context == "global" || a.keys["global"] == nil) {
				return fmt.Errorf("action %q is unavailable in %s", id, context)
			}
			if len(keys) > 8 {
				return fmt.Errorf("%s.%s has more than 8 keys", context, id)
			}
			seen := make(map[string]bool)
			for i, input := range keys {
				canonical, err := canonicalKey(input)
				if err != nil {
					return fmt.Errorf("%s.%s: %w", context, id, err)
				}
				if seen[collisionKey(canonical)] {
					return fmt.Errorf("%s.%s repeats equivalent key %q", context, id, input)
				}
				seen[collisionKey(canonical)] = true
				keys[i] = canonical
				if err := reservedShortcut(context, id, canonical); err != nil {
					return err
				}
			}
			if id == "stop" && !slices.Contains(keys, "ctrl+s") {
				return errors.New("Stop must retain ctrl+s in every context; add aliases instead of replacing it")
			}
			if id == "dismiss" && !slices.Contains(keys, "esc") {
				return errors.New("local dismiss must retain esc in every context; add aliases instead of replacing it")
			}
		}
	}
	for _, context := range shortcutContexts[1:] {
		owners := make(map[string]string)
		for _, a := range shortcutActions {
			for _, k := range c.keysFor(a, context) {
				canonical := collisionKey(k)
				if owner, ok := owners[canonical]; ok && owner != a.id {
					// Escape returns from the focused AgentBox view; it is the
					// same local dismiss action, never a remote cancellation.
					if context == "agentbox" && canonical == "esc" && (a.id == "agentbox.inbox" && owner == "dismiss") {
						continue
					}
					return fmt.Errorf("%s: key %q conflicts between %s and %s", context, k, owner, a.id)
				}
				owners[canonical] = a.id
			}
		}
	}
	return nil
}

func reservedShortcut(context, id, k string) error {
	conflict := func(owner string) error { return fmt.Errorf("%s.%s: %q belongs to %s", context, id, k, owner) }
	if k == "ctrl+s" && id != "stop" {
		return conflict("independent Stop")
	}
	if k == "ctrl+c" {
		return conflict("local cancellation and exact draft preservation")
	}
	if k == "ctrl+i" || k == "ctrl+m" || k == "ctrl+h" || k == "ctrl+[" {
		return conflict("legacy Tab, Enter, Backspace or Escape; choose an unambiguous key")
	}
	if context == "global" && id != "dismiss" && k == "esc" {
		return conflict("local dismiss")
	}
	if context == "global" || context == "composer" || context == "form" || context == "picker" || context == "search" || context == "navigator" {
		if utf8.RuneCountInString(k) == 1 || k == "space" {
			return conflict("text input; put character bindings in a non-editor context")
		}
		reserved := []string{"ctrl+a", "ctrl+b", "ctrl+d", "ctrl+e", "ctrl+f", "ctrl+g", "ctrl+h", "ctrl+j", "ctrl+k", "ctrl+m", "ctrl+n", "ctrl+t", "ctrl+u", "ctrl+v", "ctrl+w", "ctrl+backspace", "alt+backspace", "alt+b", "alt+f", "alt+d", "alt+c", "alt+l", "alt+u", "left", "right", "up", "down", "home", "end", "backspace", "delete", "ctrl+left", "ctrl+right", "shift+left", "shift+right", "shift+up", "shift+down", "ctrl+shift+left", "ctrl+shift+right", "alt+left", "alt+right", "alt+enter", "shift+enter", "enter"}
		if slices.Contains(reserved, k) {
			return conflict("the focused editor")
		}
		if (k == "tab" || k == "shift+tab") && id != "focus-next" && id != "focus-previous" {
			return conflict("focus/completion")
		}
		if k == "ctrl+enter" && !(context == "composer" && id == "send-literal" || context == "form" && id == "request-submit") {
			return conflict("explicit literal send in composer or request Submit in forms")
		}
	}
	return nil
}

func canonicalKey(input string) (string, error) {
	if input == " " {
		return "space", nil
	}
	if input == "" || len(input) > 64 || strings.TrimSpace(input) != input {
		return "", fmt.Errorf("invalid key %q", input)
	}
	parts := strings.Split(input, "+")
	mods := make(map[string]bool)
	for _, mod := range parts[:len(parts)-1] {
		mod = strings.ToLower(mod)
		switch mod {
		case "control":
			mod = "ctrl"
		case "cmd", "command":
			mod = "super"
		case "option", "opt":
			mod = "alt"
		}
		if !slices.Contains([]string{"ctrl", "alt", "shift", "meta", "hyper", "super"}, mod) || mods[mod] {
			return "", fmt.Errorf("invalid or repeated modifier in %q", input)
		}
		mods[mod] = true
	}
	name := parts[len(parts)-1]
	if len(mods) > 0 || utf8.RuneCountInString(name) != 1 {
		name = strings.ToLower(name)
	}
	if name == "escape" {
		name = "esc"
	}
	valid := utf8.RuneCountInString(name) == 1
	if valid {
		r, _ := utf8.DecodeRuneInString(name)
		valid = unicode.IsPrint(r) && r != '+'
	}
	if !valid {
		valid = slices.Contains([]string{"enter", "tab", "esc", "backspace", "delete", "insert", "space", "up", "down", "left", "right", "home", "end", "pgup", "pgdown"}, name)
		if strings.HasPrefix(name, "f") {
			n, err := strconv.Atoi(strings.TrimPrefix(name, "f"))
			valid = err == nil && n >= 1 && n <= 24
		}
	}
	if !valid {
		return "", fmt.Errorf("unknown key %q", input)
	}
	if len(mods) == 1 && mods["shift"] && utf8.RuneCountInString(name) == 1 {
		return strings.ToUpper(name), nil
	}
	var result []string
	for _, mod := range []string{"ctrl", "alt", "shift", "meta", "hyper", "super"} {
		if mods[mod] {
			result = append(result, mod)
		}
	}
	return strings.Join(append(result, name), "+"), nil
}

func collisionKey(k string) string {
	switch k {
	case "ctrl+@", "ctrl+2":
		return "ctrl+space"
	case "ctrl+/", "ctrl+7":
		return "ctrl+_"
	case "ctrl+i":
		return "tab"
	case "ctrl+m":
		return "enter"
	case "ctrl+[":
		return "esc"
	}
	return k
}

func shortcutLabel(keys []string, command string) string {
	return shortcutLabelForOS(keys, command, runtime.GOOS)
}

func shortcutLabelForOS(keys []string, command, platform string) string {
	if len(keys) == 0 {
		if command != "" {
			return "/" + command
		}
		return ""
	}
	chosen := keys[0]
	// macOS prefers a delivered Command alias, always marked conditional.
	// Other terminals prefer a portable key or the discoverable command path.
	commandAlias, portable := "", ""
	for _, k := range keys {
		if strings.Contains(k, "super+") && commandAlias == "" {
			commandAlias = k
		}
		if portable == "" && !strings.Contains(k, "super+") && !(len(k) >= 2 && k[0] == 'f' && k[1] >= '0' && k[1] <= '9') {
			portable = k
		}
	}
	if platform == "darwin" && commandAlias != "" {
		chosen = commandAlias
	} else if portable != "" {
		chosen = portable
	} else if command != "" {
		return "/" + command
	}
	replacer := strings.NewReplacer("ctrl+", "Ctrl+", "alt+", "Alt+", "shift+", "Shift+", "super+", "Super+", "meta+", "Meta+", "enter", "Enter", "space", "Space", "tab", "Tab", "esc", "Esc", "up", "↑", "down", "↓", "left", "←", "right", "→")
	label := replacer.Replace(chosen)
	if platform == "darwin" {
		label = strings.ReplaceAll(label, "Super+", "Cmd+")
	}
	if len(chosen) == 1 && chosen[0] >= 'a' && chosen[0] <= 'z' {
		return chosen
	}
	if i := strings.LastIndex(label, "+"); i >= 0 && len(label[i+1:]) == 1 {
		label = label[:i+1] + strings.ToUpper(label[i+1:])
	}
	if len(chosen) >= 2 && chosen[0] == 'f' && chosen[1] >= '0' && chosen[1] <= '9' {
		label = strings.ToUpper(label)
	}
	if strings.Contains(chosen, "super+") {
		label += "*"
	}
	return label
}

func (m *Model) shortcutContext() string {
	switch {
	case m.detail != "":
		return "detail"
	case m.dialog != nil:
		if m.dialog.kind == client.EnterText || m.dialog.requestForm != nil || m.dialog.launch != nil || m.dialog.settingsForm != nil {
			return "form"
		}
		return "picker"
	case m.searching:
		return "search"
	case m.mode == "agentbox" && m.agentbox.searching:
		return "search"
	case m.mode == "agentbox" && m.agentbox.reply:
		return "composer"
	case m.agentboxOwnsKeys():
		return "agentbox"
	case m.navigator.focused && m.navigationVisible():
		return "navigator"
	case m.activityFocused:
		return "activity"
	case m.contextFocused && m.contextVisible():
		return "context"
	case m.transcriptFocus:
		return "reading"
	default:
		return "composer"
	}
}

func (m *Model) actionBinding(id string) key.Binding {
	return m.actionBindingFor(id, m.shortcutContext())
}

func (m *Model) actionBindingFor(id, context string) key.Binding {
	a, ok := shortcutDefinition(id)
	if !ok {
		return key.NewBinding(key.WithDisabled())
	}
	var config *ShortcutConfig
	if m.settings != nil {
		config = m.settings.shortcuts
	}
	keys := m.resolvedShortcutKeys(a, context, config)
	b := key.NewBinding(key.WithKeys(keys...), key.WithHelp(shortcutLabel(keys, a.command), a.label))
	b.SetEnabled(len(keys) != 0)
	if id == "request-submit" && (m.dialog == nil || m.dialog.requestForm == nil) {
		b.SetEnabled(false)
	}
	if id == "browser.search" || id == "browser.filter" {
		d := m.dialog
		navigation := d != nil && d.kind != client.ChooseMany && (d.surface == client.SurfaceBrowser || d.prompt != nil && d.prompt.Question.Navigation)
		if !navigation || id == "browser.filter" && !(d.resultsFocused || d.list.FilterState() != list.Filtering) {
			b.SetEnabled(false)
		}
	}
	return b
}

func (m *Model) resolvedShortcutKeys(a shortcutAction, context string, config *ShortcutConfig) []string {
	id := a.id
	keys := config.keysFor(a, context)
	if id == "palette" && m.settings != nil && m.settings.legacyEditorCtrlP() && (context == "composer" || context == "form" || context == "picker" || context == "search") {
		keys = slices.DeleteFunc(keys, func(k string) bool { return k == "ctrl+p" })
	}
	if id == "palette" && context == "picker" {
		// Lists own Ctrl+P as previous row; re-opening Commands has Ctrl+Space.
		keys = slices.DeleteFunc(keys, func(k string) bool { return k == "ctrl+p" })
	}
	return keys
}

func (m *Model) shortcutsText() string {
	path := "shortcuts.json beside terminal.json"
	if m.settings != nil && m.settings.shortcuts != nil && m.settings.shortcuts.path != "" {
		path = m.settings.shortcuts.path
	}
	hint := func(id string) string { return m.actionBindingFor(id, "composer").Help().Key }
	parts := []string{"Optional manual file: " + path, "Read at startup. Remove the file to restore defaults. No live reload or settings editor.", "* Command/Super aliases require a terminal that actually forwards the enhanced key. Core actions remain in Commands. Enter adds a newline to the ordinary composer; " + hint("send") + " sends; " + hint("send-literal") + " sends literal text; " + hint("steer") + " steers. Ctrl+S always retains independent Stop."}
	var config *ShortcutConfig
	if m.settings != nil {
		config = m.settings.shortcuts
	}
	for _, context := range shortcutContexts[1:] {
		parts = append(parts, strings.ToUpper(context))
		for _, a := range shortcutActions {
			keys := m.resolvedShortcutKeys(a, context, config)
			if len(keys) > 0 {
				parts = append(parts, a.id+"  "+strings.Join(keys, ", "))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
