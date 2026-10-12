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
	"testing/iotest"
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
		bucket          string
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
			name:     "EmptyBucket",
			endpoint: defaultS3Endpoint,
			region:   "us-east-1",
			partSize: 5 << 20,
			wantErr:  "invalid S3 bucket: bucket is empty",
		},
		{
			name:            "MissingAccessKeyID",
			secretAccessKey: "secret-access-key",
			endpoint:        defaultS3Endpoint,
			region:          "us-east-1",
			bucket:          "test-bucket",
			partSize:        5 << 20,
			wantErr:         "invalid S3 credentials: access key ID is empty",
		},
		{
			name:        "MissingSecretAccessKey",
			accessKeyID: "access-key-id",
			endpoint:    defaultS3Endpoint,
			region:      "us-east-1",
			bucket:      "test-bucket",
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
				bucket:          tt.bucket,
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
				transport: httpRoundTripFunc(func(req *http.Request) (*http.Response, error) {
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

func TestParseS3Endpoint(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{name: "IPv4", endpoint: "127.0.0.1:9000"},
		{name: "IPv6", endpoint: "[::1]"},
		{name: "IPv6WithPort", endpoint: "[::1]:9000"},
		{name: "UnbracketedIPv6", endpoint: "::1", wantErr: true},
		{name: "UnbracketedIPv6WithPort", endpoint: "::1:9000", wantErr: true},
		{name: "MultiplePorts", endpoint: "localhost:9000:9001", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := parseS3Endpoint(tt.endpoint, false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := endpoint.String(), "https://"+tt.endpoint; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestS3CacherGet(t *testing.T) {
	const (
		bucket  = "test-bucket"
		key     = "example.com/@v/v1.0.0.mod"
		content = "module example.com\n"
		etag    = `"test-etag"`
	)
	lastModified := time.Unix(1700000000, 0).UTC()

	for _, tt := range []struct {
		name             string
		prefix           string
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
			name:           "FullResponseWithPrefix",
			prefix:         "goproxy/prod/",
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
			prefix:           "goproxy/prod/",
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
			prefix:         "goproxy-",
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
				if got, want := req.URL.Path, "/"+bucket+"/"+tt.prefix+key; got != want {
					t.Errorf("got path %q, want %q", got, want)
				}
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

			proxy := &goproxy.Goproxy{Cacher: newTestS3Cacher(t, server, bucket, tt.prefix)}
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

	t.Run("NotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/xml")
			rw.WriteHeader(http.StatusNotFound)
			rw.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, "test-bucket", "")
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

		cacher := newTestS3Cacher(t, server, "test-bucket", "")
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

		cache, err := newTestS3Cacher(t, server, "test-bucket", "").Get(t.Context(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		defer cache.Close()
		if _, err := io.ReadAll(cache); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("got %v, want %v", err, fs.ErrNotExist)
		}
	})
}

func TestS3CacherPut(t *testing.T) {
	const content = "test content"

	for _, tt := range []struct {
		name            string
		prefix          string
		key             string
		wantContentType string
	}{
		{
			name:            "VersionInfo",
			key:             "example.com/@v/v1.0.0.info",
			wantContentType: "application/json; charset=utf-8",
		},
		{
			name:            "GoMod",
			key:             "example.com/@v/v1.0.0.mod",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "ModuleZip",
			key:             "example.com/@v/v1.0.0.zip",
			wantContentType: "application/zip",
		},
		{
			name:            "Unknown",
			key:             "other",
			wantContentType: "application/octet-stream",
		},
		{
			name:            "PrefixWithTrailingSlash",
			prefix:          "goproxy/prod/",
			key:             "example.com/@v/v1.0.0.mod",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "PrefixWithoutTrailingSlash",
			prefix:          "goproxy-",
			key:             "example.com/@v/v1.0.0.mod",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "LiteralPrefix",
			prefix:          "/goproxy//./../space %?#/",
			key:             "example.com/@v/v1.0.0.mod",
			wantContentType: "text/plain; charset=utf-8",
		},
		{
			name:            "SumDBWithPrefix",
			prefix:          "goproxy/prod/",
			key:             "sumdb/sum.golang.org/latest",
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

			cacher := newTestS3Cacher(t, server, "test-bucket", tt.prefix)
			if err := cacher.Put(t.Context(), tt.key, strings.NewReader(content)); err != nil {
				t.Fatal(err)
			}
			if got, want := gotMethod, http.MethodPut; got != want {
				t.Errorf("got method %q, want %q", got, want)
			}
			if got, want := gotPath, "/test-bucket/"+tt.prefix+tt.key; got != want {
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

	t.Run("ContentErrors", func(t *testing.T) {
		wantErr := errors.New("content failed")
		for _, tt := range []struct {
			name     string
			whence   int
			failOn   int
			failRead bool
		}{
			{"SeekEnd", io.SeekEnd, 1, false},
			{"SeekStart", io.SeekStart, 1, false},
			{"Read", io.SeekCurrent, 0, true},
			{"HashRewind", io.SeekStart, 2, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var attempts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					attempts.Add(1)
					rw.WriteHeader(http.StatusBadRequest)
				}))
				defer server.Close()
				cacher := newTestS3Cacher(t, server, "test-bucket", "")
				seeks := 0
				rs := strings.NewReader("test")
				var reader io.Reader = rs
				if tt.failRead {
					reader = iotest.ErrReader(wantErr)
				}
				content := struct {
					io.Reader
					io.Seeker
				}{reader, seekerFunc(func(offset int64, whence int) (int64, error) {
					if whence == tt.whence {
						seeks++
						if seeks == tt.failOn {
							return 0, wantErr
						}
					}
					return rs.Seek(offset, whence)
				})}
				if err := cacher.Put(t.Context(), "file", content); !errors.Is(err, wantErr) {
					t.Errorf("got error %v, want %v", err, wantErr)
				}
				if got := attempts.Load(); got != 0 {
					t.Errorf("got upload attempts %d, want 0", got)
				}
			})
		}
	})

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

				cacher := newTestS3Cacher(t, server, "test-bucket", "")
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
			prefix   = "goproxy/prod/"
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
			if got, want := req.URL.Path, "/"+bucket+"/"+prefix+key; got != want {
				t.Errorf("got path %q, want %q", got, want)
			}
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
						`<Key>` + prefix + key + `</Key>` +
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
						`<Key>` + prefix + key + `</Key>` +
						`<ETag>"test-etag"</ETag>` +
						`</CompleteMultipartUploadResult>`,
				))
			default:
				t.Errorf("unexpected request: %s %s", req.Method, req.URL)
				rw.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer server.Close()

		cacher := newTestS3Cacher(t, server, bucket, prefix)
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

	t.Run("MultipartSizeLimit", func(t *testing.T) {
		const partSize = 5 << 20
		for _, tt := range []struct {
			name         string
			size         int64
			wantErr      string
			wantAttempts int32
		}{
			{"AtLimit", partSize * 10_000, "create failed", 1},
			{"AboveLimit", partSize*10_000 + 1, "exceeds the maximum for part size", 0},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var attempts atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
					attempts.Add(1)
					rw.Header().Set("Content-Type", "application/xml")
					rw.WriteHeader(http.StatusBadRequest)
					io.WriteString(rw, `<Error><Code>InvalidRequest</Code><Message>create failed</Message></Error>`)
				}))
				defer server.Close()
				cacher := newTestS3Cacher(t, server, "test-bucket", "")
				content := struct {
					io.Reader
					io.Seeker
				}{strings.NewReader(""), seekerFunc(func(_ int64, whence int) (int64, error) {
					if whence == io.SeekEnd {
						return tt.size, nil
					}
					return 0, nil
				})}
				if err := cacher.Put(t.Context(), "file", content); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("got error %v, want content %q", err, tt.wantErr)
				}
				if got, want := attempts.Load(), tt.wantAttempts; got != want {
					t.Errorf("got upload attempts %d, want %d", got, want)
				}
			})
		}
	})

	t.Run("MultipartErrors", func(t *testing.T) {
		const (
			partSize = 5 << 20
			uploadID = "test-upload-id"
		)
		data := bytes.Repeat([]byte("a"), partSize+1)
		readErr := errors.New("content failed")
		for _, tt := range []struct {
			name            string
			failOperation   string
			missingUploadID bool
			failRead        bool
			failAbort       bool
			wantOperations  []string
		}{
			{"CreateFailure", "CreateMultipartUpload", false, false, false, []string{"CreateMultipartUpload"}},
			{"MissingUploadID", "", true, false, false, []string{"CreateMultipartUpload"}},
			{"PartReadFailure", "", false, true, false, []string{"CreateMultipartUpload", "AbortMultipartUpload"}},
			{"PartUploadFailure", "UploadPart", false, false, false, []string{"CreateMultipartUpload", "UploadPart", "AbortMultipartUpload"}},
			{"CompletionFailure", "CompleteMultipartUpload", false, false, false, []string{"CreateMultipartUpload", "UploadPart", "UploadPart", "CompleteMultipartUpload", "AbortMultipartUpload"}},
			{"AbortFailure", "", false, true, true, []string{"CreateMultipartUpload", "AbortMultipartUpload"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var mu sync.Mutex
				var operations []string
				server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					if _, err := io.Copy(io.Discard, req.Body); err != nil {
						t.Error(err)
						rw.WriteHeader(http.StatusBadRequest)
						return
					}
					var operation string
					query := req.URL.Query()
					switch {
					case req.Method == http.MethodPost && query.Has("uploads"):
						operation = "CreateMultipartUpload"
					case req.Method == http.MethodPut && query.Get("uploadId") == uploadID:
						operation = "UploadPart"
					case req.Method == http.MethodPost && query.Get("uploadId") == uploadID:
						operation = "CompleteMultipartUpload"
					case req.Method == http.MethodDelete && query.Get("uploadId") == uploadID:
						operation = "AbortMultipartUpload"
					default:
						t.Errorf("unexpected request: %s %s", req.Method, req.URL)
						rw.WriteHeader(http.StatusBadRequest)
						return
					}
					mu.Lock()
					operations = append(operations, operation)
					mu.Unlock()
					if operation == tt.failOperation || operation == "AbortMultipartUpload" && tt.failAbort {
						rw.Header().Set("Content-Type", "application/xml")
						rw.WriteHeader(http.StatusBadRequest)
						io.WriteString(rw, `<Error><Code>InvalidRequest</Code><Message>`+operation+` failed</Message></Error>`)
						return
					}
					switch operation {
					case "CreateMultipartUpload":
						rw.Header().Set("Content-Type", "application/xml")
						body := `<InitiateMultipartUploadResult/>`
						if !tt.missingUploadID {
							body = `<InitiateMultipartUploadResult><UploadId>` + uploadID + `</UploadId></InitiateMultipartUploadResult>`
						}
						io.WriteString(rw, body)
					case "UploadPart":
						rw.Header().Set("ETag", `"test-etag"`)
						rw.WriteHeader(http.StatusOK)
					case "AbortMultipartUpload":
						rw.WriteHeader(http.StatusNoContent)
					}
				}))
				defer server.Close()
				cacher := newTestS3Cacher(t, server, "test-bucket", "")
				rs := bytes.NewReader(data)
				var reader io.Reader = rs
				if tt.failRead {
					reader = iotest.ErrReader(readErr)
				}
				content := struct {
					io.Reader
					io.Seeker
				}{reader, rs}
				err := cacher.Put(t.Context(), "file", content)
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.missingUploadID && err.Error() != "S3 multipart upload response has no upload ID" {
					t.Errorf("got error %v, want a missing upload ID error", err)
				}
				if tt.failRead && !errors.Is(err, readErr) {
					t.Errorf("got error %v, want %v", err, readErr)
				}
				if tt.failOperation != "" && !strings.Contains(err.Error(), tt.failOperation+" failed") {
					t.Errorf("got error %v, want a %s error", err, tt.failOperation)
				}
				if tt.failAbort && (!strings.Contains(err.Error(), "abort S3 multipart upload:") || !strings.Contains(err.Error(), "AbortMultipartUpload failed")) {
					t.Errorf("got error %v, want an abort error", err)
				}
				mu.Lock()
				gotOperations := slices.Clone(operations)
				mu.Unlock()
				if !slices.Equal(gotOperations, tt.wantOperations) {
					t.Errorf("got operations %q, want %q", gotOperations, tt.wantOperations)
				}
			})
		}
	})

	t.Run("MultipartFailure", func(t *testing.T) {
		const (
			bucket   = "test-bucket"
			prefix   = "goproxy/prod/"
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
			if got, want := req.URL.Path, "/"+bucket+"/"+prefix+key; got != want {
				t.Errorf("got path %q, want %q", got, want)
			}
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
						`<Key>` + prefix + key + `</Key>` +
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

		cacher := newTestS3Cacher(t, server, bucket, prefix)
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

func TestComputeContentMD5(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    string
	}{
		{"Empty", "", "1B2M2Y8AsgTpgAmY7PhCfg=="},
		{"Content", "test", "CY9rzUYh03PK3k6DJie09g=="},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := strings.NewReader(tt.content)
			got, err := computeContentMD5(content)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got digest %q, want %q", got, tt.want)
			}
			if data, err := io.ReadAll(content); err != nil || string(data) != tt.content {
				t.Errorf("got rewound content (%q, %v), want (%q, nil)", data, err, tt.content)
			}
		})
	}

	wantErr := errors.New("content failed")
	for _, tt := range []struct {
		name    string
		content io.ReadSeeker
	}{
		{"ReadError", struct {
			io.Reader
			io.Seeker
		}{iotest.ErrReader(wantErr), strings.NewReader("")}},
		{"SeekError", struct {
			io.Reader
			io.Seeker
		}{strings.NewReader("test"), seekerFunc(func(int64, int) (int64, error) { return 0, wantErr })}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if digest, err := computeContentMD5(tt.content); digest != "" || !errors.Is(err, wantErr) {
				t.Errorf("got (%q, %v), want an empty digest and %v", digest, err, wantErr)
			}
		})
	}
}

