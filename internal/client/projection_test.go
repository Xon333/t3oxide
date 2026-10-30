package client

import (
	"encoding/json"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestControlWakeOmitsUnchangedSnapshotAndRetainedReadIsDefensive(t *testing.T) {
	m, _, environment := recoveryManager(t)
	s, err := m.NewSession(environment.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	first := s.renderEvent()
	if first.Snapshot == nil {
		t.Fatal("first authoritative view omitted")
	}
	title := first.Snapshot.Metadata.Title
	first.Snapshot.Metadata.Title = "caller changed"
	s.mu.Lock()
	s.delivery = "steer"
	s.mu.Unlock()
	second := s.renderEvent()
	if second.Snapshot != nil || second.Status.Delivery != "steer" || second.Revision <= first.Revision {
		t.Fatal("control wake cloned unchanged content or lost current controls")
	}
	s.store.Select("thread")
	retained := s.renderEvent()
	if retained.Snapshot == nil || retained.Snapshot.Metadata.Title != title {
		t.Fatal("event caller modified retained recovery view")
	}
	retained.Snapshot.Metadata.Title = "changed retained read"
	if s.snapshot().Metadata.Title != title {
		t.Fatal("retained event exposes mutable session state")
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	if next := s.renderEvent(); next.Snapshot == nil || next.Snapshot.Revision != 1 {
		t.Fatal("new helper with equal numeric revision did not publish")
	}
}

func TestProjectionControlWakeCannotConsumeLatestAndFirstBoundedPageFollows(t *testing.T) {
	m, _, environment := recoveryManager(t)
	s, err := m.NewSession(environment.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	_ = s.renderEvent()
	s.scrollGoal = "live"
	if control := s.renderEvent(); control.Snapshot != nil || control.ResetScroll || control.Follow || s.scrollGoal != "live" {
		t.Fatal("unchanged control wake consumed explicit Latest goal")
	}
	var bounded map[string]any
	if err := json.Unmarshal(recoveryView(2, false, nil), &bounded); err != nil {
		t.Fatal(err)
	}
	bounded["state"], bounded["live"] = "synchronizing", false
	data, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: data}); err != nil {
		t.Fatal(err)
	}
	first := s.renderEvent()
	if first.Snapshot == nil || !first.ResetScroll || !first.Follow || first.Snapshot.Metadata.Live || s.scrollGoal != "" {
		t.Fatal("new bounded Latest did not follow, or acquired false live authority")
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(3, false, nil)}); err != nil {
		t.Fatal(err)
	}
	live := s.renderEvent()
	if live.Snapshot == nil || !live.Snapshot.Metadata.Live || live.ResetScroll || live.Follow {
		t.Fatal("completion marker lost authority update or reset reading twice")
	}
}
