package cachers

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
	"github.com/minio/minio-go/v6"
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

	secure := strings.ToLower(u.Scheme) == "https"
	u.Scheme = ""
	m.client, m.loadError = minio.New(
		strings.TrimPrefix(u.String(), "//"),
		m.AccessKeyID,
		m.SecretAccessKey,
		secure,
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

	object, err := m.client.GetObjectWithContext(
		ctx,
		m.BucketName,
		path.Join(m.Root, name),
		minio.GetObjectOptions{},
	)
	if err != nil {
		if er, ok := err.(minio.ErrorResponse); ok &&
			er.StatusCode == http.StatusNotFound {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	objectInfo, err := object.Stat()
	if err != nil {
		return nil, err
	}

	checksum, err := hex.DecodeString(strings.Trim(objectInfo.ETag, `"`))
	if err != nil {
		return nil, err
	}

	return &minioCache{
		object:   object,
		name:     name,
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

	_, err := m.client.PutObjectWithContext(
		ctx,
		m.BucketName,
		path.Join(m.Root, c.Name()),
		c,
		c.Size(),
		minio.PutObjectOptions{
			ContentType: mimeTypeByExtension(path.Ext(c.Name())),
		},
	)

	return err
}

// minioCache implements the `goproxy.Cache`. It is the cache unit of the
// `MinIO`.
type minioCache struct {
	object   *minio.Object
	name     string
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
