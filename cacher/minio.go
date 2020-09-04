package cacher

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"hash"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// MinIO implements the `goproxy.Cacher` by using the MinIO.
type MinIO struct {
	// Endpoint is the endpoint of the MinIO.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKeyID is the access key ID of the MinIO.
	AccessKeyID string `mapstructure:"access_key_id"`

	// SecretAccessKey is the secret access key of the MinIO.
	SecretAccessKey string `mapstructure:"secret_access_key"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// BucketLocation is the location of the bucket. It is used to avoid
	// bucket location lookup operations.
	BucketLocation string `mapstructure:"bucket_location"`

	// VirtualHosted indicates whether the MinIO is virtual hosted.
	VirtualHosted bool `mapstructure:"virtual_hosted"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce  sync.Once
	loadError error
	client    *minio.Client
}

// load loads the stuff of the m up.
func (m *MinIO) load() {
	var u *url.URL
	if u, m.loadError = url.Parse(m.Endpoint); m.loadError != nil {
		return
	}

	signerType := credentials.SignatureDefault
	if s3utils.IsGoogleEndpoint(*u) {
		signerType = credentials.SignatureV2
	}

	options := &minio.Options{
		Creds: credentials.NewStatic(
			m.AccessKeyID,
			m.SecretAccessKey,
			"",
			signerType,
		),
		Secure:       strings.ToLower(u.Scheme) == "https",
		Region:       m.BucketLocation,
		BucketLookup: minio.BucketLookupPath,
	}
	if m.VirtualHosted {
		options.BucketLookup = minio.BucketLookupDNS
	}

	u.Scheme = ""
	m.client, m.loadError = minio.New(
		strings.TrimPrefix(u.String(), "//"),
		options,
	)
}

// NewHash implements the `goproxy.Cacher`.
func (m *MinIO) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `goproxy.Cacher`.
func (m *MinIO) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	if m.loadOnce.Do(m.load); m.loadError != nil {
		return nil, m.loadError
	}

	object, err := m.client.GetObject(
		ctx,
		m.BucketName,
		path.Join(m.Root, name),
		minio.GetObjectOptions{},
	)
	if err != nil {
		if isMinIOObjectNotExist(err) {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	objectInfo, err := object.Stat()
	if err != nil {
		// Somehow it should be checked again. The check above for some
		// implementations (such as `Kodo`) is not sufficient.
		if isMinIOObjectNotExist(err) {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	dashIndex := strings.Index(objectInfo.ETag, "-")
	if dashIndex < 0 {
		dashIndex = len(objectInfo.ETag)
	}

	checksum, err := hex.DecodeString(objectInfo.ETag[:dashIndex])
	if err != nil {
		return nil, err
	}

	return &minioCache{
		object:   object,
		name:     name,
		mimeType: objectInfo.ContentType,
		size:     objectInfo.Size,
		modTime:  objectInfo.LastModified,
		checksum: checksum,
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (m *MinIO) SetCache(ctx context.Context, c goproxy.Cache) error {
	if m.loadOnce.Do(m.load); m.loadError != nil {
		return m.loadError
	}

	_, err := m.client.PutObject(
		ctx,
		m.BucketName,
		path.Join(m.Root, c.Name()),
		c,
		c.Size(),
		minio.PutObjectOptions{
			ContentType: c.MIMEType(),
		},
	)

	return err
}

// isMinIOObjectNotExist reports whether the err means that the MinIO object
// does not exist.
func isMinIOObjectNotExist(err error) bool {
	return minio.ToErrorResponse(err).StatusCode == http.StatusNotFound
}

// minioCache implements the `goproxy.Cache`. It is the cache unit of the
// `MinIO`.
type minioCache struct {
	object   *minio.Object
	name     string
	mimeType string
	size     int64
	modTime  time.Time
	checksum []byte
}

// Read implements the `goproxy.Cache`.
func (mc *minioCache) Read(b []byte) (int, error) {
	return mc.object.Read(b)
}

// Seek implements the `goproxy.Cache`.
func (mc *minioCache) Seek(offset int64, whence int) (int64, error) {
	return mc.object.Seek(offset, whence)
}

// Close implements the `goproxy.Cache`.
func (mc *minioCache) Close() error {
	return mc.object.Close()
}

// Name implements the `goproxy.Cache`.
func (mc *minioCache) Name() string {
	return mc.name
}

// MIMEType implements the `goproxy.Cache`.
func (mc *minioCache) MIMEType() string {
	return mc.mimeType
}

// Size implements the `goproxy.Cache`.
func (mc *minioCache) Size() int64 {
	return mc.size
}

// ModTime implements the `goproxy.Cache`.
func (mc *minioCache) ModTime() time.Time {
	return mc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (mc *minioCache) Checksum() []byte {
	return mc.checksum
}
