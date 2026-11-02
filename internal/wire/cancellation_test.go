package wire

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadCancellationNegotiatedExactIDAndLegacySilence(t *testing.T) {
	for _, feature := range []string{"True", "False"} {
		t.Run(feature, func(t *testing.T) {
			c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line)
 if p['op']=='hello': emit({'id':p['id'],'result':{'features':{'readCancellation':`+feature+`}}})
 elif p['op']=='list': emit({'event':'started','data':{'id':p['id']}})
 elif p['op']=='cancel-read': emit({'event':'cancelled','data':{'id':p['requestId']}})
 else: emit({'id':p['id'],'result':{}})
`, Options{RequestTimeout: time.Second})
			if _, err := c.Request(context.Background(), "hello", nil, ""); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { _, err := c.Request(ctx, "list", nil, "exact-read-id"); done <- err }()
			nextPacket(t, c)
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if feature == "True" {
				p := nextPacket(t, c)
				if string(p.Data) != `{"id": "exact-read-id"}` {
					t.Fatal(string(p.Data))
				}
			}
			if _, err := c.Request(context.Background(), "projects", nil, ""); err != nil {
				t.Fatal(err)
			}
			if feature == "False" {
				select {
				case p := <-c.Events():
					t.Fatalf("legacy received cancellation: %+v", p)
				default:
				}
			}
		})
	}
}

func TestCancellationAllowlistExcludesEveryMutationAndHello(t *testing.T) {
	for _, op := range []string{"hello", "send", "stop", "answer", "launch", "launch-project", "thread-action", "select-model", "cancel-read"} {
		if cancellableRead(op) {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"list", "watch", "history", "models", "activity", "inspect-item", "projects", "launch-defaults", "native-defaults", "thread-preview", "thread-review"} {
		if !cancellableRead(op) {
			t.Fatal(op)
		}
	}
}
