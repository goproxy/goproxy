package cachers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/goproxy/goproxy"
)

// S3 implements the `goproxy.Cacher` by using the Amazon Simple Storage
// Service.
type S3 struct {
	// AccessKeyID is the access key ID of the Amazon Web Services.
	//
	// If both the `AccessKeyID` and the `SecretAccessKey` are empty, then
	// they will try to be read from the file targeted by
	// "~/.aws/credentials".
	AccessKeyID string `mapstructure:"access_key_id"`

	// SecretAccessKey is the secret access key of the Amazon Web Services.
	//
	// If both the `AccessKeyID` and the `SecretAccessKey` are empty, then
	// they will try to be read from the file targeted by
	// "~/.aws/credentials".
	SecretAccessKey string `mapstructure:"secret_access_key"`

	// SessionToken is the session token of the Amazon Web Services.
	SessionToken string `mapstructure:"session_token"`

	// BucketName is the name of the bucket.
	BucketName string `mapstructure:"bucket_name"`

	// Root is the root of the caches.
	Root string `mapstructure:"root"`

	loadOnce   sync.Once
	loadError  error
	uploader   *s3manager.Uploader
	downloader *s3manager.Downloader
}

// load loads the stuff of the m up.
func (s *S3) load() {
	sess, err := session.NewSession()
	if err != nil {
		s.loadError = err
		return
	}

	gblo, err := s3.New(sess).GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: &s.BucketName,
	})
	if err != nil {
		s.loadError = err
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	region, err := s3manager.GetBucketRegion(
		ctx,
		sess,
		s.BucketName,
		*gblo.LocationConstraint,
	)
	if err != nil {
		s.loadError = err
		return
	}

	config := &aws.Config{
		Region: &region,
	}
	if s.AccessKeyID == "" || s.SecretAccessKey == "" {
		config.Credentials = credentials.NewStaticCredentials(
			s.AccessKeyID,
			s.SecretAccessKey,
			s.SessionToken,
		)
	}

	sess, err = session.NewSession(config)
	if err != nil {
		s.loadError = err
		return
	}

	s.uploader = s3manager.NewUploader(sess)
	s.downloader = s3manager.NewDownloader(sess)
}

// NewHash implements the `goproxy.Cacher`.
func (s *S3) NewHash() hash.Hash {
	return md5.New()
}

// Cache implements the `goproxy.Cacher`.
func (s *S3) Cache(ctx context.Context, name string) (goproxy.Cache, error) {
	if s.loadOnce.Do(s.load); s.loadError != nil {
		return nil, s.loadError
	}

	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	objectName := path.Join(s.Root, name)
	hoo, err := s3.New(sess).HeadObjectWithContext(
		aws.Context(ctx),
		&s3.HeadObjectInput{
			Bucket: &s.BucketName,
			Key:    &objectName,
		},
	)
	if err != nil {
		return nil, err
	}

	checksum, err := hex.DecodeString(strings.Trim(*hoo.ETag, `"`))
	if err != nil {
		return nil, err
	}

	return &s3Cache{
		ctx:        ctx,
		downloader: s.downloader,
		bucketName: s.BucketName,
		objectName: objectName,
		name:       name,
		size:       *hoo.ContentLength,
		modTime:    *hoo.LastModified,
		checksum:   checksum,
	}, nil
}

// SetCache implements the `goproxy.Cacher`.
func (s *S3) SetCache(ctx context.Context, c goproxy.Cache) error {
	if s.loadOnce.Do(s.load); s.loadError != nil {
		return s.loadError
	}

	objectName := path.Join(s.Root, c.Name())
	contentType := mimeTypeByExtension(path.Ext(c.Name()))
	_, err := s.uploader.UploadWithContext(
		aws.Context(ctx),
		&s3manager.UploadInput{
			Bucket:      &s.BucketName,
			Key:         &objectName,
			ContentType: &contentType,
			Body:        c,
		},
	)

	return err
}

// s3Cache implements the `goproxy.Cache`. It is the cache unit of the `S3`.
type s3Cache struct {
	ctx        context.Context
	downloader *s3manager.Downloader
	bucketName string
	objectName string
	offset     int64
	closed     bool
	name       string
	size       int64
	modTime    time.Time
	checksum   []byte
}

// Read implements the `goproxy.Cache`.
func (sc *s3Cache) Read(b []byte) (int, error) {
	if sc.closed {
		return 0, os.ErrClosed
	} else if sc.offset >= sc.size {
		return 0, io.EOF
	}

	pr, pw := io.Pipe()
	rangeHeader := fmt.Sprintf("bytes=%d-", sc.offset)
	if _, err := sc.downloader.DownloadWithContext(
		aws.Context(sc.ctx),
		&fakeWriterAt{pw},
		&s3.GetObjectInput{
			Bucket: &sc.bucketName,
			Key:    &sc.objectName,
			Range:  &rangeHeader,
		},
	); err != nil {
		return 0, err
	}

	n, err := pr.Read(b)
	sc.offset += int64(n)

	return n, err
}

// Seek implements the `goproxy.Cache`.
func (sc *s3Cache) Seek(offset int64, whence int) (int64, error) {
	if sc.closed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += sc.offset
	case io.SeekEnd:
		offset += sc.size
	default:
		return 0, errors.New("invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("negative position")
	}

	sc.offset = offset

	return sc.offset, nil
}

// Close implements the `goproxy.Cache`.
func (sc *s3Cache) Close() error {
	if sc.closed {
		return os.ErrClosed
	}

	sc.closed = true

	return nil
}

// Name implements the `goproxy.Cache`.
func (sc *s3Cache) Name() string {
	return sc.name
}

// Size implements the `goproxy.Cache`.
func (sc *s3Cache) Size() int64 {
	return sc.size
}

// ModTime implements the `goproxy.Cache`.
func (sc *s3Cache) ModTime() time.Time {
	return sc.modTime
}

// Checksum implements the `goproxy.Cache`.
func (sc *s3Cache) Checksum() []byte {
	return sc.checksum
}