func TestReadSeekerAtReadAt(t *testing.T) {
	for _, tt := range []struct {
		name    string
		offset  int64
		size    int
		want    string
		wantErr error
	}{
		{"Start", 0, 2, "te", nil},
		{"Middle", 1, 2, "es", nil},
		{"End", 2, 2, "st", nil},
		{"Empty", 4, 0, "", nil},
		{"ShortRead", 3, 2, "t", io.ErrUnexpectedEOF},
		{"PastEnd", 5, 1, "", io.EOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := readSeekerAt{strings.NewReader("test")}
			for range 2 {
				buf := make([]byte, tt.size)
				n, err := r.ReadAt(buf, tt.offset)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got error %v, want %v", err, tt.wantErr)
				}
				if n != len(tt.want) {
					t.Fatalf("got bytes read %d, want %d", n, len(tt.want))
				}
				if got := string(buf[:n]); got != tt.want {
					t.Errorf("got content %q, want %q", got, tt.want)
				}
			}
		})
	}

	wantErr := errors.New("content failed")
	for _, tt := range []struct {
		name    string
		content io.ReadSeeker
	}{
		{"ReadError", struct {
			io.Reader
			io.Seeker
		}{iotest.ErrReader(wantErr), strings.NewReader("")}},
		{"SeekError", struct {
			io.Reader
			io.Seeker
		}{strings.NewReader("test"), seekerFunc(func(int64, int) (int64, error) { return 0, wantErr })}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := readSeekerAt{tt.content}
			if n, err := r.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(err, wantErr) {
				t.Errorf("got (%d, %v), want (0, %v)", n, err, wantErr)
			}
		})
	}
}

