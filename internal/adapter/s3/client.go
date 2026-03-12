package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectInfo is the minimal object metadata needed by the backup restore flow.
type ObjectInfo struct {
	Key          string
	LastModified time.Time
	Size         int64
}

var (
	errS3BucketRequired      = errors.New("s3 bucket is required")
	errS3ClientNil           = errors.New("s3 client is nil")
	errS3EndpointRequired    = errors.New("s3 endpoint is required")
	errS3EndpointMissingHost = errors.New("parse s3 endpoint: missing host")
	errS3EndpointBadScheme   = errors.New("parse s3 endpoint: unsupported scheme")
)

// Client wraps the minimal S3 operations ImSub needs for backups.
type Client struct {
	mc     *minio.Client
	bucket string
}

// NewClient creates an S3-compatible client for a dedicated backup bucket.
func NewClient(endpoint, bucket, accessKey, secretKey, region string) (*Client, error) {
	normalizedEndpoint, secure, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, errS3BucketRequired
	}
	mc, err := minio.New(normalizedEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("new minio client: %w", err)
	}
	return &Client{mc: mc, bucket: strings.TrimSpace(bucket)}, nil
}

// Upload writes an object to the dedicated bucket.
func (c *Client) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if c == nil || c.mc == nil {
		return errS3ClientNil
	}
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

// Download opens an object stream from the dedicated bucket.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if c == nil || c.mc == nil {
		return nil, errS3ClientNil
	}
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("stat object %q: %w", key, err)
	}
	return obj, nil
}

// ListPrefix returns objects under the provided prefix.
func (c *Client) ListPrefix(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if c == nil || c.mc == nil {
		return nil, errS3ClientNil
	}
	objects := make([]ObjectInfo, 0, 16)
	for object := range c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if object.Err != nil {
			return nil, fmt.Errorf("list objects with prefix %q: %w", prefix, object.Err)
		}
		objects = append(objects, ObjectInfo{
			Key:          object.Key,
			LastModified: object.LastModified,
			Size:         object.Size,
		})
	}
	return objects, nil
}

func normalizeEndpoint(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, errS3EndpointRequired
	}
	if !strings.Contains(raw, "://") {
		return raw, true, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse s3 endpoint: %w", err)
	}
	if u.Host == "" {
		return "", false, errS3EndpointMissingHost
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("%w %q", errS3EndpointBadScheme, u.Scheme)
	}
}
