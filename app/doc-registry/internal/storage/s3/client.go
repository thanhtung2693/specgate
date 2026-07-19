package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/specgate/doc-registry/internal/config"
)

type Client struct {
	s3        *awss3.Client
	presigner *awss3.PresignClient
	bucket    string
	region    string
}

// NewForTest creates a Client that points at a fake S3 endpoint (suitable for
// unit tests that only call PresignPut; no real HTTP calls are made
// during presigning). Pass any URL as endpoint (e.g. "http://127.0.0.1:1").
func NewForTest(ctx context.Context, endpoint string) (*Client, error) {
	return New(ctx, config.S3Config{
		Endpoint:     endpoint,
		Region:       "us-east-1",
		Bucket:       "test-bucket",
		AccessKey:    "test",
		SecretKey:    "test",
		UsePathStyle: true,
		EnsureBucket: false,
	})
}

func New(ctx context.Context, cfg config.S3Config) (*Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	s3c := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	c := &Client{
		s3:        s3c,
		presigner: awss3.NewPresignClient(s3c),
		bucket:    cfg.Bucket,
		region:    cfg.Region,
	}
	if cfg.EnsureBucket {
		if err := c.ensureBucket(ctx); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	if c.bucket == "" {
		return nil
	}
	_, err := c.s3.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	_, err = c.s3.CreateBucket(ctx, createBucketInput(c.bucket, c.region))
	if err == nil {
		return nil
	}
	var owned *types.BucketAlreadyOwnedByYou
	if errors.As(err, &owned) {
		return nil
	}
	var exists *types.BucketAlreadyExists
	if errors.As(err, &exists) {
		return nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return nil
		}
	}
	return fmt.Errorf("ensure s3 bucket %q: %w", c.bucket, err)
}

func createBucketInput(bucket, region string) *awss3.CreateBucketInput {
	in := &awss3.CreateBucketInput{Bucket: aws.String(bucket)}
	if region != "" && region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	return in
}

// ObjectKey builds an artifact-ID-scoped key. The optional prefix (e.g.
// "doc-registry/") is prepended verbatim so the same bucket can host multiple
// services without collision. Version remains in the key for operator clarity;
// artifact ID provides collision isolation before database commit.
func ObjectKey(prefix, artifactID, version, filename string) string {
	return prefix + fmt.Sprintf("artifacts/%s/%s/%s", artifactID, version, filename)
}

func (c *Client) PutObject(ctx context.Context, key string, body []byte) error {
	return c.PutObjectWithContentType(ctx, key, body, http.DetectContentType(body))
}

// PutObjectWithContentType stores an object with an explicit Content-Type. Use
// this when content sniffing is unreliable — e.g. HTML that an iframe loads by
// URL, where a mis-detected type (text/plain) would render as source instead of
// a page.
func (c *Client) PutObjectWithContentType(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := c.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put s3 object %s: %w", key, err)
	}
	return nil
}

func (c *Client) GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("get s3 object %s: positive read limit required", key)
	}
	out, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get s3 object %s: %w", key, err)
	}
	defer out.Body.Close()
	body, err := readObjectBody(out.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read s3 object %s: %w", key, err)
	}
	return body, nil
}

func readObjectBody(body io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("positive read limit required")
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("object exceeds %d-byte read limit", maxBytes)
	}
	return data, nil
}

// PresignPut returns a time-limited URL for uploading exactly sizeBytes with
// the given Content-Type.
func (c *Client) PresignPut(ctx context.Context, key, contentType string, sizeBytes int64, ttl time.Duration) (string, error) {
	in := &awss3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		ContentLength: aws.Int64(sizeBytes),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	out, err := c.presigner.PresignPutObject(ctx, in, func(opts *awss3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("presign put s3 object %s: %w", key, err)
	}
	return out.URL, nil
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete s3 object %s: %w", key, err)
	}
	return nil
}
