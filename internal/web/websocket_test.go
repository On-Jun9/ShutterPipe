package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
