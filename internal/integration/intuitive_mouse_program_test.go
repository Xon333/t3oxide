package integration_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/charmbracelet/x/ansi"
)

var intuitiveMouseSizes = [][2]int{{60, 16}, {80, 24}, {120, 35}, {170, 40}}

// These coordinates come from the complete View captured by the real program,
// not the product's hit-test helpers. SGR input also exercises the terminal
// decoder. The local HTTP/WebSocket fixture records actual Node dispatches.
func clickIntuitiveControl(t *testing.T, r *tuiProgram, label string) {
	t.Helper()
	r.waitText(label)
	x, y, best := 0, 0, -1
	// Prompt prose can name Cancel/Submit before the actual final controls.
	lastAction := label == "Cancel" || label == "Back" || label == "Next" || strings.HasPrefix(label, "Submit")
	for row, line := range strings.Split(r.views.text(), "\n") {
		if strings.Contains(line, "Filter: ") {
			continue
		}
		if i := strings.Index(line, label); i >= 0 {
			// A modal's offered row wins over the same project/model name
			// in the still-visible AgentBox summary behind it. Its selected
			// description is explanatory text, not a second offered row.
			rank := 0
			if strings.Contains(line[:i], "│ ") && !strings.Contains(line[:i], "│ → ") {
				rank = 1
			}
			if rank > best || lastAction && rank == best {
				best = rank
				x, y = ansi.StringWidth(line[:i]), row
			}
		}
	}
	if best >= 0 {
		r.send(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
		return
	}
	t.Fatalf("no visible control %q:\n%s", label, r.views.text())
}

func sizeIntuitiveProgram(t *testing.T, r *tuiProgram, size [2]int, mode string) {
	t.Helper()
	r.program.Send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	waitWorkspaceFrame(t, r, func(view string) bool {
		lines := strings.Split(view, "\n")
		return len(lines) == size[1] && ansi.StringWidth(lines[0]) == size[0]
	}, fmt.Sprintf("%dx%d layout", size[0], size[1]))
	if mode == "agentbox" {
		r.send("\x1b[98;7u")
		r.waitText("AgentBox")
		r.send("v")
		r.modalReady("╭─ Inbox")
		r.send("All\r")
		waitWorkspaceFrame(t, r, func(view string) bool {
			return strings.Contains(view, "AgentBox") && strings.Contains(view, "Alpha") && !strings.Contains(view, "Loading…")
		}, "ready AgentBox source")
	}
}

func intuitiveCommand(r *tuiProgram, title string) {
	r.send("\x10")
	r.modalReady("Commands")
	r.send(title + "\r")
}

func assertIntuitiveCounts(t *testing.T, f *fixture, count int) {
	t.Helper()
	if effects, dispatches := f.counts(); effects != count || dispatches != count {
		t.Fatalf("expected %d native effects/dispatches, got %d/%d", count, effects, dispatches)
	}
}

func TestIntuitiveMouseProgramLaunchFieldsChoosersCancelAndOneCreate(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				f := newFixture(t)
				manager, state := newAppManager(t, f)
				const sourceDraft = "  untouched source\nsecond line  "
				if err := state.SetDraft("env-a", "thread-a", sourceDraft); err != nil {
					t.Fatal(err)
				}
				a, err := manager.NewSession(f.profile.ID, "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				source := clone(f.threads["thread-a"]["thread"])
				f.mu.Unlock()
				r, _ := startAppTUI(t, a)
				r.waitText("Initial Markdown for Alpha")
				sizeIntuitiveProgram(t, r, size, mode)
				intuitiveCommand(r, "New conversation")
				r.modalReady("Project")
				clickIntuitiveControl(t, r, "Fixture project")
				r.waitText("First message")
				clickIntuitiveControl(t, r, "Title  ")
				const title = "Mouse launch"
				r.send("\x01\x0b" + title + "\r")
				r.waitText("[Create]")
				r.send("\x1b[C")
				r.waitText("[Cancel]")
				r.send("\x1b[A")
				r.waitText("[Create]")
				assertIntuitiveCounts(t, f, 0)
				clickIntuitiveControl(t, r, "First message")
				r.waitText("Enter newline")
				const message = "  exact mouse message\nsecond line  "
				r.send("\x1b[200~" + message + "\x1b[201~")
				r.waitText("second line")
				for _, choice := range []struct{ action, heading, value string }{
					{"Model", "Model · New conversation", "Other Model"},
					{"Effort", "Reasoning · New conversation", "High"},
					{"Tier", "Service tier · New conversation", "Fast"},
					{"Workspace", "╭─ Workspace", "Shared project root"},
				} {
					clickIntuitiveControl(t, r, choice.action)
					r.modalReady(choice.heading)
					clickIntuitiveControl(t, r, choice.value)
					r.waitText("First message")
					assertIntuitiveCounts(t, f, 0)
				}
				clickIntuitiveControl(t, r, "Cancel")
				waitWorkspaceFrame(t, r, func(view string) bool {
					return !strings.Contains(view, "First message") && !strings.Contains(view, "╭─ Workspace") && strings.Contains(view, "Alpha")
				}, "source after launch cancellation")
				if a.Status().ThreadID != "thread-a" || a.Draft() != sourceDraft {
					t.Fatal("Cancel changed the source identity or exact draft")
				}
				intuitiveCommand(r, "New conversation")
				r.modalReady("Project")
				clickIntuitiveControl(t, r, "Fixture project")
				r.waitText("First message")
				r.waitText(title)
				r.waitText("second line")
				assertIntuitiveCounts(t, f, 0)
				clickIntuitiveControl(t, r, "Create")
				// Create returns a new Session to the UI; the old source Session
				// is intentionally closed. Observe the actual replacement view.
				waitWorkspaceFrame(t, r, func(view string) bool {
					return strings.Contains(view, title) && !strings.Contains(view, "First message") && state.Pending("env-a", false) == nil
				}, "acknowledged newly created source")
				f.mu.Lock()
				var launch object
				for _, command := range f.commands {
					if command["initialMessage"] != nil {
						launch = clone(command)
					}
				}
				current := clone(f.threads["thread-a"]["thread"])
				f.mu.Unlock()
				selection := object{"instanceId": "fixture-provider", "model": "fixture-other", "options": []any{object{"id": "reasoningEffort", "value": "high"}, object{"id": "serviceTier", "value": "fast"}}}
				if launch == nil || launch["title"] != title || launch["initialMessage"].(object)["text"] != message || launch["projectId"] != "project-a" || !reflect.DeepEqual(launch["workspaceStrategy"], object{"type": "root"}) || !reflect.DeepEqual(launch["modelSelection"], selection) {
					t.Fatalf("mouse Create changed reviewed payload: %+v", launch)
				}
				if !reflect.DeepEqual(source, current) || state.Draft("env-a", "thread-a") != sourceDraft || state.Draft("env-a", "\x00new/project-launch/project-a") != "" || state.Pending("env-a", false) != nil {
					t.Fatal("launch changed source or failed to settle only its own saved input")
				}
				assertIntuitiveCounts(t, f, 1)
				quitIntuitiveProgram(t, r)
			})
		}
	}
}

