package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAuditListEnvelopeRetainsCompletenessAndReadOnlyBoundary(t *testing.T) {
	path, log := readOnlyCLIConfig(t)
	var out bytes.Buffer
	if err := run(context.Background(), []string{"threads", "--config", path, "--env", "fixture", "--json-envelope", "--scope", "all", "--cursor", "24", "--limit", "48"}, &out); err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(out.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["schemaVersion"] != float64(1) || data["more"] != true || data["threads"] == nil {
		t.Fatalf("lost envelope metadata: %v", data)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var req map[string]any
		if json.Unmarshal([]byte(line), &req) != nil {
			t.Fatal("bad fixture request")
		}
		if req["op"] == "list" && (req["lifecycle"] != "all" || req["cursor"] != float64(24) || req["limit"] != float64(48)) {
			t.Fatalf("lost scope/paging %v", req)
		}
	}
	for _, args := range [][]string{{"--json-envelope"}, {"--cursor", "0"}, {"envs", "--json-envelope"}, {"threads", "--limit", "0"}, {"threads", "--cursor", "-1"}, {"threads", "--scope", "guess"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
