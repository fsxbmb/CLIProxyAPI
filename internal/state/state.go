package state

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const envHome = "CLIPROXY_LITE_HOME"

type Paths struct {
	Root        string
	ConfigFile  string
	SecretsFile string
	AuthDir     string
	PluginsDir  string
}

type Secrets struct {
	ManagementKey string `json:"management_key"`
	APIKey        string `json:"api_key"`
}

func Resolve(explicitHome string) (Paths, error) {
	root := strings.TrimSpace(explicitHome)
	if root == "" {
		root = strings.TrimSpace(os.Getenv(envHome))
	}
	if root == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		root = filepath.Join(configDir, "CLIProxyAPI-Lite")
		// Keep compatibility with the original Linux path used by earlier
		// releases, without affecting fresh installations.
		if runtime.GOOS == "linux" {
			legacy := filepath.Join(configDir, "cliproxyapi-lite")
			if _, legacyErr := os.Stat(legacy); legacyErr == nil {
				if _, currentErr := os.Stat(root); errors.Is(currentErr, fs.ErrNotExist) {
					root = legacy
				}
			}
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data directory: %w", err)
	}
	return Paths{
		Root:        abs,
		ConfigFile:  filepath.Join(abs, "config.yaml"),
		SecretsFile: filepath.Join(abs, "secrets.json"),
		AuthDir:     filepath.Join(abs, "auth"),
		PluginsDir:  filepath.Join(abs, "plugins"),
	}, nil
}

func Ensure(paths Paths) (Secrets, bool, error) {
	for _, dir := range []string{paths.Root, paths.AuthDir, paths.PluginsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Secrets{}, false, fmt.Errorf("create %s: %w", dir, err)
		}
		if err := securePrivate(dir, true); err != nil {
			return Secrets{}, false, fmt.Errorf("secure %s: %w", dir, err)
		}
	}

	secrets, created, err := loadOrCreateSecrets(paths.SecretsFile)
	if err != nil {
		return Secrets{}, false, err
	}
	if err = ensureConfig(paths, secrets); err != nil {
		return Secrets{}, false, err
	}
	return secrets, created, nil
}

func loadOrCreateSecrets(path string) (Secrets, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var secrets Secrets
		if err = json.Unmarshal(data, &secrets); err != nil {
			return Secrets{}, false, fmt.Errorf("parse secrets file: %w", err)
		}
		if strings.TrimSpace(secrets.ManagementKey) == "" || strings.TrimSpace(secrets.APIKey) == "" {
			return Secrets{}, false, errors.New("secrets file is missing required keys")
		}
		if err = securePrivate(path, false); err != nil {
			return Secrets{}, false, fmt.Errorf("secure secrets file: %w", err)
		}
		return secrets, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Secrets{}, false, fmt.Errorf("read secrets file: %w", err)
	}

	managementKey, err := randomKey("mgmt")
	if err != nil {
		return Secrets{}, false, err
	}
	apiKey, err := randomKey("sk-local")
	if err != nil {
		return Secrets{}, false, err
	}
	secrets := Secrets{ManagementKey: managementKey, APIKey: apiKey}
	encoded, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return Secrets{}, false, fmt.Errorf("encode secrets: %w", err)
	}
	encoded = append(encoded, '\n')
	if err = writeExclusive(path, encoded, 0o600); err != nil {
		return Secrets{}, false, err
	}
	if err = securePrivate(path, false); err != nil {
		return Secrets{}, false, fmt.Errorf("secure secrets file: %w", err)
	}
	return secrets, true, nil
}

func ensureConfig(paths Paths, secrets Secrets) error {
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		if err = securePrivate(paths.ConfigFile, false); err != nil {
			return fmt.Errorf("secure config file: %w", err)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect config file: %w", err)
	}

	content := defaultConfig(paths, secrets)
	if err := writeExclusive(paths.ConfigFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	if err := securePrivate(paths.ConfigFile, false); err != nil {
		return fmt.Errorf("secure config file: %w", err)
	}
	return nil
}

func defaultConfig(paths Paths, secrets Secrets) string {
	return fmt.Sprintf(`# CLIProxyAPI-Lite local-only configuration.
# Secrets are generated locally and this file is never meant to be committed.
host: "127.0.0.1"
port: 8317
proxy-url: ""

tls:
  enable: false
  cert: ""
  key: ""

remote-management:
  allow-remote: false
  secret-key: %s
  disable-control-panel: true
  disable-auto-update-panel: true

auth-dir: %s

api-keys:
  - %s

plugins:
  enabled: false
  dir: %s

debug: false
pprof:
  enable: false
  addr: "127.0.0.1:8316"
commercial-mode: true
logging-to-file: false
logs-max-total-size-mb: 0
error-logs-max-files: 5
usage-statistics-enabled: false
request-log: false
ws-auth: true
passthrough-headers: false
force-model-prefix: false
save-cooldown-status: false

request-retry: 2
max-retry-credentials: 3
max-retry-interval: 20

quota-exceeded:
  switch-project: true
  switch-preview-model: true
  antigravity-credits: true

routing:
  strategy: "round-robin"
  session-affinity: false

openai-compatibility: []
`, yamlString(secrets.ManagementKey), yamlString(paths.AuthDir), yamlString(secrets.APIKey), yamlString(paths.PluginsDir))
}

func randomKey(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func writeExclusive(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func IsLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func ValidatePermissions(paths Paths) error {
	return validatePrivate(paths)
}
