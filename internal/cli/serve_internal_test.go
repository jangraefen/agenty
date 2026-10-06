package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "127.1.2.3:0", "[::1]:8080", "localhost:8080"} {
		require.NoError(t, checkLoopback(addr), addr)
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", "10.0.0.1:80", "example.com:80", "localhost.example.com:80"} {
		assert.Error(t, checkLoopback(addr), addr)
	}
}
