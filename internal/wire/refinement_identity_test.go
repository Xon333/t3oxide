package wire

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNewCompanionReadsRemainReadOnlyAndUnknownOperationsStayRejected(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line);emit({'id':p['id'],'result':{'operation':p['op']}})
`, Options{RequestTimeout: 2 * time.Second})
	for _, op := range []string{"native-defaults", "thread-preview"} {
		reply, err := c.Request(context.Background(), op, map[string]any{"threadId": "thread"}, "")
		if err != nil || reply["operation"] != op {
			t.Fatal(reply, err)
		}
	}
	if _, err := c.Request(context.Background(), "invented-native-write", nil, ""); err == nil {
		t.Fatal("unknown operation admitted")
	}
}

func TestProviderIdentityBoundedOptionalAndDefensivelyCloned(t *testing.T) {
	snapshot := &Snapshot{Metadata: Metadata{ProviderIdentity: &ProviderIdentity{InstanceID: "A", Label: "Same label", AccountLabel: "Native label", AccountReportedAt: "2026-10-30T08:00:00Z"}}}
	copy := CloneSnapshot(snapshot)
	copy.Metadata.ProviderIdentity.InstanceID = "B"
	if snapshot.Metadata.ProviderIdentity.InstanceID != "A" {
		t.Fatal("provider identity escaped through shared pointer")
	}
	if err := snapshot.Metadata.ProviderIdentity.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := *snapshot.Metadata.ProviderIdentity
	invalid.AccountReportedAt = "unverified time"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid account telemetry accepted")
	}
	var fields object
	if err := json.Unmarshal([]byte(`{"providerIdentity":{"instanceId":"A","driver":"native","label":"Provider","accountLabel":"","accountReportedAt":"","future":true}}`), &fields); err != nil {
		t.Fatal(err)
	}
	var metadata Metadata
	if err := decodeExtensions(fields, &metadata); err != nil || metadata.ProviderIdentity == nil || metadata.ProviderIdentity.AccountLabel != "" {
		t.Fatal(metadata, err)
	}
}
