// Command watchcat-agent collects host metrics and serves them for a remote
// Prometheus server to scrape.
//
// Usage:
//
//	watchcat-agent run          # run in the foreground (or under a service manager)
//	watchcat-agent install      # install as a system service and start it
//	watchcat-agent uninstall    # stop and remove the system service
//	watchcat-agent start|stop|restart|status
//	watchcat-agent config       # print the effective configuration
//	watchcat-agent version
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
	"gopkg.in/yaml.v3"

	"github.com/aritaone/watchcat-agent/internal/agent"
	"github.com/aritaone/watchcat-agent/internal/buildinfo"
	"github.com/aritaone/watchcat-agent/internal/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command given")
	}

	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to the YAML configuration file")
	listen := fs.String("listen", "", "override server.listen_address, e.g. 0.0.0.0:9800")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	// A --listen flag wins over the file; the config layer reads it as env.
	if *listen != "" {
		if err := os.Setenv("WATCHCAT_LISTEN_ADDRESS", *listen); err != nil {
			return err
		}
	}

	switch cmd {
	case "run":
		return agent.RunService(*cfgPath)

	case "install", "uninstall", "start", "stop", "restart", "status":
		return control(cmd, *cfgPath)

	case "config":
		return printConfig(*cfgPath)

	case "version", "--version", "-v":
		fmt.Println(buildinfo.String())
		return nil

	case "help", "--help", "-h":
		usage()
		return nil

	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// control drives the platform service manager.
func control(action, cfgPath string) error {
	svc, err := agent.NewService(cfgPath)
	if err != nil {
		return err
	}

	if action == "status" {
		status, err := svc.Status()
		if err != nil {
			return err
		}
		switch status {
		case service.StatusRunning:
			fmt.Println("running")
		case service.StatusStopped:
			fmt.Println("stopped")
		default:
			fmt.Println("unknown")
		}
		return nil
	}

	if err := service.Control(svc, action); err != nil {
		return fmt.Errorf("%s service: %w (administrator/root privileges are usually required)", action, err)
	}

	switch action {
	case "install":
		fmt.Printf("installed service %q\n", agent.ServiceName)
		// Installing without starting leaves nothing collecting metrics.
		if err := service.Control(svc, "start"); err != nil {
			return fmt.Errorf("start service after install: %w", err)
		}
		fmt.Println("service started")
	case "uninstall":
		fmt.Printf("removed service %q\n", agent.ServiceName)
	default:
		fmt.Printf("service %s\n", action+"ed")
	}
	return nil
}

// printConfig shows the configuration the agent would actually use, after
// defaults, the file and the environment have been merged.
func printConfig(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	// Never print the shared secret.
	if cfg.Server.BearerToken != "" {
		cfg.Server.BearerToken = "<redacted>"
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("# effective configuration (source: %s)\n%s", path, out)
	return nil
}

// defaultConfigPath returns the conventional config location per platform.
func defaultConfigPath() string {
	if v := os.Getenv("WATCHCAT_CONFIG"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "watchcat-agent", "watchcat-agent.yaml")
	case "darwin":
		return "/usr/local/etc/watchcat-agent/watchcat-agent.yaml"
	default:
		return "/etc/watchcat-agent/watchcat-agent.yaml"
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `%s

Usage: watchcat-agent <command> [flags]

Commands:
  run          Run in the foreground, or under a service manager
  install      Install as a system service and start it (needs root/Administrator)
  uninstall    Stop and remove the system service
  start        Start the installed service
  stop         Stop the installed service
  restart      Restart the installed service
  status       Report whether the service is running
  config       Print the effective configuration
  version      Print build information

Flags:
  --config <path>   Configuration file (default %s)
  --listen <addr>   Override the listen address, e.g. 0.0.0.0:9800

Environment:
  WATCHCAT_CONFIG, WATCHCAT_LISTEN_ADDRESS, WATCHCAT_BEARER_TOKEN,
  WATCHCAT_TLS_CERT_FILE, WATCHCAT_TLS_KEY_FILE, WATCHCAT_LOG_LEVEL,
  WATCHCAT_LOG_FILE, WATCHCAT_SCRAPE_TIMEOUT, WATCHCAT_PER_CPU, WATCHCAT_LABELS
`, buildinfo.String(), defaultConfigPath())
}
