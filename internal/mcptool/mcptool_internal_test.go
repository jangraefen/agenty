package mcptool

import (
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestContentText_UnknownContentBecomesAPlaceholder(t *testing.T) {
	assert.Equal(t, "[unsupported content omitted]", contentText([]mcp.Content{&mcp.ToolUseContent{}})) //nolint:staticcheck // SA1019: deprecated, which is why real servers will not send it in tool results
}

func TestCommand_ServerGetsOnlyItsOwnEnvironment(t *testing.T) {
	t.Setenv("AGENTY_TEST_SECRET", "s3cret")
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"configured variables plus PATH", map[string]string{"GREETING": "hello"}, []string{"PATH=" + os.Getenv("PATH"), "GREETING=hello"}},
		{"a configured PATH replaces Agenty's", map[string]string{"PATH": "/opt/mcp/bin"}, []string{"PATH=/opt/mcp/bin"}},
		{"nothing configured", nil, []string{"PATH=" + os.Getenv("PATH")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := command(Server{Command: "tickets-mcp", Args: []string{"--read-only"}, Env: tt.env})

			assert.Equal(t, []string{"tickets-mcp", "--read-only"}, cmd.Args)
			assert.ElementsMatch(t, tt.want, cmd.Environ(), "Agenty's own environment, such as AGENTY_TEST_SECRET, never reaches the server")
		})
	}
}

func TestClientVersion_IsNeverEmpty(t *testing.T) {
	assert.NotEmpty(t, clientVersion())
}
