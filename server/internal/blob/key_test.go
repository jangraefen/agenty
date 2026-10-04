package blob_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jangraefen/agenty/server/internal/blob"
)

func TestValidateKeyAcceptsPortableKeys(t *testing.T) {
	for _, key := range []string{
		"a",
		"report.txt",
		"runs/0f8e4c1a-6b1d-4f7e-9c2a-3d5e7f9a1b2c/artifacts/out_v1.2-final.csv",
		"A/b/C",
		"a..b",
		"trailing.",
		strings.Repeat("k", blob.MaxKeyLength),
	} {
		assert.NoError(t, blob.ValidateKey(key), "key %q", key)
	}
}

func TestValidateKeyRejectsInvalidKeys(t *testing.T) {
	for _, key := range invalidKeys() {
		assert.ErrorIs(t, blob.ValidateKey(key), blob.ErrInvalidKey, "key %q", key)
	}
}
