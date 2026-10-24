package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Xon333/t3oxide/internal/config"
)

func TestHelpAndBareJSONDoNotOpenConfigurationOrState(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"bare-json", []string{"--json"}, ""},
		{"bare-json-env", []string{"--json", "--env", "fixture"}, ""},
		{"help-unknown-before", []string{"--unknown", "--help"}, help},
		{"help-unknown-after", []string{"-h", "--unknown"}, help},
		{"help-missing-value", []string{"--env", "--help"}, help},
		{"help-missing-at-end", []string{"--help", "--thread"}, help},
		{"help-bad-command-theme", []string{"send", "--theme", "invalid", "-h"}, help},
		{"help-before-version-json", []string{"--version", "--json", "--help"}, help},
		{"version-before-bare-json", []string{"--json", "--version"}, "t3oxide " + version + " · Bubble Tea v2.1.0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Missing config must never replace an invocation/help result with
			// a config error; the absent state parent detects state creation.
			stateDir := filepath.Join(dir, "must-not-create")
			args := append([]string{"--config", filepath.Join(dir, "missing.json"), "--state", filepath.Join(stateDir, "drafts.json")}, tc.args...)
			var out bytes.Buffer
			err := run(context.Background(), args, &out)
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "--json requires a read-only command") {
					t.Fatalf("expected invocation error before config/state: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("unexpected stdout: %q", out.String())
			}
			if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
				t.Fatalf("invocation accessed state: %v", err)
			}
		})
	}
}

func TestCLIHelpAdvertisesPortableControlsAndActualJSONContracts(t *testing.T) {
	for _, want := range []string{"Ctrl+O", "Ctrl+P", "/edit", "/recall", "/help", "Ctrl+S", "/retry [stop]", "doctor [--env m1] [--json]", "status --env m1 [--thread ID]", "always JSON"} {
		if !strings.Contains(help, want) {
			t.Fatalf("missing help contract %q", want)
		}
	}
	if strings.Contains(help, "terminal companion") || strings.Contains(help, "TUI: /switch") {
		t.Fatal("help retained the tagline or exhaustive TUI inventory")
	}
}

