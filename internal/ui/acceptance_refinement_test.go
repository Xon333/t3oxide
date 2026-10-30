package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

// These are real Model.Update/View interactions. Native dispatch and exact
// whitespace payloads are covered separately by the actual adapter fixtures.
func TestPacketLaunchActionExactEmptySemanticsAndExplicitActivation(t *testing.T) {
	for _, message := range []string{"", "   ", "\n\n", "  exact\nmessage  "} {
		for _, size := range flowSizes {
			for _, mode := range []string{"developer", "agentbox"} {
				for _, input := range []string{"keyboard", "mouse"} {
					t.Run(fmt.Sprintf("%s/%s/%dx%d/%q", mode, input, size[0], size[1], message), func(t *testing.T) {
						m := testModel(t)
						m.mode, m.mouse = mode, true
						m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						m.composer.SetValue("exact source draft\n  ")
						q := launchQuestion(client.LaunchInput{Title: "same title", Message: message})
						p, reply, _ := intuitivePrompt(t, q)
						m.openPrompt(p)
						m.restoreFocus()
						m.Update(keyPress(tea.KeyTab))
						m.Update(keyPress(tea.KeyEnter))
						label := "Create & send"
						if message == "" {
							label = "Create thread"
						}
						if view := ansi.Strip(m.View().Content); !strings.Contains(view, "["+label+"]") || m.launchInput() != q.LaunchReview.Input {
							t.Fatalf("native emptiness/bytes or focused label changed: %q\n%s", message, view)
						}
						assertIntuitiveNoReply(t, reply)
						m.Update(keyPress(tea.KeyRight))
						m.Update(keyPress(tea.KeyLeft))
						assertIntuitiveNoReply(t, reply)
						if input == "mouse" {
							flowClick(t, m, label)
						} else {
							m.Update(keyPress(tea.KeyEnter))
						}
						flowAwaitReply(t, reply)
						if m.dialog != nil || m.currentDraft() != "exact source draft\n  " {
							t.Fatal("explicit launch activation changed source draft or kept retired form")
						}
					})
				}
			}
		}
	}
}

func TestPacketBusyDeliveryControlStagesInBothModesAndLateAckCannotClearRestaging(t *testing.T) {
	for _, size := range flowSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				if mode == "agentbox" {
					seedAgentBox(m)
					m.agentbox.reply = true
				}
				m.mode, m.mouse = mode, true
				m.status.RunState, m.status.ActiveRunID, m.status.Delivery = "running", "run", "auto"
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.composer.SetValue("exact next draft\n  ")
				m.restoreFocus()
				m.composer.CursorEnd()
				line, col := m.composer.Line(), m.composer.Column()
				view := ansi.Strip(m.View().Content)
				if !strings.Contains(view, "T3 decides") || !strings.Contains(view, "Send") {
					t.Fatalf("busy primary intent missing:\n%s", view)
				}
				flowClick(t, m, "Auto ▾")
				if m.dialog == nil || m.dialog.local != "delivery" || m.actionBusy {
					t.Fatal("delivery control did not stage locally")
				}
				flowClick(t, m, "Queue")
				if m.dialog != nil || m.nextDelivery != "queue" || m.status.Delivery != "auto" || m.actionBusy || m.currentDraft() != "exact next draft\n  " || m.composer.Line() != line || m.composer.Column() != col {
					t.Fatal("staging changed saved intent, exact draft, caret or mutation admission")
				}
				oldSerial := m.deliverySerial
				m.actionID, m.actionName, m.actionBusy = 41, "send", true
				m.stageDelivery("queue") // deliberately stage the same value again
				m.Update(actionMsg{generation: m.generation, id: 41, name: "send", delivery: "queue", deliverySerial: oldSerial})
				if m.nextDelivery != "queue" || m.currentDraft() != "exact next draft\n  " {
					t.Fatal("late acknowledgement cleared newly staged same-value intent or newer text")
				}
				m.actionID, m.actionName, m.actionBusy = 42, "send", true
				m.Update(actionMsg{generation: m.generation, id: 42, name: "send", delivery: "queue", deliverySerial: m.deliverySerial})
				if m.nextDelivery != "" || m.status.Delivery != "auto" {
					t.Fatal("matching acknowledgement did not consume only its one-message intent")
				}
			})
		}
	}
}

