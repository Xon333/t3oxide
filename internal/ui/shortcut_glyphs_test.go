package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"runtime"
	"strings"
	"testing"
)

func shortcutHintText(text string) string {
	if runtime.GOOS != "darwin" {
		return text
	}
	return strings.NewReplacer("Cmd+", "⌘", "Ctrl+", "⌃", "Alt+", "⌥", "Shift+", "⇧").Replace(text)
}

func TestMacModifierGlyphsAndPortableHelp(t *testing.T) {
	for _, tc := range []struct {
		keys []string
		want string
	}{
		{[]string{"super+i"}, "⌘I"}, {[]string{"alt+h", "ctrl+alt+h"}, "⌥H"},
		{[]string{"ctrl+alt+r"}, "⌃⌥R"}, {[]string{"shift+super+enter"}, "⇧⌘Enter"},
		{[]string{"ctrl+space"}, "⌃Space"},
	} {
		if got := shortcutLabelForOS(tc.keys, "", "darwin"); got != tc.want {
			t.Fatalf("%v: %q != %q", tc.keys, got, tc.want)
		}
	}
	m := testModel(t)
	if !key.Matches(keyPress('h', tea.ModAlt), m.actionBinding("help")) || key.Matches(keyPress('h', tea.ModSuper), m.actionBinding("help")) {
		t.Fatal("Help does not use Option-H or consumes OS Hide")
	}
	m.Update(keyPress('h', tea.ModAlt))
	if m.detailTitle != "Keyboard and commands" {
		t.Fatal("Option-H failed to dispatch Help")
	}
}

func TestNavigatorFooterUsesRegistryAndNoOrderProse(t *testing.T) {
	m := refinementNavigation(t, 20)
	m.navigator.page.Page.More = true
	m.navigator.orderDeferred = true
	view := ansi.Strip(m.navigatorView(40, 24))
	for _, old := range []string{"More available", "Order held", "Activity", "Cmd+H"} {
		if strings.Contains(view, old) {
			t.Fatalf("obsolete footer %q", old)
		}
	}
	for _, want := range []string{"↓", "ⓘ", m.actionBindingFor("refresh", "navigator").Help().Key, "(?)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("footer exceeds rail: %q", line)
		}
	}
	m.navigator.page.Page.More = false
	m.navigator.focused = true
	m.navigator.list.Select(len(m.navigator.list.VisibleItems()) - 1)
	if strings.Contains(ansi.Strip(m.navigatorView(40, 24)), "↓") {
		t.Fatal("continuation cue remained at the final loaded row")
	}

}

func TestNavigationHintsRespectManualBindings(t *testing.T) {
	m := refinementNavigation(t, 2)
	m.settings.shortcuts = &ShortcutConfig{Version: 1, Bindings: map[string]map[string][]string{
		"global":    {"help": {"alt+k"}, "info": {"alt+i"}},
		"navigator": {"refresh": {"alt+t"}},
	}}
	view := ansi.Strip(m.navigatorView(40, 24))
	for _, want := range []string{shortcutLabel([]string{"alt+k"}, "help"), shortcutLabel([]string{"alt+i"}, "info"), shortcutLabel([]string{"alt+t"}, "refresh")} {
		if !strings.Contains(view, want) {
			t.Fatalf("manual hint missing %q: %s", want, view)
		}
	}
	if !key.Matches(keyPress('k', tea.ModAlt), m.actionBinding("help")) || key.Matches(keyPress('h', tea.ModAlt), m.actionBinding("help")) {
		t.Fatal("manual Help override did not own dispatch")
	}
}

func TestNewDefaultsAvoidTerminalWindowAndKeepManualOverride(t *testing.T) {
	m := testModel(t)
	for _, context := range []string{"composer", "navigator"} {
		binding := m.actionBindingFor("new", context)
		keys := []string{"ctrl+n"}
		if context == "composer" {
			keys = []string{"ctrl+alt+n"}
		}
		if binding.Help().Key != shortcutLabel(keys, "new") || key.Matches(keyPress('n', tea.ModSuper), binding) {
			t.Fatalf("%s advertises terminal New Window: %v", context, binding.Help())
		}
	}
	m.settings.shortcuts = &ShortcutConfig{Version: 1, Bindings: map[string]map[string][]string{"composer": {"new": {"super+n"}}}}
	if !key.Matches(keyPress('n', tea.ModSuper), m.actionBindingFor("new", "composer")) {
		t.Fatal("explicit manual Command-N was discarded")
	}
}
