package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/config"
	"github.com/jangraefen/agenty/server/internal/logging"
)

func TestLoggerRedactsDatabasePassword(t *testing.T) {
	const (
		password = "s3cr3t/p@ss"
		escaped  = "s3cr3t%2Fp%40ss"
		dsn      = "postgres://agenty:" + escaped + "@db.internal:5432/agenty?sslmode=disable"
	)
	for _, format := range []logging.Format{logging.FormatText, logging.FormatJSON, logging.FormatLogfmt} {
		t.Run(string(format), func(t *testing.T) {
			var out bytes.Buffer
			cfg := config.Config{
				Log:      config.Log{Format: format, Level: slog.LevelInfo},
				Database: config.Database{URL: dsn},
			}

			logger, err := newLogger(cfg, &out)
			require.NoError(t, err)
			logger.Info("connecting with "+password, "dsn", dsn, "password", password)

			assert.NotContains(t, out.String(), password, "plain password reached the log")
			assert.NotContains(t, out.String(), escaped, "URL-escaped password reached the log")
			assert.Contains(t, out.String(), logging.Redacted, "password must be replaced")
		})
	}
}

func TestDatabaseWithoutPasswordRegistersNoSecret(t *testing.T) {
	var out bytes.Buffer
	cfg := config.Config{
		Log:      config.Log{Format: logging.FormatText, Level: slog.LevelInfo},
		Database: config.Database{URL: "postgres://agenty@db.internal:5432/agenty"},
	}

	logger, err := newLogger(cfg, &out)
	require.NoError(t, err)
	logger.Info("connecting", "user", "agenty")

	assert.NotContains(t, out.String(), logging.Redacted, "nothing may be redacted without a password")
}
