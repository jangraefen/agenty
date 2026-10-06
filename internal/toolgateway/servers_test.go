package toolgateway_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/toolgateway"
	"github.com/jangraefen/agenty/internal/toolgateway/gatewaytest"
)

// serverConfig grants files_read. The files server serves files_read and
// files_delete; the mail server serves nothing the harness grants.
func serverConfig() (toolgateway.Config, *gatewaytest.Server, *gatewaytest.Server) {
	files := &gatewaytest.Server{Tools: []toolgateway.Tool{
		&gatewaytest.Tool{Name: "files_read", Result: json.RawMessage(`{"content":"x"}`)},
		&gatewaytest.Tool{Name: "files_delete"},
	}}
	mail := &gatewaytest.Server{Tools: []toolgateway.Tool{&gatewaytest.Tool{Name: "mail_send"}}}
	return toolgateway.Config{
		Granted:      []string{"files_read"},
		Servers:      map[string]toolgateway.ToolServer{"files": files, "mail": mail},
		MaxToolCalls: 10,
		Policy:       &gatewaytest.Policy{},
		Audit:        &gatewaytest.Audit{},
	}, files, mail
}

func TestNew_StartsOnlyGrantedServers(t *testing.T) {
	cfg, files, mail := serverConfig()

	gw, err := toolgateway.New(context.Background(), cfg)

	require.NoError(t, err)
	assert.Equal(t, []string{"files"}, files.StartedAs, "a server is started under its configured name")
	assert.Empty(t, mail.StartedAs, "no grant needs the mail server, so it never runs")
	defs := gw.Definitions()
	require.Len(t, defs, 1)
	assert.Equal(t, "files_read", defs[0].Name, "only granted server tools are offered")

	result, err := gw.Start().Call(context.Background(), toolgateway.ToolCall{Name: "files_read"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"x"}`, string(result))
	_, err = gw.Start().Call(context.Background(), toolgateway.ToolCall{Name: "files_delete"})
	require.ErrorIs(t, err, toolgateway.ErrDenied, "a server's ungranted tools stay denied")

	require.NoError(t, gw.Close())
	assert.Equal(t, 1, files.Closed)
	require.NoError(t, gw.Close(), "closing twice stops nothing twice")
	assert.Equal(t, 1, files.Closed)
}

func TestNew_ServerFailuresStopStartedServers(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(cfg *toolgateway.Config, files, mail *gatewaytest.Server)
		wantErr string
		// wantClosed is how often each started server was stopped.
		wantFilesClosed, wantMailClosed int
	}{
		{
			name:            "start fails",
			setup:           func(_ *toolgateway.Config, _, mail *gatewaytest.Server) { mail.StartErr = assert.AnError },
			wantErr:         "server mail: start: " + assert.AnError.Error(),
			wantFilesClosed: 1,
		},
		{
			name:            "listing tools fails",
			setup:           func(_ *toolgateway.Config, _, mail *gatewaytest.Server) { mail.ToolsErr = assert.AnError },
			wantErr:         "server mail: tools: " + assert.AnError.Error(),
			wantFilesClosed: 1, wantMailClosed: 1,
		},
		{
			name: "tool outside the server's name",
			setup: func(_ *toolgateway.Config, _, mail *gatewaytest.Server) {
				mail.Tools = []toolgateway.Tool{&gatewaytest.Tool{Name: "files_send"}}
			},
			wantErr:         `server mail: tool "files_send" is not named mail_<tool>`,
			wantFilesClosed: 1, wantMailClosed: 1,
		},
		{
			name: "server lists a nil tool",
			setup: func(_ *toolgateway.Config, _, mail *gatewaytest.Server) {
				mail.Tools = []toolgateway.Tool{nil}
			},
			wantErr:         `server mail: tool "<nil>" is not named mail_<tool>`,
			wantFilesClosed: 1, wantMailClosed: 1,
		},
		{
			name: "server lists a tool twice",
			setup: func(_ *toolgateway.Config, _, mail *gatewaytest.Server) {
				mail.Tools = append(mail.Tools, &gatewaytest.Tool{Name: "mail_send"})
			},
			wantErr:         `duplicate tool "mail_send"`,
			wantFilesClosed: 1, wantMailClosed: 1,
		},
		{
			name: "stopping fails too",
			setup: func(_ *toolgateway.Config, files, mail *gatewaytest.Server) {
				mail.StartErr = assert.AnError
				files.CloseErr = gatewaytest.ErrAuditDown
			},
			wantErr:         gatewaytest.ErrAuditDown.Error(),
			wantFilesClosed: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, files, mail := serverConfig()
			cfg.Granted = append(cfg.Granted, "mail_send")
			tt.setup(&cfg, files, mail)

			gw, err := toolgateway.New(context.Background(), cfg)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, gw)
			assert.Equal(t, tt.wantFilesClosed, files.Closed)
			assert.Equal(t, tt.wantMailClosed, mail.Closed)
		})
	}
}

func TestNew_ValidatesBeforeStartingServers(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(cfg *toolgateway.Config)
		wantErr string
	}{
		{"invalid config", func(cfg *toolgateway.Config) { cfg.Policy = nil }, "policy is required"},
		{"nil server", func(cfg *toolgateway.Config) { cfg.Servers["tickets"] = nil }, "server tickets is nil"},
		{"invalid server name", func(cfg *toolgateway.Config) { cfg.Servers["my_tickets"] = &gatewaytest.Server{} }, `server name "my_tickets" must be`},
		{"server name clashes with a tool", func(cfg *toolgateway.Config) {
			cfg.Tools = []toolgateway.Tool{&gatewaytest.Tool{Name: "files_read"}}
		}, `tool "files_read" is named like a tool of server files`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, files, _ := serverConfig()
			tt.setup(&cfg)

			_, err := toolgateway.New(context.Background(), cfg)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, files.StartedAs, "nothing starts on a config that cannot work")
		})
	}
}

func TestClose_ReportsEveryServer(t *testing.T) {
	cfg, files, mail := serverConfig()
	cfg.Granted = append(cfg.Granted, "mail_send")
	files.CloseErr = assert.AnError
	mail.CloseErr = gatewaytest.ErrAuditDown
	gw, err := toolgateway.New(context.Background(), cfg)
	require.NoError(t, err)

	err = gw.Close()

	require.ErrorIs(t, err, assert.AnError)
	require.ErrorIs(t, err, gatewaytest.ErrAuditDown)
	require.ErrorContains(t, err, "server files: stop")
	assert.Equal(t, 1, files.Closed)
	assert.Equal(t, 1, mail.Closed, "one failure does not leave the next server running")
}
