// Package platform holds process-level concerns shared by the platform
// packages: configuration today, logging and health in later tasks.
package platform

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// Config is the validated runtime configuration of `rulefare serve`.
type Config struct {
	// HTTPAddr is the listen address. The default binds to loopback only;
	// containers set RULEFARE_HTTP_ADDR=:8080 explicitly.
	HTTPAddr string
	// ShutdownTimeout bounds how long in-flight requests may run after
	// SIGINT/SIGTERM before connections are closed.
	ShutdownTimeout time.Duration
}

const (
	defaultHTTPAddr        = "127.0.0.1:8080"
	defaultShutdownTimeout = 15 * time.Second
)

// LoadConfig reads RULEFARE_* variables through getenv (os.Getenv in
// production) and reports every invalid value at once.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{HTTPAddr: defaultHTTPAddr, ShutdownTimeout: defaultShutdownTimeout}
	var errs []error

	if v := getenv("RULEFARE_HTTP_ADDR"); v != "" {
		if _, port, err := net.SplitHostPort(v); err != nil || port == "" {
			errs = append(errs, fmt.Errorf("RULEFARE_HTTP_ADDR must be host:port, got %q", v))
		} else {
			cfg.HTTPAddr = v
		}
	}
	if v := getenv("RULEFARE_SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("RULEFARE_SHUTDOWN_TIMEOUT must be a positive duration such as 15s, got %q", v))
		} else {
			cfg.ShutdownTimeout = d
		}
	}
	return cfg, errors.Join(errs...)
}
