package wire

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
)

func fixtureConnection(t *testing.T, body string, options Options) *Connection {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 needed for isolated process fixture")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fixture.py")
	header := "import sys, json, time, os\ndef emit(p):\n p.update(v=1, environmentId='fixture-env')\n sys.stdout.write(json.dumps(p)+'\\n'); sys.stdout.flush()\n"
	if err = os.WriteFile(script, []byte(header+body), 0600); err != nil {
		t.Fatal(err)
	}
	env := config.Environment{ID: "fixture", Label: "Fixture", EnvironmentID: "fixture-env", Node: python, Adapter: script, HostConfig: filepath.Join(dir, "host.json"), Enabled: true}
	c, err := StartWithOptions(context.Background(), env, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := c.WaitClosed(ctx); err != nil {
			t.Error("owned process was not reaped:", err)
		}
	})
	return c
}
func nextPacket(t *testing.T, c *Connection) Packet {
	t.Helper()
	select {
	case p, ok := <-c.Events():
		if !ok {
			t.Fatal("events closed:", c.Err())
		}
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("event timed out")
	}
	return Packet{}
}

func TestSmallFramesAreDeliveredWithoutFillingReadBuffer(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line)
 frame=json.dumps(dict(v=1,environmentId='fixture-env',id=p['id'],result={'ok':True}))+'\n'
 for ch in frame:
  sys.stdout.write(ch);sys.stdout.flush()
`, Options{RequestTimeout: 2 * time.Second})
	result, err := c.Request(context.Background(), "hello", nil, "")
	if err != nil || result["ok"] != true {
		t.Fatalf("small frame did not arrive: %v %v", result, err)
	}
}
func TestFinalAcknowledgementSurvivesImmediateProcessExit(t *testing.T) {
	for i := 0; i < 5; i++ {
		c := fixtureConnection(t, `p=json.loads(sys.stdin.readline());emit({'id':p['id'],'result':{'sequence':7}})
