package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fsxbmb/CLIProxyAPI/internal/admin"
	"github.com/fsxbmb/CLIProxyAPI/internal/outboundproxy"
	"github.com/fsxbmb/CLIProxyAPI/internal/state"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func Run(ctx context.Context, args []string, info BuildInfo, stdout, stderr io.Writer) error {
	command := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "serve":
		return serve(ctx, args, info, stdout, stderr)
	case "init":
		return initialize(args, stdout, stderr)
	case "keys":
		return printKeys(args, stdout, stderr)
	case "doctor":
		return doctor(args, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "cliproxy-lite %s (%s, %s)\n", info.Version, info.Commit, info.Date)
		return nil
	case "help", "-h", "--help":
		printHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q; run cliproxy-lite help", command)
	}
}

func serve(ctx context.Context, args []string, info BuildInfo, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "data directory (default: OS user config directory)")
	uiPort := flags.Int("ui-port", 8318, "local Web UI port")
	noOpen := flags.Bool("no-open", false, "do not open the Web UI in a browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *uiPort < 1 || *uiPort > 65535 {
		return fmt.Errorf("invalid UI port %d", *uiPort)
	}
	proxy, err := outboundproxy.Configure()
	if err != nil {
		fmt.Fprintf(stderr, "warning: outbound proxy detection failed: %v\n", err)
	} else if proxy.Source != "" {
		fmt.Fprintf(stdout, "Outbound proxy: %s (%s)\n", proxy.URL(), proxy.Source)
	}

	paths, err := state.Resolve(*home)
	if err != nil {
		return err
	}
	secrets, created, err := state.Ensure(paths)
	if err != nil {
		return err
	}
	cfg, err := sdkconfig.LoadConfig(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if strings.TrimSpace(cfg.ProxyURL) == "" {
		if proxyURL := proxy.URL(); proxyURL != "" {
			// The upstream SDK's uTLS and WebSocket clients use this field instead
			// of the standard HTTP proxy environment variables.
			cfg.ProxyURL = proxyURL
		}
	}
	if !state.IsLoopbackHost(cfg.Host) {
		return fmt.Errorf("refusing to bind API host %q; use 127.0.0.1, localhost, or ::1", cfg.Host)
	}
	if cfg.RemoteManagement.AllowRemote {
		return errors.New("refusing to start while remote-management.allow-remote is true")
	}
	if strings.TrimSpace(cfg.RemoteManagement.SecretKey) == "" {
		return errors.New("management API is disabled; restore remote-management.secret-key or rerun with a fresh home directory")
	}

	apiHost := cfg.Host
	if strings.EqualFold(apiHost, "localhost") || apiHost == "::1" {
		apiHost = "127.0.0.1"
	}
	apiOrigin := fmt.Sprintf("http://%s:%d", apiHost, cfg.Port)
	apiBase := apiOrigin + "/v1"
	adminURL := fmt.Sprintf("http://127.0.0.1:%d/ui/", *uiPort)
	ui, err := admin.New(
		net.JoinHostPort("127.0.0.1", fmt.Sprint(*uiPort)),
		apiOrigin,
		admin.Meta{Version: info.Version, APIBase: apiBase, AdminURL: adminURL},
	)
	if err != nil {
		return err
	}
	if err = ui.Start(ctx); err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ui.Shutdown(shutdownCtx)
	}()

	service, err := cliproxy.NewBuilder().
		WithConfig(cfg).
		WithConfigPath(paths.ConfigFile).
		WithLocalManagementPassword(secrets.ManagementKey).
		WithHooks(cliproxy.Hooks{OnAfterStart: func(_ *cliproxy.Service) {
			fmt.Fprintf(stdout, "\nCLIProxyAPI-Lite is ready\n  API:    %s\n  Web UI: %s\n  Data:   %s\n", apiBase, adminURL, paths.Root)
			if created {
				fmt.Fprintln(stdout, "\nKeys were generated. Run `cliproxy-lite keys` to show them again.")
			}
			if !*noOpen {
				go openBrowser(adminURL)
			}
		}}).
		Build()
	if err != nil {
		return fmt.Errorf("build proxy service: %w", err)
	}
	if err = service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func initialize(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := state.Resolve(*home)
	if err != nil {
		return err
	}
	_, created, err := state.Ensure(paths)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintln(stdout, "Initialized local configuration:", paths.Root)
	} else {
		fmt.Fprintln(stdout, "Configuration already exists:", paths.Root)
	}
	fmt.Fprintln(stdout, "Run `cliproxy-lite serve` to start the API and Web UI.")
	return nil
}

func printKeys(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("keys", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := state.Resolve(*home)
	if err != nil {
		return err
	}
	secrets, _, err := state.Ensure(paths)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Management key: %s\nAPI key:        %s\n", secrets.ManagementKey, secrets.APIKey)
	return nil
}

func doctor(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	home := flags.String("home", "", "data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := state.Resolve(*home)
	if err != nil {
		return err
	}
	if _, _, err = state.Ensure(paths); err != nil {
		return err
	}
	if err = state.ValidatePermissions(paths); err != nil {
		return err
	}
	cfg, err := sdkconfig.LoadConfig(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("config parse failed: %w", err)
	}
	if !state.IsLoopbackHost(cfg.Host) {
		return fmt.Errorf("unsafe API host: %q", cfg.Host)
	}
	if cfg.RemoteManagement.AllowRemote {
		return errors.New("remote management must remain disabled")
	}
	if strings.TrimSpace(cfg.RemoteManagement.SecretKey) == "" {
		return errors.New("management secret is empty")
	}
	fmt.Fprintf(stdout, "OK\n  Config: %s\n  Auth:   %s\n  API:    http://%s:%d/v1\n", paths.ConfigFile, paths.AuthDir, cfg.Host, cfg.Port)
	return nil
}

func openBrowser(target string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{target}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	default:
		command = "xdg-open"
		args = []string{target}
	}
	_ = exec.Command(command, args...).Start()
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `CLIProxyAPI-Lite — local multi-provider AI proxy

Usage:
  cliproxy-lite [serve] [--home PATH] [--ui-port 8318] [--no-open]
  cliproxy-lite init [--home PATH]
  cliproxy-lite keys [--home PATH]
  cliproxy-lite doctor [--home PATH]
  cliproxy-lite version`)
}
