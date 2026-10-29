package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestCapabilitiesOfflineDoesNotReadConfigOrCreateState(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	err := run(context.Background(), []string{"capabilities", "--json", "--config", filepath.Join(dir, "missing"), "--state", filepath.Join(dir, "state", "never.json")}, &out)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["ok"] != true || response["schemaVersion"] != float64(1) {
		t.Fatalf("bad envelope: %s", out.Bytes())
	}
	data := response["data"].(map[string]any)
	if data["mutationAccess"] != "interactive-only" || data["adapter"] != nil {
		t.Fatal("offline capabilities claimed a host connection or CLI writes")
	}
	if data["recovery"].(map[string]any)["automaticMutationReplay"] != false {
		t.Fatal("recovery contract changed")
	}
	workflows := data["nativeWorkflows"].(map[string]any)
	if workflows["priorityBacklogScheduler"] != false || workflows["defaultEnvironmentBridge"] != false || workflows["requiresAdapterFeature"] != "nativeWorkflows" {
		t.Fatal("offline capability report invented unavailable native authority")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("offline capabilities accessed files: %v %v", files, err)
	}
}

func TestInspectionCLIUsesOnlyExactReadAndKeepsStateUntouched(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args, ops []string
	}{
		{"host-capabilities", []string{"capabilities"}, []string{"hello"}},
		{"activity", []string{"activity", "--thread", "thread-id", "--activity-cursor", "opaque-cursor"}, []string{"hello", "activity"}},
		{"inspect", []string{"inspect", "--thread", "thread-id", "--item", "item-id", "--revision", "old-revision"}, []string{"hello", "inspect-item"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, log := readOnlyCLIConfig(t)
			t.Setenv("T3OXIDE_CLI_ACTIVITY", "1")
			state := filepath.Join(t.TempDir(), "must-not-exist", "state.json")
			args := append(tc.args, "--config", path, "--state", state, "--env", "fixture", "--json")
			var out bytes.Buffer
			if err := run(context.Background(), args, &out); err != nil {
				t.Fatal(err)
			}
			var response map[string]any
			if json.Unmarshal(out.Bytes(), &response) != nil || response["ok"] != true {
				t.Fatalf("invalid read result: %s", out.Bytes())
			}
			data := response["data"].(map[string]any)
			if tc.name == "inspect" && (data["sourceThreadId"] != "thread-id" || data["requestedRevision"] != "old-revision" || data["changed"] != true) {
				t.Fatal("lost source/revision evidence")
			}
			if tc.name == "activity" && data["activity"].(map[string]any)["historyCursor"] != "opaque-history-cursor" {
				t.Fatal("lost continuation")
			}
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Fatal("inspection touched recovery state")
			}
			logBytes, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var ops []string
			for _, line := range strings.Split(strings.TrimSpace(string(logBytes)), "\n") {
				var req map[string]any
				if json.Unmarshal([]byte(line), &req) != nil {
					t.Fatal("invalid log")
				}
				ops = append(ops, req["op"].(string))
				if req["op"] == "activity" && req["cursor"] != "opaque-cursor" {
					t.Fatal("cursor changed")
				}
			}
			if !reflect.DeepEqual(ops, tc.ops) {
				t.Fatalf("unexpected operations: %v", ops)
			}
		})
	}
}

func TestInspectionJSONFailureIsExplicitAndRejectsWrongSource(t *testing.T) {
	for _, capable := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-adapter", true: "wrong-source"}[capable], func(t *testing.T) {
			path, _ := readOnlyCLIConfig(t)
			if capable {
				t.Setenv("T3OXIDE_CLI_ACTIVITY", "1")
				t.Setenv("T3OXIDE_CLI_WRONG_SOURCE", "1")
			}
			var out bytes.Buffer
			err := run(context.Background(), []string{"inspect", "--json", "--config", path, "--env", "fixture", "--thread", "thread-id", "--item", "item-id"}, &out)
			if err == nil {
				t.Fatal("unsupported/misbound inspection succeeded")
			}
			var response inspectionEnvelope
			if json.Unmarshal(out.Bytes(), &response) != nil || response.OK || response.Error == nil || response.Error.Uncertain {
				t.Fatalf("invalid failure contract: %s", out.Bytes())
			}
			if !capable && response.Error.Code != "unsupported" {
				t.Fatal("missing feature negotiation error")
			}
		})
	}
}