func TestS3CacheRead(t *testing.T) {
	t.Run("Content", func(t *testing.T) {
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
			t.Fatalf("got body opens %d, want 1", opens)
		}
	})

	t.Run("OpenError", func(t *testing.T) {
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

	t.Run("ReadError", func(t *testing.T) {
		wantErr := errors.New("read failed")
		closes := 0
		cache := &s3Cache{
			size: 1,
			openAt: func(int64) (io.ReadCloser, error) {
				return struct {
					io.Reader
					io.Closer
				}{iotest.ErrReader(wantErr), closerFunc(func() error {
					closes++
					return nil
				})}, nil
			},
		}
		if n, err := cache.Read(make([]byte, 1)); n != 0 || !errors.Is(err, wantErr) {
			t.Errorf("got (%d, %v), want (0, %v)", n, err, wantErr)
		}
		if cache.body != nil || closes != 1 {
			t.Errorf("got body %v and closes %d, want a closed body and one close", cache.body, closes)
		}
	})

	t.Run("Closed", func(t *testing.T) {
		cache := &s3Cache{closed: true}
		if n, err := cache.Read(make([]byte, 1)); n != 0 || !errors.Is(err, fs.ErrClosed) {
			t.Errorf("got (%d, %v), want (0, %v)", n, err, fs.ErrClosed)
		}
	})
}

func TestS3CacheSeek(t *testing.T) {
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
			closes := 0
			cache := &s3Cache{
				pos: 4, size: 10,
				body: struct {
					io.Reader
					io.Closer
				}{strings.NewReader("test"), closerFunc(func() error {
					closes++
					return nil
				})},
			}
			defer cache.Close()
			got, err := cache.Seek(tt.offset, tt.whence)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("got %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
			wantPos, wantCloses := int64(4), 0
			if tt.wantErr == "" {
				wantPos = tt.want
				if wantPos != 4 {
					wantCloses = 1
				}
			}
			if cache.pos != wantPos || closes != wantCloses {
				t.Errorf("got position %d and closes %d, want %d and %d", cache.pos, closes, wantPos, wantCloses)
			}
		})
	}

	t.Run("Closed", func(t *testing.T) {
		cache := &s3Cache{closed: true}
		if pos, err := cache.Seek(0, io.SeekStart); pos != 0 || !errors.Is(err, fs.ErrClosed) {
			t.Errorf("got (%d, %v), want (0, %v)", pos, err, fs.ErrClosed)
		}
	})
}

