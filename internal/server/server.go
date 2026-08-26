// Package server exposes the collected metrics over HTTP for Prometheus to pull.
package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/aritaone/watchcat-agent/internal/buildinfo"
	"github.com/aritaone/watchcat-agent/internal/config"
)

// Server serves the Prometheus scrape endpoint.
type Server struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Server
	ln   net.Listener

	// collectors is the human readable collector list shown on the landing page.
	collectors string
}

// New builds the HTTP server around the given registry.
func New(cfg *config.Config, reg *prometheus.Registry, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, log: log}

	metricsHandler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog:            slogErrorLog{log},
		ErrorHandling:       promhttp.ContinueOnError,
		MaxRequestsInFlight: 4,
		Timeout:             cfg.Server.ScrapeTimeout,
		EnableOpenMetrics:   true,
	})

	mux := http.NewServeMux()
	mux.Handle(cfg.Server.MetricsPath, s.authenticate(metricsHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", s.index)

	s.http = &http.Server{
		Addr:              cfg.Server.ListenAddress,
		Handler:           s.recoverPanic(s.logRequests(mux)),
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		ErrorLog:          slogErrorLog{log}.stdLogger(),
	}
	if cfg.TLSEnabled() {
		s.http.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return s
}

// Start binds the listener and serves in the background. It returns as soon as
// the port is bound, so a service manager sees a fast, reliable startup.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Server.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Server.ListenAddress, err)
	}
	s.ln = ln

	scheme := "http"
	if s.cfg.TLSEnabled() {
		scheme = "https"
	}
	s.log.Info("metrics endpoint listening",
		"url", fmt.Sprintf("%s://%s%s", scheme, ln.Addr().String(), s.cfg.Server.MetricsPath),
		"auth", s.cfg.Server.BearerToken != "")

	go func() {
		var serr error
		if s.cfg.TLSEnabled() {
			serr = s.http.ServeTLS(ln, s.cfg.Server.TLSCertFile, s.cfg.Server.TLSKeyFile)
		} else {
			serr = s.http.Serve(ln)
		}
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			s.log.Error("http server stopped", "err", serr)
		}
	}()
	return nil
}

// Shutdown drains in-flight scrapes and closes the listener.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Addr reports the bound address, useful when the port was chosen by the OS.
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.cfg.Server.ListenAddress
	}
	return s.ln.Addr().String()
}

// authenticate enforces the bearer token when one is configured.
func (s *Server) authenticate(next http.Handler) http.Handler {
	token := s.cfg.Server.BearerToken
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		// Constant-time compare keeps the token safe from timing probes.
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="watchcat"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Debug("request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"remote", clientIP(r), "duration", time.Since(start).String())
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request", "path", r.URL.Path, "panic", rec)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><title>watchcat-agent</title>
<style>body{font-family:system-ui,sans-serif;margin:3rem auto;max-width:40rem;line-height:1.6}code{background:#eee;padding:.1rem .3rem;border-radius:3px}</style>
</head><body>
<h1>watchcat-agent</h1>
<p>{{.Build}}</p>
<p>Metrics: <a href="{{.Path}}">{{.Path}}</a></p>
<p>Health: <a href="/healthz">/healthz</a></p>
<p>Enabled collectors: <code>{{.Collectors}}</code></p>
</body></html>`))

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, struct{ Build, Path, Collectors string }{
		Build:      buildinfo.String(),
		Path:       s.cfg.Server.MetricsPath,
		Collectors: s.collectors,
	})
}

// SetCollectors records the active collector names for the landing page.
func (s *Server) SetCollectors(names []string) { s.collectors = strings.Join(names, ", ") }

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
