package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDuration_IsReadable(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, "1h"},
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h30m"},
		{10 * time.Second, "10s"},
		{20 * time.Millisecond, "20ms"},
	} {
		assert.Equal(t, tt.want, duration(tt.d))
	}
}
