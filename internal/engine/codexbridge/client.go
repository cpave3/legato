package codexbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"nhooyr.io/websocket"
)

// frame is one JSON-RPC message of the Codex app-server protocol. Codex
// leaves out the "jsonrpc" member, and so does the bridge.
type frame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("codex app server: %s (code %d)", e.Message, e.Code)
}

// frameLimit bounds one message from the app server. A resumed thread
// arrives whole, with every turn's tool output.
const frameLimit = 256 << 20

// rpcClient is the bridge's connection to a Codex app server: one client
// among several, the Codex terminal UI being another.
type rpcClient struct {
	conn *websocket.Conn

	writeMu sync.Mutex

	mu      sync.Mutex
	next    int64
	waiting map[string]chan frame
	// done is closed once no further answer can arrive.
	done     chan struct{}
	shutOnce sync.Once
}

func dialAppServer(ctx context.Context, url, token string) (*rpcClient, error) {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, fmt.Errorf("connecting to the Codex app server: %w", err)
	}
	conn.SetReadLimit(frameLimit)
	return &rpcClient{conn: conn, waiting: map[string]chan frame{}, done: make(chan struct{})}, nil
}

func (c *rpcClient) write(ctx context.Context, message frame) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// call makes a request and waits for its answer. It must not be made from
// the goroutine that reads the connection, which is what delivers answers.
func (c *rpcClient) call(ctx context.Context, method string, params, result any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	select {
	case <-c.done:
		return errConnectionClosed
	default:
	}
	c.mu.Lock()
	c.next++
	id := "legato-" + strconv.FormatInt(c.next, 10)
	answered := make(chan frame, 1)
	c.waiting[id] = answered
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}()

	rawID, _ := json.Marshal(id)
	if err := c.write(ctx, frame{ID: rawID, Method: method, Params: encoded}); err != nil {
		return fmt.Errorf("calling %s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errConnectionClosed
	case answer := <-answered:
		if answer.Error != nil {
			return answer.Error
		}
		if result == nil || len(answer.Result) == 0 {
			return nil
		}
		return json.Unmarshal(answer.Result, result)
	}
}

func (c *rpcClient) notify(ctx context.Context, method string) error {
	return c.write(ctx, frame{Method: method})
}

// respond answers a request the app server made of its clients.
func (c *rpcClient) respond(ctx context.Context, id json.RawMessage, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.write(ctx, frame{ID: id, Result: encoded})
}

var errConnectionClosed = errors.New("the Codex app server connection is closed")

// read returns the next notification or request from the app server. Answers
// to the bridge's own calls are handed to their callers on the way.
func (c *rpcClient) read(ctx context.Context) (frame, error) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.shut()
			return frame{}, err
		}
		var incoming frame
		if json.Unmarshal(data, &incoming) != nil {
			continue
		}
		if incoming.Method != "" {
			return incoming, nil
		}
		var id string
		if json.Unmarshal(incoming.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		answered := c.waiting[id]
		c.mu.Unlock()
		if answered != nil {
			answered <- incoming
		}
	}
}

// shut fails every call still waiting: no answer is coming.
func (c *rpcClient) shut() {
	c.shutOnce.Do(func() { close(c.done) })
}

func (c *rpcClient) close() {
	c.shut()
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}
