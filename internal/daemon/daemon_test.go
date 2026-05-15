package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func newTestDaemon(url, tokenDir string) *Daemon {
	return New(Config{
		GatewayURL:    url,
		InstanceID:    "test-instance-001",
		BootstrapPath: filepath.Join(tokenDir, "token"),
		SessionPath:   filepath.Join(tokenDir, "session"),
		LogLevel:      slog.LevelError,
	})
}

func TestHelloWelcomeHandshake(t *testing.T) {
	tokenDir := t.TempDir()
	os.WriteFile(filepath.Join(tokenDir, "token"), []byte("bootstrap-secret"), 0600)

	var gotHello helloParams
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}

		var req jsonRPCRequest
		json.Unmarshal(data, &req)
		json.Unmarshal(req.Params, &gotHello)

		result, _ := json.Marshal(welcomeResult{
			SessionID:                "sess-001",
			HeartbeatIntervalSeconds: 30,
		})
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  result,
			ID:      *req.ID,
		}
		respData, _ := json.Marshal(resp)
		conn.WriteMessage(websocket.TextMessage, respData)

		// Close cleanly after handshake
		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer srv.Close()

	d := newTestDaemon(wsURL(srv), tokenDir)
	defer d.Shutdown()

	go func() {
		time.Sleep(500 * time.Millisecond)
		d.Shutdown()
	}()

	d.Run()

	if gotHello.InstanceID != "test-instance-001" {
		t.Errorf("expected instance_id %q, got %q", "test-instance-001", gotHello.InstanceID)
	}
	if gotHello.Token != "bootstrap-secret" {
		t.Errorf("expected token %q, got %q", "bootstrap-secret", gotHello.Token)
	}
}

func TestSessionTokenRotation(t *testing.T) {
	tokenDir := t.TempDir()
	bootstrapPath := filepath.Join(tokenDir, "token")
	sessionPath := filepath.Join(tokenDir, "session")
	os.WriteFile(bootstrapPath, []byte("bootstrap-secret"), 0600)

	sessionToken := "new-session-token-xyz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		_, data, _ := conn.ReadMessage()
		var req jsonRPCRequest
		json.Unmarshal(data, &req)

		result, _ := json.Marshal(welcomeResult{
			SessionID:                "sess-002",
			SessionToken:             &sessionToken,
			HeartbeatIntervalSeconds: 30,
		})
		resp := jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  result,
			ID:      *req.ID,
		}
		respData, _ := json.Marshal(resp)
		conn.WriteMessage(websocket.TextMessage, respData)

		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer srv.Close()

	d := newTestDaemon(wsURL(srv), tokenDir)
	defer d.Shutdown()

	go func() {
		time.Sleep(500 * time.Millisecond)
		d.Shutdown()
	}()

	d.Run()

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("session file not written: %v", err)
	}
	if string(data) != sessionToken {
		t.Errorf("expected session token %q, got %q", sessionToken, string(data))
	}

	if _, err := os.Stat(bootstrapPath); !os.IsNotExist(err) {
		t.Error("bootstrap token file should have been deleted after rotation")
	}
}

func TestRPCDispatch(t *testing.T) {
	tokenDir := t.TempDir()
	os.WriteFile(filepath.Join(tokenDir, "token"), []byte("tok"), 0600)

	var mu sync.Mutex
	var gotResult json.RawMessage

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		// Read hello
		_, data, _ := conn.ReadMessage()
		var req jsonRPCRequest
		json.Unmarshal(data, &req)

		// Send welcome
		result, _ := json.Marshal(welcomeResult{SessionID: "s1", HeartbeatIntervalSeconds: 30})
		resp := jsonRPCResponse{JSONRPC: "2.0", Result: result, ID: *req.ID}
		respData, _ := json.Marshal(resp)
		conn.WriteMessage(websocket.TextMessage, respData)

		// Send an RPC request to the daemon
		rpcID := "rpc-1"
		params, _ := json.Marshal(map[string]string{"key": "value"})
		rpcReq := jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  "test.echo",
			Params:  params,
			ID:      &rpcID,
		}
		rpcData, _ := json.Marshal(rpcReq)
		conn.WriteMessage(websocket.TextMessage, rpcData)

		// Read the response
		_, respRaw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		mu.Lock()
		gotResult = respRaw
		mu.Unlock()

		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer srv.Close()

	d := newTestDaemon(wsURL(srv), tokenDir)
	defer d.Shutdown()

	d.RegisterHandler("test.echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		var m map[string]string
		json.Unmarshal(params, &m)
		return m, nil
	})

	go func() {
		time.Sleep(2 * time.Second)
		d.Shutdown()
	}()

	d.Run()

	mu.Lock()
	defer mu.Unlock()

	if gotResult == nil {
		t.Fatal("no RPC response received")
	}

	var rpcResp jsonRPCResponse
	json.Unmarshal(gotResult, &rpcResp)

	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	var m map[string]string
	json.Unmarshal(rpcResp.Result, &m)
	if m["key"] != "value" {
		t.Errorf("expected echo result key=value, got %v", m)
	}
}

