package internal

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	// defaultS3Endpoint enables the AWS SDK's region-based endpoint resolution.
	defaultS3Endpoint = "s3.amazonaws.com"

	// minS3PartSize is the minimum supported S3 multipart upload part size.
	minS3PartSize int64 = 5 << 20

	// maxS3PartSize is the maximum supported S3 multipart upload part size.
	maxS3PartSize int64 = 5 << 30
)

// s3Cacher implements [github.com/goproxy/goproxy.Cacher] using an
// S3-compatible service.
type s3Cacher struct {
	client   *s3.Client
	bucket   *string
	partSize int64
}

// s3CacherOptions is the options for creating a new [s3Cacher].
type s3CacherOptions struct {
	accessKeyID     string
	secretAccessKey string
	endpoint        string
	disableTLS      bool
	transport       http.RoundTripper
	region          string
	bucket          string
	forcePathStyle  bool
	partSize        int64
}

// newS3Cacher creates a new [s3Cacher].
func newS3Cacher(opts s3CacherOptions) (*s3Cacher, error) {
	endpoint, err := parseS3Endpoint(opts.endpoint, opts.disableTLS)
	if err != nil {
		return nil, fmt.Errorf("invalid S3 endpoint: %w", err)
	}
	if opts.partSize < minS3PartSize || opts.partSize > maxS3PartSize {
		return nil, fmt.Errorf("invalid S3 part size: %d is outside the range [%d, %d]", opts.partSize, minS3PartSize, maxS3PartSize)
	}
	if opts.region == "" {
		return nil, errors.New("invalid S3 region: region is empty")
	}

	var baseEndpoint *string
	if !strings.EqualFold(endpoint.Host, defaultS3Endpoint) {
		baseEndpoint = aws.String(endpoint.String())
	}

	var credentialsProvider aws.CredentialsProvider
	switch {
	case opts.accessKeyID == "" && opts.secretAccessKey == "":
		credentialsProvider = aws.AnonymousCredentials{}
	case opts.accessKeyID == "":
		return nil, errors.New("invalid S3 credentials: access key ID is empty")
	case opts.secretAccessKey == "":
		return nil, errors.New("invalid S3 credentials: secret access key is empty")
	default:
		credentialsProvider = credentials.NewStaticCredentialsProvider(opts.accessKeyID, opts.secretAccessKey, "")
	}

	client := s3.New(s3.Options{
		Credentials:     credentialsProvider,
		BaseEndpoint:    baseEndpoint,
		EndpointOptions: s3.EndpointResolverOptions{DisableHTTPS: opts.disableTLS},
		HTTPClient: &http.Client{
			Transport:     opts.transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		Region:                     opts.region,
		UsePathStyle:               opts.forcePathStyle,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})

	return &s3Cacher{
		client:   client,
		bucket:   aws.String(opts.bucket),
		partSize: opts.partSize,
	}, nil
}

// parseS3Endpoint validates endpoint and adds its configured URL scheme.
func parseS3Endpoint(endpoint string, disableTLS bool) (*url.URL, error) {
	if endpoint == "" {
		return nil, errors.New("endpoint is empty")
	}

	scheme := "https"
	if disableTLS {
		scheme = "http"
	}
	endpointURL, err := url.Parse(scheme + "://" + endpoint)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", endpoint, err)
	}
	if endpointURL.Hostname() == "" || endpointURL.Host != strings.TrimSuffix(endpoint, "/") {
		return nil, fmt.Errorf("%q must be a host or host:port", endpoint)
	}
	if strings.HasSuffix(endpointURL.Host, ":") {
		return nil, fmt.Errorf("%q has an empty port", endpoint)
	}
	if port := endpointURL.Port(); port != "" {
		if portNumber, err := strconv.ParseUint(port, 10, 16); err != nil || portNumber == 0 {
			return nil, fmt.Errorf("%q has an invalid port", endpoint)
		}
	}
	return endpointURL, nil
}

// Get implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	key := aws.String(name)
	headOutput, err := s3c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: s3c.bucket,
		Key:    key,
	})
	if err != nil {
		return nil, translateS3NotFoundError(err)
	}
	getObjectInput := s3.GetObjectInput{
		Bucket:  s3c.bucket,
		IfMatch: headOutput.ETag,
		Key:     key,
	}
	return &s3Cache{
		openAt: func(pos int64) (io.ReadCloser, error) {
			input := getObjectInput
			if pos > 0 {
				input.Range = aws.String(fmt.Sprintf("bytes=%d-", pos))
			}
			getOutput, err := s3c.client.GetObject(ctx, &input)
			if err != nil {
				return nil, translateS3NotFoundError(err)
			}
			return getOutput.Body, nil
		},
		size:         aws.ToInt64(headOutput.ContentLength),
		lastModified: aws.ToTime(headOutput.LastModified),
		etag:         aws.ToString(headOutput.ETag),
	}, nil
}

// Put implements [github.com/goproxy/goproxy.Cacher].
func (s3c *s3Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return err
	}

	contentType := "application/octet-stream"
	nameExt := filepath.Ext(name)
	switch {
	case nameExt == ".info", strings.HasSuffix(name, "/@latest"):
		contentType = "application/json; charset=utf-8"
	case nameExt == ".mod", strings.HasSuffix(name, "/@v/list"):
		contentType = "text/plain; charset=utf-8"
	case nameExt == ".zip":
		contentType = "application/zip"
	case strings.HasPrefix(name, "sumdb/"):
		if elems := strings.Split(name, "/"); len(elems) >= 3 {
			switch elems[2] {
			case "latest", "lookup":
				contentType = "text/plain; charset=utf-8"
			}
		}
	}

	if size <= s3c.partSize {
		return s3c.putObject(ctx, name, contentType, content, size)
	}
	return s3c.putObjectMultipart(ctx, name, contentType, content, size)
}

