package cachers

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// DOS implements the `goproxy.Cacher` by using the DigitalOcean Spaces.
type DOS struct {
	// Endpoint is the endpoint of the DigitalOcean Spaces.
	//
	// If the `Endpoint` is empty, the "https://nyc3.digitaloceanspaces.com"
	// is used.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKey is the access key of the DigitalOcean.
	AccessKey string `mapstructure:"access_key"`

	// SecretKey is the secret_key of the DigitalOcean.
	SecretKey string `mapstructure:"secret_key"`

	// SpaceName is the name of the space.
	SpaceName string `mapstructure:"space_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (d *DOS) load() {
	endpoint := d.Endpoint
	if endpoint == "" {
		endpoint = "https://nyc3.digitaloceanspaces.com"
	}

	d.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     d.AccessKey,
		SecretAccessKey: d.SecretKey,
		BucketName:      d.SpaceName,
		Root:            d.Root,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (d *DOS) NewHash() hash.Hash {
	return d.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (d *DOS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	d.loadOnce.Do(d.load)
	return d.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (d *DOS) SetCache(ctx context.Context, c goproxy.Cache) error {
	d.loadOnce.Do(d.load)
	return d.minio.SetCache(ctx, c)
}
