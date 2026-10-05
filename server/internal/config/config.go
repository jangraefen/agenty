// Package config loads the agenty server configuration from a YAML file plus
// environment overrides and validates it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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
	Server   Server
	Log      Log
	Database Database
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

// envDatabaseURL overrides database.url.
const envDatabaseURL = "AGENTY_DATABASE_URL"

// Database configures the PostgreSQL connection pool.
type Database struct {
	// URL is a PostgreSQL connection URL or keyword/value string as accepted
	// by pgx, including pool settings such as pool_max_conns. It may contain
	// the password, so every printed form of a Database redacts it.
	URL string
}

// redacted replaces a connection string that cannot be parsed, and so cannot
// be printed without risking its password.
const redacted = "[redacted]"

// String returns the connection target as a URL without the password or any
// other parameter, so that printing a Database, or a Config containing it,
// never reveals the password (ARCHITECTURE §3 invariant 6).
func (d Database) String() string {
	c, err := pgconn.ParseConfig(d.URL)
	if err != nil {
		return redacted
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.User(c.User),
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:   "/" + c.Database,
	}
	return u.String()
}

// GoString is the %#v form, with the URL redacted as in String.
func (d Database) GoString() string {
	return fmt.Sprintf("config.Database{URL:%q}", d.String())
}

// Format prints d with every fmt verb in its redacted form: %#v as GoString,
// %q quoted, and every other verb as String.
func (d Database) Format(f fmt.State, verb rune) {
	switch {
	case verb == 'v' && f.Flag('#'):
		_, _ = io.WriteString(f, d.GoString())
	case verb == 'q':
		_, _ = io.WriteString(f, strconv.Quote(d.String()))
	default:
		_, _ = io.WriteString(f, d.String())
	}
}

// MarshalJSON encodes d with the URL redacted as in String.
func (d Database) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{ URL string }{URL: d.String()})
}

// Secrets returns the password and the forms in which it can appear inside a
// connection URL, for registration with the log redaction (ARCHITECTURE §3
// invariant 6). It returns nil when there is no password or the URL cannot be
// parsed.
func (d Database) Secrets() []string {
	c, err := pgconn.ParseConfig(d.URL)
	if err != nil || c.Password == "" {
		return nil
	}
	forms := []string{c.Password}
	for _, escaped := range []string{
		url.UserPassword("", c.Password).String()[1:], // userinfo escaping, without the leading ':'
		url.QueryEscape(c.Password),
		url.PathEscape(c.Password),
	} {
		if !slices.Contains(forms, escaped) {
			forms = append(forms, escaped)
		}
	}
	return forms
}

// LogValue logs the connection target without the password, so a Database
// can be logged safely (ARCHITECTURE §3 invariant 6).
func (d Database) LogValue() slog.Value {
	c, err := pgconn.ParseConfig(d.URL)
	if err != nil {
		return slog.StringValue(redacted)
	}
	return slog.GroupValue(
		slog.String("host", c.Host),
		slog.Int("port", int(c.Port)),
		slog.String("name", c.Database),
		slog.String("user", c.User),
	)
}

// file mirrors the YAML layout. Values stay strings until validation so that
// every error can name the offending field.
type file struct {
	Server struct {
		Address         *string `yaml:"address"`
		ShutdownTimeout *string `yaml:"shutdownTimeout"`
	} `yaml:"server"`
	Database struct {
		URL *string `yaml:"url"`
	} `yaml:"database"`
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
	databaseURL := setting(f.Database.URL, "", envDatabaseURL, lookupEnv)

	var cfg Config
	errs := []error{
		parseAddress(address, "server.address", &cfg.Server.Address),
		parsePositiveDuration(shutdownTimeout, "server.shutdownTimeout", &cfg.Server.ShutdownTimeout),
		parseDatabaseURL(databaseURL, "database.url", &cfg.Database.URL),
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

// parseDatabaseURL requires a PostgreSQL connection string that the pgx pool
// accepts, including its pool_* settings, so a value that database.Open would
// reject fails here, naming the field. The error never repeats the value or
// pgx's parse error, which could contain the password (ARCHITECTURE §3
// invariant 6); for an unreadable file, such as sslrootcert, it adds only the
// file's path and the file-system error.
func parseDatabaseURL(v value, field string, dst *string) error {
	if v.raw == "" {
		return fmt.Errorf("%s: required; set it in the file or in %s", v.source(field), envDatabaseURL)
	}
	if _, err := pgxpool.ParseConfig(v.raw); err != nil {
		const invalid = "not a valid PostgreSQL connection URL or keyword/value string"
		if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
			return fmt.Errorf("%s: %s: cannot read %q: %w", v.source(field), invalid, pathErr.Path, pathErr.Err)
		}
		return fmt.Errorf("%s: %s", v.source(field), invalid)
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

// parseLogLevel accepts the four level names in any case, but not the
// offsets (such as info+2) that slog.Level.UnmarshalText also accepts.
func parseLogLevel(v value, field string, dst *slog.Level) error {
	for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if strings.EqualFold(v.raw, l.String()) {
			*dst = l
			return nil
		}
	}
	return fmt.Errorf("%s: %q is not a log level: use debug, info, warn, or error", v.source(field), v.raw)
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
