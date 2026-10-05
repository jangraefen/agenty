package mcptool

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestContentText_UnknownContentBecomesAPlaceholder(t *testing.T) {
	assert.Equal(t, "[unsupported content omitted]", contentText([]mcp.Content{&mcp.ToolUseContent{}})) //nolint:staticcheck // SA1019: deprecated, which is why real servers will not send it in tool results
}
