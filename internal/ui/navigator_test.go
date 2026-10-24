package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func navigatorFixture(t *testing.T, width, height int) *Model {
	t.Helper()
	m := testModel(t)
	m.width, m.height = width, height
	m.status = client.Status{ProfileID: "build-host", EnvironmentID: "build-id", ThreadID: "design", ProfileIDs: []string{"build-host", "linux-host"}, ModelName: "Provider model · high", RunState: "running", ConnectionState: "connected", Connected: true, Writable: true, Activity: true, ActiveRunID: "run-1", Location: client.Location{Environment: "Build host", Title: "Terminal redesign", Workspace: "/work/t3oxide", ThreadID: "design"}}
	parent, sub := "design", "subagent"
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, page: client.NavigatorPage{ProfileID: "linux-host", Label: "Linux host", Tree: true, Navigation: client.NavigationState{Filter: "all", Lifecycle: "active", Expanded: map[string]bool{}}, Page: client.ThreadPage{ObservedAt: "2026-10-24T12:00:00Z", More: true, Limit: 24, NextCursor: new(24), Threads: []client.Thread{
		{ID: "fleet", Title: "Runtime ownership", Status: "waiting", Project: "Fleet", ChildCount: 2, Lineage: &wire.Lineage{}},
		{ID: "design", Title: "Terminal redesign", Status: "running", Project: "Terminal", Lineage: &wire.Lineage{}},
		{ID: "child", Title: "Protocol review", Status: "running", Depth: 1, Lineage: &wire.Lineage{RelationshipToParent: &sub, ParentThreadID: &parent}},
		{ID: "docs", Title: "Documentation", Status: "idle", Project: "Fleet", Lineage: &wire.Lineage{}},
	}}}})
	m.snapshot = &wire.Snapshot{Revision: 1, Metadata: wire.Metadata{ThreadID: "design", Title: "Terminal redesign", Live: true, HasMore: true, Activity: &wire.ActivityPage{Scope: "page", Items: []wire.ActivityItem{{Key: "design/tool-1", ItemID: "tool-1", SourceThreadID: "design", Kind: "command_execution", Title: "go test ./...", Status: "completed"}, {Key: "design/tool-2", ItemID: "tool-2", SourceThreadID: "design", Kind: "file_change", Title: "Update navigation", Status: "running"}}, Total: 2}}, Items: []wire.Row{
		{Key: "design/human", Kind: "user_message", Text: "Keep the attached conversation visible while I browse another environment."},
		{Key: "design/agent", Kind: "assistant_message", Text: "## A quieter workspace\n\nThe conversation stays attached on **Build host**. The sidebar is a bounded snapshot of **Linux host**.\n\n- Open a conversation deliberately\n- Expand children only when needed\n- Keep drafts and the current reading position\n\nThe existing T3 server still owns execution.", Streaming: true},
	}}
	m.composer.SetValue("Keep the source identity visible.")
	m.resize()
	finishCommand(t, m, m.requestRender(true, true))
	return m
}

