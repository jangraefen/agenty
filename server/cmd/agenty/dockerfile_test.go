package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The container image (deploy/agenty.Dockerfile) must be a multi-stage build
// with the workspace's Go version, pin its base images by digest, inject the
// version, label it, and run as a non-root user.
func TestDockerfile(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	dockerfile := readFile(t, filepath.Join(root, "deploy", "agenty.Dockerfile"))
	goVersion := regexp.MustCompile(`(?m)^go (\S+)$`).FindStringSubmatch(readFile(t, filepath.Join(root, "go.work")))
	if goVersion == nil {
		t.Fatal("go.work has no go directive")
	}

	froms := regexp.MustCompile(`(?m)^FROM\s+(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	if len(froms) < 2 {
		t.Fatalf("found %d FROM lines, want a multi-stage build", len(froms))
	}
	for _, from := range froms {
		if !regexp.MustCompile(`@sha256:[0-9a-f]{64}$`).MatchString(from[1]) {
			t.Errorf("base image %q is not pinned by digest", from[1])
		}
	}
	if want := "FROM golang:" + goVersion[1] + "-"; !strings.Contains(dockerfile, want) {
		t.Errorf("builder stage does not use the go.work Go version: missing %q", want)
	}
	if want := "-X github.com/jangraefen/agenty/server/internal/buildinfo.Version="; !strings.Contains(dockerfile, want) {
		t.Errorf("Dockerfile does not inject the version: missing %q", want)
	}
	if want := `org.opencontainers.image.version="${VERSION}"`; !strings.Contains(dockerfile, want) {
		t.Errorf("Dockerfile does not set the OCI version label: missing %q", want)
	}
	// Dependencies are downloaded in their own layer from go.mod and go.sum, so
	// that source changes do not invalidate the module cache.
	if !regexp.MustCompile(`(?m)^COPY\s+server/go\.mod\s+server/go\.su(m|\[m\])\s+\./\s*\nRUN go mod download$`).MatchString(dockerfile) {
		t.Error("Dockerfile does not copy go.mod and go.sum before `go mod download`")
	}
	users := regexp.MustCompile(`(?m)^USER\s+(\S+)`).FindAllStringSubmatch(dockerfile, -1)
	if len(users) == 0 {
		t.Fatal("Dockerfile sets no USER")
	}
	if user := users[len(users)-1][1]; user == "root" || user == "0" || strings.HasPrefix(user, "0:") || strings.HasPrefix(user, "root:") {
		t.Errorf("runtime USER = %q, want a non-root user", user)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
