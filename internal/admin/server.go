package admin

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type Meta struct {
	Version  string `json:"version"`
	APIBase  string `json:"api_base"`
	AdminURL string `json:"admin_url"`
}

type Server struct {
	address       string
	target        *url.URL
	meta          Meta
	http          *http.Server
	counts        *requestCountStore
	managementKey string
}

func New(address, target string, meta Meta) (*Server, error) {
	return NewWithRequestCounts(address, target, meta, RequestCountsConfig{})
}

func NewWithRequestCounts(address, target string, meta Meta, countsConfig RequestCountsConfig) (*Server, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse API target: %w", err)
	}
	counts, err := newRequestCountStore(countsConfig.Path)
	if err != nil {
		return nil, err
	}
	server := &Server{address: address, target: targetURL, meta: meta, counts: counts, managementKey: strings.TrimSpace(countsConfig.ManagementKey)}
	server.http = &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	return server, nil
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("start Web UI on %s: %w", s.address, err)
	}
	go s.pollRequestCounts(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()
	go func() {
		if serveErr := s.http.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			fmt.Printf("Web UI server stopped: %v\n", serveErr)
		}
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// assetTag is a short fingerprint of the embedded web assets. It is appended to
// the script/stylesheet URLs in index.html so a rebuilt binary always busts any
// copy the browser is holding on to, even though the file names never change.
// versionedIndex serves index.html with ?v=<tag> appended to its local asset
// references; every other file is served untouched.
func versionedIndex(assets fs.FS, tag string) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	rewrite := strings.NewReplacer(
		`href="styles.css"`, `href="styles.css?v=`+tag+`"`,
		`src="app.js"`, `src="app.js?v=`+tag+`"`,
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// StripPrefix leaves an empty path for the /ui/ root.
		if r.URL.Path != "" && r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}
		page, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		body := rewrite.Replace(string(page))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	})
}

func assetTag(assets fs.FS) string {
	sum := sha256.New()
	_ = fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(assets, path)
		if readErr != nil {
			return readErr
		}
		_, _ = sum.Write([]byte(path))
		_, _ = sum.Write(data)
		return nil
	})
	return hex.EncodeToString(sum.Sum(nil))[:12]
}

func (s *Server) Handler() http.Handler {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	static := http.StripPrefix("/ui/", versionedIndex(assets, assetTag(assets)))
	managementProxy := s.reverseProxy("/api/", "/v0/management/")
	apiProxy := s.reverseProxy("/proxy/", "/")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})
	mux.Handle("/ui/", securityHeaders(static))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/meta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.meta)
	})
	mux.Handle("/api/", managementProxy)
	mux.Handle("/proxy/", apiProxy)
	return localOnly(mux)
}

func (s *Server) reverseProxy(fromPrefix, toPrefix string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(s.target)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalPath := request.URL.Path
		originalDirector(request)
		remainder := strings.TrimPrefix(originalPath, fromPrefix)
		request.URL.Path = path.Join(toPrefix, remainder)
		if strings.HasSuffix(originalPath, "/") && !strings.HasSuffix(request.URL.Path, "/") {
			request.URL.Path += "/"
		}
		request.Host = s.target.Host
	}
	if fromPrefix == "/api/" && toPrefix == "/v0/management/" && s.counts != nil {
		proxy.ModifyResponse = s.decorateAuthFilesResponse
	}
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":   "local proxy service is unavailable",
			"details": err.Error(),
		})
	}
	return proxy
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(strings.TrimSpace(host))
		if ip == nil || !ip.IsLoopback() {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "local access only"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
