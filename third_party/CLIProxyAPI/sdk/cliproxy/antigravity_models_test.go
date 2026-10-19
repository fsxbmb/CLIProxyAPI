package cliproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchAntigravityAccountArgonCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != antigravityModelsPath || r.Header.Get("Authorization") != "Bearer test-token" || !strings.HasPrefix(r.Header.Get("User-Agent"), "antigravity/hub/") {
			t.Error("incorrect account model discovery request")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["project"] != "test-project" {
			t.Error("missing account project")
		}
		_, _ = w.Write([]byte(`{"models":{"gemini-4-argon-medium":{"maxTokens":1048576,"maxOutputTokens":65536}}}`))
	}))
	defer server.Close()
	s := &Service{}
	hints := s.fetchAntigravityModelCapabilityHintsForAuth(context.Background(), &coreauth.Auth{
		Metadata: map[string]any{"access_token": "test-token", "project_id": "test-project", "base_url": server.URL},
	})
	models := applyAntigravityFetchedModelCapabilities(nil, hints)
	if len(models) != 1 || models[0].ID != "gemini-4-argon-medium" {
		t.Fatalf("account catalog was not registered: %+v", models)
	}
}

func TestAntigravityFetchedArgonModels(t *testing.T) {
	hints := parseAntigravityModelCapabilityHints([]byte(`{"models":{"gemini-4-argon-medium":{"displayName":"Gemini 4 Argon (Medium)","maxTokens":1048576,"maxOutputTokens":65536,"supportsImages":true},"chat_internal":{}},"webSearchModelIds":["gemini-4-argon-medium"]}`))
	models := applyAntigravityFetchedModelCapabilities(nil, hints)
	if len(models) != 1 || models[0].ID != "gemini-4-argon-medium" {
		t.Fatalf("expected only the account-discovered Argon model, got %+v", models)
	}
	m := models[0]
	if m.ContextLength != 1048576 || m.MaxCompletionTokens != 65536 || !m.SupportsWebSearch || len(m.SupportedInputModalities) != 2 {
		t.Fatalf("missing fetched metadata: %+v", m)
	}
	if got := applyAntigravityFetchedModelCapabilities(models, hints); len(got) != 1 {
		t.Fatal("duplicate model after refresh")
	}
	if got := applyAntigravityFetchedModelCapabilities(nil, parseAntigravityModelCapabilityHints([]byte(`{"models":{}}`))); len(got) != 0 {
		t.Fatal("must not expose Argon to an account without it")
	}
}

func TestAntigravityFetchedFlashPreservesLegacyModel(t *testing.T) {
	hints := parseAntigravityModelCapabilityHints([]byte(`{"models":{"gemini-3.8-flash-tiered":{"maxTokens":1048576},"gemini-3.8-flash-low":{},"gemini-3.8-flash-medium":{},"gemini-3.8-flash-high":{},"gemini-4-argon-disabled":{}}}`))
	legacy := &ModelInfo{ID: "gemini-3-flash", DisplayName: "Gemini 3 Flash"}
	models := applyAntigravityFetchedModelCapabilities([]*ModelInfo{legacy}, hints)
	if len(models) != 5 || models[0] != legacy {
		t.Fatalf("must retain legacy Flash and add four discovered variants: %+v", models)
	}
	for _, model := range models {
		if model.ID == "gemini-4-argon-disabled" {
			t.Fatal("disabled entitlement must not be registered")
		}
		if model.DisplayName == "" {
			t.Fatal("missing display name fallback")
		}
	}
	if got := applyAntigravityFetchedModelCapabilities(models, hints); len(got) != 5 {
		t.Fatal("refresh duplicated Flash models")
	}
}
