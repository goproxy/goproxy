package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/spf13/cobra"
)

// newServerCmd creates a new server command.
func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start a Go module proxy server",
		Long: strings.TrimSpace(`
Start a Go module proxy server.

Make sure that the Go binary and the version control systems (such as Git) that
need to be supported are installed and properly configured in the current
environment, as they are required for direct module fetching.

During a direct module fetch, the Go binary is called while holding a lock file
in the module cache directory (specified by GOMODCACHE) to prevent potential
conflicts. Misuse of a shared GOMODCACHE may lead to deadlocks.
`),
	}
	cfg := newServerCmdConfig(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runServerCmd(cmd, args, cfg) }
	return cmd
}

// serverCmdConfig is the configuration for server command.
type serverCmdConfig struct {
	address          string
	tlsCertFile      string
	tlsKeyFile       string
	pathPrefix       string
	goBin            string
	maxDirectFetches int
	proxiedSumDBs    []string
	cacher           string
	cacherDir        string
	s3CacherOpts     s3CacherOptions
	tempDir          string
	insecure         bool
	connectTimeout   time.Duration
	fetchTimeout     time.Duration
	shutdownTimeout  time.Duration
	logFormat        string
}

// newServerCmdConfig creates a new [serverCmdConfig].
func newServerCmdConfig(cmd *cobra.Command) *serverCmdConfig {
	cfg := &serverCmdConfig{}
	fs := cmd.Flags()
	fs.StringVar(&cfg.address, "address", "localhost:8080", "TCP address that the server listens on")
	fs.StringVar(&cfg.tlsCertFile, "tls-cert-file", "", "path to the TLS certificate file")
	fs.StringVar(&cfg.tlsKeyFile, "tls-key-file", "", "path to the TLS key file")
	fs.StringVar(&cfg.pathPrefix, "path-prefix", "", "prefix for all request paths")
	fs.StringVar(&cfg.goBin, "go-bin", "go", "path to the Go binary that is used to execute direct fetches")
	fs.IntVar(&cfg.maxDirectFetches, "max-direct-fetches", 0, "maximum number (0 means no limit) of concurrent direct fetches")
	fs.StringSliceVar(&cfg.proxiedSumDBs, "proxied-sumdbs", nil, "list of proxied checksum databases")
	fs.StringVar(&cfg.cacher, "cacher", "dir", "cacher to use (valid values: dir, s3)")
	fs.StringVar(&cfg.cacherDir, "cacher-dir", "caches", "directory for the dir cacher")
	fs.StringVar(&cfg.s3CacherOpts.accessKeyID, "cacher-s3-access-key-id", "", "access key ID for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.secretAccessKey, "cacher-s3-secret-access-key", "", "secret access key for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.endpoint, "cacher-s3-endpoint", "s3.amazonaws.com", "endpoint for the S3 cacher")
	fs.BoolVar(&cfg.s3CacherOpts.disableTLS, "cacher-s3-disable-tls", false, "disable TLS for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.region, "cacher-s3-region", "us-east-1", "region for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.bucket, "cacher-s3-bucket", "", "bucket name for the S3 cacher")
	fs.BoolVar(&cfg.s3CacherOpts.forcePathStyle, "cacher-s3-force-path-style", false, "force path-style addressing for the S3 cacher")
	fs.Int64Var(&cfg.s3CacherOpts.partSize, "cacher-s3-part-size", 100<<20, "multipart upload part size for the S3 cacher")
	fs.StringVar(&cfg.tempDir, "temp-dir", os.TempDir(), "directory for storing temporary files")
	fs.BoolVar(&cfg.insecure, "insecure", false, "allow insecure TLS connections")
	fs.DurationVar(&cfg.connectTimeout, "connect-timeout", 30*time.Second, "maximum amount of time (0 means no limit) will wait for an outgoing connection to establish")
	fs.DurationVar(&cfg.fetchTimeout, "fetch-timeout", 10*time.Minute, "maximum amount of time (0 means no limit) will wait for a fetch to complete")
	fs.DurationVar(&cfg.shutdownTimeout, "shutdown-timeout", 10*time.Second, "maximum amount of time (0 means no limit) will wait for the server to shutdown")
	fs.StringVar(&cfg.logFormat, "log-format", "text", "log format to use (valid values: text, json)")
	return cfg
}

// runServerCmd runs the server command.
func runServerCmd(cmd *cobra.Command, args []string, cfg *serverCmdConfig) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: cfg.connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.insecure}
	transport.RegisterProtocol("file", http.NewFileTransport(httpDirFS{}))
	g := &goproxy.Goproxy{
		Fetcher: &goproxy.GoFetcher{
			GoBin:            cfg.goBin,
			MaxDirectFetches: cfg.maxDirectFetches,
			TempDir:          cfg.tempDir,
			Transport:        transport,
		},
		ProxiedSumDBs: cfg.proxiedSumDBs,
		TempDir:       cfg.tempDir,
		Transport:     transport,
	}

	switch cfg.cacher {
	case "dir":
		g.Cacher = goproxy.DirCacher(cfg.cacherDir)
	case "s3":
		s3CacherOpts := cfg.s3CacherOpts
		s3CacherOpts.transport = transport
		s3c, err := newS3Cacher(s3CacherOpts)
		if err != nil {
			return err
		}
		g.Cacher = s3c
	default:
		return fmt.Errorf("invalid --cacher: %q", cfg.cacher)
	}

	var logHandler slog.Handler
	switch cfg.logFormat {
	case "text":
		logHandler = slog.NewTextHandler(os.Stderr, nil)
	case "json":
		logHandler = slog.NewJSONHandler(os.Stderr, nil)
	default:
		return fmt.Errorf("invalid --log-format: %q", cfg.logFormat)
	}
	g.Logger = slog.New(logHandler)

	handler := http.Handler(g)
	if cfg.pathPrefix != "" {
		handler = http.StripPrefix(cfg.pathPrefix, handler)
	}
	if cfg.fetchTimeout > 0 {
		handler = func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				ctx, cancel := context.WithTimeout(req.Context(), cfg.fetchTimeout)
				h.ServeHTTP(rw, req.WithContext(ctx))
				cancel()
			})
		}(handler)
	}

	server := &http.Server{
		Addr:        cfg.address,
		Handler:     handler,
		BaseContext: func(_ net.Listener) context.Context { return cmd.Context() },
	}
	stopCtx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var serverErr error
	go func() {
		if cfg.tlsCertFile != "" && cfg.tlsKeyFile != "" {
			serverErr = server.ListenAndServeTLS(cfg.tlsCertFile, cfg.tlsKeyFile)
		} else {
			serverErr = server.ListenAndServe()
		}
		stop()
	}()
	<-stopCtx.Done()
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		return serverErr
	}

	shutdownCtx := context.Background()
	if cfg.shutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(shutdownCtx, cfg.shutdownTimeout)
		defer cancel()
	}
	return server.Shutdown(shutdownCtx)
}

// httpDirFS implements [http.FileSystem] for the local file system.
type httpDirFS struct{}

// Open implements [http.FileSystem].
func (fs httpDirFS) Open(name string) (http.File, error) {
	name = filepath.FromSlash(name)
	if filepath.Separator == '\\' {
		name = name[1:]
		volumeName := filepath.VolumeName(name)
		if volumeName == "" || strings.HasPrefix(volumeName, `\\`) {
			return nil, errors.New("file URL missing drive letter")
		}
	}
	if !filepath.IsAbs(name) {
		return nil, errors.New("path is not absolute")
	}
	return os.Open(name)
}
