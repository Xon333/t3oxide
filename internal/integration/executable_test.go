package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var binaryBuild struct {
	once      sync.Once
	dir, path string
	err       error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryBuild.dir != "" {
		_ = os.RemoveAll(binaryBuild.dir)
	}
	os.Exit(code)
}

func sourceRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func builtExecutable(t *testing.T) string {
	t.Helper()
	binaryBuild.once.Do(func() {
		binaryBuild.dir, binaryBuild.err = os.MkdirTemp("", "t3oxide-executable-test-")
		if binaryBuild.err != nil {
			return
		}
		binaryBuild.path = filepath.Join(binaryBuild.dir, "t3oxide")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", binaryBuild.path, "./cmd/t3oxide")
		cmd.Dir = sourceRoot()
		output, err := cmd.CombinedOutput()
		if err != nil {
			binaryBuild.err = fmt.Errorf("assemble executable: %w\n%s", err, output)
		}
	})
	if binaryBuild.err != nil {
		t.Fatal(binaryBuild.err)
	}
	return binaryBuild.path
}

func profileFile(t *testing.T, f *fixture) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "environments.json")
	data, err := json.Marshal(object{"version": 1, "environments": []any{f.profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(dir, "bubbletea-drafts.json")
}

func TestAssembledExecutableReadOnlyCommandsUseActualAdapter(t *testing.T) {
	f := newFixture(t)
	binary := builtExecutable(t)
	config, state := profileFile(t, f)
	for _, command := range [][]string{
		{"envs", "--json"},
		{"threads", "--env", "fixture", "Alpha", "--json"},
		{"status", "--env", "fixture", "--thread", "thread-a", "--json"},
		{"doctor", "--env", "fixture", "--json"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		args := append([]string{"--config", config, "--state", state}, command...)
		output, err := exec.CommandContext(ctx, binary, args...).Output()
		cancel()
		if err != nil {
			t.Fatalf("%v: %v\n%s", command, err, output)
		}
		var result any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("%v returned invalid JSON: %v\n%s", command, err, output)
		}
		if strings.Contains(string(output), "fixture-token-not-a-credential") {
			t.Fatal("credential escaped read-only CLI")
		}
		if len(command) > 2 && command[0] == "threads" && (!strings.Contains(string(output), "thread-a") || strings.Contains(string(output), "thread-b")) {
			t.Fatalf("CLI thread filtering changed: %s", output)
		}
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("non-interactive discovery caused a mutation")
	}
}

// executablePTY tests the assembled binary, normal CLI startup, real Linux or
// Darwin termios, a real terminal input parser and the unchanged Node adapter.
// It is not a claim about a particular native Mac terminal or clipboard.
type executablePTY struct {
	t              *testing.T
	cmd            *exec.Cmd
	input          io.WriteCloser
	output         captureBuffer
	stderr         bytes.Buffer
	done           chan struct{}
	err            error
	exit           *int
	restored       bool
	termiosChanges map[string]any
}

func startExecutablePTY(t *testing.T, f *fixture) (*executablePTY, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PTY test requires POSIX termios")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 stdlib required for executable PTY test")
	}
	config, state := profileFile(t, f)
	r := &executablePTY{t: t, done: make(chan struct{})}
	r.cmd = exec.Command(python, filepath.Join(sourceRoot(), "internal", "integration", "testdata", "pty_driver.py"), builtExecutable(t), "--config", config, "--state", state, "--env", "fixture", "--thread", "thread-a")
	r.cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	r.input, err = r.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := r.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	r.cmd.Stderr = &r.stderr
	if err = r.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		decoder := json.NewDecoder(output)
		for {
			var event struct {
				Output         string         `json:"output"`
				Exit           *int           `json:"exit"`
				Restored       bool           `json:"restored"`
				TermiosChanges map[string]any `json:"termiosChanges"`
			}
			if err := decoder.Decode(&event); err != nil {
				if err != io.EOF {
					r.err = err
				}
				break
			}
			if event.Output != "" {
				data, err := base64.StdEncoding.DecodeString(event.Output)
				if err != nil {
					r.err = err
					break
				}
				_, _ = r.output.Write(data)
			}
			if event.Exit != nil {
				r.exit = event.Exit
				r.restored = event.Restored
				r.termiosChanges = event.TermiosChanges
			}
		}
		if err := r.cmd.Wait(); err != nil && r.err == nil {
			r.err = err
		}
		close(r.done)
	}()
	t.Cleanup(func() {
		select {
		case <-r.done:
		default:
			_ = json.NewEncoder(r.input).Encode(object{"signal": "TERM"})
			select {
			case <-r.done:
			case <-time.After(5 * time.Second):
				_ = r.cmd.Process.Kill()
				<-r.done
			}
		}
		_ = r.input.Close()
		if t.Failed() {
			t.Logf("PTY output:\n%s\nDriver stderr: %s", r.output.text(), r.stderr.String())
		}
	})
	return r, state
}

