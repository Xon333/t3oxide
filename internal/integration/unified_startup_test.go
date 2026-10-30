package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

func TestUnifiedStartupRealAdapterPartialHostsDuplicateIDsAndMouseOpen(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, activation := range []string{"enter", "mouse"} {
			t.Run(fmt.Sprintf("%s/%dx%d", activation, size[0], size[1]), func(t *testing.T) {
				unifiedStartupActivation(t, size, activation)
			})
		}
	}
}

func unifiedStartupActivation(t *testing.T, size [2]int, activation string) {
	t.Helper()
	first := newEnvironmentFixture(t, "env-first", "first", "First")
	second := newEnvironmentFixture(t, "env-second", "second", "Second")
	unavailable := newEnvironmentFixture(t, "actual-offline", "offline", "Offline")
	bad := unavailable.profile
	bad.EnvironmentID = "expected-offline"
	first.mu.Lock()
	first.threads["thread-a"]["thread"].(object)["title"] = "First alpha"
	first.mu.Unlock()
	second.mu.Lock()
	second.threads["thread-a"]["thread"].(object)["title"] = "Second alpha"
	second.threads["thread-a"]["visibleTurnItems"] = []any{row("thread-a", "first-thread-a", "Exact second environment transcript")}
	second.mu.Unlock()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{first.profile, second.profile, bad}, state)
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	if err := state.SetDraft("env-first", "thread-a", "first source draft"); err != nil {
		t.Fatal(err)
	}
	if err := state.SetDraft("env-second", "thread-a", "second source draft"); err != nil {
		t.Fatal(err)
	}
	startup, err := manager.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, startup)
	r.waitText("First alpha")
	r.waitText("Second alpha")
	r.waitText("! offline")
	sizeIntuitiveProgram(t, r, size, "developer")
	for _, host := range []*fixture{first, second, unavailable} {
		counts := host.requestCounts()
		if counts["rpc:orchestration.subscribeThread"] != 0 {
			t.Fatal("metadata startup subscribed a row")
		}
		for path, n := range counts {
			if n > 0 && strings.Contains(path, "/threads/") {
				t.Fatalf("metadata read fetched a transcript: %s", path)
			}
		}
	}
	r.send("Second alpha")
	deadline := time.Now().Add(5 * time.Second)
	filtered := false
	for time.Now().Before(deadline) {
		lines := strings.Split(r.views.text(), "\n")
		if len(lines) > 4 && strings.Contains(lines[3], "Second alpha") && !strings.Contains(r.views.text(), "First alpha") {
			filtered = true
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !filtered {
		t.Fatalf("real filter did not finish before the click: %s", r.views.text())
	}
	r.send("\x1b[B\x1b[A")
	if first.requestCounts()["rpc:orchestration.subscribeThread"] != 0 || second.requestCounts()["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("result arrows attached before explicit activation")
	}
	if activation == "mouse" {
		// The query repeats the title; the project/kind bubble identifies the
		// actual rendered row. Full source identity remains in its selected
		// details, and the subscription/draft checks below verify the exact
		// environment half of the otherwise duplicate native thread ID.
		clickIntuitiveControl(t, r, "[FI◇] Second alpha")
	} else {
		r.send("\r")
	}
	r.waitText("Exact second environment transcript")
	r.waitText("second source draft")
	if first.requestCounts()["rpc:orchestration.subscribeThread"] != 0 || second.requestCounts()["rpc:orchestration.subscribeThread"] != 1 {
		t.Fatal("explicit open lost the environment half of a duplicate thread identity")
	}
	if state.Draft("env-first", "thread-a") != "first source draft" || state.Draft("env-second", "thread-a") != "second source draft" {
		t.Fatal("source drafts crossed during explicit open")
	}
	if effects, dispatches := second.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("metadata open performed a mutation")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program failed to exit")
	}
	assertTerminalReset(t, r.output.raw())
}
