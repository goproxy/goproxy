package main

import (
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
)

func main() {
	flag.Parse()

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: *insecure,
	}

	server := &http.Server{
		Addr: *address,
		Handler: &goproxy.Goproxy{
			GoBinName:           *goBinName,
			GoBinMaxWorkers:     *goBinMaxWorkers,
			PathPrefix:          *pathPrefix,
			Cacher:              goproxy.DirCacher(*cacherDir),
			CacherMaxCacheBytes: *cacherMaxCacheBytes,
			ProxiedSUMDBs:       strings.Split(*proxiedSUMDBs, ","),
			TempDir:             *tempDir,
		},
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
