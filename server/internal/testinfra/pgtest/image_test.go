package pgtest_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/testinfra/pgtest"
)

// The Compose file for local development must run the same image as the tests.
func TestComposeUsesSameImage(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose.yaml"))
	require.NoError(t, err, "read compose file")
	assert.Contains(t, string(compose), "image: "+pgtest.Image, "deploy/compose.yaml does not run the test image")
}

func TestImageMatchesPostgresMajor(t *testing.T) {
	image, tag := pgtest.Image, "-pg"+strconv.Itoa(pgtest.PostgresMajor)+"-"
	assert.Contains(t, image, tag, "Image does not match PostgresMajor")
}