func packetStateSession(t *testing.T) (*client.Session, *config.State) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager(nil, state)
	session, err := manager.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	return session, state
}

func TestPacketGlobalRecoveryCueBeforeAttachmentAndAcrossModes(t *testing.T) {
	session, state := packetStateSession(t)
	if err := state.SavePending("removed-source", false, map[string]any{"id": "original-op", "environmentId": "removed-source", "threadId": "original-thread", "op": "send", "message": "retained\r\n\ttext"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"developer", "agentbox"} {
		m := testModel(t)
		m.session, m.mode = session, mode
		m.navigator.loaded, m.navigator.focused = true, true
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"May already be sent", "Review saved action"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%s startup hid global recovery %q:\n%s", mode, want, view)
			}
		}
		if m.commandUnavailable("recover") != "" || len(session.SavedActions()) != 1 {
			t.Fatal("missing original route hid recovery")
		}
		found := false
		for _, hit := range m.hitZones {
			found = found || hit.action == "navigator-recover" || hit.action == "inbox-recover"
		}
		if !found {
			t.Fatal("visible recovery cue is not mouse reachable")
		}
		// Rendering and local inspection never dispatch or clear the record.
		if state.Pending("removed-source", false)["id"] != "original-op" || m.actionBusy {
			t.Fatal("recovery cue changed operation identity or dispatched")
		}
		m.session = nil
	}
}

func TestPacketReviewPositionRestoresOnlyExactSourceAndCompleteDiffHashes(t *testing.T) {
	m := testModel(t)
	m.session, _ = packetStateSession(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	target := client.InboxTarget{ProfileID: "profile", EnvironmentID: "environment", ProjectID: "project", ThreadID: "thread"}
	m.status.ProfileID, m.status.EnvironmentID, m.status.ThreadID, m.status.Location.ProjectID = target.ProfileID, target.EnvironmentID, target.ThreadID, target.ProjectID
	review := wire.ThreadReview{ThreadID: target.ThreadID, ProjectID: target.ProjectID, Workspace: "/synthetic/work", GeneratedAt: "2026-10-30T00:00:00Z", Sources: []wire.ReviewSource{{Kind: "working-tree", Title: "Workspace", Hash: strings.Repeat("a", 64)}}, Diff: strings.Repeat("+ synthetic line\n", 100)}
	open := func(v wire.ThreadReview) {
		m.inboxReviewFinished(inboxReviewMsg{generation: m.generation, serial: m.agentbox.reviewSerial, target: target, review: v, title: "Synthetic review"})
	}
	open(review)
	m.detailViewport.SetYOffset(17)
	_, cmd := m.Update(keyPress(tea.KeyEscape))
	finishCommand(t, m, cmd)
	open(review)
	if m.detailViewport.YOffset() != 17 || !strings.Contains(m.notice, "Restored local reading position") {
		t.Fatal("unchanged native hashes did not restore saved local reading position")
	}
	for _, want := range []string{"Diff observed: 2026-10-30T00:00:00Z", "Sources: 1", "clipped: false", "PR sync: unreported", strings.Repeat("a", 64), "does not establish tests"} {
		if !strings.Contains(m.detail, want) {
			t.Fatalf("review provenance missing %q", want)
		}
	}
	changed := review
	changed.Sources = []wire.ReviewSource{{Kind: "working-tree", Hash: strings.Repeat("b", 64)}}
	open(changed)
	if m.detailViewport.YOffset() != 0 || !strings.Contains(m.notice, "Changes updated") {
		t.Fatal("changed diff retained a stale position")
	}
	changed.DiffTruncated = true
	open(changed)
	if m.detailViewport.YOffset() != 0 || !strings.Contains(m.notice, "clipped") {
		t.Fatal("clipped diff restored position")
	}
	other := target
	other.EnvironmentID = "other-environment"
	if offset, _ := m.session.ReviewPosition(other, review); offset != 0 {
		t.Fatal("same hashes on another source inherited review position")
	}
}

func TestPacketInfoSeparatesExactProviderInstanceAccountAndSelectionProvenance(t *testing.T) {
	for _, instance := range []string{"same-driver/instance-a", "same-driver/instance-b"} {
		m := testModel(t)
		m.status.ProfileID, m.status.EnvironmentID, m.status.ThreadID = "profile", "environment", "thread"
		m.status.ModelName, m.status.NextModel, m.status.Delivery, m.nextDelivery = "Saved model", "Next model", "auto", "queue"
		m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ProviderIdentity: &wire.ProviderIdentity{InstanceID: instance, Label: "same label", Driver: "same-driver"}}}
		detail := m.conversationDetailText()
		for _, want := range []string{"Environment ID: environment", "Provider instance: " + instance, "Account: unreported", "Saved model: Saved model", "Next message: Next model", "Saved delivery: auto", "Next-send delivery: Queue (next)"} {
			if !strings.Contains(detail, want) {
				t.Fatalf("exact identity/selection provenance missing %q", want)
			}
		}
		m.snapshot.Metadata.ProviderIdentity.AccountLabel = "Reported account"
		m.snapshot.Metadata.ProviderIdentity.AccountReportedAt = "2026-10-30T00:00:00Z"
		if detail = m.conversationDetailText(); !strings.Contains(detail, "Account: Reported account") || !strings.Contains(detail, "Account telemetry observed: 2026-10-30T00:00:00Z") {
			t.Fatal("reported account lost explicit provenance")
		}
	}
}