func (r *executablePTY) send(command object) {
	r.t.Helper()
	if err := json.NewEncoder(r.input).Encode(command); err != nil {
		r.t.Fatal(err)
	}
}
func (r *executablePTY) keys(keys string) { r.send(object{"write": keys}) }
func (r *executablePTY) waitText(want string) {
	r.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.output.text(), want) {
			return
		}
		select {
		case <-r.done:
			r.t.Fatalf("executable exited before %q: %v / %s", want, r.err, r.stderr.String())
		default:
		}
		time.Sleep(15 * time.Millisecond)
	}
	r.t.Fatalf("executable never rendered %q:\n%s", want, r.output.text())
}
func (r *executablePTY) finish() {
	r.t.Helper()
	r.keys("\x11")
	r.awaitCleanExit()
}

func (r *executablePTY) awaitCleanExit() {
	r.t.Helper()
	select {
	case <-r.done:
	case <-time.After(6 * time.Second):
		r.t.Fatal("assembled executable failed to close after local quit/cancellation")
	}
	if r.err != nil || r.exit == nil || *r.exit != 0 {
		r.t.Fatalf("executable exit=%v driver=%v stderr=%s", r.exit, r.err, r.stderr.String())
	}
	if !r.restored {
		r.t.Fatalf("assembled executable did not restore actual PTY termios: %#v", r.termiosChanges)
	}
	assertTerminalReset(r.t, r.output.raw())
}

func TestAssembledExecutablePTYCtrlCAndSignalRestoreTerminalWithoutRemoteStop(t *testing.T) {
	for _, method := range []string{"ctrl-c", "sigterm"} {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t)
			r, _ := startExecutablePTY(t, f)
			r.waitText("Initial Markdown for Alpha")
			if method == "ctrl-c" {
				r.keys("\x03")
			} else {
				r.send(object{"signal": "TERM"})
			}
			r.awaitCleanExit()
			if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
				t.Fatalf("local quit/cancellation stopped or changed remote T3: %d/%d", effects, dispatches)
			}
		})
	}
}

func TestAssembledExecutablePTYSendThemeResizeStopAndTerminalRestore(t *testing.T) {
	f := newFixture(t)
	r, statePath := startExecutablePTY(t, f)
	// This is a new transcript row, not a short status whose unchanged suffix
	// can be retained by the terminal's differential renderer.
	r.waitText("Initial Markdown for Alpha")
	r.keys("Hello Živjo 日本語 🙂\r")
	f.waitMessage(t, "Hello Živjo 日本語 🙂")
	r.waitText("Response after Hello")
	r.keys("/theme cyan\r")
	settingsPath := filepath.Join(filepath.Dir(statePath), "terminal.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(settingsPath)
		var settings object
		_ = json.Unmarshal(data, &settings)
		if settings["theme"] == "cyan" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("theme chosen in executable did not persist")
		}
		time.Sleep(15 * time.Millisecond)
	}
	r.send(object{"resize": []int{44, 16}})
	r.keys("Unsaved draft survives Stop")
	r.keys("\x13")
	deadline = time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		var stop object
		for _, command := range f.commands {
			if command["type"] == "run.interrupt" {
				stop = clone(command)
			}
		}
		f.mu.Unlock()
		if stop != nil {
			if stop["runId"] != "run-thread-a" || stop["holdQueue"] != true {
				t.Fatalf("executable changed Stop semantics: %#v", stop)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real PTY Ctrl+S did not dispatch Stop")
		}
		time.Sleep(15 * time.Millisecond)
	}
	r.send(object{"resize": []int{160, 42}})
	r.finish()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("Unsaved draft survives Stop")) {
		t.Fatalf("quit lost unsent draft: %s", data)
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("executable duplicated a write or quit stopped remote work: %d/%d", effects, dispatches)
	}
}