`, Options{RequestTimeout: 2 * time.Second})
		result, err := c.Request(context.Background(), "send", map[string]any{"threadId": "thread", "message": "hello"}, NewID())
		if err != nil || result["sequence"] != json.Number("7") {
			t.Fatalf("final ACK lost: %v %v", result, err)
		}
	}
}
func TestStopHasReservedAdmissionWhenAllNormalAcknowledgementsStall(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line)
 emit({'event':'seen','data':{'op':p['op'],'id':p['id']}})
 if p['op']=='stop':emit({'id':p['id'],'result':{'sequence':8}})
`, Options{RequestTimeout: 3 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Request(ctx, "send", map[string]any{"threadId": "thread", "message": "hello"}, NewID())
		}()
	}
	for i := 0; i < 8; i++ {
		nextPacket(t, c)
	}
	if _, err := c.Request(ctx, "list", nil, ""); err == nil || IsUncertain(err) {
		t.Fatalf("full normal lane did not reject before send: %v", err)
	}
	start := time.Now()
	result, err := c.Request(ctx, "stop", map[string]any{"threadId": "thread", "runId": "run"}, "stable-stop")
	if err != nil || result["sequence"] != json.Number("8") {
		t.Fatalf("Stop blocked by normal lane: %v %v", result, err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Stop waited for normal acknowledgement")
	}
	cancel()
	wg.Wait()
}
func TestWriterPrioritizesStopOverUnstartedNormalFrames(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line);emit({'event':'seen','data':{'op':p['op']}})
`, Options{})
	c.mu.Lock()
	c.regularQueue = []writeFrame{{data: []byte("{\"op\":\"send\"}\n")}}
	c.regularWrites = 1
	c.stopQueue = []writeFrame{{data: []byte("{\"op\":\"stop\"}\n"), stop: true}}
	c.stopWrites = 1
	c.mu.Unlock()
	signal(c.wake)
	first, second := nextPacket(t, c), nextPacket(t, c)
	var a, b map[string]string
	_ = json.Unmarshal(first.Data, &a)
	_ = json.Unmarshal(second.Data, &b)
	if a["op"] != "stop" || b["op"] != "send" {
		t.Fatalf("wrong queue order: %v %v", a, b)
	}
}
func TestStructuredDefiniteAndUncertainErrorsArePreserved(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line);emit({'id':p['id'],'error':{'code':'stale','message':'Review current request','uncertain':p['id']=='uncertain'}})
`, Options{})
	for _, id := range []string{"definite", "uncertain"} {
		_, err := c.Request(context.Background(), "answer", map[string]any{"threadId": "thread"}, id)
		var fault *Fault
		if !errors.As(err, &fault) || fault.Code != "stale" || IsUncertain(err) != (id == "uncertain") {
			t.Fatalf("lost error semantics: %v", err)
		}
	}
}
func TestTimeoutAndDisconnectNeverAutomaticallyReplayWrites(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line);emit({'event':'seen','data':{'id':p['id']}})
`, Options{RequestTimeout: 100 * time.Millisecond})
	_, err := c.Request(context.Background(), "send", map[string]any{"threadId": "thread", "message": "hello"}, "same-id")
	if !IsUncertain(err) {
		t.Fatal("lost ACK must be uncertain:", err)
	}
	nextPacket(t, c)
	select {
	case p := <-c.Events():
		t.Fatalf("automatic retry emitted another write: %v", p)
	case <-time.After(200 * time.Millisecond):
	}
	c.Close()
	_, err = c.Request(context.Background(), "send", nil, NewID())
	if err == nil || IsUncertain(err) {
		t.Fatal("pre-admission disconnect should report nothing sent")
	}
}
func TestBlockedPipeWriteHasDeadlineAndCleansUpOwnedProcess(t *testing.T) {
	c := fixtureConnection(t, "time.sleep(30)\n", Options{RequestTimeout: 2 * time.Second, WriteTimeout: 100 * time.Millisecond})
	_, err := c.Request(context.Background(), "send", map[string]any{"threadId": "thread", "message": strings.Repeat("x", 90*1024)}, "blocked-write")
	if !IsUncertain(err) {
		t.Fatalf("blocked pipe did not preserve uncertainty: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("write deadline did not close stalled pipe")
	}
}
func TestMalformedIdentityAndOversizedFramesFailClosed(t *testing.T) {
	cases := map[string]string{
		"identity":     "sys.stdout.write('{\"v\":1,\"environmentId\":\"wrong\",\"event\":\"status\",\"data\":{}}\\n');sys.stdout.flush();time.sleep(30)\n",
		"malformed":    "sys.stdout.write('{not json}\\n');sys.stdout.flush();time.sleep(30)\n",
		"unterminated": "sys.stdout.write('{\"v\":1');sys.stdout.flush()\n",
		"oversized":    "sys.stdout.write('x'* (384*1024+2));sys.stdout.flush();time.sleep(30)\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c := fixtureConnection(t, body, Options{})
			select {
			case <-c.Done():
				if c.Err() == nil {
					t.Fatal("protocol error not retained")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("invalid frame accepted or reader stalled")
			}
		})
	}
}
func TestStalledEventConsumerIsBounded(t *testing.T) {
	c := fixtureConnection(t, "for i in range(12):emit({'event':'status','data':{'message':'x'*(300*1024)}})\ntime.sleep(30)\n", Options{})
	select {
	case <-c.Done():
		if c.Err() == nil || !strings.Contains(c.Err().Error(), "consumer stalled") {
			t.Fatalf("unexpected error: %v", c.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unbounded event delivery queue")
	}
}
func TestFrameReaderRejectsPartialAndOversizedButHandlesMultiple(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader("one\ntwo\n"), 16)
	for _, want := range []string{"one", "two"} {
		b, err := readFrame(r)
		if err != nil || string(b) != want {
			t.Fatalf("%q %v", b, err)
		}
	}
	if _, err := readFrame(r); err != io.EOF {
		t.Fatal(err)
	}
	if _, err := readFrame(bufio.NewReader(strings.NewReader("partial"))); err == nil || err == io.EOF {
		t.Fatal("partial frame silently discarded")
	}
	if _, err := readFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", MaxFrameBytes+1) + "\n"))); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
