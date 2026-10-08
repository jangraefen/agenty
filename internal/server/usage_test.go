package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/api"
	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/model/modeltest"
)

func TestRun_ReportsUsage(t *testing.T) {
	f := newFixture(t, options{})
	f.putNotes(t)
	calling := modeltest.CallTools(call("c1", "files_read", `{"path":"notes.md"}`))
	calling.Response.Usage = &model.Usage{InputTokens: 100, OutputTokens: 10, CacheWriteTokens: 1000}
	answering := modeltest.Reply("- milk")
	answering.Response.Usage = &model.Usage{InputTokens: 20, OutputTokens: 5, CacheReadTokens: 1000}
	f.script(calling, answering)

	run := f.startRun(t, "tidy my notes")
	finished := f.finish(t, run.ID)

	want := api.Usage{InputTokens: 120, OutputTokens: 15, CacheWriteTokens: 1000, CacheReadTokens: 1000}
	assert.Equal(t, want, finished.Usage, "the finished event carries the run's usage")
	var got api.Run
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, home+"/runs/"+run.ID, nil, &got))
	assert.Equal(t, want, got.Usage)
	transcript, err := f.store.Transcript(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, transcript, 4)
	assert.Nil(t, transcript[0].Usage, "an input has no usage")
	assert.Equal(t, &model.Usage{InputTokens: 100, OutputTokens: 10, CacheWriteTokens: 1000}, transcript[1].Usage, "each reply keeps its call's usage")
	assert.Equal(t, &model.Usage{InputTokens: 20, OutputTokens: 5, CacheReadTokens: 1000}, transcript[3].Usage)
}
