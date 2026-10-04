//go:build module

package blob_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/blob"
)

// fakeS3 runs an in-process S3-compatible server (gofakes3, MIT; ARCHITECTURE
// §16) over TLS and returns a client for it, so the real AWS SDK talks the S3
// protocol over HTTP.
func fakeS3(t *testing.T) *s3.Client {
	t.Helper()
	srv := httptest.NewTLSServer(gofakes3.New(s3mem.New()).Server())
	t.Cleanup(srv.Close)
	return s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
		HTTPClient:  srv.Client(),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(srv.URL)
		o.UsePathStyle = true
	})
}

func newS3(t *testing.T, prefix string) (blob.Blob, *s3.Client, string) {
	t.Helper()
	client := fakeS3(t)
	const bucket = "agenty-test"
	_, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	s, err := blob.NewS3(client, bucket, prefix)
	require.NoError(t, err)
	return s, client, bucket
}

func TestS3Contract(t *testing.T) {
	runContract(t, func(t *testing.T) blob.Blob {
		s, _, _ := newS3(t, "")
		return s
	})
}

func TestS3ContractWithPrefix(t *testing.T) {
	runContract(t, func(t *testing.T) blob.Blob {
		s, _, _ := newS3(t, "agenty/blobs/")
		return s
	})
}

func TestS3StoresObjectsUnderPrefix(t *testing.T) {
	s, client, bucket := newS3(t, "tenant/")
	put(t, s, "runs/r1/out", "data")

	out, err := client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("tenant/runs/r1/out"),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(4), aws.ToInt64(out.ContentLength))
}

func TestS3MissingBucketIsNotNotFound(t *testing.T) {
	s, err := blob.NewS3(fakeS3(t), "no-such-bucket", "")
	require.NoError(t, err)
	ctx := context.Background()

	_, err = s.Get(ctx, "k")
	require.Error(t, err)
	assert.NotErrorIs(t, err, blob.ErrNotFound, "Get")
	assert.Error(t, s.Put(ctx, "k", strings.NewReader("v"), 1), "Put")
}
