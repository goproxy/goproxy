package cachers

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// GCS implements the `goproxy.Cacher` by using the Google Cloud Storage.
type GCS struct {
	// Endpoint is the endpoint of the Google Cloud Storage.
	//
	// If the `Endpoint` is empty, the "https://storage.googleapis.com" is
	// used.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKey is the access key of the Google Cloud Platform.
	AccessKey string `mapstructure:"access_key"`

	// SecretKey is the secret key of the Google Cloud Platform.
	SecretKey string `mapstructure:"secret_key"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (g *GCS) load() {
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = "https://storage.googleapis.com"
	}

	g.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     g.AccessKey,
		SecretAccessKey: g.SecretKey,
		BucketName:      g.BucketName,
		Root:            g.Root,
		virtualHosted:   true,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (g *GCS) NewHash() hash.Hash {
	return g.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (g *GCS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	g.loadOnce.Do(g.load)
	return g.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (g *GCS) SetCache(ctx context.Context, c goproxy.Cache) error {
	g.loadOnce.Do(g.load)
	return g.minio.SetCache(ctx, c)
}
