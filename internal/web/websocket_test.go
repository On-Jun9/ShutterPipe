package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestHubRun_RegisterBroadcastUnregisterFlow는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHubRun_RegisterBroadcastUnregisterFlow(t *testing.T) {
	// Hub Run 루프는 register/broadcast/unregister 이벤트를 처리해야 한다.
	h := NewHub()
	go h.Run()

	client := &Client{
		hub:  h,
		send: make(chan []byte, 1),
	}

	h.register <- client
	waitForHubClientCount(t, h, 1)

	h.broadcast <- []byte("hello")
	select {
	case msg := <-client.send:
		if string(msg) != "hello" {
			t.Fatalf("unexpected broadcast payload: %s", string(msg))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting broadcast message")
	}

	h.unregister <- client
	waitForHubClientCount(t, h, 0)

	select {
	case _, ok := <-client.send:
		if ok {
			t.Fatal("expected client send channel to be closed")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting client channel close")
	}
}

// TestHubRun_RemovesClientWhenSendChannelIsBlocked는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHubRun_RemovesClientWhenSendChannelIsBlocked(t *testing.T) {
	// client.send이 막혀 있으면 default 분기로 클라이언트를 정리해야 한다.
	h := NewHub()
	go h.Run()

	blockedClient := &Client{
		hub:  h,
		send: make(chan []byte), // unbuffered + reader 없음 => broadcast 시 block
	}

	h.register <- blockedClient
	waitForHubClientCount(t, h, 1)

	h.broadcast <- []byte("x")
	waitForHubClientCount(t, h, 0)
}

// TestHandleWebSocket_UpgradeSuccessAndWritePumpDeliversMessage는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleWebSocket_UpgradeSuccessAndWritePumpDeliversMessage(t *testing.T) {
	// 정상 websocket 업그레이드 후 hub broadcast가 클라이언트로 전달되어야 한다.
	s := NewServer()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	waitUntil(t, 2*time.Second, func() bool {
		s.hub.mu.RLock()
		defer s.hub.mu.RUnlock()
		return len(s.hub.clients) == 1
	})

	s.hub.broadcast <- []byte(`{"type":"ping"}`)

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read websocket message: %v", err)
	}
	if string(msg) != `{"type":"ping"}` {
		t.Fatalf("unexpected websocket message: %s", string(msg))
	}
}

func TestHandleWebSocket_ClientCloseImmediatelyUnregisters(t *testing.T) {
	s := NewServer()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}

	waitForHubClientCount(t, s.hub, 1)
	s.hub.mu.RLock()
	var serverClient *Client
	for client := range s.hub.clients {
		serverClient = client
	}
	s.hub.mu.RUnlock()
	if serverClient == nil {
		t.Fatal("expected registered server client")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("failed to close websocket: %v", err)
	}
	waitForHubClientCount(t, s.hub, 0)

	select {
	case _, ok := <-serverClient.send:
		if ok {
			t.Fatal("expected hub to close client send channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for client send channel close")
	}

	// readPump과 writePump이 모두 종료된 뒤에도 unregister는 한 번만 요청되어야 한다.
	serverClient.requestUnregister()
}

func TestClientRequestUnregisterSendsOnce(t *testing.T) {
	h := NewHub()
	h.unregister = make(chan *Client, 2)
	client := &Client{
		hub:  h,
		send: make(chan []byte),
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.requestUnregister()
		}()
	}
	wg.Wait()

	select {
	case got := <-h.unregister:
		if got != client {
			t.Fatalf("unexpected client: %p", got)
		}
	default:
		t.Fatal("expected one unregister request")
	}

	select {
	case <-h.unregister:
		t.Fatal("expected unregister request to be sent only once")
	default:
	}
}

func TestHubShutdownClosesClientsAndPumpsCanUnregister(t *testing.T) {
	h := NewHub()
	go h.Run()
	client := &Client{hub: h, send: make(chan []byte, 1)}
	h.register <- client
	waitForHubClientCount(t, h, 1)

	h.Shutdown()
	waitForHubClientCount(t, h, 0)
	if _, ok := <-client.send; ok {
		t.Fatal("expected client send channel to close during hub shutdown")
	}

	done := make(chan struct{})
	go func() {
		client.requestUnregister()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("client unregister blocked after hub shutdown")
	}
}

func TestServerBroadcastAfterHubShutdownDoesNotBlock(t *testing.T) {
	h := NewHub()
	go h.Run()
	h.Shutdown()
	s := &Server{hub: h}

	done := make(chan struct{})
	go func() {
		s.broadcastJSON(map[string]string{"type": "status"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked after hub shutdown")
	}
}

func TestHandleWebSocket_RejectsCrossOriginUpgrade(t *testing.T) {
	s := NewServer()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	headers := http.Header{}
	headers.Set("Origin", "https://attacker.example")

	conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected cross-origin websocket upgrade to fail")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got response=%v", response)
	}
}

func TestHandleWebSocket_AllowsSameOriginUpgrade(t *testing.T) {
	s := NewServer()

	ts := httptest.NewServer(s.router)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	headers := http.Header{}
	headers.Set("Origin", ts.URL)

	conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("expected same-origin websocket upgrade to succeed, response=%v err=%v", response, err)
	}
	conn.Close()
}

// waitForHubClientCount는 테스트 코드 동작을 검증하거나 보조합니다.
func waitForHubClientCount(t *testing.T, h *Hub, expected int) {
	t.Helper()
	waitUntil(t, 2*time.Second, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == expected
	})
}

// waitUntil는 테스트 코드 동작을 검증하거나 보조합니다.
func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}

// TestServerStart_ReturnsErrorOnInvalidAddress는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerStart_ReturnsErrorOnInvalidAddress(t *testing.T) {
	// Start는 잘못된 listen 주소를 받으면 즉시 에러를 반환해야 한다.
	s := NewServer()
	err := s.Start("://bad-address")
	if err == nil {
		t.Fatal("expected listen error for invalid address")
	}
}

// TestHandleWebSocket_InvalidHandshakeStatus는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleWebSocket_InvalidHandshakeStatus(t *testing.T) {
	// websocket 헤더 없이 /api/ws 호출 시 업그레이드 실패 상태를 반환해야 한다.
	s := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	rr := httptest.NewRecorder()
	s.handleWebSocket(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 for invalid handshake, got %d", rr.Code)
	}
}

// TestHandleWebSocket_UpgradeURLIsValid는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleWebSocket_UpgradeURLIsValid(t *testing.T) {
	// 테스트 서버 URL을 websocket URL로 변환했을 때 유효해야 한다.
	ts := httptest.NewServer(http.NewServeMux())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	if _, err := url.Parse(wsURL); err != nil {
		t.Fatalf("invalid websocket url: %v", err)
	}
}
