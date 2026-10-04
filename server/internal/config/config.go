// Package config loads the agenty server configuration from a YAML file plus
// environment overrides and validates it.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/jangraefen/agenty/server/internal/logging"
)

// Defaults for values the configuration file may omit.
const (
	DefaultAddress         = ":8080"
	DefaultShutdownTimeout = 30 * time.Second
	DefaultLogFormat       = logging.FormatText
	DefaultLogLevel        = slog.LevelInfo
)

// Config is the validated server configuration.
type Config struct {
	Server Server
	Log    Log
}

// Log configures log output.
type Log struct {
	// Format is text (development) or json or logfmt (production).
	Format logging.Format
	// Level is the minimum level written: debug, info, warn, or error.
	Level slog.Level
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
	Log struct {
		Format *string `yaml:"format"`
		Level  *string `yaml:"level"`
	} `yaml:"log"`
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
		parseLogFormat(setting(f.Log.Format, string(DefaultLogFormat), "AGENTY_LOG_FORMAT", lookupEnv), "log.format", &cfg.Log.Format),
		parseLogLevel(setting(f.Log.Level, DefaultLogLevel.String(), "AGENTY_LOG_LEVEL", lookupEnv), "log.level", &cfg.Log.Level),
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

func parseLogFormat(v value, field string, dst *logging.Format) error {
	f, err := logging.ParseFormat(v.raw)
	if err != nil {
		return fmt.Errorf("%s: %q is not a log format: use text, json, or logfmt", v.source(field), v.raw)
	}
	*dst = f
	return nil
}

func parseLogLevel(v value, field string, dst *slog.Level) error {
	if err := dst.UnmarshalText([]byte(v.raw)); err != nil {
		return fmt.Errorf("%s: %q is not a log level: use debug, info, warn, or error", v.source(field), v.raw)
	}
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
