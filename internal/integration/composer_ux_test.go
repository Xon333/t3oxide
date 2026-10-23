package integration_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBubbleTeaComposerLiteralPathsCompletionAndExactRecoveredPaste(t *testing.T) {
	f := newFixture(t)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Initial Markdown for Alpha")
	path := "/Users/example/project/file.go"
	r.send(path + "\r")
	f.waitMessage(t, path)
	r.waitText("Response after /Users/example")
	r.send("/hel")
	r.waitText("Tab complete")
	r.send("\t")
	r.send("\r")
	r.waitText("Keyboard and commands")
	r.program.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	r.waitText("Message")
	r.send("/mouse invalid\r")
	r.waitText("F2 error")
	// Invalid local arguments leave their source editable; Ctrl+X is explicit
	// recoverable clear, and cannot send a command or remote Stop.
	r.send("\x18")
	r.waitText("Draft cleared")
	r.program.Send(tea.PasteMsg{Content: "\tindented\r\nreplacement �\n "})
	r.waitText("F5 recall")
	r.program.Send(tea.KeyPressMsg{Code: tea.KeyF5})
	r.waitText("Recall prompt")
	r.send("\r")
	r.waitText("Exact draft")
	r.send("\r")
	exact := "\tindented\r\nreplacement �\n "
	f.waitMessage(t, exact)
	r.waitText("T3 acknowledged the message")
	r.send("typed immediately")
	r.waitText("typed immediately")
	if state.Pending("env-a", false) != nil {
		t.Fatal("exact recovered send left a pending operation")
	}
	r.send("\x11")
	<-r.done
	assertTerminalReset(t, r.output.raw())
}
