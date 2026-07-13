package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/On-Jun9/ShutterPipe/internal/pipeline"
	"github.com/gorilla/mux"
)

// TestServerSetupRoutesAndVersionRoute는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerSetupRoutesAndVersionRoute(t *testing.T) {
	// 라우트 설정 후 /api/version이 현재 버전을 반환해야 한다.
	s := &Server{
		router:  mux.NewRouter(),
		hub:     NewHub(),
		version: "v9.9.9",
	}
	s.setupRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "localhost:8080"
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}
	if body["version"] != "v9.9.9" {
		t.Fatalf("unexpected version response: %+v", body)
	}
}

// TestNewServerAndSetVersion는 테스트 코드 동작을 검증하거나 보조합니다.
func TestNewServerAndSetVersion(t *testing.T) {
	// NewServer와 SetVersion 호출 후 버전 응답이 반영되어야 한다.
	s := NewServer()
	s.SetVersion("v1.2.3")

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "localhost:8080"
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}
	if body["version"] != "v1.2.3" {
		t.Fatalf("unexpected version response: %+v", body)
	}
}

func TestServerInstanceIDIsStableAndProcessScoped(t *testing.T) {
	first := NewServer()
	second := NewServer()
	t.Cleanup(func() {
		first.hub.Shutdown()
		second.hub.Shutdown()
	})
	if first.instanceID() == "" || first.instanceID() != first.instanceID() {
		t.Fatal("server instance ID must be non-empty and stable")
	}
	if first.instanceID() == second.instanceID() {
		t.Fatal("different server instances must not share revision epochs")
	}
}

// TestServerBroadcastJSONAndProgress는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerBroadcastJSONAndProgress(t *testing.T) {
	// broadcastJSON/broadcastProgress는 hub broadcast 채널로 메시지를 전달해야 한다.
	s := &Server{hub: NewHub()}
	done := make(chan []byte, 2)

	go func() {
		done <- <-s.hub.broadcast
		done <- <-s.hub.broadcast
	}()

	s.broadcastJSON(map[string]string{"type": "status"})
	s.broadcastProgress(pipeline.ProgressUpdate{Type: "complete"})

	select {
	case first := <-done:
		if len(first) == 0 {
			t.Fatal("expected first broadcast payload")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting first broadcast")
	}

	select {
	case second := <-done:
		if len(second) == 0 {
			t.Fatal("expected second broadcast payload")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting second broadcast")
	}
}

// TestHandleWebSocket_UpgradeFailurePath는 테스트 코드 동작을 검증하거나 보조합니다.
func TestHandleWebSocket_UpgradeFailurePath(t *testing.T) {
	// 웹소켓 핸드셰이크가 아닌 요청은 업그레이드 실패 처리로 빠져야 한다.
	s := &Server{hub: NewHub()}
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	rr := httptest.NewRecorder()

	s.handleWebSocket(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 response for invalid websocket handshake, got %d", rr.Code)
	}
}

// TestServerSetupRoutes_ContainsRunCancelRoute는 테스트 코드 동작을 검증하거나 보조합니다.
func TestServerSetupRoutes_ContainsRunCancelRoute(t *testing.T) {
	// /api/run/cancel 라우트가 등록되어 취소 핸들러로 연결되어야 한다.
	s := NewServer()

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", strings.NewReader(`{"run_id":"route-test"}`))
	req.Host = "localhost:8080"
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected status 409 when no run is active, got %d", rr.Code)
	}
}

func TestServerSetupRoutes_RejectsCrossOriginStateChange(t *testing.T) {
	s := NewServer()

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", strings.NewReader(`{"run_id":"same-origin-test"}`))
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "https://attacker.example")
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for cross-origin request, got %d", rr.Code)
	}

	var response APIErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response.Message != "cross-origin request denied" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestServerSetupRoutes_AllowsSameOriginStateChange(t *testing.T) {
	s := NewServer()

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", strings.NewReader(`{"run_id":"same-origin-test"}`))
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://"+req.Host)
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected handler status 409 for same-origin request, got %d", rr.Code)
	}
}

func TestServerSetupRoutes_RejectsDifferentSchemeStateChange(t *testing.T) {
	s := NewServer()

	req := httptest.NewRequest(http.MethodPost, "/api/run/cancel", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "https://"+req.Host)
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for different-scheme request, got %d", rr.Code)
	}
}

func TestServerSetupRoutes_RejectsNonLoopbackHost(t *testing.T) {
	s := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "attacker.example:8080"
	rr := httptest.NewRecorder()

	s.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected non-loopback host to be rejected, got %d", rr.Code)
	}
}

func TestValidateListenAddress(t *testing.T) {
	for _, addr := range []string{"localhost:8080", "127.0.0.1:8080", "[::1]:8080"} {
		if err := validateListenAddress(addr); err != nil {
			t.Errorf("expected loopback address %q to be allowed: %v", addr, err)
		}
	}

	for _, addr := range []string{"0.0.0.0:8080", ":8080", "192.168.0.10:8080", "://bad-address"} {
		if err := validateListenAddress(addr); err == nil {
			t.Errorf("expected address %q to be rejected", addr)
		}
	}
}

func TestServerShutdownCancelsAndWaitsForActiveRun(t *testing.T) {
	s := &Server{}
	cancelObserved := make(chan struct{})
	if !s.runs().Start("shutdown-run", func() {
		go func() {
			close(cancelObserved)
			s.runs().Finish("shutdown-run", RunStatusCancelled, nil, "")
		}()
	}) {
		t.Fatal("failed to arrange active run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case <-cancelObserved:
	default:
		t.Fatal("shutdown did not cancel the active run")
	}
	if status := s.runs().Status(); status.Status != RunStatusCancelled || status.RunID != "shutdown-run" {
		t.Fatalf("shutdown did not wait for terminal state: %+v", status)
	}
}

func TestServerCannotStartAfterShutdownWasRequested(t *testing.T) {
	s := NewServer()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if err := s.Start("localhost:0"); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("expected server closed after pre-start shutdown, got %v", err)
	}
}

func TestServerShutdownRejectsRunAdmissionBeforeWaiting(t *testing.T) {
	s := &Server{}
	cancelObserved := make(chan struct{})
	releaseRun := make(chan struct{})
	s.runs().Start("active-run", func() {
		close(cancelObserved)
		go func() {
			<-releaseRun
			s.runs().Finish("active-run", RunStatusCancelled, nil, "")
		}()
	})

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		shutdownDone <- s.Shutdown(ctx)
	}()
	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not reach active run cancellation")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/run", nil)
	rr := httptest.NewRecorder()
	s.handleRun(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected run admission to be rejected during shutdown, got %d", rr.Code)
	}

	close(releaseRun)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}
