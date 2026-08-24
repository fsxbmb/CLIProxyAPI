package outboundproxy

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const clashVergeSupportDir = "io.github.clash-verge-rev.clash-verge-rev"

type Result struct {
	Source     string
	HTTPProxy  string
	HTTPSProxy string
	AllProxy   string
}

func (result Result) URL() string {
	if result.HTTPSProxy != "" {
		return result.HTTPSProxy
	}
	if result.HTTPProxy != "" {
		return result.HTTPProxy
	}
	return result.AllProxy
}

// Configure preserves explicit proxy environment variables, then falls back to
// macOS system settings and Clash Verge's generated configuration.
func Configure() (Result, error) {
	if result := fromEnvironment(); result.Source != "" {
		ensureLoopbackBypass()
		return result, nil
	}
	if runtime.GOOS != "darwin" {
		return Result{}, nil
	}

	if output, err := exec.Command("/usr/sbin/scutil", "--proxy").Output(); err == nil {
		if result := parseSystemProxy(string(output)); result.Source != "" {
			apply(result)
			return result, nil
		}
	}

	result, err := fromClashVergeConfig()
	if err != nil {
		return Result{}, err
	}
	if result.Source != "" {
		apply(result)
	}
	return result, nil
}

func fromEnvironment() Result {
	result := Result{
		HTTPProxy:  firstEnvironment("HTTP_PROXY", "http_proxy"),
		HTTPSProxy: firstEnvironment("HTTPS_PROXY", "https_proxy"),
		AllProxy:   firstEnvironment("ALL_PROXY", "all_proxy"),
	}
	if result.HTTPProxy != "" || result.HTTPSProxy != "" || result.AllProxy != "" {
		result.Source = "environment"
	}
	return result
}

func parseSystemProxy(output string) Result {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	result := Result{Source: "macOS system settings"}
	if values["HTTPEnable"] == "1" {
		result.HTTPProxy = proxyURL("http", values["HTTPProxy"], values["HTTPPort"])
	}
	if values["HTTPSEnable"] == "1" {
		result.HTTPSProxy = proxyURL("http", values["HTTPSProxy"], values["HTTPSPort"])
	}
	if result.HTTPProxy == "" && result.HTTPSProxy == "" && values["SOCKSEnable"] == "1" {
		result.AllProxy = proxyURL("socks5", values["SOCKSProxy"], values["SOCKSPort"])
	}
	if result.HTTPProxy == "" && result.HTTPSProxy == "" && result.AllProxy == "" {
		return Result{}
	}
	return result
}

func fromClashVergeConfig() (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, fmt.Errorf("resolve home for Clash Verge proxy: %w", err)
	}
	paths := []string{
		filepath.Join(home, "Library", "Application Support", clashVergeSupportDir, "clash-verge.yaml"),
		filepath.Join(home, "Library", "Application Support", clashVergeSupportDir, "config.yaml"),
	}
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		ports := parseClashPorts(string(data))
		for _, candidate := range []struct {
			name   string
			scheme string
		}{
			{name: "mixed-port", scheme: "http"},
			{name: "port", scheme: "http"},
			{name: "socks-port", scheme: "socks5"},
		} {
			port := ports[candidate.name]
			if port == 0 || !portReachable(port) {
				continue
			}
			proxy := fmt.Sprintf("%s://127.0.0.1:%d", candidate.scheme, port)
			if candidate.scheme == "socks5" {
				return Result{Source: "Clash Verge config", AllProxy: proxy}, nil
			}
			return Result{Source: "Clash Verge config", HTTPProxy: proxy, HTTPSProxy: proxy}, nil
		}
	}
	return Result{}, nil
}

func parseClashPorts(config string) map[string]int {
	ports := make(map[string]int)
	scanner := bufio.NewScanner(strings.NewReader(config))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") || len(line) != len(strings.TrimLeft(line, " \t")) {
			continue
		}
		key, raw, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "mixed-port" && key != "port" && key != "socks-port" {
			continue
		}
		raw = strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		port, err := strconv.Atoi(raw)
		if err == nil && port > 0 && port <= 65535 {
			ports[key] = port
		}
	}
	return ports
}

func proxyURL(scheme, host, rawPort string) string {
	host = strings.TrimSpace(host)
	port, err := strconv.Atoi(strings.TrimSpace(rawPort))
	if host == "" || err != nil || port < 1 || port > 65535 {
		return ""
	}
	value := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	return value.String()
}

func portReachable(port int) bool {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func apply(result Result) {
	setProxyPair("HTTP_PROXY", "http_proxy", result.HTTPProxy)
	setProxyPair("HTTPS_PROXY", "https_proxy", result.HTTPSProxy)
	setProxyPair("ALL_PROXY", "all_proxy", result.AllProxy)
	ensureLoopbackBypass()
}

func setProxyPair(upper, lower, value string) {
	if value == "" {
		return
	}
	_ = os.Setenv(upper, value)
	_ = os.Setenv(lower, value)
}

func ensureLoopbackBypass() {
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		values := strings.Split(os.Getenv(key), ",")
		seen := make(map[string]bool)
		for _, value := range values {
			seen[strings.TrimSpace(value)] = true
		}
		for _, local := range []string{"127.0.0.1", "localhost", "::1"} {
			if !seen[local] {
				values = append(values, local)
			}
		}
		filtered := values[:0]
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				filtered = append(filtered, value)
			}
		}
		_ = os.Setenv(key, strings.Join(filtered, ","))
	}
}

func firstEnvironment(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
