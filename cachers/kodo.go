package cachers

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/qiniu/api.v7/auth/qbox"
	"github.com/qiniu/api.v7/storage"
)

// Kodo implements the `goproxy.Cacher` by using the Qiniu Cloud Kodo.
type Kodo struct {
	// AccessKey is the access key of the Qiniu Cloud.
	AccessKey string `mapstructure:"access_key"`

	// SecretKey is the secret key of the Qiniu Cloud.
	SecretKey string `mapstructure:"secret_key"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// BucketEndpoint is the endpoint of the bucket.
	BucketEndpoint string `mapstructure:"bucket_endpoint"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce  sync.Once
	loadError error
	mac       *qbox.Mac
	config    *storage.Config
	bucket    *storage.BucketManager
}

// load loads the stuff of the m up.
func (k *Kodo) load() {
	k.mac = qbox.NewMac(k.AccessKey, k.SecretKey)

	zone, err := storage.GetZone(k.AccessKey, k.BucketName)
	if err != nil {
		k.loadError = err
		return
	}

	k.config = &storage.Config{
		Zone: zone,
	}

	k.bucket = storage.NewBucketManager(k.mac, k.config)
}

// NewHash implements the `goproxy.Cacher`.
func (k *Kodo) NewHash() hash.Hash {
	return sha1.New()
}

// Cache implements the `goproxy.Cacher`.
func (k *Kodo) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	if k.loadOnce.Do(k.load); k.loadError != nil {
		return nil, k.loadError
	}

	objectName := path.Join(k.Root, name)
	objectInfo, err := k.bucket.Stat(k.BucketName, objectName)
	if err != nil {
		if err.Error() == "no such file or directory" {
			return nil, goproxy.ErrCacheNotFound
		}

		return nil, err
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Hour)
	}

	url := storage.MakePrivateURL(
		k.mac,
		k.BucketEndpoint,
		objectName,
		deadline.Unix(),
	)

	checksum, err := base64.URLEncoding.DecodeString(objectInfo.Hash)
	if err != nil {
		return nil, err
	}

	if checksum[0] == 0x16 {
		checksum = checksum[1:]
	} else {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		res, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()

		h := sha1.New()
		if _, err := io.Copy(h, res.Body); err != nil {
			return nil, err
		}

		checksum = h.Sum(nil)
	}

	return &kodoCache{
		ctx:  ctx,
		url:  url,
		name: name,
		size: objectInfo.Fsize,
		modTime: time.Unix(
			objectInfo.PutTime*100/int64(time.Second),
			0,
		),
		checksum: checksum,
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (k *Kodo) SetCache(ctx context.Context, c goproxy.Cache) error {
	if k.loadOnce.Do(k.load); k.loadError != nil {
		return k.loadError
	}

	objectName := path.Join(k.Root, c.Name())

	return storage.NewFormUploader(k.config).Put(
		ctx,
		nil,
		(&storage.PutPolicy{
			Scope: fmt.Sprintf("%s:%s", k.BucketName, objectName),
		}).UploadToken(k.mac),
		objectName,
		c,
		c.Size(),
		&storage.PutExtra{
			MimeType: mimeTypeByExtension(path.Ext(c.Name())),
		},
	)
}

// kodoCache implements the `goproxy.Cache`. It is the cache unit of the `Kodo`.
type kodoCache struct {
	ctx      context.Context
	url      string
	offset   int64
	closed   bool
	name     string
	size     int64
	modTime  time.Time
	checksum []byte
}

// Read implements the `goproxy.Cache`.
func (kc *kodoCache) Read(b []byte) (int, error) {
	if kc.closed {
		return 0, os.ErrClosed
	} else if kc.offset >= kc.size {
		return 0, io.EOF
	}

	req, err := http.NewRequest(http.MethodGet, kc.url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", kc.offset))

	res, err := http.DefaultClient.Do(req.WithContext(kc.ctx))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	return res.Body.Read(b)
}

// Seek implements the `goproxy.Cache`.
func (kc *kodoCache) Seek(offset int64, whence int) (int64, error) {
	if kc.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += kc.offset
	case io.SeekEnd:
		offset += kc.size
	default:
		return 0, errors.New("invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("negative position")
	}

	kc.offset = offset

	return kc.offset, nil
}

// Close implements the `goproxy.Cache`.
func (kc *kodoCache) Close() error {
	if kc.closed {
		return os.ErrClosed
	}

	kc.closed = true

	return nil
}

// Name implements the `goproxy.Cache`.
func (kc *kodoCache) Name() string {
	return kc.name
}

// Size implements the `goproxy.Cache`.
func (kc *kodoCache) Size() int64 {
	return kc.size
}

// ModTime implements the `goproxy.Cache`.
func (kc *kodoCache) ModTime() time.Time {
	return kc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (kc *kodoCache) Checksum() []byte {
	return kc.checksum
}
