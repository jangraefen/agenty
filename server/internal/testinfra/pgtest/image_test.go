package pgtest_test

import (
	"os"
	"path/filepath"
	"regexp"
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

// Log redaction replaces every occurrence of the database password, so the
// development password must not be part of names that appear in logs, such as
// the product, database, and user name "agenty".
func TestComposePasswordIsDistinctive(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose.yaml"))
	require.NoError(t, err, "read compose file")
	m := regexp.MustCompile(`(?m)^\s*POSTGRES_PASSWORD:\s*(\S+)\s*$`).FindStringSubmatch(string(compose))
	require.Len(t, m, 2, "deploy/compose.yaml sets no POSTGRES_PASSWORD")
	assert.NotContains(t, "agenty", m[1], "development password %q is part of the name agenty, so log redaction would hide that name", m[1])
}

func TestImageMatchesPostgresMajor(t *testing.T) {
	image, tag := pgtest.Image, "-pg"+strconv.Itoa(pgtest.PostgresMajor)+"-"
	assert.Contains(t, image, tag, "Image does not match PostgresMajor")
}
