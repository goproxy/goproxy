package cachers

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/goproxy/goproxy"
)

// MABS implements the `goproxy.Cacher` by using the Microsoft Azure Blob
// Storage.
type MABS struct {
	// AccountName is the account name of the Microsoft Azure.
	AccountName string `mapstructure:"account_name"`

	// AccountKey is the account key of the Microsoft Azure.
	AccountKey string `mapstructure:"account_key"`

	// ContainerNameis the name of the container.
	ContainerName string `mapstructure:"bucket_container"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce     sync.Once
	loadError    error
	containerURL azblob.ContainerURL
}

// load loads the stuff of the m up.
func (m *MABS) load() {
	var creds *azblob.SharedKeyCredential
	if creds, m.loadError = azblob.NewSharedKeyCredential(
		m.AccountName,
		m.AccountKey,
	); m.loadError != nil {
		return
	}

	u, _ := url.Parse(fmt.Sprintf(
		"https://%s.blob.core.windows.net/%s",
		m.AccountName,
		m.ContainerName,
	))
	m.containerURL = azblob.NewContainerURL(
		*u,
		azblob.NewPipeline(creds, azblob.PipelineOptions{}),
	)
}

// NewHash implements the `goproxy.Cacher`.
func (m *MABS) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `goproxy.Cacher`.
func (m *MABS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	if m.loadOnce.Do(m.load); m.loadError != nil {
		return nil, m.loadError
	}

	blobURL := m.containerURL.NewBlockBlobURL(path.Join(m.Root, name))
	res, err := blobURL.GetProperties(ctx, azblob.BlobAccessConditions{})
	if err != nil {
		if se, ok := err.(azblob.StorageError); ok &&
			se.ServiceCode() == azblob.ServiceCodeBlobNotFound {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	return &mabsCache{
		ctx:      ctx,
		blobURL:  blobURL,
		name:     name,
		size:     res.ContentLength(),
		modTime:  res.LastModified(),
		checksum: res.ContentMD5(),
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (m *MABS) SetCache(ctx context.Context, c goproxy.Cache) error {
	if m.loadOnce.Do(m.load); m.loadError != nil {
		return m.loadError
	}

	_, err := m.containerURL.NewBlockBlobURL(
		path.Join(m.Root, c.Name()),
	).Upload(
		ctx,
		c,
		azblob.BlobHTTPHeaders{
			ContentType: mimeTypeByExtension(path.Ext(c.Name())),
		},
		azblob.Metadata{},
		azblob.BlobAccessConditions{},
	)

	return err
}

// mabsCache implements the `goproxy.Cache`. It is the cache unit of the `MABS`.
type mabsCache struct {
	ctx      context.Context
	blobURL  azblob.BlockBlobURL
	offset   int64
	closed   bool
	name     string
	size     int64
	modTime  time.Time
	checksum []byte
}

// Read implements the `goproxy.Cache`.
func (mc *mabsCache) Read(b []byte) (int, error) {
	if mc.closed {
		return 0, os.ErrClosed
	} else if mc.offset >= mc.size {
		return 0, io.EOF
	}

	res, err := mc.blobURL.Download(
		mc.ctx,
		mc.offset,
		0,
		azblob.BlobAccessConditions{},
		false,
	)
	if err != nil {
		return 0, err
	}

	rc := res.Body(azblob.RetryReaderOptions{})
	defer rc.Close()

	n, err := rc.Read(b)
	mc.offset += int64(n)

	return n, err
}

// Seek implements the `goproxy.Cache`.
func (mc *mabsCache) Seek(offset int64, whence int) (int64, error) {
	if mc.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += mc.offset
	case io.SeekEnd:
		offset += mc.size
	default:
		return 0, errors.New("invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("negative position")
	}

	mc.offset = offset

	return mc.offset, nil
}

// Close implements the `goproxy.Cache`.
func (mc *mabsCache) Close() error {
	if mc.closed {
		return os.ErrClosed
	}

	mc.closed = true

	return nil
}

// Name implements the `goproxy.Cache`.
func (mc *mabsCache) Name() string {
	return mc.name
}

// Size implements the `goproxy.Cache`.
func (mc *mabsCache) Size() int64 {
	return mc.size
}

// ModTime implements the `goproxy.Cache`.
func (mc *mabsCache) ModTime() time.Time {
	return mc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (mc *mabsCache) Checksum() []byte {
	return mc.checksum
}
