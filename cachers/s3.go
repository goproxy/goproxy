package cachers

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// S3 implements the `goproxy.Cacher` by using the Amazon Simple Storage
// Service.
type S3 struct {
	// Endpoint is the endpoint of the Amazon Simple Storage Service.
	//
	// If the `Endpoint` is empty, the "https://s3.amazonaws.com" is used.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKeyID is the access key ID of the Amazon Web Services.
	AccessKeyID string `mapstructure:"access_key_id"`

	// SecretAccessKey is the secret access key of the Amazon Web Services.
	SecretAccessKey string `mapstructure:"secret_access_key"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (s *S3) load() {
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}

	s.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     s.AccessKeyID,
		SecretAccessKey: s.SecretAccessKey,
		BucketName:      s.BucketName,
		Root:            s.Root,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (s *S3) NewHash() hash.Hash {
	return s.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (s *S3) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	s.loadOnce.Do(s.load)
	return s.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (s *S3) SetCache(ctx context.Context, c goproxy.Cache) error {
	s.loadOnce.Do(s.load)
	return s.minio.SetCache(ctx, c)
}