func TestIntuitiveMouseProgramSettingsStagesThenAppliesExactSelection(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				f := newFixture(t)
				manager, state := newAppManager(t, f)
				const draft = "exact settings source\nretain spaces  "
				if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
					t.Fatal(err)
				}
				a, err := manager.NewSession(f.profile.ID, "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				r, _ := startAppTUI(t, a)
				r.waitText("Initial Markdown for Alpha")
				sizeIntuitiveProgram(t, r, size, mode)
				intuitiveCommand(r, "Settings")
				r.waitText("Settings · Fixture / Alpha")
				for _, choice := range []struct{ label, heading, value string }{
					{"Model  ", "Model · staged choice", "Other Model"},
					{"Reasoning  ", "Reasoning · staged choice", "High"},
					{"Service tier  ", "Service tier · staged choice", "Fast"},
				} {
					clickIntuitiveControl(t, r, choice.label)
					r.waitText(choice.heading)
					clickIntuitiveControl(t, r, choice.value)
					r.waitText("Settings · Fixture / Alpha")
					assertIntuitiveCounts(t, f, 0)
				}
				clickIntuitiveControl(t, r, "Apply changes")
				id := waitCommandType(t, f, "thread.model-selection.set")
				awaitStatus(t, a, func(s client.Status) bool { return !s.PendingAction && strings.Contains(s.ModelName, "fixture-other") })
				f.mu.Lock()
				payload := clone(f.commands[id])
				f.mu.Unlock()
				selection := object{"instanceId": "fixture-provider", "model": "fixture-other", "options": []any{object{"id": "reasoningEffort", "value": "high"}, object{"id": "serviceTier", "value": "fast"}}}
				if payload["threadId"] != "thread-a" || !reflect.DeepEqual(payload["modelSelection"], selection) || a.Draft() != draft || state.Draft("env-a", "thread-a") != draft {
					t.Fatalf("Apply changed exact reviewed selection/source/draft: %+v", payload)
				}
				assertIntuitiveCounts(t, f, 1)
				quitIntuitiveProgram(t, r)
			})
		}
	}
}

