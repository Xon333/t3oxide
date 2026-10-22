package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/Xon333/t3oxide/internal/config"
)

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
