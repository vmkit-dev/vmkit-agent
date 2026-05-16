package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"
)

type Handler func(ctx context.Context, params json.RawMessage) (any, error)

type Config struct {
	GatewayURL    string
	InstanceID    string
	BootstrapPath string
	SessionPath   string
	LogLevel      slog.Level
}

type Daemon struct {
	gatewayURL    string
	instanceID    string
	sessionPath   string
	bootstrapPath string

	conn    *websocket.Conn
	sendCh  chan json.RawMessage
	handlers map[string]Handler
	pending  map[string]chan json.RawMessage
	mu       sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
	start  time.Time
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      *string         `json:"id,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      string          `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type helloParams struct {
	InstanceID string `json:"instance_id"`
	Token      string `json:"token"`
	Version    string `json:"version"`
}

type welcomeResult struct {
	SessionID                  string  `json:"session_id"`
	SessionToken               *string `json:"session_token,omitempty"`
	HeartbeatIntervalSeconds   int     `json:"heartbeat_interval_seconds"`
}

var Version = "0.1.0"

func New(cfg Config) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	return &Daemon{
		gatewayURL:    cfg.GatewayURL,
		instanceID:    cfg.InstanceID,
		sessionPath:   cfg.SessionPath,
		bootstrapPath: cfg.BootstrapPath,
		sendCh:        make(chan json.RawMessage, 64),
		handlers:      make(map[string]Handler),
		pending:       make(map[string]chan json.RawMessage),
		ctx:           ctx,
		cancel:        cancel,
		logger:        slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})),
		start:         time.Now(),
	}
}

func (d *Daemon) RegisterHandler(method string, h Handler) {
	d.handlers[method] = h
}

func (d *Daemon) Shutdown() {
	d.cancel()
}

func (d *Daemon) Run() error {
	backoff := time.Second
	for d.ctx.Err() == nil {
		err := d.runOnce()
		if err == nil {
			backoff = time.Second
			continue
		}
		if d.ctx.Err() != nil {
			return d.ctx.Err()
		}
		d.logger.Warn("connection failed, retrying", "error", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-d.ctx.Done():
			return d.ctx.Err()
		}
		backoff = min(backoff*2, 60*time.Second)
	}
	return d.ctx.Err()
}

func (d *Daemon) runOnce() error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(d.ctx, d.gatewayURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	d.conn = conn
	defer func() {
		conn.Close()
		d.conn = nil
	}()

	if err := d.sendHello(); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if err := d.recvWelcome(); err != nil {
		return fmt.Errorf("welcome: %w", err)
	}

	d.logger.Info("connected to gateway", "instance_id", d.instanceID)

	g, gctx := errgroup.WithContext(d.ctx)
	g.Go(func() error { return d.readLoop(gctx) })
	g.Go(func() error { return d.writeLoop(gctx) })
	g.Go(func() error { return d.heartbeatLoop(gctx) })

	// When any goroutine exits, close the connection to unblock the others.
	g.Go(func() error {
		<-gctx.Done()
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		return conn.Close()
	})

	return g.Wait()
}

func (d *Daemon) readToken() (string, error) {
	if data, err := os.ReadFile(d.sessionPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), nil
	}
	data, err := os.ReadFile(d.bootstrapPath)
	if err != nil {
		return "", fmt.Errorf("no token available: cannot read bootstrap (%s) or session (%s)", d.bootstrapPath, d.sessionPath)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("bootstrap token file is empty: %s", d.bootstrapPath)
	}
	return strings.TrimSpace(string(data)), nil
}

func (d *Daemon) sendHello() error {
	token, err := d.readToken()
	if err != nil {
		return err
	}

	params, _ := json.Marshal(helloParams{
		InstanceID: d.instanceID,
		Token:      token,
		Version:    Version,
	})

	id := "hello-1"
	msg := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "hello",
		Params:  params,
		ID:      &id,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal hello: %w", err)
	}
	d.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return d.conn.WriteMessage(websocket.TextMessage, data)
}