func TestIntuitiveMouseProgramRequestsStageBackCancelAndExactSubmit(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				f := newFixture(t)
				f.addRequests()
				f.mu.Lock()
				const approvalWarning = "Grants permission for this command only."
				f.threads["thread-a"]["turnItems"].([]any)[0].(object)["options"].([]any)[0].(object)["warning"] = approvalWarning
				f.threads["thread-a"]["turnItems"].([]any)[1].(object)["questions"] = []any{
					object{"id": "choice", "header": "Exact choices", "required": true, "multiSelect": true, "allowCustomAnswer": true, "options": []any{object{"label": "First option", "value": "opaque/first"}, object{"label": "Second option", "value": "opaque/second"}}},
					object{"id": "notes", "header": "Exact notes", "required": false},
				}
				f.mu.Unlock()
				manager, state := newAppManager(t, f)
				const draft = "formal answers do not send this\nexact source  "
				if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
					t.Fatal(err)
				}
				a, err := manager.NewSession(f.profile.ID, "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				r, _ := startAppTUI(t, a)
				r.waitText("Initial Markdown for Alpha")
				awaitStatus(t, a, func(s client.Status) bool { return s.Requests == 2 })
				sizeIntuitiveProgram(t, r, size, mode)
				if mode == "agentbox" {
					clickIntuitiveControl(t, r, "Alpha")
					r.waitText("1 Requests")
					clickIntuitiveControl(t, r, "1 Requests")
				} else {
					r.send("\x12")
				}
				r.modalReady("Pending T3 requests")
				clickIntuitiveControl(t, r, "Approve the fixture?")
				r.waitText("T3 approval")
				r.waitText(approvalWarning)
				clickIntuitiveControl(t, r, "Once")
				r.waitText("✓ Once")
				r.send("\x1d") // Ordinary Send is never request-submit.
				assertIntuitiveCounts(t, f, 0)
				clickIntuitiveControl(t, r, "Cancel")
				waitWorkspaceFrame(t, r, func(view string) bool { return !strings.Contains(view, "T3 approval") }, "cancelled approval")
				assertIntuitiveCounts(t, f, 0)
				intuitiveCommand(r, "Requests")
				r.modalReady("Pending T3 requests")
				clickIntuitiveControl(t, r, "Approve the fixture?")
				r.waitText("T3 approval")
				clickIntuitiveControl(t, r, "Decline")
				r.waitText("✓ Decline")
				clickIntuitiveControl(t, r, "Submit decision")
				approval := f.waitRequest(t, "approval-a")
				if approval["threadId"] != "thread-a" || approval["decision"] != "decline" {
					t.Fatalf("approval lost exact request/decision: %+v", approval)
				}
				awaitStatus(t, a, func(s client.Status) bool { return s.Requests == 1 && !s.PendingAction })
				waitWorkspaceFrame(t, r, func(view string) bool { return !strings.Contains(view, "T3 approval") }, "settled approval")
				intuitiveCommand(r, "Requests")
				r.waitText("Exact choices")
				clickIntuitiveControl(t, r, "Next")
				r.waitText("An answer is required")
				clickIntuitiveControl(t, r, "First option")
				r.waitText("✓ First option")
				clickIntuitiveControl(t, r, "Second option")
				r.waitText("✓ Second option")
				clickIntuitiveControl(t, r, "Custom answer")
				r.waitText("› Custom answer")
				const custom = "  custom exact  "
				r.send("\x1b[200~" + custom + "\x1b[201~")
				r.waitText("custom exact")
				clickIntuitiveControl(t, r, "Next")
				r.waitText("Exact notes")
				clickIntuitiveControl(t, r, "Answer")
				r.waitText("› Answer")
				const notes = "  optional exact\nsecond note  "
				r.send("\x1b[200~" + notes + "\x1b[201~")
				r.waitText("second note")
				clickIntuitiveControl(t, r, "Back")
				r.waitText("Exact choices")
				r.waitText("custom exact")
				clickIntuitiveControl(t, r, "Next")
				r.waitText("Exact notes")
				r.waitText("second note")
				assertIntuitiveCounts(t, f, 1)
				clickIntuitiveControl(t, r, "Submit answers")
				question := f.waitRequest(t, "question-a")
				want := object{"choice": []any{"opaque/first", "opaque/second", custom}, "notes": notes}
				if question["threadId"] != "thread-a" || !reflect.DeepEqual(question["answers"], want) {
					t.Fatalf("question changed exact values: %+v", question)
				}
				awaitStatus(t, a, func(s client.Status) bool { return s.Requests == 0 && !s.PendingAction })
				if a.Draft() != draft || a.Status().ThreadID != "thread-a" || state.Draft("env-a", "thread-a") != draft {
					t.Fatal("request form changed source draft or attachment")
				}
				assertIntuitiveCounts(t, f, 2)
				quitIntuitiveProgram(t, r)
			})
		}
	}
}

