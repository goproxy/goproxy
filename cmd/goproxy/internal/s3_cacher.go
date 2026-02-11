package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3Cacher implements [github.com/goproxy/goproxy.Cacher] using an
// S3-compatible service.
type s3Cacher struct {
	client         *s3.Client
	transferClient *transfermanager.Client
	bucket         *string
}

// s3CacherOptions is the options for creating a new [s3Cacher].
type s3CacherOptions struct {
	accessKeyID     string
	secretAccessKey string
	endpoint        string
	disableTLS      bool
	transport       http.RoundTripper
	region          string
	bucket          string
	forcePathStyle  bool
	partSize        int64
}

// newS3Cacher creates a new [s3Cacher].
func newS3Cacher(opts s3CacherOptions) (*s3Cacher, error) {
	if strings.Contains(opts.endpoint, "://") {
		return nil, fmt.Errorf("invalid S3 endpoint: %q contains URL scheme", opts.endpoint)
	}
	endpoint := "https://" + opts.endpoint
	if opts.disableTLS {
		endpoint = "http://" + opts.endpoint
	}

	client := s3.New(s3.Options{
		Credentials:  credentials.NewStaticCredentialsProvider(opts.accessKeyID, opts.secretAccessKey, ""),
		BaseEndpoint: aws.String(endpoint),
		HTTPClient:   &http.Client{Transport: opts.transport},
		Region:       opts.region,
		UsePathStyle: opts.forcePathStyle,
	})
	transferClient := transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = opts.partSize
	})

	return &s3Cacher{
		client:         client,
		transferClient: transferClient,
		bucket:         aws.String(opts.bucket),
	}, nil
}

// Get implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	o, err := s3c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: s3c.bucket,
		Key:    aws.String(name),
	})
	if err != nil {
		if errors.As(err, new(*types.NoSuchKey)) || errors.As(err, new(*types.NotFound)) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return &s3Cache{
		ReadCloser:   o.Body,
		lastModified: aws.ToTime(o.LastModified),
		etag:         aws.ToString(o.ETag),
	}, nil
}

// Put implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	contentType := "application/octet-stream"
	nameExt := filepath.Ext(name)
	switch {
	case nameExt == ".info", strings.HasSuffix(name, "/@latest"):
		contentType = "application/json; charset=utf-8"
	case nameExt == ".mod", strings.HasSuffix(name, "/@v/list"):
		contentType = "text/plain; charset=utf-8"
	case nameExt == ".zip":
		contentType = "application/zip"
	case strings.HasPrefix(name, "sumdb/"):
		if elems := strings.Split(name, "/"); len(elems) >= 3 {
			switch elems[2] {
			case "latest", "lookup":
				contentType = "text/plain; charset=utf-8"
			}
		}
	}

	_, err := s3c.transferClient.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      s3c.bucket,
		Key:         aws.String(name),
		Body:        content,
		ContentType: aws.String(contentType),
	})
	return err
}

// s3Cache is the cache returned by [s3Cacher.Get].
type s3Cache struct {
	io.ReadCloser
	lastModified time.Time
	etag         string
}

// LastModified implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) LastModified() time.Time {
	return s3c.lastModified
}

// ETag implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) ETag() string {
	return s3c.etag
}
