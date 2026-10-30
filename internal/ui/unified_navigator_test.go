package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func unifiedPage(profile, environment, updated string) client.NavigatorPage {
	return client.NavigatorPage{ProfileID: profile, EnvironmentID: environment, Label: profile, Page: client.ThreadPage{ObservedAt: "2026-10-29T00:00:00Z", Limit: 24, Threads: []client.Thread{{ID: "same-id", ProjectID: "same-project", Project: "Project", Title: "Task on " + profile, UpdatedAt: updated, Status: "running", Lineage: &wire.Lineage{}}}}}
}

func TestUnifiedNavigatorExactIdentityRecentOrderAndPartialHostUsability(t *testing.T) {
	m := testModel(t)
	m.navigator.profile = allProfiles
	m.navigator.pending = map[string]bool{"alpha": true, "beta": true, "offline": true}
	a := unifiedPage("alpha", "env-a", "2026-10-29T12:00:00Z")
	b := unifiedPage("beta", "env-b", "2026-10-29T13:00:00Z")
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "alpha", page: a})
	page, row, ok := m.navigatorSource()
	if !ok || !m.navigator.loading || page.ProfileID != "alpha" || row.ID != "same-id" {
		t.Fatal("healthy source unavailable behind pending hosts")
	}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "beta", page: b})
	if m.navigator.list.Items()[0].(navigatorItem).profile != "beta" {
		t.Fatal("unified view did not sort by recent native activity")
	}
	page, _, ok = m.navigatorSource()
	if !ok || page.ProfileID != "alpha" {
		t.Fatal("inserted newer duplicate ID stole selected source")
	}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "offline", err: errors.New("identity mismatch")})
	if !m.navigator.loaded || m.navigator.loading || len(m.navigator.list.Items()) != 2 || !strings.Contains(m.navigatorHealth(), "offline") {
		t.Fatal("partial host state hidden or healthy rows lost")
	}
	m.openNavigatorActions()
	token := navigatorReviewToken(t, m, "details")
	m.navigator.pending = map[string]bool{"beta": true}
	m.dialog = nil
	m.navigatorAction(token)
	if !strings.Contains(m.detail, "Environment ID: env-a") {
		t.Fatal("unrelated host refresh retired healthy reviewed source")
	}
	m.detail = ""
	m.openNavigatorActions()
	token = navigatorReviewToken(t, m, "details")
	m.navigator.pending["alpha"] = true
	m.dialog = nil
	m.navigatorAction(token)
	if m.detail != "" || !strings.Contains(m.notice, "changed") {
		t.Fatal("source refresh failed to retire its old review")
	}
}

func TestUnifiedNavigatorStableProjectHighlightHasNonColourSelection(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.resizeNavigator()
	view := m.navigator.list.View()
	selected := m.navigator.list.SelectedItem().(navigatorItem)
	if !strings.Contains(ansi.Strip(view), projectBubble(selected.thread.Project, selected.kind)) || !strings.Contains(ansi.Strip(view), ">") {
		t.Fatal("selection/source cue relies only on color")
	}
	a := projectAccent("env-a", "project-a", "violet")
	m.navigator.list.Select(2)
	m.rebuildUnifiedNavigator()
	if a != projectAccent("env-a", "project-a", "violet") {
		t.Fatal("list mutation changed project accent")
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatal("real renderer omitted highlighting")
	}
}

func TestCompactNavigatorReturnReflowsTheActualConversation(t *testing.T) {
	m := navigatorFixture(t, 60, 16)
	draft, snapshot := m.currentDraft(), m.snapshot
	width := m.contentWidth
	finishCommand(t, m, m.focusNavigator())
	if !m.navigator.focused || m.navigatorWidth() != 60 {
		t.Fatal("compact navigator did not take the available screen")
	}
	_, command := m.Update(keyPress('o', tea.ModCtrl))
	finishCommand(t, m, command)
	view := ansi.Strip(m.View().Content)
	if m.navigator.focused || m.contentWidth != width || !strings.Contains(view, "Terminal redesign") || !strings.Contains(view, "Message") {
		t.Fatalf("return retained a hidden/narrow conversation allocation: width=%d expected=%d\n%s", m.contentWidth, width, view)
	}
	if m.currentDraft() != draft || m.snapshot != snapshot {
		t.Fatal("layout focus change replaced the source work")
	}
}

func TestActualActivityStripCellsMatchMouseGeometry(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {170, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := navigatorFixture(t, size[0], size[1])
			m.snapshot.Metadata.Activity.Items = []wire.ActivityItem{{Key: "design/tool-exact", SourceThreadID: "design", ItemID: "tool-exact", Kind: "command_execution", Title: "Command", Summary: "go test ./...", Status: "running"}}
			m.composer.SetValue("  exact source draft\nsecond line  ")
			m.resize()
			plain := ansi.Strip(m.View().Content)
			found := false
			for y, line := range strings.Split(plain, "\n") {
				if index := strings.Index(line, "> go"); index >= 0 {
					x := ansi.StringWidth(line[:index])
					var hit *hitZone
					for i := len(m.hitZones) - 1; i >= 0; i-- {
						if m.hitZones[i].contains(x, y) {
							hit = &m.hitZones[i]
							break
						}
					}
					if hit == nil || hit.action != "activity" || hit.args != "design/tool-exact" {
						t.Fatalf("visible source activity at %d,%d has wrong hit: %+v", x, y, hit)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("activity strip missing from actual View")
			}
		})
	}
}