func TestS3CacheClose(t *testing.T) {
	for _, tt := range []struct {
		name    string
		open    bool
		wantErr error
	}{
		{"Unopened", false, nil},
		{"Opened", true, nil},
		{"CloseError", true, errors.New("close failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cache := &s3Cache{}
			closes := 0
			if tt.open {
				cache.body = struct {
					io.Reader
					io.Closer
				}{strings.NewReader("test"), closerFunc(func() error {
					closes++
					return tt.wantErr
				})}
			}
			if err := cache.Close(); !errors.Is(err, tt.wantErr) {
				t.Errorf("got error %v, want %v", err, tt.wantErr)
			}
			if cache.body != nil {
				t.Error("response body remains open after closing")
			}
			if err := cache.Close(); err != nil {
				t.Errorf("unexpected error from repeated close %v", err)
			}
			wantCloses := 0
			if tt.open {
				wantCloses = 1
			}
			if closes != wantCloses {
				t.Errorf("got closes %d, want %d", closes, wantCloses)
			}
			if _, err := cache.Read(make([]byte, 1)); !errors.Is(err, fs.ErrClosed) {
				t.Errorf("got read error %v, want %v", err, fs.ErrClosed)
			}
			if _, err := cache.Seek(0, io.SeekStart); !errors.Is(err, fs.ErrClosed) {
				t.Errorf("got seek error %v, want %v", err, fs.ErrClosed)
			}
		})
	}
}

func newTestS3Cacher(t *testing.T, server *httptest.Server, bucket string, prefix string) *s3Cacher {
	t.Helper()

	cacher, err := newS3Cacher(s3CacherOptions{
		accessKeyID:     "access-key-id",
		secretAccessKey: "secret-access-key",
		endpoint:        server.Listener.Addr().String(),
		disableTLS:      true,
		transport:       server.Client().Transport,
		region:          "us-east-1",
		bucket:          bucket,
		prefix:          prefix,
		forcePathStyle:  true,
		partSize:        5 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cacher
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

type httpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type seekerFunc func(int64, int) (int64, error)

func (f seekerFunc) Seek(offset int64, whence int) (int64, error) {
	return f(offset, whence)
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}
