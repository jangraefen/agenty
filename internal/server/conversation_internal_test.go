package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/store"
)

func TestConversationCursor_RoundTripsMicroseconds(t *testing.T) {
	cv := store.ConversationSummary{ID: "R1", UpdatedAt: time.Date(2026, 10, 8, 12, 0, 0, 123456000, time.UTC)}

	at, id, err := parseConversationCursor(conversationCursor(cv))

	require.NoError(t, err)
	assert.True(t, cv.UpdatedAt.Equal(at), "the cursor keeps the microseconds the store compares")
	assert.Equal(t, "R1", id)
}