func TestIntuitiveMouseProgramAgentBoxDecisionsPreserveDraftAndExactReview(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			f := newFixture(t)
			f.finishNativeThread("thread-a", "Review this completed result.")
			manager, state := newAppManager(t, f)
			const draft = "exact unsent reply\nretained second line  "
			if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
				t.Fatal(err)
			}
			a, err := manager.NewSession(f.profile.ID, "thread-a")
			if err != nil {
				t.Fatal(err)
			}
			r, _ := startAppTUI(t, a)
			r.waitText("Review this completed result.")
			sizeIntuitiveProgram(t, r, size, "agentbox")
			clickIntuitiveControl(t, r, "Alpha")
			r.waitText("1 Changes")
			clickIntuitiveControl(t, r, "1 Changes")
			r.waitText("export.go")
			r.send("\x1b")
			waitWorkspaceFrame(t, r, func(view string) bool {
				return strings.Contains(view, "1 Changes") && !strings.Contains(view, "╭─ Changes")
			}, "returned decision")
			clickIntuitiveControl(t, r, "2 Reply")
			r.waitText("Reply · Alpha")
			r.waitText("retained second line")
			assertIntuitiveCounts(t, f, 0)
			r.send("\x1b")
			r.waitText("1 Changes")
			clickIntuitiveControl(t, r, "3 Reviewed")
			r.waitText("Mark reviewed")
			assertIntuitiveCounts(t, f, 0)
			clickIntuitiveControl(t, r, "Mark reviewed")
			id := waitCommandType(t, f, "thread.settle")
			f.mu.Lock()
			payload := clone(f.commands[id])
			f.mu.Unlock()
			if payload["threadId"] != "thread-a" || a.Status().ThreadID != "thread-a" || a.Draft() != draft || state.Draft("env-a", "thread-a") != draft {
				t.Fatalf("review changed target, attachment or draft: %+v", payload)
			}
			assertIntuitiveCounts(t, f, 1)
			quitIntuitiveProgram(t, r)
		})
	}
}

func TestIntuitiveMouseProgramPlainTextSubmitCancelAndExactDraft(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				f := newFixture(t)
				manager, state := newAppManager(t, f)
				const draft = "original ordinary draft\nkeep exact  "
				if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
					t.Fatal(err)
				}
				a, err := manager.NewSession(f.profile.ID, "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				r, _ := startAppTUI(t, a)
				r.waitText("Initial Markdown for Alpha")
				sizeIntuitiveProgram(t, r, size, mode)
				intuitiveCommand(r, "File multiple tasks")
				r.modalReady("╭─ File multiple tasks")
				const tasks = "  First exact task  \nSecond exact task  "
				// This is a plain EnterText prompt; bracketed paste preserves
				// canonical multiline input in the same ordered terminal stream.
				r.send("\x1b[200~" + tasks + "\x1b[201~")
				r.waitText("Second exact task")
				clickIntuitiveControl(t, r, "Cancel")
				waitWorkspaceFrame(t, r, func(view string) bool { return !strings.Contains(view, "╭─ File multiple tasks") }, "cancelled plain text prompt")
				source := "\x00file-many/" + config.InboxIdentity("env-a", "project-a", "thread-a")
				if state.Draft("env-a", source) != tasks || a.Draft() != draft {
					t.Fatal("plain text Cancel lost canonical form/source drafts")
				}
				assertIntuitiveCounts(t, f, 0)
				intuitiveCommand(r, "File multiple tasks")
				r.modalReady("╭─ File multiple tasks")
				r.waitText("Second exact task")
				r.send("\t")
				r.waitText("[Submit]")
				clickIntuitiveControl(t, r, "First exact task")
				r.waitText("Enter answer")
				clickIntuitiveControl(t, r, "Submit")
				r.modalReady("╭─ Project")
				clickIntuitiveControl(t, r, "Fixture project")
				r.waitText("First message")
				r.waitText("First exact task")
				assertIntuitiveCounts(t, f, 0)
				clickIntuitiveControl(t, r, "Cancel")
				waitWorkspaceFrame(t, r, func(view string) bool { return !strings.Contains(view, "First message") }, "paused explicit batch review")
				if a.Status().ThreadID != "thread-a" || a.Draft() != draft || state.Draft("env-a", source) != tasks {
					t.Fatal("plain Submit implicitly launched or consumed the reviewed list")
				}
				assertIntuitiveCounts(t, f, 0)
				quitIntuitiveProgram(t, r)
			})
		}
	}
}