func TestPacketCompactNavigatorKeepsAttachedRequestsStopAndConnectionVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 16}} {
		m := navigatorFixture(t, size[0], size[1])
		m.navigator.focused, m.mouse = true, true
		m.status.Requests, m.status.RunState, m.status.ConnectionState = 2, "failed", "live"
		m.resize()
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"Attached:", "Requests (2)", "Stop", "! Failed", "live"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%v compact navigation concealed %q:\n%s", size, want, view)
			}
		}
		for _, action := range []string{"requests", "stop"} {
			found := false
			for _, hit := range m.hitZones {
				if hit.action == "toolbar" && hit.args == action && hit.y == 4 {
					found = true
				}
			}
			if !found {
				t.Fatalf("compact %s is not reachable at its visible control", action)
			}
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) != size[0] {
				t.Fatalf("compact operational controls overflowed: %q", line)
			}
		}
	}
}

func TestPacketNewDestinationDisplaysEffectiveCLIThenPinWithoutCopyingDefaults(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "alpha", EnvironmentID: "env-alpha"}, {ID: "beta", EnvironmentID: "env-beta"}}, state)
	session, err := manager.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	if err = session.PinCreationDestination("beta", "env-beta", "project-beta"); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.session = session
	m.navigator.focused, m.navigator.loaded = true, true
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	manager.CreationProfile = "alpha"
	if label := m.creationDestinationLabel(); !strings.Contains(label, "alpha (CLI)") || strings.Contains(label, "beta") {
		t.Fatal("destination label disagrees with explicit CLI authority", label)
	}
	manager.CreationProfile = ""
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "New  More  → beta / project-beta") {
		t.Fatal("pin is not visible beside New", view)
	}
	found := false
	for _, hit := range m.hitZones {
		found = found || hit.action == "navigator" && hit.args == "destination" && hit.y == 2
	}
	if !found || !strings.Contains(m.conversationDetailText(), "New environment ID: env-beta") {
		t.Fatal("creation destination lacks exact-source inspection or pointer reachability")
	}
	if err = session.ClearCreationDestination(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.creationDestinationLabel(), "beta") || m.actionBusy {
		t.Fatal("reset retained stale destination or admitted a native mutation")
	}
	m.session = nil
}
