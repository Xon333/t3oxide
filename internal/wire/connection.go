package wire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/config"
)

type Fault struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Uncertain bool   `json:"uncertain"`
}

func (f *Fault) Error() string   { return f.Message }
func IsUncertain(err error) bool { var fault *Fault; return errors.As(err, &fault) && fault.Uncertain }

type Packet struct {
	V             int             `json:"v"`
	EnvironmentID string          `json:"environmentId"`
	ID            string          `json:"id,omitempty"`
	Event         string          `json:"event,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         *Fault          `json:"error,omitempty"`
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic random operation IDs unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}

type Options struct {
	RequestTimeout time.Duration
	WriteTimeout   time.Duration
}
type response struct {
	result map[string]any
	err    error
}
type pendingRequest struct {
	stop  bool
	reply chan response
}
type writeFrame struct {
	data []byte
	stop bool
}
type queuedEvent struct {
	packet Packet
	bytes  int
}

// Connection owns exactly one OpenSSH or local adapter process. A dedicated
// writer chooses Stop before unsent regular frames; a partially written NDJSON
// frame is never interleaved. Responses do not block that writer, so a stalled
// ordinary acknowledgement cannot occupy Stop's admission or wire path.
type Connection struct {
	mu                          sync.Mutex
	environment                 config.Environment
	options                     Options
	cmd                         *exec.Cmd
	input                       *os.File
	output                      io.ReadCloser
	stderr                      io.ReadCloser
	done                        chan struct{}
	reaped                      chan struct{}
	outputDone                  chan struct{}
	events                      chan Packet
	wake                        chan struct{}
	eventWake                   chan struct{}
	closed                      bool
	err                         error
	pending                     map[string]*pendingRequest
	regularPending, stopPending int
	regularWrites, stopWrites   int
	regularQueue, stopQueue     []writeFrame
	eventQueue                  []queuedEvent
	eventBytes                  int
}

func Start(ctx context.Context, environment config.Environment) (*Connection, error) {
	return StartWithOptions(ctx, environment, Options{})
}

func StartWithOptions(ctx context.Context, environment config.Environment, options Options) (*Connection, error) {
	if err := environment.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 50 * time.Second
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = 10 * time.Second
	}
	executable, args := environment.Command()
	cmd := exec.Command(executable, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	input, ok := stdin.(*os.File)
	if !ok {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, errors.New("adapter input is not a pipe")
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start host adapter: %w", err)
	}
	c := &Connection{environment: environment, options: options, cmd: cmd, input: input, output: stdout, stderr: stderr, done: make(chan struct{}), reaped: make(chan struct{}), outputDone: make(chan struct{}), events: make(chan Packet), wake: make(chan struct{}, 1), eventWake: make(chan struct{}, 1), pending: map[string]*pendingRequest{}}
	go c.readOutput()
	go c.writeInput()
	go c.deliverEvents()
	go func() { _, _ = io.Copy(io.Discard, stderr) }() // Drain diagnostics without copying host secrets into the UI.
	go func() {
		// Wait closes StdoutPipe. Drain complete frames first so an immediately
		// exiting helper cannot lose its final authoritative acknowledgement.
		<-c.outputDone
		err := cmd.Wait()
		close(c.reaped)
		if err != nil {
			c.finish(errors.New("host adapter process ended; check SSH and host configuration"))
		} else {
			c.finish(io.EOF)
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.finish(ctx.Err())
		case <-c.done:
		}
	}()
	return c, nil
}

func (c *Connection) Events() <-chan Packet { return c.events }
func (c *Connection) Done() <-chan struct{} { return c.done }
func (c *Connection) Err() error            { c.mu.Lock(); defer c.mu.Unlock(); return c.err }
func (c *Connection) Close() {
	c.finish(errors.New("connection closed; unacknowledged operations remain saved"))
}
func (c *Connection) WaitClosed(ctx context.Context) error {
	select {
	case <-c.reaped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Request never retries. Callers must durably save mutation IDs before calling
// it and retain them for every ambiguous outcome. An empty id is for reads.
func (c *Connection) Request(ctx context.Context, op string, fields map[string]any, id string) (map[string]any, error) {
	if id == "" {
		id = NewID()
	}
	if !identifier(id, MaxIdentifierBytes) {
		return nil, &Fault{Code: "invalid_request", Message: "invalid request ID"}
	}
	switch op {
	case "hello", "list", "watch", "history", "models", "activity", "inspect-item", "select-model", "send", "stop", "answer", "launch", "projects", "launch-defaults", "launch-project", "thread-action", "thread-review", "thread-preview", "native-defaults":
	default:
		return nil, &Fault{Code: "invalid_request", Message: "unsupported companion operation"}
	}
	packet := make(map[string]any, len(fields)+4)
	for k, v := range fields {
		packet[k] = v
	}
	packet["v"] = 1
	packet["id"] = id
	packet["op"] = op
	packet["environmentId"] = c.environment.EnvironmentID
	var encoded bytes.Buffer
	enc := json.NewEncoder(&encoded)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(packet); err != nil {
		return nil, &Fault{Code: "invalid_request", Message: "request could not be encoded"}
	}
	if encoded.Len()-1 > MaxInputBytes {
		return nil, &Fault{Code: "too_large", Message: "request exceeds 96 KiB; nothing sent"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := &pendingRequest{stop: op == "stop", reply: make(chan response, 1)}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, &Fault{Code: "disconnected", Message: "not connected; nothing sent"}
	}
	if c.pending[id] != nil || p.stop && (c.stopPending >= 1 || c.stopWrites >= 1) || !p.stop && (c.regularPending >= 8 || c.regularWrites >= 8) {
		c.mu.Unlock()
		return nil, &Fault{Code: "backpressure", Message: "connection request lane is full; nothing sent"}
	}
	c.pending[id] = p
	f := writeFrame{append([]byte(nil), encoded.Bytes()...), p.stop}
	if p.stop {
		c.stopPending++
		c.stopWrites++
		c.stopQueue = append(c.stopQueue, f)
	} else {
		c.regularPending++
		c.regularWrites++
		c.regularQueue = append(c.regularQueue, f)
	}
	c.mu.Unlock()
	signal(c.wake)
	defer c.removePending(id, p)
	wait, cancel := context.WithTimeout(ctx, c.options.RequestTimeout)
	defer cancel()
	select {
	case reply := <-p.reply:
		return reply.result, reply.err
	case <-wait.Done():
		select {
		case reply := <-p.reply:
			return reply.result, reply.err
		default:
		}
		return nil, &Fault{Code: "timeout", Message: "no acknowledgement; acceptance is unknown—retry only the saved operation ID", Uncertain: true}
	case <-c.done:
		select {
		case reply := <-p.reply:
			return reply.result, reply.err
		default:
		}
		var fault *Fault
		if errors.As(c.Err(), &fault) {
			return nil, &Fault{Code: fault.Code, Message: fault.Message + "; acceptance is unknown—retry only the saved operation ID", Uncertain: true}
		}
		return nil, &Fault{Code: "disconnected", Message: "connection closed; acceptance is unknown—retry only the saved operation ID", Uncertain: true}
	}
}

func (c *Connection) removePending(id string, p *pendingRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[id] != p {
		return
	}
	delete(c.pending, id)
	if p.stop {
		c.stopPending--
	} else {
		c.regularPending--
	}
}
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (c *Connection) writeInput() {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		var frame writeFrame
		if len(c.stopQueue) > 0 {
			frame = c.stopQueue[0]
			c.stopQueue = c.stopQueue[1:]
		} else if len(c.regularQueue) > 0 {
			frame = c.regularQueue[0]
			c.regularQueue = c.regularQueue[1:]
		} else {
			c.mu.Unlock()
			select {
			case <-c.wake:
				continue
			case <-c.done:
				return
			}
		}
		c.mu.Unlock()
		if err := c.input.SetWriteDeadline(time.Now().Add(c.options.WriteTimeout)); err != nil {
			c.finish(errors.New("could not set bounded adapter pipe deadline"))
			return
		}
		data := frame.data
		for len(data) > 0 {
			n, err := c.input.Write(data)
			if err != nil {
				c.finish(errors.New("adapter input closed or stalled; operation acceptance is unknown"))
				return
			}
			if n == 0 {
				c.finish(io.ErrShortWrite)
				return
			}
			data = data[n:]
		}
		c.mu.Lock()
		if frame.stop {
			c.stopWrites--
		} else {
			c.regularWrites--
		}
		c.mu.Unlock()
	}
}

// readFrame never allocates an unbounded line, and accepts a partial frame only
// after its newline. EOF mid-frame is a protocol failure, not a valid packet.
func readFrame(reader *bufio.Reader) ([]byte, error) {
	var assembled []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(assembled) > 0 {
				return nil, errors.New("adapter output ended with an incomplete frame")
			}
			return nil, err
		}
		if b == '\n' {
			return assembled, nil
		}
		// Check every available byte: ReadSlice would wait for its next full
		// internal buffer after a hostile sender crosses the limit by one byte.
		if len(assembled) == MaxFrameBytes {
			return nil, errors.New("adapter frame exceeds 384 KiB")
		}
		assembled = append(assembled, b)
	}
}

func (c *Connection) readOutput() {
	defer close(c.outputDone)
	reader := bufio.NewReaderSize(c.output, 32*1024)
	for {
		frame, err := readFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.finish(err)
			} else {
				c.finish(&Fault{Code: "incompatible", Message: err.Error()})
			}
			return
		}
		if len(frame) == 0 {
			continue
		}
		if !utf8.Valid(frame) {
			c.finish(&Fault{Code: "incompatible", Message: "adapter output is not UTF-8"})
			return
		}
		var packet Packet
		if json.Unmarshal(frame, &packet) != nil || packet.V != 1 || packet.ID == "" && packet.Event == "" || packet.ID != "" && packet.Event != "" {
			c.finish(&Fault{Code: "incompatible", Message: "adapter protocol does not match"})
			return
		}
		if packet.EnvironmentID != c.environment.EnvironmentID {
			c.finish(&Fault{Code: "identity_mismatch", Message: "adapter environment identity does not match"})
			return
		}
		if packet.ID != "" {
			var result map[string]any
			var replyErr error
			if packet.Error != nil {
				replyErr = packet.Error
			} else {
				dec := json.NewDecoder(bytes.NewReader(packet.Result))
				dec.UseNumber()
				if dec.Decode(&result) != nil || result == nil {
					c.finish(&Fault{Code: "incompatible", Message: "adapter acknowledgement is not an object"})
					return
				}
			}
			c.mu.Lock()
			if p := c.pending[packet.ID]; p != nil {
				select {
				case p.reply <- response{result, replyErr}:
				default:
				}
			}
			c.mu.Unlock()
			continue
		}
		if _, err := parseObject(packet.Data); err != nil {
			c.finish(&Fault{Code: "incompatible", Message: "adapter event data is not an object"})
			return
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		if len(c.eventQueue) >= 1024 || c.eventBytes+len(frame) > 2*1024*1024 {
			c.mu.Unlock()
			c.finish(errors.New("adapter consumer stalled; reconnect for a fresh snapshot"))
			return
		}
		c.eventQueue = append(c.eventQueue, queuedEvent{packet, len(frame)})
		c.eventBytes += len(frame)
		c.mu.Unlock()
		signal(c.eventWake)
	}
}

func (c *Connection) deliverEvents() {
	defer close(c.events)
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		if len(c.eventQueue) == 0 {
			c.mu.Unlock()
			select {
			case <-c.eventWake:
				continue
			case <-c.done:
				return
			}
		}
		event := c.eventQueue[0]
		c.mu.Unlock()
		select {
		case c.events <- event.packet:
		case <-c.done:
			return
		}
		c.mu.Lock()
		if len(c.eventQueue) > 0 {
			c.eventQueue[0] = queuedEvent{}
			c.eventQueue = c.eventQueue[1:]
			c.eventBytes -= event.bytes
		}
		c.mu.Unlock()
	}
}

func (c *Connection) finish(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.err = err
	close(c.done)
	c.regularQueue = nil
	c.stopQueue = nil
	c.eventQueue = nil
	c.eventBytes = 0
	c.mu.Unlock()
	// Closing pipe endpoints unblocks reads and writes. EOF gives a normal
	// adapter its graceful shutdown; escalation targets only this owned PID.
	_ = c.input.Close()
	_ = c.output.Close()
	_ = c.stderr.Close()
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-c.reaped:
			return
		case <-timer.C:
		}
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		timer.Reset(time.Second)
		select {
		case <-c.reaped:
			return
		case <-timer.C:
		}
		_ = c.cmd.Process.Kill()
	}()
}
