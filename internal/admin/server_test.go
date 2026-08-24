package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerRejectsRemoteClients(t *testing.T) {
	server, err := New("127.0.0.1:8318", "http://127.0.0.1:8317", Meta{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/meta", nil)
	request.RemoteAddr = "192.168.1.20:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestHandlerServesLocalMetadata(t *testing.T) {
	server, err := New("127.0.0.1:8318", "http://127.0.0.1:8317", Meta{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/meta", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store, got %q", got)
	}
}
