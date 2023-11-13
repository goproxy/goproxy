package internal

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Cacher implements [github.com/goproxy/goproxy.Cacher] using an
// S3-compatible service.
type s3Cacher struct {
	client   *minio.Client
	bucket   string
	partSize int64
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
	clientOpts := &minio.Options{
		Creds:        credentials.NewStaticV4(opts.accessKeyID, opts.secretAccessKey, ""),
		Secure:       !opts.disableTLS,
		Transport:    opts.transport,
		Region:       opts.region,
		BucketLookup: minio.BucketLookupDNS,
	}
	if opts.forcePathStyle {
		clientOpts.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(opts.endpoint, clientOpts)
	if err != nil {
		return nil, err
	}
	return &s3Cacher{
		client:   client,
		bucket:   opts.bucket,
		partSize: opts.partSize,
	}, nil
}

// Get implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	o, err := s3c.client.GetObject(ctx, s3c.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	oi, err := o.Stat()
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return newS3Cache(o, oi), nil
}

// Put implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return err
	}

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

	_, err = s3c.client.PutObject(ctx, s3c.bucket, name, content, size, minio.PutObjectOptions{
		ContentType:    contentType,
		PartSize:       uint64(s3c.partSize),
		SendContentMd5: true,
	})
	return err
}

// s3Cache is the cache returned by [s3Cacher.Get].
type s3Cache struct {
	*minio.Object
	minio.ObjectInfo
}

// newS3Cache creates a new [s3Cache].
func newS3Cache(o *minio.Object, oi minio.ObjectInfo) *s3Cache {
	return &s3Cache{o, oi}
}

// LastModified implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) LastModified() time.Time {
	return s3c.ObjectInfo.LastModified
}

// ETag implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) ETag() string {
	if s3c.ObjectInfo.ETag != "" {
		return strconv.Quote(s3c.ObjectInfo.ETag)
	}
	return ""
}
