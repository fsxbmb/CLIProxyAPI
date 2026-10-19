package cliproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

const (
	antigravityModelBaseURLDaily = "https://daily-cloudcode-pa.googleapis.com"
	antigravityModelBaseURLProd  = "https://cloudcode-pa.googleapis.com"
	antigravityModelsPath        = "/v1internal:fetchAvailableModels"
)

type antigravityFetchAvailableModelsResponse struct {
	WebSearchModelIDs []string `json:"webSearchModelIds"`
	Models            map[string]struct {
		DisplayName     string `json:"displayName"`
		MaxTokens       int    `json:"maxTokens"`
		MaxOutputTokens int    `json:"maxOutputTokens"`
		SupportsImages  bool   `json:"supportsImages"`
	} `json:"models"`
}

type antigravityModelCapabilityHints struct {
	WebSearchModelIDs map[string]struct{}
	DiscoveredModels  []*ModelInfo
}

func (s *Service) fetchAntigravityModelCapabilityHintsForAuth(ctx context.Context, auth *coreauth.Auth) antigravityModelCapabilityHints {
	if auth == nil || auth.Metadata == nil {
		return antigravityModelCapabilityHints{}
	}
	accessToken, _ := auth.Metadata["access_token"].(string)
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return antigravityModelCapabilityHints{}
	}
	payload := map[string]string{}
	if project, ok := auth.Metadata["project_id"].(string); ok && strings.TrimSpace(project) != "" {
		payload["project"] = strings.TrimSpace(project)
	}
	requestBody, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return antigravityModelCapabilityHints{}
	}

	client := &http.Client{}
	if transport, _, errProxy := proxyutil.BuildHTTPTransport(s.antigravityModelFetchProxyURL(auth)); errProxy == nil && transport != nil {
		client.Transport = transport
	}

	for _, baseURL := range antigravityModelBaseURLs(auth) {
		req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+antigravityModelsPath, strings.NewReader(string(requestBody)))
		if errReq != nil {
			continue
		}
		req.Close = true
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", misc.AntigravityUserAgent())

		resp, errDo := client.Do(req)
		if errDo != nil {
			continue
		}
		body, errRead := io.ReadAll(resp.Body)
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debugf("antigravity model fetch: close response body: %v", errClose)
		}
		if errRead != nil {
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			continue
		}
		hints := parseAntigravityModelCapabilityHints(body)
		if len(hints.WebSearchModelIDs) > 0 || len(hints.DiscoveredModels) > 0 {
			return hints
		}
	}
	return antigravityModelCapabilityHints{}
}

func (s *Service) antigravityModelFetchProxyURL(auth *coreauth.Auth) string {
	if auth != nil {
		if proxyURL := strings.TrimSpace(auth.ProxyURL); proxyURL != "" {
			return proxyURL
		}
	}
	if s != nil && s.cfg != nil {
		return strings.TrimSpace(s.cfg.ProxyURL)
	}
	return ""
}

func antigravityModelBaseURLs(auth *coreauth.Auth) []string {
	if baseURL := resolveAntigravityModelBaseURL(auth); baseURL != "" {
		return []string{baseURL}
	}
	return []string{antigravityModelBaseURLDaily, antigravityModelBaseURLProd}
}

func resolveAntigravityModelBaseURL(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if value := strings.TrimSpace(auth.Attributes["base_url"]); value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	if auth.Metadata != nil {
		if value, ok := auth.Metadata["base_url"].(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return strings.TrimRight(value, "/")
			}
		}
	}
	return ""
}

func parseAntigravityModelCapabilityHints(body []byte) antigravityModelCapabilityHints {
	var parsed antigravityFetchAvailableModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return antigravityModelCapabilityHints{}
	}
	webSearchModels := make(map[string]struct{}, len(parsed.WebSearchModelIDs))
	for _, modelID := range parsed.WebSearchModelIDs {
		modelID = normalizeAntigravityFetchedModelID(modelID)
		if modelID != "" {
			webSearchModels[modelID] = struct{}{}
		}
	}
	hints := antigravityModelCapabilityHints{WebSearchModelIDs: webSearchModels}
	for id, fetched := range parsed.Models {
		switch id {
		case "gemini-4-argon-low", "gemini-4-argon-medium", "gemini-4-argon-high":
		case "gemini-3.8-flash-tiered", "gemini-3.8-flash-low", "gemini-3.8-flash-medium", "gemini-3.8-flash-high":
		default:
			continue
		}
		inputs := []string{"text"}
		if fetched.SupportsImages {
			inputs = append(inputs, "image")
		}
		displayName := fetched.DisplayName
		if displayName == "" {
			displayName = id
		}
		hints.DiscoveredModels = append(hints.DiscoveredModels, &ModelInfo{
			ID: id, Object: "model", OwnedBy: "google", Type: "antigravity",
			DisplayName: displayName, Name: id, Version: id,
			ContextLength: fetched.MaxTokens, MaxCompletionTokens: fetched.MaxOutputTokens,
			SupportedInputModalities: inputs, SupportedOutputModalities: []string{"text"},
		})
	}
	sort.Slice(hints.DiscoveredModels, func(i, j int) bool { return hints.DiscoveredModels[i].ID < hints.DiscoveredModels[j].ID })
	return hints
}

func applyAntigravityFetchedModelCapabilities(models []*ModelInfo, hints antigravityModelCapabilityHints) []*ModelInfo {
	// Only advertise new models on accounts whose upstream catalog includes them.
	for _, fetched := range hints.DiscoveredModels {
		found := false
		for _, existing := range models {
			if existing != nil && existing.ID == fetched.ID {
				found = true
				break
			}
		}
		if !found {
			models = append(models, fetched)
		}
	}

	for _, model := range models {
		if model == nil {
			continue
		}
		modelID := normalizeAntigravityFetchedModelID(model.ID)
		if _, ok := hints.WebSearchModelIDs[modelID]; ok {
			model.SupportsWebSearch = true
		}
	}
	return models
}

func normalizeAntigravityFetchedModelID(modelID string) string {
	return strings.ToLower(strings.TrimSpace(modelID))
}
