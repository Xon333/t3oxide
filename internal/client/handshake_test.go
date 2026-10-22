package client

import (
	"context"
	"errors"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestEarlyConnectedStatusCannotAuthorizeRetainedConversation(t *testing.T) {
	s, state, path := recoverySession(t, "out={'result':{'protocol':99,'environmentId':'fixture-env','orchestrationProtocol':2,'writesEnabled':True}}")
	s.mu.Lock()
	s.retained = s.store.Snapshot()
	s.store.Reset()
	s.store.Select("thread")
	s.ready, s.writable, s.helloVerified = false, false, false
	s.mu.Unlock()
	connected := wire.Packet{Event: "status", Data: []byte(`{"state":"connected","writesEnabled":true}`)}
	if err := s.applyPacket(connected); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); status.Connected || status.Writable {
		t.Fatalf("early status bypassed identity handshake: %#v", status)
	}
	if err := s.Send(context.Background(), "must remain local"); err == nil {
		t.Fatal("retained snapshot authorized pre-handshake send")
	}
	if state.Pending("fixture-env", false) != nil || len(requestsFromLog(t, path)) != 0 {
		t.Fatal("pre-handshake send created recovery state or reached host")
	}
	if err := s.prepare(context.Background(), s.conn); err == nil {
		t.Fatal("wrong protocol hello accepted")
	}
	if err := s.applyPacket(connected); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(); status.Connected || status.Writable {
		t.Fatal("connected status bypassed rejected hello")
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 1 || packets[0]["op"] != "hello" {
		t.Fatalf("invalid hello continued setup or wrote: %#v", packets)
	}
}

func TestValidatedHelperCanRecoverT3ConnectionStatus(t *testing.T) {
	s, _, _ := recoverySession(t, "out={'result':{'sequence':10}}")
	for _, state := range []string{"disconnected", "connected"} {
		if err := s.applyPacket(wire.Packet{Event: "status", Data: []byte(`{"state":"` + state + `","writesEnabled":true}`)}); err != nil {
			t.Fatal(err)
		}
		if got := s.Status().Connected; got != (state == "connected") {
			t.Fatalf("status %s: connected=%v", state, got)
		}
	}
	s.Close()
	if err := s.applyPacket(wire.Packet{Event: "status", Data: []byte(`{"state":"connected","writesEnabled":true}`)}); err != nil {
		t.Fatal(err)
	}
	if s.Status().Connected {
		t.Fatal("closed helper remained verified")
	}
}

func TestRetainedLivePageCannotAuthorizeOrdinaryWritesAfterHello(t *testing.T) {
	s, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	s.mu.Lock()
	s.retained = s.store.Snapshot()
	s.store.Select("thread")
	// Hello succeeded on the replacement helper; watch has not produced its
	// authoritative page yet. The old page remains visible for reading.
	s.ready, s.writable, s.helloVerified = true, true, true
	s.mu.Unlock()
	for _, op := range []string{"send", "answer", "launch"} {
		if _, err := s.perform(context.Background(), op, map[string]any{"threadId": "thread", "message": "local only"}, "", false); err == nil {
			t.Fatalf("%s used retained page as current helper authority", op)
		}
	}
	if state.Pending("fixture-env", false) != nil || len(requestsFromLog(t, path)) != 0 {
		t.Fatal("retained page admitted a write or created a recovery record")
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("reviewed Stop should remain available while synchronizing: %v", err)
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), "new authoritative page"); err != nil {
		t.Fatal(err)
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 2 || packets[0]["op"] != "stop" || packets[1]["op"] != "send" {
		t.Fatalf("unexpected operation admission: %#v", packets)
	}
}

func TestConsumeReportsCompletedSetupForReconnectBackoff(t *testing.T) {
	for _, complete := range []bool{false, true} {
		s, _, _ := recoverySession(t, "out={'result':{'sequence':10}}")
		setup := make(chan error, 1)
		if complete {
			setup <- nil
		} else {
			setup <- errors.New("setup rejected")
		}
		synchronized, err := s.consume(context.Background(), s.conn, setup, func(Event, error) bool { return false })
		if synchronized != complete || err == nil {
			t.Fatalf("setup complete=%v, synchronized=%v, err=%v", complete, synchronized, err)
		}
	}
}

func TestRetainedPageCannotConsumeReconnectingWatchScrollGoal(t *testing.T) {
	m, _, env := recoveryManager(t)
	s, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	s.store.Select("thread")
	s.scrollGoal = "live"
	retained := s.renderEvent()
	if retained.Snapshot == nil || retained.ResetScroll || retained.Follow || s.scrollGoal != "live" {
		t.Fatalf("retained page consumed navigation: %#v, goal=%q", retained, s.scrollGoal)
	}
	// Revision one on a new helper is valid and must finish this watch, even
	// when the retained helper also last used revision one.
	if err = s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	current := s.renderEvent()
	if !current.ResetScroll || !current.Follow || s.scrollGoal != "" || current.Revision <= retained.Revision {
		t.Fatalf("new authoritative page did not complete navigation: %#v, goal=%q", current, s.scrollGoal)
	}
	if repeated := s.renderEvent(); repeated.ResetScroll || repeated.Follow {
		t.Fatal("completed scroll goal was emitted twice")
	}
}