func (d *Daemon) recvWelcome() error {
	d.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, data, err := d.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	d.conn.SetReadDeadline(time.Time{})

	var resp jsonRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("unmarshal welcome: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("welcome error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		return fmt.Errorf("welcome missing result")
	}

	var welcome welcomeResult
	if err := json.Unmarshal(resp.Result, &welcome); err != nil {
		return fmt.Errorf("unmarshal welcome result: %w", err)
	}

	if welcome.SessionToken != nil {
		if err := d.rotateToken(*welcome.SessionToken); err != nil {
			d.logger.Error("failed to persist session token", "error", err)
		}
	}

	d.logger.Info("handshake complete", "session_id", welcome.SessionID)
	return nil
}

func (d *Daemon) rotateToken(sessionToken string) error {
	if err := os.WriteFile(d.sessionPath, []byte(sessionToken), 0600); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	if err := os.Remove(d.bootstrapPath); err != nil && !os.IsNotExist(err) {
		d.logger.Warn("failed to remove bootstrap token file", "error", err)
	}
	d.logger.Info("rotated to session token", "session_path", d.sessionPath)
	return nil
}

func (d *Daemon) readLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, data, err := d.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		var frame struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method,omitempty"`
			Params  json.RawMessage `json:"params,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *jsonRPCError   `json:"error,omitempty"`
			ID      *string         `json:"id,omitempty"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			d.logger.Warn("invalid JSON-RPC frame", "error", err)
			continue
		}

		switch {
		case frame.ID != nil && frame.Method == "":
			// Response to a pending RPC we sent
			d.mu.Lock()
			ch, ok := d.pending[*frame.ID]
			if ok {
				delete(d.pending, *frame.ID)
			}
			d.mu.Unlock()
			if ok {
				ch <- data
			}

		case frame.Method != "" && frame.ID != nil:
			// Incoming RPC request — dispatch to handler
			go d.handleRequest(ctx, frame.Method, frame.Params, *frame.ID)

		case frame.Method != "" && frame.ID == nil:
			// Notification (no response expected)
			go d.handleNotification(ctx, frame.Method, frame.Params)
		}
	}
}

func (d *Daemon) handleRequest(ctx context.Context, method string, params json.RawMessage, id string) {
	handler, ok := d.handlers[method]
	if !ok {
		d.sendResponse(id, nil, &jsonRPCError{Code: -32601, Message: "method not found: " + method})
		return
	}

	result, err := handler(ctx, params)
	if err != nil {
		d.sendResponse(id, nil, &jsonRPCError{Code: -32000, Message: err.Error()})
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		d.sendResponse(id, nil, &jsonRPCError{Code: -32603, Message: "failed to marshal result"})
		return
	}
	d.sendResponse(id, resultJSON, nil)
}

func (d *Daemon) handleNotification(ctx context.Context, method string, params json.RawMessage) {
	handler, ok := d.handlers[method]
	if !ok {
		d.logger.Warn("no handler for notification", "method", method)
		return
	}
	if _, err := handler(ctx, params); err != nil {
		d.logger.Error("notification handler failed", "method", method, "error", err)
	}
}

func (d *Daemon) sendResponse(id string, result json.RawMessage, rpcErr *jsonRPCError) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		d.logger.Error("failed to marshal response", "error", err)
		return
	}
	select {
	case d.sendCh <- data:
	default:
		d.logger.Warn("send channel full, dropping response", "id", id)
	}
}

func (d *Daemon) writeLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-d.sendCh:
			d.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := d.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return fmt.Errorf("write: %w", err)
			}
		}
	}
}

func (d *Daemon) heartbeatLoop(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			params, _ := json.Marshal(map[string]any{
				"uptime_seconds": int(time.Since(d.start).Seconds()),
			})
			msg := jsonRPCRequest{
				JSONRPC: "2.0",
				Method:  "heartbeat",
				Params:  params,
			}
			data, _ := json.Marshal(msg)
			select {
			case d.sendCh <- data:
			default:
				d.logger.Warn("send channel full, dropping heartbeat")
			}
		}
	}
}