func TestUnifiedStartupActualViewsHaveEmptyRightPaneAndNoHiddenEditorHits(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "alpha", EnvironmentID: "env-a", Enabled: true}}, state)
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	session, err := manager.NewSession("alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {180, 45}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := NewModel(context.Background(), session, nil)
			defer m.Close()
			m.width, m.height = size[0], size[1]
			m.navigator.profile = allProfiles
			m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, page: unifiedPage("alpha", "env-a", "2026-10-29T12:00:00Z")})
			m.resize()
			view := m.View().Content
			plain := ansi.Strip(view)
			lines := strings.Split(plain, "\n")
			if len(lines) != size[1] {
				t.Fatalf("height=%d", len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != size[0] {
					t.Fatalf("width mismatch %q", line)
				}
			}
			if !strings.Contains(plain, "Task on alpha") || strings.Contains(plain, "Message") || strings.Contains(plain, "No conversation open") {
				t.Fatalf("startup clutter or missing metadata: %s", plain)
			}
			for _, hit := range m.hitZones {
				if hit.action == "composer" || hit.action == "transcript" || hit.action == "toolbar" {
					t.Fatalf("invisible %s is clickable", hit.action)
				}
			}
			m.Update(keyPress(tea.KeyTab))
			if !m.navigator.focused || m.composer.Focused() {
				t.Fatal("startup focused an invisible editor")
			}
			if dir := os.Getenv("T3OXIDE_UI_PREVIEW_DIR"); dir != "" {
				name := filepath.Join(dir, fmt.Sprintf("startup-%dx%d", size[0], size[1]))
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

func TestWorkspaceReturnRestoresExactEditorAndAnchorWithoutTranscriptCache(t *testing.T) {
	m, state := draftSessionModel(t)
	first := m.session
	exact := "first line\nsecond 界"
	if err := m.storeDraft(exact); err != nil {
		t.Fatal(err)
	}
	m.Update(keyPress(tea.KeyLeft, tea.ModShift))
	line, column, selected := m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
	m.transcriptRows = []renderedRow{{key: "thread/first", start: 0, lines: 20}, {key: "thread/second", start: 20, lines: 30}}
	m.viewport.SetContent(strings.Repeat("row\n", 80))
	m.viewport.SetYOffset(23)
	m.follow = false
	m.transcriptFocus = true
	m.rememberWorkspace()
	if len(m.workspaceReturns) != 1 {
		t.Fatal("source identity not cached")
	}
	m.restoreDraft("other thread")
	m.composer.CursorStart()
	m.recallWorkspace(first)
	if m.currentDraft() != exact || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selected || !m.transcriptFocus {
		t.Fatal("same source did not recover editor selection/focus")
	}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: first.Status(), Draft: &exact}})
	if m.composer.Column() != column || m.composer.SelectedText() != selected {
		t.Fatal("unchanged draft event reset restored selection")
	}
	m.viewport.SetContent(strings.Repeat("new row\n", 100))
	if !m.restoreWorkspaceReading([]renderedRow{{key: "thread/first", start: 0, lines: 25}, {key: "thread/second", start: 25, lines: 30}}) || m.viewport.YOffset() != 28 || m.follow {
		t.Fatal("native row anchor not restored after reflow")
	}
	if err := state.SetDraft("env", "thread", "changed by another client"); err != nil {
		t.Fatal(err)
	}
	// The session's canonical draft remains the owner. A changed canonical
	// value must not be replaced by the cached editor.
	if err := first.SetDraft("new canonical"); err != nil {
		t.Fatal(err)
	}
	m.restoreDraft("new canonical")
	m.recallWorkspace(first)
	if m.currentDraft() != "new canonical" {
		t.Fatal("cached editor overwrote newer canonical draft")
	}
}

func TestActivityDetailTabReturnsLocallyAndCannotSubmit(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	draft := m.currentDraft()
	m.viewport.SetContent(strings.Repeat("row\n", 70))
	m.viewport.SetYOffset(8)
	m.follow = false
	m.openDetail("Activity", "exact source tool result")
	m.detailIsActivity = true
	m.detailGeneration = m.generation
	m.handleKey(keyPress(tea.KeyTab))
	if m.detail != "" || !m.activityFocused || m.currentDraft() != draft || m.viewport.YOffset() != 8 || m.actionBusy {
		t.Fatal("Tab left the local tool return path")
	}
	m.openDetail("Info", "state")
	m.handleKey(keyPress(tea.KeyTab))
	if m.detail == "" || m.currentDraft() != draft || m.actionBusy {
		t.Fatal("Tab closed or submitted an unrelated detail")
	}
}

func TestWorkspaceReturnKeepsActivityFocusUntilAuthoritativePageArrives(t *testing.T) {
	m, _ := draftSessionModel(t)
	m.status.Activity = true
	activity := &wire.ActivityPage{Items: []wire.ActivityItem{{Key: "thread/tool", ItemID: "tool", SourceThreadID: "thread", Title: "Result", Status: "completed"}}}
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", Activity: activity}}
	m.activityFocused, m.activityFocusKey = true, "thread/tool"
	m.rememberWorkspace()
	m.activityFocused, m.activityFocusKey, m.snapshot = false, "", nil
	m.recallWorkspace(m.session)
	m.restoreFocus()
	if !m.activityFocused || m.activityFocusKey != "thread/tool" || m.composer.Focused() {
		t.Fatal("return lost source activity focus before the native page arrived")
	}
	status := m.status
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: status, Snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", Activity: activity}}}})
	if !m.activityFocused || m.composer.Focused() {
		t.Fatal("matching native page failed to retain reviewed activity focus")
	}
	m.restoreWorkspaceReading(nil)
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Status: status, Snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", Activity: &wire.ActivityPage{}}}}})
	if m.activityFocused || !m.transcriptFocus {
		t.Fatal("authoritative removal kept a vanished activity focus")
	}
}