func TestInspectionHumanOutputPreservesLinesAndStripsTerminalControls(t *testing.T) {
	path, _ := readOnlyCLIConfig(t)
	t.Setenv("T3OXIDE_CLI_ACTIVITY", "1")
	var out bytes.Buffer
	if err := run(context.Background(), []string{"inspect", "--config", path, "--env", "fixture", "--thread", "thread-id", "--item", "item-id"}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "fixture\nsecond line") {
		t.Fatalf("unsafe or flattened output: %q", out.String())
	}
}

func TestInspectionHumanOutputDistinguishesResultCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
		present, omitted   bool
	}{
		{name: "missing", want: "No result reported."},
		{name: "omitted", omitted: true, want: "Output is not available from this response."},
		{name: "empty", present: true, want: "Returned output is empty."},
		{name: "partial", present: true, omitted: true, output: "partial output", want: "T3 reports additional omitted output."},
		{name: "complete", present: true, output: "complete output", want: "\nOutput\ncomplete output\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			detail := wire.ActivityDetail{SourceThreadID: "source-thread", ItemID: "tool-id", Title: "Command", Status: "completed", Output: tc.output, OutputPresent: tc.present, OutputOmitted: tc.omitted}
			if err := writeActivityDetail(&out, "Fixture", detail); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "Fixture / source-thread / tool-id") {
				t.Fatalf("lost result state or identity: %q", out.String())
			}
			if tc.output != "" && !strings.Contains(out.String(), tc.output) {
				t.Fatalf("lost available output: %q", out.String())
			}
			if tc.present && strings.Contains(out.String(), "No result reported.") {
				t.Fatalf("present result was called missing: %q", out.String())
			}
		})
	}
}

func TestInspectionHumanRevisionDoesNotAssumeNewerOrReportedResult(t *testing.T) {
	detail := wire.ActivityDetail{SourceThreadID: "source-thread", ItemID: "tool-id", RequestedRevision: "2026-10-24T11:00:00Z", UpdatedAt: "2026-10-24T10:00:00Z", Changed: true}
	var out bytes.Buffer
	if err := writeActivityDetail(&out, "Fixture", detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Result revision differs from the requested revision.") || strings.Contains(out.String(), "Updated since") {
		t.Fatalf("claimed revision ordering: %q", out.String())
	}
	detail.Changed, detail.UpdatedAt = false, ""
	out.Reset()
	if err := writeActivityDetail(&out, "Fixture", detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Result revision unreported.") {
		t.Fatalf("unreported revision was silent: %q", out.String())
	}
}

func TestActivityHistoryHumanHeadingRetainsSourceWithoutTitle(t *testing.T) {
	var out bytes.Buffer
	page := wire.ActivityList{ThreadID: "source-thread", Activity: wire.ActivityPage{Scope: "history", Items: []wire.ActivityItem{}}}
	if err := writeActivityList(&out, "Fixture", page); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Fixture / source-thread\n") {
		t.Fatalf("empty history page lost target: %q", out.String())
	}
}

func TestInspectionArgumentsAndThreadTypeContract(t *testing.T) {
	for _, args := range [][]string{{"activity"}, {"inspect", "--thread", "id"}, {"envs", "--item", "id"}, {"inspect", "--thread", "id", "--item", "id", "--activity-cursor", "cursor"}, {"activity", "--thread", "x\n"}, {"inspect", "--thread", "id", "--item", "id", "--revision", strings.Repeat("x", 129)}, {"threads", "--type", "guess"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
	path, log := readOnlyCLIConfig(t)
	var out bytes.Buffer
	if err := run(context.Background(), []string{"threads", "--config", path, "--env", "fixture", "--type", "subagents", "--scope", "all", "--json-envelope"}, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil || !bytes.Contains(b, []byte(`"typeFilter":"subagents"`)) {
		t.Fatalf("type omitted: %s %v", b, err)
	}
}
