package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RequestCountsConfig enables restart-persistent per-credential request counters.
// Only credential identifiers and success/failure integers are stored.
type RequestCountsConfig struct {
	Path          string
	ManagementKey string
}

type requestCount struct {
	Success        int64 `json:"success"`
	Failed         int64 `json:"failed"`
	RuntimeSuccess int64 `json:"runtime_success"`
	RuntimeFailed  int64 `json:"runtime_failed"`
}

type requestCountFile struct {
	Accounts map[string]requestCount `json:"accounts"`
}

type requestCountStore struct {
	mu       sync.Mutex
	path     string
	accounts map[string]requestCount
}

func newRequestCountStore(path string) (*requestCountStore, error) {
	store := &requestCountStore{path: strings.TrimSpace(path), accounts: make(map[string]requestCount)}
	if store.path == "" {
		return store, nil
	}
	data, err := os.ReadFile(store.path)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read request counts: %w", err)
	}
	var saved requestCountFile
	if err = json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("parse request counts: %w", err)
	}
	if saved.Accounts != nil {
		store.accounts = saved.Accounts
	}
	return store, nil
}

func requestCountKey(entry map[string]any) string {
	for _, field := range []string{"auth_index", "authIndex", "id", "name"} {
		if value, ok := entry[field].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func jsonInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	default:
		return 0
	}
}

// applyTo writes both the cumulative (success/failed) and current-process
// (runtime_*) counters onto an auth-files entry so the UI can show both.
func (c requestCount) applyTo(entry map[string]any) {
	entry["success"] = c.Success
	entry["failed"] = c.Failed
	entry["runtime_success"] = c.RuntimeSuccess
	entry["runtime_failed"] = c.RuntimeFailed
}

func (s *requestCountStore) merge(entries []any) (bool, error) {
	if s == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := requestCountKey(entry)
		if key == "" {
			continue
		}
		currentSuccess := jsonInt64(entry["success"])
		currentFailed := jsonInt64(entry["failed"])
		count := s.accounts[key]
		if currentSuccess >= count.RuntimeSuccess {
			count.Success += currentSuccess - count.RuntimeSuccess
		} else {
			// A lower runtime value means the proxy process restarted.
			count.Success += currentSuccess
		}
		if currentFailed >= count.RuntimeFailed {
			count.Failed += currentFailed - count.RuntimeFailed
		} else {
			count.Failed += currentFailed
		}
		if count.RuntimeSuccess != currentSuccess || count.RuntimeFailed != currentFailed {
			changed = true
		}
		count.RuntimeSuccess = currentSuccess
		count.RuntimeFailed = currentFailed
		s.accounts[key] = count
		count.applyTo(entry)
	}
	if changed {
		return true, s.saveLocked()
	}
	return false, nil
}

func (s *requestCountStore) decorate(entries []any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if count, found := s.accounts[requestCountKey(entry)]; found {
			count.applyTo(entry)
		}
	}
}

func (s *requestCountStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(requestCountFile{Accounts: s.accounts}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode request counts: %w", err)
	}
	data = append(data, '\n')
	if err = os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create request counts directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".request-counts-*")
	if err != nil {
		return fmt.Errorf("create request counts temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write request counts: %w", err)
	}
	if err = os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace request counts: %w", err)
	}
	return nil
}

func (s *Server) decorateAuthFilesResponse(response *http.Response) error {
	if response == nil || response.Request == nil || response.StatusCode != http.StatusOK || !strings.HasSuffix(response.Request.URL.Path, "/auth-files") {
		return nil
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return err
	}
	entries, _ := payload["files"].([]any)
	_, _ = s.counts.merge(entries)
	s.counts.decorate(entries)
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(data))
	response.ContentLength = int64(len(data))
	response.Header.Set("Content-Length", fmt.Sprint(len(data)))
	return nil
}

func (s *Server) pollRequestCounts(ctx context.Context) {
	if s.counts == nil || strings.TrimSpace(s.managementKey) == "" {
		return
	}
	poll := func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.target.String()+"/v0/management/auth-files", nil)
		if err != nil {
			return
		}
		request.Header.Set("Authorization", "Bearer "+s.managementKey)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return
		}
		var payload map[string]any
		if json.NewDecoder(response.Body).Decode(&payload) != nil {
			return
		}
		entries, _ := payload["files"].([]any)
		_, _ = s.counts.merge(entries)
	}
	poll()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			poll()
			return
		case <-ticker.C:
			poll()
		}
	}
}
