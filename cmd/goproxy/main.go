package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/goproxy/goproxy"
)

var (
	address             = flag.String("address", "localhost:8080", "TCP address that the HTTP server listens on")
	tlsCertFile         = flag.String("tls-cert-file", "", "path to the TLS certificate file")
	tlsKeyFile          = flag.String("tls-key-file", "", "path to the TLS key file")
	goBinName           = flag.String("go-bin-name", "go", "name of the Go binary")
	goBinMaxWorkers     = flag.Int("go-bin-max-workers", 0, "maximum number (0 means no limit) of commands allowed for the Go binary to execute at the same time")
	pathPrefix          = flag.String("path-prefix", "", "prefix of all request paths")
	cacherDir           = flag.String("cacher-dir", "caches", "directory that used to cache module files")
	cacherMaxCacheBytes = flag.Int("cacher-max-cache-bytes", 0, "maximum number (0 means no limit) of bytes allowed for the cacher to store a cache")
	proxiedSUMDBs       = flag.String("proxied-sumdbs", "", "comma-separated list of proxied checksum databases")
	tempDir             = flag.String("temp-dir", os.TempDir(), "directory for storing temporary files")
	insecure            = flag.Bool("insecure", false, "allow insecure TLS connections")
	fetchTimeout        = flag.Duration("fetch-timeout", 0, "maximum amount of time (0 means no limit) will wait for a fetch to complete")
)

func main() {
	flag.Parse()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: *insecure}

	g := &goproxy.Goproxy{
		GoBinName:           *goBinName,
		GoBinMaxWorkers:     *goBinMaxWorkers,
		PathPrefix:          *pathPrefix,
		Cacher:              goproxy.DirCacher(*cacherDir),
		CacherMaxCacheBytes: *cacherMaxCacheBytes,
		ProxiedSUMDBs:       strings.Split(*proxiedSUMDBs, ","),
		Transport:           transport,
		TempDir:             *tempDir,
	}

	server := &http.Server{Addr: *address}
	if *fetchTimeout == 0 {
		server.Handler = g
	} else {
		server.Handler = http.HandlerFunc(func(
			rw http.ResponseWriter,
			req *http.Request,
		) {
			ctx, cancel := context.WithTimeout(
				req.Context(),
				*fetchTimeout,
			)
			g.ServeHTTP(rw, req.WithContext(ctx))
			cancel()
		})
	}

	var err error
	if *tlsCertFile != "" && *tlsKeyFile != "" {
		err = server.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile)
	} else {
		err = server.ListenAndServe()
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("http server error: %v\n", err)
		return
	}
}
