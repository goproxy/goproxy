package cachers

import (
	"context"
	"crypto/md5"
	"errors"
	"hash"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/goproxy/goproxy"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// GCS implements the `goproxy.Cacher` by using the Google Cloud Storage.
type GCS struct {
	// CredentialsJSON is the credentials JSON of the Google Cloud Platform.
	//
	// If the `CredentialsJSON` is empty, then it will try to be read from
	// the file targeted by the GOOGLE_APPLICATION_CREDENTIALS environment
	// variable.
	//
	// Note that if you are running on the Google Cloud Platform, you
	// usually do not need to provide the `CredentialsJSON`.
	CredentialsJSON string `mapstructure:"credentials_json"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce  sync.Once
	loadError error
	bucket    *storage.BucketHandle
}

// load loads the stuff of the m up.
func (g *GCS) load() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cos := []option.ClientOption{}
	if g.CredentialsJSON != "" {
		creds, err := google.CredentialsFromJSON(
			ctx,
			[]byte(g.CredentialsJSON),
		)
		if err != nil {
			g.loadError = err
			return
		}

		cos = append(cos, option.WithCredentials(creds))
	}

	client, err := storage.NewClient(ctx, cos...)
	if err != nil {
		g.loadError = err
		return
	}

	g.bucket = client.Bucket(g.BucketName)
}

// NewHash implements the `goproxy.Cacher`.
func (g *GCS) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `goproxy.Cacher`.
func (g *GCS) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	if g.loadOnce.Do(g.load); g.loadError != nil {
		return nil, g.loadError
	}

	oh := g.bucket.Object(path.Join(g.Root, name))
	attrs, err := oh.Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	return &gcsCache{
		ctx:      ctx,
		oh:       oh,
		name:     name,
		size:     attrs.Size,
		modTime:  attrs.Updated,
		checksum: attrs.MD5,
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (g *GCS) SetCache(ctx context.Context, c goproxy.Cache) error {
	if g.loadOnce.Do(g.load); g.loadError != nil {
		return g.loadError
	}

	w := g.bucket.Object(path.Join(g.Root, c.Name())).NewWriter(ctx)
	w.ContentType = mimeTypeByExtension(path.Ext(c.Name()))
	if _, err := io.Copy(w, c); err != nil {
		return err
	}

	return w.Close()
}

// gcsCache implements the `goproxy.Cache`. It is the cache unit of the `GCS`.
type gcsCache struct {
	ctx      context.Context
	oh       *storage.ObjectHandle
	offset   int64
	closed   bool
	name     string
	size     int64
	modTime  time.Time
	checksum []byte
}

// Read implements the `goproxy.Cache`.
func (gc *gcsCache) Read(b []byte) (int, error) {
	if gc.closed {
		return 0, os.ErrClosed
	} else if gc.offset >= gc.size {
		return 0, io.EOF
	}

	r, err := gc.oh.NewRangeReader(gc.ctx, gc.offset, -1)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	n, err := r.Read(b)
	gc.offset += int64(n)

	return n, err
}

// Seek implements the `goproxy.Cache`.
func (gc *gcsCache) Seek(offset int64, whence int) (int64, error) {
	if gc.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += gc.offset
	case io.SeekEnd:
		offset += gc.size
	default:
		return 0, errors.New("invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("negative position")
	}

	gc.offset = offset

	return gc.offset, nil
}

// Close implements the `goproxy.Cache`.
func (gc *gcsCache) Close() error {
	if gc.closed {
		return os.ErrClosed
	}

	gc.closed = true

	return nil
}

// Name implements the `goproxy.Cache`.
func (gc *gcsCache) Name() string {
	return gc.name
}

// Size implements the `goproxy.Cache`.
func (gc *gcsCache) Size() int64 {
	return gc.size
}

// ModTime implements the `goproxy.Cache`.
func (gc *gcsCache) ModTime() time.Time {
	return gc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (gc *gcsCache) Checksum() []byte {
	return gc.checksum
}
