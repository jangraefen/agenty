package pgtest_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jangraefen/agenty/server/internal/testinfra/pgtest"
)

// The Compose file for local development must run the same image as the tests.
func TestComposeUsesSameImage(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	if want := "image: " + pgtest.Image; !strings.Contains(string(compose), want) {
		t.Errorf("deploy/compose.yaml does not contain %q", want)
	}
}

func TestImageMatchesPostgresMajor(t *testing.T) {
	image, tag := pgtest.Image, "-pg"+strconv.Itoa(pgtest.PostgresMajor)+"-"
	if !strings.Contains(image, tag) {
		t.Errorf("Image %q does not contain %q", image, tag)
	}
}
