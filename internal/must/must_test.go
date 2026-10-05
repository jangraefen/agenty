package must_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jangraefen/agenty/internal/must"
)

func TestValue(t *testing.T) {
	assert.Equal(t, 42, must.Value(42, nil))
	assert.PanicsWithError(t, "boom", func() { must.Value(0, errors.New("boom")) })
}
