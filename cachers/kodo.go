package cachers

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// Kodo implements the `goproxy.Cacher` by using the Qiniu Cloud Kodo.
type Kodo struct {
	// Endpoint is the endpoint of the Qiniu Cloud Kodo.
	//
	// If the `Endpoint` is empty, the "https://s3-cn-east-1.qiniucs.com" is
	// used.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKey is the access key of the Qiniu Cloud.
	AccessKey string `mapstructure:"access_key"`

	// SecretKey is the secret key of the Qiniu Cloud.
	SecretKey string `mapstructure:"secret_key"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (k *Kodo) load() {
	endpoint := k.Endpoint
	if endpoint == "" {
		endpoint = "https://s3-cn-east-1.qiniucs.com"
	}

	k.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     k.AccessKey,
		SecretAccessKey: k.SecretKey,
		BucketName:      k.BucketName,
		Root:            k.Root,
		virtualHosted:   true,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (k *Kodo) NewHash() hash.Hash {
	return k.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (k *Kodo) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	k.loadOnce.Do(k.load)
	return k.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (k *Kodo) SetCache(ctx context.Context, c goproxy.Cache) error {
	k.loadOnce.Do(k.load)
	return k.minio.SetCache(ctx, c)
}
