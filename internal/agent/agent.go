// Package agent wires configuration, collectors and the HTTP endpoint together
// and manages the process lifecycle.
package agent

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/aritaone/watchcat-agent/internal/buildinfo"
	"github.com/aritaone/watchcat-agent/internal/collector"
	"github.com/aritaone/watchcat-agent/internal/config"
	"github.com/aritaone/watchcat-agent/internal/server"
)

// Agent is one running instance of the metrics agent.
type Agent struct {
	cfg    *config.Config
	log    *slog.Logger
	srv    *server.Server
	closer io.Closer
}

// New loads the configuration at cfgPath and prepares everything to run.
func New(cfgPath string) (*Agent, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	log, closer, err := newLogger(cfg.Log)
	if err != nil {
		return nil, err
	}

	coll, err := collector.New(cfg, log)
	if err != nil {
		closeQuietly(closer)
		return nil, err
	}

	reg := prometheus.NewRegistry()
	if err := reg.Register(coll); err != nil {
		closeQuietly(closer)
		return nil, err
	}
	if cfg.Collectors.Go {
		// Runtime metrics for the agent process itself.
		reg.MustRegister(collectors.NewGoCollector())
	}

	srv := server.New(cfg, reg, log)
	srv.SetCollectors(coll.Enabled())

	log.Info("agent initialised",
		"version", buildinfo.Version,
		"config", cfgPath,
		"collectors", coll.Enabled())

	return &Agent{cfg: cfg, log: log, srv: srv, closer: closer}, nil
}

// Start binds the listener and begins serving. It does not block.
func (a *Agent) Start() error { return a.srv.Start() }

// Stop shuts the endpoint down, waiting for in-flight scrapes to finish.
func (a *Agent) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := a.srv.Shutdown(ctx)
	a.log.Info("agent stopped")
	closeQuietly(a.closer)
	return err
}

// Logger exposes the agent logger to the command layer.
func (a *Agent) Logger() *slog.Logger { return a.log }

// Addr reports the bound listen address.
func (a *Agent) Addr() string { return a.srv.Addr() }

func closeQuietly(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}