func TestNavigatorWorkspaceResponsiveActualViews(t *testing.T) {
	for _, size := range []struct{ w, h int }{{60, 16}, {80, 24}, {120, 35}, {170, 40}} {
		t.Run(fmtName(size.w, size.h), func(t *testing.T) {
			m := navigatorFixture(t, size.w, size.h)
			if size.w >= 170 {
				m.sidebar = true
				m.resize()
				finishCommand(t, m, m.requestRender(false, false))
			}
			view := m.View().Content
			plain := ansi.Strip(view)
			rows := strings.Split(plain, "\n")
			if len(rows) != size.h {
				t.Fatalf("height %d != %d", len(rows), size.h)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != size.w {
					t.Fatalf("width %d != %d: %q", ansi.StringWidth(row), size.w, row)
				}
			}
			for _, want := range []string{"Build host", "Terminal redesign", "Actions", "Settings", "Stop"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("missing %q\n%s", want, plain)
				}
			}
			if size.w >= 104 && !strings.Contains(plain, "Linux host") {
				t.Fatal("bounded navigation missing")
			}
			if size.w < 104 && strings.Contains(plain, "Runtime ownership") {
				t.Fatal("narrow layout squeezed in sidebar")
			}
			if size.w >= 170 && !strings.Contains(plain, "go test ./...") {
				t.Fatal("context omits real activity")
			}
			if dir := os.Getenv("T3OXIDE_UI_PREVIEW_DIR"); dir != "" {
				name := filepath.Join(dir, fmtName(size.w, size.h))
				if err := os.WriteFile(name+".txt", []byte(plain+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name+".ansi", []byte(view+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func fmtName(width, height int) string { return fmt.Sprintf("workspace-%dx%d", width, height) }

func TestNavigatorFocusFilteringAndClicksKeepAttachedDraftAndReading(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.transcript = strings.Repeat("reading anchor\n", 60)
	m.viewport.SetContent(m.transcript)
	m.viewport.SetYOffset(9)
	m.follow = false
	draft, snapshot, offset := m.currentDraft(), m.snapshot, m.viewport.YOffset()
	finishCommand(t, m, m.focusNavigator())
	m.Update(keyPress('d'))
	m.Update(keyPress('o'))
	if m.navigator.list.FilterInput.Value() != "do" || len(m.navigator.list.VisibleItems()) != 1 {
		t.Fatal("native synchronous navigator filter failed")
	}
	m.Update(keyPress(tea.KeyTab))
	if m.navigator.focused || !m.composer.Focused() {
		t.Fatal("Tab did not restore composer")
	}
	if m.currentDraft() != draft || m.snapshot != snapshot || m.viewport.YOffset() != offset || m.follow {
		t.Fatal("metadata focus changed the attachment, draft or anchor")
	}
	m.View()
	_, cmd := m.Update(tea.MouseClickMsg{X: m.mainOriginX() + 3, Y: m.transcriptTop() + 3, Button: tea.MouseLeft})
	finishCommand(t, m, cmd)
	if !m.transcriptFocus || m.composer.Focused() {
		t.Fatal("transcript click failed to claim keyboard")
	}
	m.Update(keyPress(tea.KeyTab))
	if !m.activityFocused {
		t.Fatal("focus cycle omitted visible activity")
	}
	m.Update(keyPress(tea.KeyTab))
	if !m.navigator.focused {
		t.Fatal("focus cycle omitted visible navigation")
	}
}

func TestNavigatorRejectsStaleLoadsAndMouseGeometry(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	old := m.navigator.page
	m.navigatorLoaded(navigatorMsg{generation: m.generation - 1, serial: m.navigator.serial, page: client.NavigatorPage{ProfileID: "wrong"}})
	if m.navigator.page.ProfileID != old.ProfileID {
		t.Fatal("retired attachment replaced metadata")
	}
	m.View()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if m.dialog != nil || m.actionBusy {
		t.Fatal("resize reused an old sidebar button")
	}
	m.View()
	if m.navigationVisible() {
		t.Fatal("compact layout retained hidden sidebar")
	}
}

func TestNavigatorMouseDialogChoosesExactOfferedIdentity(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	p, replies, _ := intuitivePrompt(t, client.Question{Kind: client.ChooseOne, Title: "Exact offered approval", Choices: []client.Choice{{ID: "allow-one", Label: "Allow once"}, {ID: "deny-this", Label: "Deny"}}})
	m.openPrompt(p)
	d := m.dialog
	m.View()
	var selected hitZone
	for _, z := range m.hitZones {
		if z.action == "dialog-row" && z.args == "deny-this" {
			selected = z
		}
	}
	if selected.width == 0 {
		t.Fatal("offered row has no drawn hit target")
	}
	m.Update(tea.MouseClickMsg{X: selected.x, Y: selected.y, Button: tea.MouseLeft})
	if d.list.SelectedItem().(choiceItem).id != "deny-this" {
		t.Fatal("click selected another offered identity")
	}
	select {
	case err := <-replies:
		if err == nil || !strings.Contains(err.Error(), "environment was not offered") {
			t.Fatalf("unexpected broker result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exact choice was not activated")
	}
}

func TestNavigatorSnapshotsDoNotInventChildrenOrActivity(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.resizeNavigator()
	before := m.navigator.list.Items()[0].(navigatorItem).thread
	m.status.ProfileID = "linux-host"
	m.status.ThreadID = "fleet"
	m.status.RunState = "completed"
	m.reconcileNavigator()
	after := m.navigator.list.Items()[0].(navigatorItem).thread
	if before.ChildCount != after.ChildCount || after.Status != "completed" || len(m.navigator.list.Items()) != 4 {
		t.Fatal("attached status reconciliation changed metadata scope")
	}
	if m.navigator.list.Items()[1].(navigatorItem).thread.Status != "running" {
		t.Fatal("invented another thread's freshness")
	}
	parent, sub := "root", "subagent"
	row := navigatorItem{thread: client.Thread{ID: "native", Title: "Native child", Status: "running", Lineage: &wire.Lineage{RelationshipToParent: &sub, ParentThreadID: &parent}}}
	l := list.New([]list.Item{row}, navigatorDelegate{theme: "violet"}, 32, 3)
	if !strings.Contains(ansi.Strip(l.View()), "↳") {
		t.Fatal("depth-zero subagent looked like a main thread")
	}
}

func TestNavigationPreferenceIsAdditiveAndPreservesAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.json")
	if err := os.WriteFile(path, []byte(`{"theme":"cyan","mouse":false,"sidebar":true,"environmentThemes":{"one":"lime"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.navigationHidden() {
		t.Fatal("older settings hid new navigation by default")
	}
	if err = s.SetNavigationHidden(true); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.navigationHidden() || loaded.Theme != "cyan" || loaded.Mouse || !loaded.Sidebar || loaded.ThemeFor("one", []string{"one"}) != "lime" {
		t.Fatal("navigation preference reset unrelated appearance")
	}
}

func TestNavigatorInitialLoadWaitsForAttachedHandshake(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	manager := client.NewManager([]config.Environment{{ID: "host", Label: "Host", EnvironmentID: "native"}}, state)
	session, err := manager.NewSession("host", "thread")
	if err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.session = session
	m.width = 120
	m.height = 35
	m.status = client.Status{ThreadID: "thread", ProfileID: "host", Connected: false}
	if m.loadNavigator("", "", false) != nil || m.navigator.loading {
		t.Fatal("connecting attachment spawned a temporary metadata helper")
	}
}

func TestVisibleActivityStripHasExactTargetAndCannotSendOnReplacement(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "Files · running  Update navigation") {
		t.Fatal("ordinary workspace hides source-owned work")
	}
	item := m.recentActivity()
	if item == nil || item.Key != "design/tool-2" {
		t.Fatal("strip did not prefer current work")
	}
	var hit hitZone
	for _, z := range m.hitZones {
		if z.action == "activity" && z.args == item.Key {
			hit = z
		}
	}
	if hit.width == 0 {
		t.Fatal("strip omitted exact source hit target")
	}
	draft := m.currentDraft()
	finishCommand(t, m, m.focusActivity())
	m.snapshot.Metadata.Activity.Items = nil
	m.Update(keyPress(tea.KeyEnter))
	if m.currentDraft() != draft || m.actionBusy || !strings.Contains(m.notice, "Activity changed") {
		t.Fatal("replaced tool target caused a send or implicit retarget")
	}
	if m.activityStripRows() != 0 {
		t.Fatal("empty activity reserved a decorative row")
	}
}

func TestActivityStripUsesActualCommandSummary(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.snapshot.Metadata.Activity.Items = []wire.ActivityItem{{Key: "design/command", SourceThreadID: "design", ItemID: "command", Kind: "command_execution", Title: "Command", Summary: "go test ./...\nnot another label", Status: "running"}}
	strip := ansi.Strip(m.activityStripView(80))
	if strip != "Command · running  go test ./..." {
		t.Fatalf("tool title concealed actual work: %q", strip)
	}
}

func TestContextResizeReleasesHiddenFocus(t *testing.T) {
	m := navigatorFixture(t, 170, 40)
	m.sidebar = true
	m.contextFocused = true
	m.resize()
	m.restoreFocus()
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	finishCommand(t, m, cmd)
	if m.contextFocused || m.navigationVisible() || !m.transcriptFocus {
		t.Fatal("narrow layout trapped keyboard in a hidden pane")
	}
	m.Update(keyPress(tea.KeyTab))
	if !m.activityFocused {
		t.Fatal("reading focus lost visible tool inspection")
	}
}
