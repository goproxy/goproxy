package cachers

import (
	"context"
	"hash"
	"sync"

	"github.com/goproxy/goproxy"
)

// MABS implements the `goproxy.Cacher` by using the Microsoft Azure Blob
// Storage.
type MABS struct {
	// Endpoint is the endpoint of the Microsoft Azure Blob Storage.
	//
	// If the `Endpoint` is empty, the
	// "https://<AccountName>.blob.core.windows.net" is used.
	Endpoint string `mapstructure:"endpoint"`

	// AccountName is the account name of the Microsoft Azure.
	AccountName string `mapstructure:"account_name"`

	// AccountKey is the account key of the Microsoft Azure.
	AccountKey string `mapstructure:"account_key"`

	// ContainerNameis the name of the container.
	ContainerName string `mapstructure:"bucket_container"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce sync.Once
	minio    *MinIO
}

// load loads the stuff of the m up.
func (m *MABS) load() {
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = "https://" + m.AccountName + ".blob.core.windows.net"
	}

	m.minio = &MinIO{
		Endpoint:        endpoint,
		AccessKeyID:     m.AccountName,
		SecretAccessKey: m.AccountKey,
		BucketName:      m.ContainerName,
		Root:            m.Root,
	}
}

// NewHash implements the `goproxy.Cacher`.
func (m *MABS) NewHash() hash.Hash {
	return m.minio.NewHash()
}

// Cache implements the `goproxy.Cacher`.
func (m *MABS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	m.loadOnce.Do(m.load)
	return m.minio.Cache(ctx, name)
}

// SetCache implements the `goproxy.Cacher`.
func (m *MABS) SetCache(ctx context.Context, c goproxy.Cache) error {
	m.loadOnce.Do(m.load)
	return m.minio.SetCache(ctx, c)
}
