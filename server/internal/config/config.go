// Package config loads the agenty server configuration from a YAML file plus
// environment overrides and validates it.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/goccy/go-yaml"
)

// Defaults for values the configuration file may omit.
const (
	DefaultAddress         = ":8080"
	DefaultShutdownTimeout = 30 * time.Second
)

// Config is the validated server configuration.
type Config struct {
	Server Server
}

// Server configures the HTTP server of the api role.
type Server struct {
	// Address is the host:port the HTTP server listens on.
	Address string
	// ShutdownTimeout bounds how long shutdown waits for in-flight requests.
	ShutdownTimeout time.Duration
}

// file mirrors the YAML layout. Values stay strings until validation so that
// every error can name the offending field.
type file struct {
	Server struct {
		Address         *string `yaml:"address"`
		ShutdownTimeout *string `yaml:"shutdownTimeout"`
	} `yaml:"server"`
}

// value is a raw setting together with where it came from.
type value struct {
	raw    string
	envVar string // set when the environment provided the value
}

func (v value) source(field string) string {
	if v.envVar != "" {
		return fmt.Sprintf("%s (from %s)", field, v.envVar)
	}
	return field
}

// Load reads the YAML file at path, applies overrides from the environment
// (looked up with lookupEnv, typically os.LookupEnv), and validates the
// result. Every validation error names the offending field.
func Load(path string, lookupEnv func(string) (string, bool)) (Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // The path is operator-supplied by design.
	if err != nil {
		return Config{}, fmt.Errorf("read configuration: %w", err)
	}
	var f file
	if err := yaml.UnmarshalWithOptions(data, &f, yaml.DisallowUnknownField()); err != nil {
		return Config{}, fmt.Errorf("invalid configuration file %s: %s", path, yaml.FormatError(err, false, true))
	}

	address := setting(f.Server.Address, DefaultAddress, "AGENTY_SERVER_ADDRESS", lookupEnv)
	shutdownTimeout := setting(f.Server.ShutdownTimeout, DefaultShutdownTimeout.String(), "AGENTY_SERVER_SHUTDOWN_TIMEOUT", lookupEnv)

	var cfg Config
	errs := []error{
		parseAddress(address, "server.address", &cfg.Server.Address),
		parsePositiveDuration(shutdownTimeout, "server.shutdownTimeout", &cfg.Server.ShutdownTimeout),
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// setting resolves one value: environment over file over default.
func setting(fromFile *string, def, envVar string, lookupEnv func(string) (string, bool)) value {
	if v, ok := lookupEnv(envVar); ok {
		return value{raw: v, envVar: envVar}
	}
	if fromFile != nil {
		return value{raw: *fromFile}
	}
	return value{raw: def}
}

func parseAddress(v value, field string, dst *string) error {
	_, port, err := net.SplitHostPort(v.raw)
	if err != nil {
		return fmt.Errorf("%s: %q is not a host:port address", v.source(field), v.raw)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n > 65535 {
		return fmt.Errorf("%s: %q has an invalid port", v.source(field), v.raw)
	}
	*dst = v.raw
	return nil
}

func parsePositiveDuration(v value, field string, dst *time.Duration) error {
	d, err := time.ParseDuration(v.raw)
	if err != nil {
		return fmt.Errorf("%s: %q is not a duration such as 30s", v.source(field), v.raw)
	}
	if d <= 0 {
		return fmt.Errorf("%s: %q must be positive", v.source(field), v.raw)
	}
	*dst = d
	return nil
}
