package internal

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goproxy/goproxy"
)

func TestNewS3Cacher(t *testing.T) {
	for _, tt := range []struct {
		name            string
		accessKeyID     string
		secretAccessKey string
		endpoint        string
		region          string
		partSize        int64
		wantErr         string
	}{
		{
			name:     "EmptyEndpoint",
			partSize: 5 << 20,
			wantErr:  "invalid S3 endpoint: endpoint is empty",
		},
		{
			name:     "InvalidEndpoint",
			endpoint: "https://s3.amazonaws.com",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: "https://s3.amazonaws.com" must be a host or host:port`,
		},
		{
			name:     "EndpointWithPath",
			endpoint: "s3.amazonaws.com/bucket",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: "s3.amazonaws.com/bucket" must be a host or host:port`,
		},
		{
			name:     "EndpointWithUserInformation",
			endpoint: "user@s3.amazonaws.com",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: "user@s3.amazonaws.com" must be a host or host:port`,
		},
		{
			name:     "EndpointWithoutHost",
			endpoint: ":9000",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: ":9000" must be a host or host:port`,
		},
		{
			name:     "EndpointWithEmptyPort",
			endpoint: "s3.amazonaws.com:",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: "s3.amazonaws.com:" has an empty port`,
		},
		{
			name:     "EndpointWithInvalidPort",
			endpoint: "s3.amazonaws.com:65536",
			partSize: 5 << 20,
			wantErr:  `invalid S3 endpoint: "s3.amazonaws.com:65536" has an invalid port`,
		},
		{
			name:     "EmptyRegion",
			endpoint: defaultS3Endpoint,
			partSize: 5 << 20,
			wantErr:  "invalid S3 region: region is empty",
		},
		{
			name:            "MissingAccessKeyID",
			secretAccessKey: "secret-access-key",
			endpoint:        defaultS3Endpoint,
			region:          "us-east-1",
			partSize:        5 << 20,
			wantErr:         "invalid S3 credentials: access key ID is empty",
		},
		{
			name:        "MissingSecretAccessKey",
			accessKeyID: "access-key-id",
			endpoint:    defaultS3Endpoint,
			region:      "us-east-1",
			partSize:    5 << 20,
			wantErr:     "invalid S3 credentials: secret access key is empty",
		},
		{
			name:     "PartSizeBelowMinimum",
			endpoint: "s3.amazonaws.com",
			partSize: -1,
			wantErr:  "invalid S3 part size: -1 is outside the range [5242880, 5368709120]",
		},
		{
			name:     "PartSizeAboveMaximum",
			endpoint: "s3.amazonaws.com",
			partSize: 5<<30 + 1,
			wantErr:  "invalid S3 part size: 5368709121 is outside the range [5242880, 5368709120]",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newS3Cacher(s3CacherOptions{
				accessKeyID:     tt.accessKeyID,
				secretAccessKey: tt.secretAccessKey,
				endpoint:        tt.endpoint,
				region:          tt.region,
				partSize:        tt.partSize,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); got != tt.wantErr {
				t.Fatalf("got %q, want %q", got, tt.wantErr)
			}
		})
	}

	for _, tt := range []struct {
		name           string
		endpoint       string
		disableTLS     bool
		forcePathStyle bool
		wantScheme     string
		wantHost       string
		wantPath       string
	}{
		{
			name:       "AWSVirtualHosted",
			endpoint:   defaultS3Endpoint,
			wantScheme: "https",
			wantHost:   "test-bucket.s3.us-west-2.amazonaws.com",
			wantPath:   "/test",
		},
		{
			name:           "AWSPathStyle",
			endpoint:       defaultS3Endpoint,
			forcePathStyle: true,
			wantScheme:     "https",
			wantHost:       "s3.us-west-2.amazonaws.com",
			wantPath:       "/test-bucket/test",
		},
		{
			name:           "AWSWithoutTLS",
			endpoint:       defaultS3Endpoint,
			disableTLS:     true,
			forcePathStyle: true,
			wantScheme:     "http",
			wantHost:       "s3.us-west-2.amazonaws.com",
			wantPath:       "/test-bucket/test",
		},
		{
			name:           "Custom",
			endpoint:       "s3.example.com",
			forcePathStyle: true,
			wantScheme:     "https",
			wantHost:       "s3.example.com",
			wantPath:       "/test-bucket/test",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := make(chan *http.Request, 1)
			cacher, err := newS3Cacher(s3CacherOptions{
				endpoint:   tt.endpoint,
				disableTLS: tt.disableTLS,
				transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					request <- req
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`<Error><Code>NoSuchKey</Code></Error>`)),
						Request:    req,
					}, nil
				}),
				region:         "us-west-2",
				bucket:         "test-bucket",
				forcePathStyle: tt.forcePathStyle,
				partSize:       5 << 20,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cacher.Get(t.Context(), "test"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
			}

			req := <-request
			if got := req.URL.Scheme; got != tt.wantScheme {
				t.Errorf("got scheme %q, want %q", got, tt.wantScheme)
			}
			if got := req.URL.Host; got != tt.wantHost {
				t.Errorf("got host %q, want %q", got, tt.wantHost)
			}
			if got := req.URL.Path; got != tt.wantPath {
				t.Errorf("got path %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestS3CacherGet(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/xml")
			rw.WriteHeader(http.StatusNotFound)
			rw.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, "test-bucket")
		_, err := cacher.Get(t.Context(), "missing")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
		}
	})

	t.Run("Forbidden", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/xml")
			rw.WriteHeader(http.StatusForbidden)
			rw.Write([]byte(`<Error><Code>AccessDenied</Code></Error>`))
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, "test-bucket")
		_, err := cacher.Get(t.Context(), "forbidden")
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got %v, want a non-not-found error", err)
		}
	})

	t.Run("NotFoundAfterHead", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			switch req.Method {
			case http.MethodHead:
				rw.Header().Set("Content-Length", "1")
				rw.Header().Set("ETag", `"test-etag"`)
				rw.WriteHeader(http.StatusOK)
			case http.MethodGet:
				rw.Header().Set("Content-Type", "application/xml")
				rw.WriteHeader(http.StatusNotFound)
				rw.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
			default:
				rw.WriteHeader(http.StatusMethodNotAllowed)
			}
		}))
		defer server.Close()

		cache, err := newTestS3Cacher(t, server, "test-bucket").Get(t.Context(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()
		if _, err := io.ReadAll(cache); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
		}
	})

	const (
		bucket  = "test-bucket"
		key     = "example.com/@v/v1.0.0.mod"
		content = "module example.com\n"
		etag    = `"test-etag"`
	)
	lastModified := time.Unix(1700000000, 0).UTC()

	for _, tt := range []struct {
		name             string
		method           string
		requestRange     string
		wantStatusCode   int
		wantContent      string
		wantContentRange string
		wantRequests     []string
	}{
		{
			name:           "FullResponse",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
			wantContent:    content,
			wantRequests: []string{
				`HEAD range="" if-match=""`,
				`GET range="" if-match="\"test-etag\""`,
			},
		},
		{
			name:             "RangeResponse",
			method:           http.MethodGet,
			requestRange:     "bytes=7-",
			wantStatusCode:   http.StatusPartialContent,
			wantContent:      content[7:],
			wantContentRange: "bytes 7-18/19",
			wantRequests: []string{
				`HEAD range="" if-match=""`,
				`GET range="bytes=7-" if-match="\"test-etag\""`,
			},
		},
		{
			name:           "HeadResponse",
			method:         http.MethodHead,
			wantStatusCode: http.StatusOK,
			wantRequests: []string{
				`HEAD range="" if-match=""`,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu       sync.Mutex
				requests []string
			)
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				mu.Lock()
				requests = append(requests, req.Method+
					" range="+strconv.Quote(req.Header.Get("Range"))+
					" if-match="+strconv.Quote(req.Header.Get("If-Match")))
				mu.Unlock()

				switch req.Method {
				case http.MethodHead:
					rw.Header().Set("Content-Length", strconv.Itoa(len(content)))
					rw.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
					rw.Header().Set("ETag", etag)
					rw.WriteHeader(http.StatusOK)
				case http.MethodGet:
					start := 0
					switch req.Header.Get("Range") {
					case "":
					case "bytes=7-":
						start = 7
					default:
						rw.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
						return
					}

					rw.Header().Set("Content-Length", strconv.Itoa(len(content)-start))
					if start > 0 {
						rw.Header().Set(
							"Content-Range",
							"bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)),
						)
						rw.WriteHeader(http.StatusPartialContent)
					}
					rw.Write([]byte(content[start:]))
				default:
					rw.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			defer server.Close()

			proxy := &goproxy.Goproxy{Cacher: newTestS3Cacher(t, server, bucket)}
			req := httptest.NewRequest(tt.method, "https://goproxy.test/"+key, nil)
			req.Header.Set("Range", tt.requestRange)
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)
			res := rec.Result()
			defer res.Body.Close()

			if got, want := res.StatusCode, tt.wantStatusCode; got != want {
				t.Errorf("got status code %d, want %d", got, want)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(body), tt.wantContent; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
			if got, want := res.Header.Get("Content-Range"), tt.wantContentRange; got != want {
				t.Errorf("got Content-Range %q, want %q", got, want)
			}
			if got, want := res.Header.Get("ETag"), etag; got != want {
				t.Errorf("got ETag %q, want %q", got, want)
			}
			if got, want := res.Header.Get("Last-Modified"), lastModified.Format(http.TimeFormat); got != want {
				t.Errorf("got Last-Modified %q, want %q", got, want)
			}

			mu.Lock()
			gotRequests := slices.Clone(requests)
			mu.Unlock()
			if !slices.Equal(gotRequests, tt.wantRequests) {
				t.Errorf("got requests %q, want %q", gotRequests, tt.wantRequests)
			}
		})
	}
}

func TestS3CacherPut(t *testing.T) {
	const content = "test content"

	for _, tt := range []struct {
		name            string
		key             string
		wantContentType string
	}{
		{
			name:            "VersionInfo",
			key:             "example.com/@v/v1.0.0.info",
			wantContentType: "application/json; charset=utf-8",
		},
		{
			name:            "Latest",
			key:             "example.com/@latest",
			wantContentType: "application/json; charset=utf-8",
		},
		{
			name:            "GoMod",
			key:             "example.com/@v/v1.0.0.mod",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "VersionList",
			key:             "example.com/@v/list",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "ModuleZip",
			key:             "example.com/@v/v1.0.0.zip",
			wantContentType: "application/zip",
		},
		{
			name:            "SumDBLatest",
			key:             "sumdb/sum.golang.org/latest",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "SumDBLookup",
			key:             "sumdb/sum.golang.org/lookup/example.com@v1.0.0",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "Unknown",
			key:             "other",
			wantContentType: "application/octet-stream",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotMethod      string
				gotPath        string
				gotContentType string
				gotContent     string
			)
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					rw.WriteHeader(http.StatusInternalServerError)
					return
				}
				gotMethod = req.Method
				gotPath = req.URL.Path
				gotContentType = req.Header.Get("Content-Type")
				gotContent = string(body)
				checkContentMD5(t, req, body)
				checkNoSDKChecksumHeaders(t, req)
				rw.Header().Set("ETag", `"test-etag"`)
				rw.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			cacher := newTestS3Cacher(t, server, "test-bucket")
			if err := cacher.Put(t.Context(), tt.key, strings.NewReader(content)); err != nil {
				t.Fatal(err)
			}
			if got, want := gotMethod, http.MethodPut; got != want {
				t.Errorf("got method %q, want %q", got, want)
			}
			if got, want := gotPath, "/test-bucket/"+tt.key; got != want {
				t.Errorf("got path %q, want %q", got, want)
			}
			if got, want := gotContentType, tt.wantContentType; got != want {
				t.Errorf("got Content-Type %q, want %q", got, want)
			}
			if got, want := gotContent, content; got != want {
				t.Errorf("got content %q, want %q", got, want)
			}
		})
	}

	t.Run("Redirect", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			statusCode int
		}{
			{name: "MovedPermanently", statusCode: http.StatusMovedPermanently},
			{name: "Found", statusCode: http.StatusFound},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var targetCalls atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					targetCalls.Add(1)
					rw.WriteHeader(http.StatusOK)
				}))
				defer target.Close()

				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if _, err := io.Copy(io.Discard, req.Body); err != nil {
						t.Error(err)
						rw.WriteHeader(http.StatusInternalServerError)
						return
					}
					rw.Header().Set("Location", target.URL)
					rw.WriteHeader(tt.statusCode)
				}))
				defer server.Close()

				cacher := newTestS3Cacher(t, server, "test-bucket")
				if err := cacher.Put(t.Context(), "test", strings.NewReader(content)); err == nil {
					t.Fatal("expected error, got nil")
				}
				if got := targetCalls.Load(); got != 0 {
					t.Errorf("got %d redirected requests, want 0", got)
				}
			})
		}
	})

	t.Run("Anonymous", func(t *testing.T) {
		authorization := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			authorization <- req.Header.Get("Authorization")
			if _, err := io.Copy(io.Discard, req.Body); err != nil {
				t.Error(err)
				rw.WriteHeader(http.StatusInternalServerError)
				return
			}
			rw.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cacher, err := newS3Cacher(s3CacherOptions{
			endpoint:       server.Listener.Addr().String(),
			disableTLS:     true,
			transport:      server.Client().Transport,
			region:         "us-east-1",
			bucket:         "test-bucket",
			forcePathStyle: true,
			partSize:       5 << 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := cacher.Put(t.Context(), "test", strings.NewReader(content)); err != nil {
			t.Fatal(err)
		}
		if got := <-authorization; got != "" {
			t.Errorf("got Authorization %q, want empty", got)
		}
	})

	t.Run("Multipart", func(t *testing.T) {
		const (
			bucket   = "test-bucket"
			key      = "example.com/@v/v1.0.0.zip"
			partSize = 5 << 20
			uploadID = "test-upload-id"
		)
		content := bytes.Repeat([]byte("a"), partSize+1)
		type completedPart struct {
			ETag       string `xml:"ETag"`
			PartNumber int32  `xml:"PartNumber"`
		}
		var (
			mu                sync.Mutex
			operations        []string
			gotParts          [][]byte
			gotCompletedParts []completedPart
		)
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			query := req.URL.Query()
			switch {
			case req.Method == http.MethodPost && query.Has("uploads"):
				mu.Lock()
				operations = append(operations, "CreateMultipartUpload")
				mu.Unlock()
				rw.Header().Set("Content-Type", "application/xml")
				rw.Write([]byte(
					`<InitiateMultipartUploadResult>` +
						`<Bucket>` + bucket + `</Bucket>` +
						`<Key>` + key + `</Key>` +
						`<UploadId>` + uploadID + `</UploadId>` +
						`</InitiateMultipartUploadResult>`,
				))
			case req.Method == http.MethodPut &&
				query.Get("uploadId") == uploadID:
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					rw.WriteHeader(http.StatusInternalServerError)
					return
				}
				partNumber, err := strconv.Atoi(query.Get("partNumber"))
				if err != nil || partNumber < 1 || partNumber > 2 {
					t.Errorf("unexpected part number %q", query.Get("partNumber"))
					rw.WriteHeader(http.StatusBadRequest)
					return
				}
				checkContentMD5(t, req, body)
				checkNoSDKChecksumHeaders(t, req)
				mu.Lock()
				operations = append(operations, "UploadPart"+strconv.Itoa(partNumber))
				gotParts = append(gotParts, body)
				mu.Unlock()
				rw.Header().Set("ETag", `"test-part-etag-`+strconv.Itoa(partNumber)+`"`)
				rw.WriteHeader(http.StatusOK)
			case req.Method == http.MethodPost && query.Get("uploadId") == uploadID:
				var completed struct {
					Parts []completedPart `xml:"Part"`
				}
				if err := xml.NewDecoder(req.Body).Decode(&completed); err != nil {
					t.Error(err)
					rw.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				operations = append(operations, "CompleteMultipartUpload")
				gotCompletedParts = completed.Parts
				mu.Unlock()
				rw.Header().Set("Content-Type", "application/xml")
				rw.Write([]byte(
					`<CompleteMultipartUploadResult>` +
						`<Bucket>` + bucket + `</Bucket>` +
						`<Key>` + key + `</Key>` +
						`<ETag>"test-etag"</ETag>` +
						`</CompleteMultipartUploadResult>`,
				))
			default:
				t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				rw.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, bucket)
		if err := cacher.Put(t.Context(), key, bytes.NewReader(content)); err != nil {
			t.Fatal(err)
		}

		mu.Lock()
		gotOperations := slices.Clone(operations)
		uploadedContent := bytes.Join(gotParts, nil)
		completedParts := slices.Clone(gotCompletedParts)
		mu.Unlock()
		wantOperations := []string{
			"CreateMultipartUpload",
			"UploadPart1",
			"UploadPart2",
			"CompleteMultipartUpload",
		}
		if !slices.Equal(gotOperations, wantOperations) {
			t.Errorf("got operations %q, want %q", gotOperations, wantOperations)
		}
		if !bytes.Equal(uploadedContent, content) {
			t.Error("uploaded parts differ from content")
		}
		wantCompletedParts := []completedPart{
			{ETag: `"test-part-etag-1"`, PartNumber: 1},
			{ETag: `"test-part-etag-2"`, PartNumber: 2},
		}
		if !slices.Equal(completedParts, wantCompletedParts) {
			t.Errorf("got completed parts %v, want %v", completedParts, wantCompletedParts)
		}
	})

	t.Run("MultipartFailure", func(t *testing.T) {
		const (
			bucket   = "test-bucket"
			key      = "example.com/@v/v1.0.0.zip"
			partSize = 5 << 20
			uploadID = "test-upload-id"
		)
		var (
			mu         sync.Mutex
			operations []string
		)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			query := req.URL.Query()
			switch {
			case req.Method == http.MethodPost && query.Has("uploads"):
				mu.Lock()
				operations = append(operations, "CreateMultipartUpload")
				mu.Unlock()
				rw.Header().Set("Content-Type", "application/xml")
				rw.Write([]byte(
					`<InitiateMultipartUploadResult>` +
						`<Bucket>` + bucket + `</Bucket>` +
						`<Key>` + key + `</Key>` +
						`<UploadId>` + uploadID + `</UploadId>` +
						`</InitiateMultipartUploadResult>`,
				))
			case req.Method == http.MethodPut && query.Get("uploadId") == uploadID:
				if _, err := io.Copy(io.Discard, req.Body); err != nil {
					t.Error(err)
					rw.WriteHeader(http.StatusInternalServerError)
					return
				}
				mu.Lock()
				operations = append(operations, "UploadPart")
				mu.Unlock()
				cancel()
				rw.Header().Set("Content-Type", "application/xml")
				rw.WriteHeader(http.StatusBadRequest)
				rw.Write([]byte(`<Error><Code>InvalidRequest</Code></Error>`))
			case req.Method == http.MethodDelete && query.Get("uploadId") == uploadID:
				mu.Lock()
				operations = append(operations, "AbortMultipartUpload")
				mu.Unlock()
				rw.WriteHeader(http.StatusNoContent)
			default:
				t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				rw.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, bucket)
		content := bytes.NewReader(bytes.Repeat([]byte("a"), partSize+1))
		if err := cacher.Put(ctx, key, content); err == nil {
			t.Fatal("expected error, got nil")
		}

		mu.Lock()
		gotOperations := slices.Clone(operations)
		mu.Unlock()
		wantOperations := []string{"CreateMultipartUpload", "UploadPart", "AbortMultipartUpload"}
		if !slices.Equal(gotOperations, wantOperations) {
			t.Errorf("got operations %q, want %q", gotOperations, wantOperations)
		}
	})
}

func TestS3Cache(t *testing.T) {
	t.Run("Read", func(t *testing.T) {
		const content = "test"
		var opens int
		cache := &s3Cache{
			size: int64(len(content)),
			openAt: func(pos int64) (io.ReadCloser, error) {
				opens++
				return io.NopCloser(strings.NewReader(content[pos:])), nil
			},
		}

		if n, err := cache.Read(nil); n != 0 || err != nil {
			t.Fatalf("got (%d, %v), want (0, nil)", n, err)
		}
		buf := make([]byte, len(content))
		if n, err := cache.Read(buf); n != len(content) || err != nil {
			t.Fatalf("got (%d, %v), want (%d, nil)", n, err, len(content))
		}
		if got := string(buf); got != content {
			t.Fatalf("got %q, want %q", got, content)
		}
		if cache.body != nil {
			t.Fatal("response body remains open after reading all content")
		}
		if n, err := cache.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("got (%d, %v), want (0, %v)", n, err, io.EOF)
		}
		if opens != 1 {
			t.Fatalf("opened body %d times, want 1", opens)
		}
	})

	t.Run("ReadOpenError", func(t *testing.T) {
		wantErr := errors.New("open failed")
		cache := &s3Cache{
			size: 1,
			openAt: func(int64) (io.ReadCloser, error) {
				return nil, wantErr
			},
		}
		if n, err := cache.Read(make([]byte, 1)); n != 0 || !errors.Is(err, wantErr) {
			t.Fatalf("got (%d, %v), want (0, %v)", n, err, wantErr)
		}
	})

	t.Run("Seek", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			offset  int64
			whence  int
			want    int64
			wantErr string
		}{
			{name: "Start", offset: 3, whence: io.SeekStart, want: 3},
			{name: "Current", offset: 2, whence: io.SeekCurrent, want: 6},
			{name: "End", offset: -1, whence: io.SeekEnd, want: 9},
			{name: "Same", whence: io.SeekCurrent, want: 4},
			{name: "Negative", offset: -5, whence: io.SeekStart, wantErr: "negative position"},
			{name: "InvalidWhence", whence: -1, wantErr: "invalid whence"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cache := &s3Cache{pos: 4, size: 10}
				got, err := cache.Seek(tt.offset, tt.whence)
				if tt.wantErr != "" {
					if err == nil || err.Error() != tt.wantErr {
						t.Fatalf("got %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if got != tt.want {
					t.Fatalf("got %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("Closed", func(t *testing.T) {
		cache := &s3Cache{closed: true}
		if _, err := cache.Read(make([]byte, 1)); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("got %v, want %v", err, fs.ErrClosed)
		}
		if _, err := cache.Seek(0, io.SeekStart); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("got %v, want %v", err, fs.ErrClosed)
		}
	})
}

func checkContentMD5(t *testing.T, req *http.Request, content []byte) {
	t.Helper()

	sum := md5.Sum(content)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if got := req.Header.Get("Content-MD5"); got != want {
		t.Errorf("got Content-MD5 %q, want %q", got, want)
	}
}

func checkNoSDKChecksumHeaders(t *testing.T, req *http.Request) {
	t.Helper()

	for _, header := range []string{
		"X-Amz-Sdk-Checksum-Algorithm",
		"X-Amz-Checksum-Crc32",
		"X-Amz-Trailer",
	} {
		if got := req.Header.Get(header); got != "" {
			t.Errorf("got %s %q, want empty", header, got)
		}
	}
}

func newTestS3Cacher(t *testing.T, server *httptest.Server, bucket string) *s3Cacher {
	t.Helper()

	cacher, err := newS3Cacher(s3CacherOptions{
		accessKeyID:     "access-key-id",
		secretAccessKey: "secret-access-key",
		endpoint:        server.Listener.Addr().String(),
		disableTLS:      true,
		transport:       server.Client().Transport,
		region:          "us-east-1",
		bucket:          bucket,
		forcePathStyle:  true,
		partSize:        5 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cacher
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