func TestMethodNotFound(t *testing.T) {
	tokenDir := t.TempDir()
	os.WriteFile(filepath.Join(tokenDir, "token"), []byte("tok"), 0600)

	var mu sync.Mutex
	var gotResult json.RawMessage

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		_, data, _ := conn.ReadMessage()
		var req jsonRPCRequest
		json.Unmarshal(data, &req)

		result, _ := json.Marshal(welcomeResult{SessionID: "s1", HeartbeatIntervalSeconds: 30})
		resp := jsonRPCResponse{JSONRPC: "2.0", Result: result, ID: *req.ID}
		respData, _ := json.Marshal(resp)
		conn.WriteMessage(websocket.TextMessage, respData)

		// Send an RPC request for a method that doesn't exist
		rpcID := "rpc-missing"
		rpcReq := jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  "no.such.method",
			ID:      &rpcID,
		}
		rpcData, _ := json.Marshal(rpcReq)
		conn.WriteMessage(websocket.TextMessage, rpcData)

		_, respRaw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		mu.Lock()
		gotResult = respRaw
		mu.Unlock()

		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer srv.Close()

	d := newTestDaemon(wsURL(srv), tokenDir)
	defer d.Shutdown()

	go func() {
		time.Sleep(2 * time.Second)
		d.Shutdown()
	}()

	d.Run()

	mu.Lock()
	defer mu.Unlock()

	if gotResult == nil {
		t.Fatal("no response received")
	}

	var rpcResp jsonRPCResponse
	json.Unmarshal(gotResult, &rpcResp)

	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if rpcResp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", rpcResp.Error.Code)
	}
}

func TestSessionFilePreferred(t *testing.T) {
	tokenDir := t.TempDir()
	os.WriteFile(filepath.Join(tokenDir, "token"), []byte("bootstrap"), 0600)
	os.WriteFile(filepath.Join(tokenDir, "session"), []byte("session-tok"), 0600)

	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		_, data, _ := conn.ReadMessage()
		var req jsonRPCRequest
		json.Unmarshal(data, &req)
		var hello helloParams
		json.Unmarshal(req.Params, &hello)
		gotToken = hello.Token

		result, _ := json.Marshal(welcomeResult{SessionID: "s1", HeartbeatIntervalSeconds: 30})
		resp := jsonRPCResponse{JSONRPC: "2.0", Result: result, ID: *req.ID}
		respData, _ := json.Marshal(resp)
		conn.WriteMessage(websocket.TextMessage, respData)

		conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer srv.Close()

	d := newTestDaemon(wsURL(srv), tokenDir)
	defer d.Shutdown()

	go func() {
		time.Sleep(500 * time.Millisecond)
		d.Shutdown()
	}()

	d.Run()

	if gotToken != "session-tok" {
		t.Errorf("expected session token to be preferred, got %q", gotToken)
	}
}

func TestReadTokenNoFiles(t *testing.T) {
	tokenDir := t.TempDir()
	d := newTestDaemon("ws://localhost:9999", tokenDir)
	_, err := d.readToken()
	if err == nil {
		t.Error("expected error when no token files exist")
	}
}
