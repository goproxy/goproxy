package cacher

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// OSS implements the `goproxy.Cacher` by using the Alibaba Cloud Object Storage
// Service.
type OSS struct {
	// Endpoint is the endpoint of the Alibaba Cloud Object Storage Service.
	//
	// If the `Endpoint` is empty,
	// the "https://oss-cn-hangzhou.aliyuncs.com" is used.
	Endpoint string `mapstructure:"endpoint"`

	// AccessKeyID is the access key ID of the Alibaba Cloud.
	AccessKeyID string `mapstructure:"access_key_id"`

	// AccessKeySecret is the access key secret of the Alibaba Cloud.
	AccessKeySecret string `mapstructure:"access_key_secret"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (o *OSS) load() {
	endpoint := o.Endpoint
	if endpoint == "" {
		endpoint = "https://oss-cn-hangzhou.aliyuncs.com"
	}

	o.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     o.AccessKeyID,
		SecretAccessKey: o.AccessKeySecret,
		BucketName:      o.BucketName,
		VirtualHosted:   true,
		Root:            o.Root,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (o *OSS) NewHash() hash.Hash {
	return o.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (o *OSS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	o.loadOnce.Do(o.load)
	return o.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (o *OSS) SetCache(ctx context.Context, c goproxy.Cache) error {
	o.loadOnce.Do(o.load)
	return o.minio.SetCache(ctx, c)
}
