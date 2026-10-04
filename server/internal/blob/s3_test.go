package blob_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/blob"
)

// failingS3 fails every call; the contract suite (module tier) covers the
// behavior against an S3-compatible server.
type failingS3 struct{ err error }

func (f failingS3) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return nil, f.err
}

func (f failingS3) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, f.err
}

func (f failingS3) HeadObject(
	context.Context, *s3.HeadObjectInput, ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	return nil, f.err
}

func (f failingS3) DeleteObject(
	context.Context, *s3.DeleteObjectInput, ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	return nil, f.err
}

func TestS3PassesOnClientErrors(t *testing.T) {
	errDown := errors.New("service unavailable")
	s, err := blob.NewS3(failingS3{errDown}, "bucket", "")
	require.NoError(t, err)
	ctx := context.Background()

	require.ErrorIs(t, s.Put(ctx, "k", strings.NewReader("v"), 1), errDown, "Put")
	_, err = s.Get(ctx, "k")
	require.ErrorIs(t, err, errDown, "Get")
	_, err = s.Stat(ctx, "k")
	require.ErrorIs(t, err, errDown, "Stat")
	require.ErrorIs(t, s.Delete(ctx, "k"), errDown, "Delete")
	assert.NotErrorIs(t, err, blob.ErrNotFound)
}

func TestNewS3ValidatesArguments(t *testing.T) {
	client := failingS3{}
	tests := map[string]struct {
		client blob.S3API
		bucket string
		prefix string
	}{
		"NilClient":            {nil, "bucket", ""},
		"EmptyBucket":          {client, "", ""},
		"PrefixWithoutSlash":   {client, "bucket", "agenty"},
		"PrefixWithTraversal":  {client, "bucket", "../agenty/"},
		"PrefixOnlySlash":      {client, "bucket", "/"},
		"PrefixWithEmptyLevel": {client, "bucket", "a//"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := blob.NewS3(tt.client, tt.bucket, tt.prefix)
			assert.Error(t, err)
		})
	}

	for _, prefix := range []string{"", "agenty/", "a/b/"} {
		_, err := blob.NewS3(client, "bucket", prefix)
		assert.NoError(t, err, "prefix %q", prefix)
	}
}
