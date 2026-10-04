package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The container image (deploy/agenty.Dockerfile) must be a multi-stage build
// with the workspace's Go version, pin its base images by digest, inject the
// version, label it, and run as a non-root user.
func TestDockerfile(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	dockerfile := readFile(t, filepath.Join(root, "deploy", "agenty.Dockerfile"))
	goVersion := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(readFile(t, filepath.Join(root, "go.work")))
	require.NotNil(t, goVersion, "go.work has no go directive")

	froms := regexp.MustCompile(`(?m)^FROM\s+(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	require.GreaterOrEqual(t, len(froms), 2, "found %d FROM lines, want a multi-stage build", len(froms))
	for _, from := range froms {
		assert.Regexp(t, `@sha256:[0-9a-f]{64}$`, from[1], "base image %q is not pinned by digest", from[1])
	}
	assert.Contains(t, dockerfile, "FROM golang:"+goVersion[1]+"-", "builder stage does not use the go.work Go version")
	assert.Contains(t, dockerfile, "-X github.com/jangraefen/agenty/server/internal/buildinfo.Version=", "Dockerfile does not inject the version")
	assert.Contains(t, dockerfile, `org.opencontainers.image.version="${VERSION}"`, "Dockerfile does not set the OCI version label")
	// Dependencies are downloaded in their own layer from go.mod and go.sum, so
	// that source changes do not invalidate the module cache.
	assert.Regexp(t, `(?m)^COPY\s+server/go\.mod\s+server/go\.su(m|\[m\])\s+\./\s*\nRUN go mod download$`, dockerfile,
		"Dockerfile does not copy go.mod and go.sum before `go mod download`")
	users := regexp.MustCompile(`(?m)^USER\s+(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	require.NotEmpty(t, users, "Dockerfile sets no USER")
	user := users[len(users)-1][1]
	assert.False(t, user == "root" || user == "0" || strings.HasPrefix(user, "0:") || strings.HasPrefix(user, "root:"),
		"runtime USER = %q, want a non-root user", user)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return string(data)
}
