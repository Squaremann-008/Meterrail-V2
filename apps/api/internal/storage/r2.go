// Package storage talks to Cloudflare R2 over the S3 API. Writes go through
// presigned PUT URLs so file bytes never transit the API server.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"github.com/meterrail/api/internal/config"
)

// ErrNotConfigured is returned when R2 credentials are absent, which is the
// normal state for a local checkout that has not been given a bucket.
var ErrNotConfigured = errors.New("storage: R2 is not configured")

type Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	cfg     config.Storage
	logger  *slog.Logger
}

// New builds the R2 client. It returns (nil, ErrNotConfigured) when creds are
// missing so the caller can decide whether that is fatal.
func New(ctx context.Context, cfg config.Storage, logger *slog.Logger) (*Client, error) {
	if !cfg.Configured() {
		return nil, ErrNotConfigured
	}
	endpoint := cfg.ResolveEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("storage: set R2_ACCOUNT_ID or R2_ENDPOINT")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		// R2 has no concept of buckets as subdomains.
		o.UsePathStyle = true
	})

	logger.Info("object storage ready",
		slog.String("endpoint", endpoint), slog.String("bucket", cfg.Bucket))

	return &Client{s3: client, presign: s3.NewPresignClient(client), cfg: cfg, logger: logger}, nil
}

func (c *Client) Bucket() string { return c.cfg.Bucket }

func (c *Client) MaxUploadBytes() int64 { return c.cfg.MaxUploadBytes }

// BuildKey namespaces an object as <prefix>/<yyyy>/<mm>/<uuid><ext>, keeping
// listings shallow and filenames non-guessable.
func BuildKey(prefix, filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	now := time.Now().UTC()
	return path.Join(
		strings.Trim(prefix, "/"),
		now.Format("2006"),
		now.Format("01"),
		uuid.New().String()+ext,
	)
}

// PresignedUpload is everything the browser needs to PUT the file itself.
type PresignedUpload struct {
	Key       string            `json:"key"`
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
	PublicURL string            `json:"publicUrl,omitempty"`
}

// PresignUpload issues a time-limited PUT URL. The content type and length are
// signed in, so the client cannot substitute a different or larger object.
func (c *Client) PresignUpload(ctx context.Context, key, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	if sizeBytes > c.cfg.MaxUploadBytes {
		return nil, fmt.Errorf("storage: %d bytes exceeds the %d byte limit", sizeBytes, c.cfg.MaxUploadBytes)
	}

	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.cfg.Bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(sizeBytes),
	}, s3.WithPresignExpires(c.cfg.PresignTTL))
	if err != nil {
		return nil, fmt.Errorf("storage: presign put: %w", err)
	}

	headers := map[string]string{"Content-Type": contentType}
	for name, values := range req.SignedHeader {
		if len(values) > 0 && !strings.EqualFold(name, "host") {
			headers[name] = values[0]
		}
	}

	return &PresignedUpload{
		Key:       key,
		URL:       req.URL,
		Method:    req.Method,
		Headers:   headers,
		ExpiresAt: time.Now().UTC().Add(c.cfg.PresignTTL),
		PublicURL: c.PublicURL(key),
	}, nil
}

// PresignDownload issues a time-limited GET URL for private objects.
func (c *Client) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = c.cfg.PresignTTL
	}
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign get: %w", err)
	}
	return req.URL, nil
}

// PublicURL maps a key onto the bucket's public/CDN domain. Empty when the
// bucket is private, which signals callers to presign instead.
func (c *Client) PublicURL(key string) string {
	if c.cfg.PublicBaseURL == "" {
		return ""
	}
	base := strings.TrimRight(c.cfg.PublicBaseURL, "/")
	return base + "/" + strings.TrimLeft(url.PathEscape(key), "/")
}

// ObjectInfo is the metadata R2 reports for a stored object.
type ObjectInfo struct {
	Key         string
	SizeBytes   int64
	ContentType string
	ETag        string
	ModifiedAt  time.Time
}

// Head confirms an object exists and reports its true size, which is how the
// upload-confirmation endpoint validates what the client claims it wrote.
func (c *Client) Head(ctx context.Context, key string) (*ObjectInfo, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("storage: object %q not found", key)
		}
		return nil, fmt.Errorf("storage: head %q: %w", key, err)
	}

	info := &ObjectInfo{Key: key}
	if out.ContentLength != nil {
		info.SizeBytes = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	if out.ETag != nil {
		info.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.LastModified != nil {
		info.ModifiedAt = *out.LastModified
	}
	return info, nil
}

// Put uploads bytes server-side. Used by the worker for derived artifacts
// (thumbnails, exports) rather than by request handlers.
func (c *Client) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.cfg.Bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}

// Get streams an object back. The caller closes the reader.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get %q: %w", key, err)
	}
	info := &ObjectInfo{Key: key}
	if out.ContentLength != nil {
		info.SizeBytes = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	return out.Body, info, nil
}

// Delete removes an object. Deleting a missing key is not an error in S3.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Health verifies the credentials and bucket are usable.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.cfg.Bucket)})
	return err
}