// putObject uploads content in a single request.
func (s3c *s3Cacher) putObject(ctx context.Context, name string, contentType string, content io.ReadSeeker, size int64) error {
	contentMD5, err := computeContentMD5(content)
	if err != nil {
		return err
	}
	_, err = s3c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        s3c.bucket,
		Key:           aws.String(name),
		Body:          content,
		ContentLength: aws.Int64(size),
		ContentMD5:    aws.String(contentMD5),
		ContentType:   aws.String(contentType),
	})
	return err
}

// putObjectMultipart uploads content sequentially in multiple requests.
func (s3c *s3Cacher) putObjectMultipart(ctx context.Context, name string, contentType string, content io.ReadSeeker, size int64) (err error) {
	const maxParts = 10_000

	if size > s3c.partSize*maxParts {
		return fmt.Errorf("S3 object size %d exceeds the maximum for part size %d", size, s3c.partSize)
	}

	key := aws.String(name)
	createOutput, err := s3c.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      s3c.bucket,
		Key:         key,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return err
	}
	if createOutput.UploadId == nil {
		return errors.New("S3 multipart upload response has no upload ID")
	}
	uploadID := createOutput.UploadId

	defer func() {
		if err == nil {
			return
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, abortErr := s3c.client.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
			Bucket:   s3c.bucket,
			Key:      key,
			UploadId: uploadID,
		}); abortErr != nil {
			err = errors.Join(err, fmt.Errorf("abort S3 multipart upload: %w", abortErr))
		}
	}()

	parts := make([]types.CompletedPart, 0, (size+s3c.partSize-1)/s3c.partSize)
	source := readSeekerAt{ReadSeeker: content}
	for offset := int64(0); offset < size; offset += s3c.partSize {
		partSize := min(s3c.partSize, size-offset)
		part := io.NewSectionReader(source, offset, partSize)
		partNumber := int32(len(parts) + 1)
		contentMD5, err := computeContentMD5(part)
		if err != nil {
			return err
		}
		partOutput, err := s3c.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        s3c.bucket,
			Key:           key,
			UploadId:      uploadID,
			PartNumber:    aws.Int32(partNumber),
			Body:          part,
			ContentLength: aws.Int64(partSize),
			ContentMD5:    aws.String(contentMD5),
		})
		if err != nil {
			return err
		}
		parts = append(parts, types.CompletedPart{
			ETag:       partOutput.ETag,
			PartNumber: aws.Int32(partNumber),
		})
	}

	_, err = s3c.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          s3c.bucket,
		Key:             key,
		UploadId:        uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	return err
}

// computeContentMD5 returns the Base64-encoded MD5 digest of content and rewinds it.
func computeContentMD5(content io.ReadSeeker) (string, error) {
	hash := md5.New()
	if _, err := io.Copy(hash, content); err != nil {
		return "", err
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(hash.Sum(nil)), nil
}

// readSeekerAt adapts an [io.ReadSeeker] to [io.ReaderAt] for sequential access.
type readSeekerAt struct {
	io.ReadSeeker
}

// ReadAt implements [io.ReaderAt].
func (r readSeekerAt) ReadAt(p []byte, offset int64) (int, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r, p)
}

// translateS3NotFoundError maps an S3 404 response to [fs.ErrNotExist].
func translateS3NotFoundError(err error) error {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound {
		return fs.ErrNotExist
	}
	return err
}

// s3Cache is the cache returned by [s3Cacher.Get].
type s3Cache struct {
	openAt       func(pos int64) (io.ReadCloser, error)
	body         io.ReadCloser
	pos          int64
	size         int64
	closed       bool
	lastModified time.Time
	etag         string
}

// Read implements [io.Reader].
func (s3c *s3Cache) Read(p []byte) (int, error) {
	if s3c.closed {
		return 0, fs.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if s3c.pos >= s3c.size {
		return 0, io.EOF
	}
	if s3c.body == nil {
		body, err := s3c.openAt(s3c.pos)
		if err != nil {
			return 0, err
		}
		s3c.body = body
	}

	n, err := s3c.body.Read(p)
	s3c.pos += int64(n)
	if err != nil || s3c.pos >= s3c.size {
		_ = s3c.closeBody()
	}
	return n, err
}

// Seek implements [io.Seeker].
func (s3c *s3Cache) Seek(offset int64, whence int) (int64, error) {
	if s3c.closed {
		return 0, fs.ErrClosed
	}

	pos := s3c.pos
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos += offset
	case io.SeekEnd:
		pos = s3c.size + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if pos < 0 {
		return 0, errors.New("negative position")
	}
	if pos == s3c.pos {
		return pos, nil
	}

	_ = s3c.closeBody()
	s3c.pos = pos
	return pos, nil
}

// Close implements [io.Closer].
func (s3c *s3Cache) Close() error {
	s3c.closed = true
	return s3c.closeBody()
}

// closeBody closes the current response body.
func (s3c *s3Cache) closeBody() error {
	if s3c.body == nil {
		return nil
	}
	err := s3c.body.Close()
	s3c.body = nil
	return err
}

// LastModified implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) LastModified() time.Time {
	return s3c.lastModified
}

// ETag implements [github.com/goproxy/goproxy.Cacher.Get].
func (s3c *s3Cache) ETag() string {
	return s3c.etag
}