// The shell only launches this Go fixture with test flags. The fixture owns
// every response and logs all reads; it cannot contact a real T3 installation.
func readOnlyCLIConfig(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("T3OXIDE_CLI_TEST_BINARY", executable)
	t.Setenv("T3OXIDE_CLI_TEST_ADAPTER", "1")
	adapter, log := filepath.Join(dir, "adapter.sh"), filepath.Join(dir, "requests.jsonl")
	if err = os.WriteFile(adapter, []byte("exec \"$T3OXIDE_CLI_TEST_BINARY\" -test.run=^TestCLIReadOnlyAdapterFixture$ -- \"$@\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := config.Environment{ID: "fixture", Label: "Fixture", EnvironmentID: "fixture-env", Node: "/bin/sh", Adapter: adapter, HostConfig: log, Enabled: true}
	data, err := json.Marshal(map[string]any{"version": 1, "environments": []config.Environment{profile}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "environments.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func TestCLIReadOnlyAdapterFixture(t *testing.T) {
	if os.Getenv("T3OXIDE_CLI_TEST_ADAPTER") != "1" {
		return
	}
	log, err := os.OpenFile(os.Args[len(os.Args)-1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	defer log.Close()
	input, output := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request map[string]any
		if err := input.Decode(&request); err != nil {
			if err == io.EOF {
				os.Exit(0)
			}
			os.Exit(2)
		}
		if json.NewEncoder(log).Encode(request) != nil {
			os.Exit(2)
		}
		var result any
		switch request["op"] {
		case "hello":
			result = map[string]any{"protocol": 1, "environmentId": "fixture-env", "orchestrationProtocol": 2}
		case "list":
			result = map[string]any{"more": true, "threads": []map[string]any{{"id": "thread-id", "title": "Title 界\x1b[31m\n", "project": "Fixture", "status": "idle", "model": "model"}}}
		case "watch":
			result = map[string]any{"ok": true}
		default:
			os.Exit(3) // Reject every mutation, including any future accidental write.
		}
		if output.Encode(map[string]any{"v": 1, "environmentId": "fixture-env", "id": request["id"], "result": result}) != nil {
			os.Exit(2)
		}
		if request["op"] == "watch" {
			view := map[string]any{"threadId": request["threadId"], "title": "Fixture", "model": "model", "status": "idle", "requests": []any{}, "hasMore": false, "limited": false, "state": "live", "live": true, "historical": false, "revision": 7, "items": []any{}}
			if output.Encode(map[string]any{"v": 1, "environmentId": "fixture-env", "event": "view", "data": view}) != nil {
				os.Exit(2)
			}
		}
	}
}

func TestAllReadOnlyCLICommandsPreserveStdoutAndAvoidState(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		args          []string
		json          bool
		ops           []string
	}{
		{"envs-human", "envs", nil, false, nil},
		{"envs-json", "envs", []string{"--json"}, true, nil},
		{"threads-human", "threads", []string{"title", "project"}, false, []string{"hello", "list"}},
		{"threads-json", "threads", []string{"title", "project", "--json"}, true, []string{"hello", "list"}},
		{"status-default-json", "status", nil, true, []string{"hello", "list"}},
		{"status-explicit-json", "status", []string{"--json"}, true, []string{"hello", "list"}},
		{"status-thread", "status", []string{"--thread", "thread-id"}, true, []string{"hello", "list", "watch"}},
		{"doctor-human", "doctor", nil, false, []string{"hello", "list"}},
		{"doctor-json", "doctor", []string{"--json"}, true, []string{"hello", "list"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, log := readOnlyCLIConfig(t)
			stateDir := filepath.Join(t.TempDir(), "must-not-create")
			args := append([]string{tc.command, "--config", path, "--state", filepath.Join(stateDir, "drafts.json"), "--env", "fixture"}, tc.args...)
			var out bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := run(ctx, args, &out); err != nil {
				t.Fatal(err)
			}
			if tc.json {
				var value any
				decoder := json.NewDecoder(&out)
				if err := decoder.Decode(&value); err != nil {
					t.Fatalf("stdout is not one JSON document: %v", err)
				}
				if err := decoder.Decode(new(any)); err != io.EOF {
					t.Fatalf("stdout has extra non-JSON output: %v", err)
				}
				checkReadOnlyJSON(t, tc.command, value, slices.Contains(tc.args, "--thread"))
			} else {
				want := map[string]string{"envs": "PROFILE", "threads": "THREAD", "doctor": "fixture: OK"}[tc.command]
				if !strings.Contains(out.String(), want) || strings.Contains(out.String(), "\x1b") {
					t.Fatalf("invalid human stdout: %q", out.String())
				}
				if tc.command == "threads" && !strings.Contains(out.String(), "More matches exist") {
					t.Fatal("human listing lost truncation guidance")
				}
			}
			if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
				t.Fatalf("read-only command accessed state: %v", err)
			}
			data, err := os.ReadFile(log)
			if len(tc.ops) == 0 {
				if !os.IsNotExist(err) {
					t.Fatalf("envs started an adapter: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var ops []string
			scanner := bufio.NewScanner(bytes.NewReader(data))
			for scanner.Scan() {
				var request map[string]any
				if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
					t.Fatal(err)
				}
				ops = append(ops, fmt.Sprint(request["op"]))
				if request["op"] == "list" && tc.command == "threads" && request["query"] != "title project" {
					t.Fatalf("listing changed query: %#v", request)
				}
				if request["op"] == "watch" && request["threadId"] != "thread-id" {
					t.Fatalf("status watched the wrong thread: %#v", request)
				}
			}
			if scanner.Err() != nil || !slices.Equal(ops, tc.ops) {
				t.Fatalf("unexpected adapter operations: %v; %v", ops, scanner.Err())
			}
		})
	}
}

func checkReadOnlyJSON(t *testing.T, command string, value any, thread bool) {
	t.Helper()
	if command == "status" {
		status, ok := value.(map[string]any)
		if !ok || status["profile"] != "fixture" || status["more"] != true || status["adapter"] == nil || status["threads"] == nil {
			t.Fatalf("invalid status JSON: %#v", value)
		}
		if thread && (status["conversation"].(map[string]any)["threadId"] != "thread-id" || status["visibleRows"] != float64(0) || status["revision"] != float64(7)) {
			t.Fatalf("invalid conversation JSON: %#v", value)
		}
		return
	}
	rows, ok := value.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("invalid listing/doctor JSON: %#v", value)
	}
	row := rows[0].(map[string]any)
	switch command {
	case "envs":
		if row["id"] != "fixture" || row["transport"] != "local" || row["environmentId"] != "fixture-env" {
			t.Fatalf("invalid environment JSON: %#v", row)
		}
	case "threads":
		if row["id"] != "thread-id" || row["title"] != "Title 界\x1b[31m\n" {
			t.Fatalf("JSON changed authoritative thread values: %#v", row)
		}
	case "doctor":
		if row["profile"] != "fixture" || row["ok"] != true || row["status"] == nil {
			t.Fatalf("invalid doctor JSON: %#v", row)
		}
	}
}

func TestFlagsAndReadOnlyCommandBoundary(t *testing.T) {
	a, err := parse([]string{"threads", "--env", "m1", "psychology", "--json"})
	if err != nil || a.env != "m1" || !a.json || strings.Join(a.words, " ") != "psychology" {
		t.Fatalf("parse: %#v %v", a, err)
	}
	if a, err := parse([]string{"--theme", "terminal"}); err != nil || a.theme != "terminal" {
		t.Fatalf("terminal theme: %#v %v", a, err)
	}
	for _, args := range [][]string{{"send", "do work"}, {"--env"}, {"--theme", "unknown"}, {"status", "unexpected"}, {"--unknown"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted unsupported invocation: %q", args)
		}
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"--help", "--config", "/missing"}, &out); err != nil || !strings.Contains(out.String(), "read-only") {
		t.Fatal("help should require no configuration", err)
	}
}

func TestHumanOutputStripsTerminalControlsWithoutChangingJSON(t *testing.T) {
	label := "T3 \x1b[31mŽivjo\x1b[0m\n日本語\u009b\a"
	envs := []config.Environment{{ID: "profile\tname", Label: label, EnvironmentID: "env"}}
	var human bytes.Buffer
	if err := headless(context.Background(), arguments{command: "envs"}, envs, &human); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(human.String(), "\x1b\a\u009b") || !strings.Contains(human.String(), "Živjo") || !strings.Contains(human.String(), "日本語") {
		t.Fatalf("unsafe or corrupted human output: %q", human.String())
	}
	for _, r := range plain(label) {
		if unicode.IsControl(r) {
			t.Fatalf("control survived CLI sanitizer: %U", r)
		}
	}
	var machine bytes.Buffer
	if err := headless(context.Background(), arguments{command: "envs", json: true}, envs, &machine); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(machine.Bytes(), &rows); err != nil || len(rows) != 1 || rows[0]["label"] != label {
		t.Fatalf("JSON should preserve the actual value: %v %s", err, machine.String())
	}
}

func TestDoctorJSONKeepsMultipleFailuresMachineReadable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-node")
	envs := []config.Environment{
		{ID: "one", EnvironmentID: "one", Node: missing},
		{ID: "two", EnvironmentID: "two", Node: missing},
	}
	var out bytes.Buffer
	if err := headless(context.Background(), arguments{command: "doctor", json: true}, envs, &out); err == nil {
		t.Fatal("diagnostic failures must set an unsuccessful exit result")
	}
	var rows []struct {
		Profile string `json:"profile"`
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("invalid diagnostic JSON: %v; %s", err, out.String())
	}
	if len(rows) != 2 || rows[0].Profile != "one" || rows[1].Profile != "two" || rows[0].OK || rows[1].OK || rows[0].Error == "" || rows[1].Error == "" {
		t.Fatalf("incomplete diagnostic results: %#v", rows)
	}
}

func TestDoctorRequiresExactHandshake(t *testing.T) {
	valid := map[string]any{"protocol": json.Number("1"), "environmentId": "env", "orchestrationProtocol": float64(2)}
	if err := validateHello(valid, "env"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{
		{"protocol": 1.5, "environmentId": "env", "orchestrationProtocol": 2},
		{"protocol": 1, "environmentId": "wrong", "orchestrationProtocol": 2},
		{"protocol": 1, "environmentId": "env", "orchestrationProtocol": 3},
		{"environmentId": "env"},
	} {
		if validateHello(bad, "env") == nil {
			t.Fatalf("claimed verification for %#v", bad)
		}
	}
}
