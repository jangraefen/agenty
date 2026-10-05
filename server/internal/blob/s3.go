package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3API is the subset of the S3 client that S3 uses; *s3.Client implements it.
type S3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	DeleteObject(
		ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options),
	) (*s3.DeleteObjectOutput, error)
}

// S3 stores objects in a bucket of an S3-compatible object store, under an
// optional key prefix. S3 replaces objects atomically on its own: an upload
// that fails or is aborted never becomes visible.
type S3 struct {
	client S3API
	bucket string
	prefix string
}

var _ Blob = (*S3)(nil)

// NewS3 returns a store for bucket. A non-empty prefix (e.g. "agenty/") is put
// in front of every key; it must end with "/" and otherwise be a valid key.
func NewS3(client S3API, bucket, prefix string) (*S3, error) {
	if client == nil {
		return nil, errors.New("blob: S3 client is nil")
	}
	if bucket == "" {
		return nil, errors.New("blob: S3 bucket is empty")
	}
	if prefix != "" {
		trimmed, ok := strings.CutSuffix(prefix, "/")
		if !ok {
			return nil, fmt.Errorf("blob: S3 prefix %q does not end with /", prefix)
		}
		if err := ValidateKey(trimmed); err != nil {
			return nil, fmt.Errorf("blob: S3 prefix: %w", err)
		}
	}
	return &S3{client: client, bucket: bucket, prefix: prefix}, nil
}

func (s *S3) object(ctx context.Context, key string) (*string, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	// S3 limits the stored key, prefix included.
	if len(s.prefix)+len(key) > MaxKeyLength {
		return nil, fmt.Errorf("%w: longer than %d bytes with prefix %q", ErrInvalidKey, MaxKeyLength, s.prefix)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return aws.String(s.prefix + key), nil
}

// Put implements Blob. The body is streamed and never buffered, so it is not
// seekable: the SDK neither hashes it for the signature (UNSIGNED-PAYLOAD) nor
// adds a checksum, which lets Put work over plain HTTP as well as HTTPS, and
// it cannot retry the upload. A transient S3 error therefore fails the Put;
// callers retry the whole Put. Over plain HTTP the content has no integrity
// protection beyond the size check; use HTTPS outside trusted networks.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := checkSize(size); err != nil {
		return err
	}
	object, err := s.object(ctx, key)
	if err != nil {
		return err
	}
	body := newSizedReader(ctx, r, size)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           object,
		Body:          body,
		ContentLength: aws.Int64(size),
	}, streamingUpload)
	if err != nil {
		// The SDK does not reliably wrap errors from the body; report the
		// reader's own error so callers can match ErrSizeMismatch or a
		// failure of r.
		if body.err != nil {
			err = body.err
		}
		return fmt.Errorf("blob: put %q: %w", key, err)
	}
	return nil
}

// streamingUpload configures one PutObject call for a body that cannot be
// seeked: without TLS, payload hashing and checksums would need to rewind it.
func streamingUpload(o *s3.Options) {
	o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
}

// Get implements Blob.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.object(ctx, key)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: object})
	if err != nil {
		return nil, s3Error("get", key, err)
	}
	return out.Body, nil
}

// Stat implements Blob.
func (s *S3) Stat(ctx context.Context, key string) (Info, error) {
	object, err := s.object(ctx, key)
	if err != nil {
		return Info{}, err
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: object})
	if _, ok := errors.AsType[*types.NotFound](err); ok {
		// HeadObject has no response body, so a missing bucket (or a wrong
		// endpoint) also reports NotFound. Only an existing bucket makes it a
		// missing object. S3 answers 404 rather than 403 only to callers that
		// may list the bucket, which is what HeadBucket needs as well.
		_, bucketErr := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
		if bucketErr != nil {
			return Info{}, fmt.Errorf("blob: stat %q: bucket %q: %w", key, s.bucket, bucketErr)
		}
	}
	if err != nil {
		return Info{}, s3Error("stat", key, err)
	}
	return Info{Size: aws.ToInt64(out.ContentLength)}, nil
}

// Delete implements Blob.
func (s *S3) Delete(ctx context.Context, key string) error {
	object, err := s.object(ctx, key)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: object}); err != nil {
		return fmt.Errorf("blob: delete %q: %w", key, err)
	}
	return nil
}

// s3Error maps a missing object to ErrNotFound. GetObject reports NoSuchKey;
// HeadObject has no response body and reports NotFound, which Stat maps only
// after checking that the bucket exists.
func s3Error(op, key string, err error) error {
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	return fmt.Errorf("blob: %s %q: %w", op, key, err)
}
