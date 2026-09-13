package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
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

func TestHandlerServesFreshMultiKeyUI(t *testing.T) {
	server, err := New("127.0.0.1:8318", "http://127.0.0.1:8317", Meta{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/ui/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected UI Cache-Control no-store, got %q", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, `<textarea id="relay-key"`) {
		t.Fatal("multi-key textarea missing from UI")
	}
	if !strings.Contains(body, `id="routing-strategy"`) {
		t.Fatal("routing strategy selector missing from UI")
	}
	if !regexp.MustCompile(`app\.js\?v=[a-f0-9]{12}`).MatchString(body) {
		t.Fatal("fingerprinted app.js URL missing from UI")
	}
}

func TestManagementProxyRoutesAndPreservesAuthorization(t *testing.T) {
	var gotPath, gotQuery, gotAuthorization string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotQuery = request.URL.RawQuery
		gotAuthorization = request.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()

	server, err := New("127.0.0.1:8318", backend.URL, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/config?detail=1", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer local-management-key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if gotPath != "/v0/management/config" {
		t.Fatalf("unexpected backend path %q", gotPath)
	}
	if gotQuery != "detail=1" {
		t.Fatalf("unexpected backend query %q", gotQuery)
	}
	if gotAuthorization != "Bearer local-management-key" {
		t.Fatalf("authorization header was not preserved: %q", gotAuthorization)
	}
}
