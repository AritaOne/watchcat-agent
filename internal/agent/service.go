package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kardianos/service"
)

// ServiceName is the identifier registered with systemd, launchd or the
// Windows Service Control Manager.
const ServiceName = "watchcat-agent"

// program adapts Agent to the service.Interface contract. The service manager
// calls Start on boot and Stop on shutdown; both must return promptly.
type program struct {
	cfgPath string
	agent   *Agent
}

func (p *program) Start(service.Service) error {
	a, err := New(p.cfgPath)
	if err != nil {
		return err
	}
	p.agent = a
	return a.Start()
}

func (p *program) Stop(service.Service) error {
	if p.agent == nil {
		return nil
	}
	return p.agent.Stop()
}

// NewService builds the platform service handle for the given config path.
// cfgPath is made absolute because service managers start the process with an
// unpredictable working directory.
func NewService(cfgPath string) (service.Service, error) {
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate executable: %w", err)
	}

	cfg := &service.Config{
		Name:        ServiceName,
		DisplayName: "Watchcat Metrics Agent",
		Description: "Collects host metrics and exposes them for Prometheus to scrape.",
		Executable:  exe,
		Arguments:   []string{"run", "--config", abs},
		Option: service.KeyValue{
			// Keep the agent alive across crashes on every platform.
			"Restart":                "always",
			"RestartSec":             5,
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetDuration": "10m",
			"KeepAlive":              true,
			"RunAtLoad":              true,
		},
	}

	return service.New(&program{cfgPath: abs}, cfg)
}

// RunService runs the agent under the platform service manager when installed
// as a service, and interactively (blocking until Ctrl-C) otherwise.
func RunService(cfgPath string) error {
	svc, err := NewService(cfgPath)
	if err != nil {
		return err
	}
	return svc.Run()
}