func TestAssembledExecutablePTYUncertainSendStopPriorityAndExplicitRetry(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.dropMessages = true
	f.mu.Unlock()
	r, statePath := startExecutablePTY(t, f)
	r.waitText("Initial Markdown for Alpha")
	r.keys("Uncertain executable message\r")
	id := f.waitMessage(t, "Uncertain executable message")
	// The main action is still awaiting its acknowledgment. Stop must have
	// independent admission through the actual keyboard/update/command path.
	r.keys("\x13")
	deadline := time.Now().Add(4 * time.Second)
	for {
		f.mu.Lock()
		found := false
		for _, command := range f.commands {
			if command["type"] == "run.interrupt" && command["holdQueue"] == true {
				found = true
			}
		}
		f.mu.Unlock()
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("executable Stop was blocked behind pending send")
		}
		time.Sleep(15 * time.Millisecond)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state object
		if err = json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		stops, _ := state["pendingStops"].(object)
		if len(stops) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("independent Stop acknowledgement did not settle")
		}
		time.Sleep(15 * time.Millisecond)
	}
	f.mu.Lock()
	oldSockets := make(map[*fixtureSocket]bool, len(f.sockets))
	for socket := range f.sockets {
		oldSockets[socket] = true
	}
	f.mu.Unlock()
	f.closeSockets()
	f.mu.Lock()
	f.dropMessages = false
	f.mu.Unlock()
	deadline = time.Now().Add(8 * time.Second)
	for {
		f.mu.Lock()
		subscribed := false
		for s := range f.sockets {
			if !oldSockets[s] && s.subscription != "" {
				subscribed = true
			}
		}
		f.mu.Unlock()
		if subscribed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("executable did not reconnect its actual adapter")
		}
		time.Sleep(15 * time.Millisecond)
	}
	f.update("thread-a", "Live marker after interrupted acknowledgement", 0)
	r.waitText("Live marker after interrupted")
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("reconnect replayed an uncertain write: %d/%d", effects, dispatches)
	}
	readPending := func() object {
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state object
		if err = json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		pending, _ := state["pending"].(object)
		record, _ := pending["env-a"].(object)
		return record
	}
	if pending := readPending(); pending == nil || pending["id"] != id {
		t.Fatalf("executable lost its stable uncertain identity: %#v", pending)
	}
	// Ctrl+X explicitly clears this local composer; it cannot stop remote work. The saved
	// record still contains the original text for explicit retry.
	r.keys("\x18")
	r.keys("/retry\r")
	deadline = time.Now().Add(6 * time.Second)
	for readPending() != nil {
		if time.Now().After(deadline) {
			t.Fatal("executable explicit retry did not acknowledge the saved operation")
		}
		time.Sleep(15 * time.Millisecond)
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 3 {
		t.Fatalf("explicit retry duplicated a remote effect: %d/%d", effects, dispatches)
	}
	f.mu.Lock()
	for _, dispatched := range f.dispatches {
		if dispatched != id && f.commands[dispatched]["type"] != "run.interrupt" {
			t.Error("retry generated a replacement operation ID")
		}
	}
	f.mu.Unlock()
	r.finish()
}
