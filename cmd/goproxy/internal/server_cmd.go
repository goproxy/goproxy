package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
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
		Args: cobra.NoArgs,
	}
	cfg := newServerCmdConfig(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return runServerCmd(cmd, cfg) }
	return cmd
}

// serverCmdConfig is the configuration for server command.
type serverCmdConfig struct {
	address                    string
	tlsCertFile                string
	tlsKeyFile                 string
	pathPrefix                 string
	goBin                      string
	maxConcurrentDirectFetches int
	proxiedSumDBs              []string
	cacher                     string
	cacherDir                  string
	s3CacherOpts               s3CacherOptions
	tempDir                    string
	insecure                   bool
	readHeaderTimeout          time.Duration
	idleTimeout                time.Duration
	fetchTimeout               time.Duration
	connectTimeout             time.Duration
	shutdownTimeout            time.Duration
	logFormat                  string
}

// newServerCmdConfig creates a new [serverCmdConfig].
func newServerCmdConfig(cmd *cobra.Command) *serverCmdConfig {
	cfg := &serverCmdConfig{}
	fs := cmd.Flags()
	fs.SortFlags = false
	fs.StringVar(&cfg.address, "address", "localhost:8080", "TCP address that the server listens on")
	fs.StringVar(&cfg.tlsCertFile, "tls-cert-file", "", "path to the TLS certificate file (requires --tls-key-file)")
	fs.StringVar(&cfg.tlsKeyFile, "tls-key-file", "", "path to the TLS key file (requires --tls-cert-file)")
	fs.StringVar(&cfg.pathPrefix, "path-prefix", "", "absolute path prefix for all request paths")
	fs.StringVar(&cfg.goBin, "go-bin", "go", "path to the Go binary that is used to execute direct fetches")
	fs.IntVar(&cfg.maxConcurrentDirectFetches, "max-concurrent-direct-fetches", 0, "maximum number of concurrent direct fetches (0 means no limit)")
	fs.StringSliceVar(&cfg.proxiedSumDBs, "proxied-sumdbs", nil, "list of proxied checksum databases")
	fs.StringVar(&cfg.cacher, "cacher", "dir", "cacher to use (valid values: dir, s3)")
	fs.StringVar(&cfg.cacherDir, "cacher-dir", "caches", "directory for the dir cacher")
	fs.StringVar(&cfg.s3CacherOpts.accessKeyID, "cacher-s3-access-key-id", "", "access key ID for the S3 cacher (requires --cacher-s3-secret-access-key)")
	fs.StringVar(&cfg.s3CacherOpts.secretAccessKey, "cacher-s3-secret-access-key", "", "secret access key for the S3 cacher (requires --cacher-s3-access-key-id)")
	fs.StringVar(&cfg.s3CacherOpts.endpoint, "cacher-s3-endpoint", defaultS3Endpoint, "endpoint for the S3 cacher")
	fs.BoolVar(&cfg.s3CacherOpts.disableTLS, "cacher-s3-disable-tls", false, "disable TLS for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.region, "cacher-s3-region", "us-east-1", "region for the S3 cacher")
	fs.StringVar(&cfg.s3CacherOpts.bucket, "cacher-s3-bucket", "", "bucket name for the S3 cacher (required when --cacher=s3)")
	fs.BoolVar(&cfg.s3CacherOpts.forcePathStyle, "cacher-s3-force-path-style", false, "force path-style addressing for the S3 cacher")
	fs.Int64Var(&cfg.s3CacherOpts.partSize, "cacher-s3-part-size", 100<<20, "multipart upload part size for the S3 cacher")
	fs.StringVar(&cfg.tempDir, "temp-dir", os.TempDir(), "directory for storing temporary files")
	fs.BoolVar(&cfg.insecure, "insecure", false, "skip TLS certificate verification for outgoing HTTP requests")
	fs.DurationVar(&cfg.readHeaderTimeout, "read-header-timeout", 10*time.Second, "maximum amount of time to read incoming request headers (0 means no limit)")
	fs.DurationVar(&cfg.idleTimeout, "idle-timeout", time.Minute, "maximum amount of time to wait for the next incoming request (0 means no limit)")
	fs.DurationVar(&cfg.fetchTimeout, "fetch-timeout", 10*time.Minute, "maximum amount of time to wait for a fetch to complete (0 means no limit)")
	fs.DurationVar(&cfg.connectTimeout, "connect-timeout", 30*time.Second, "maximum amount of time to establish an outgoing connection (0 means no limit)")
	fs.DurationVar(&cfg.shutdownTimeout, "shutdown-timeout", 10*time.Second, "maximum amount of time to shut down the server gracefully (0 means no limit)")
	fs.StringVar(&cfg.logFormat, "log-format", "text", "log format to use (valid values: text, json)")
	return cfg
}

// validate validates the server command configuration.
func (cfg *serverCmdConfig) validate() error {
	if (cfg.tlsCertFile == "") != (cfg.tlsKeyFile == "") {
		return errors.New("invalid TLS configuration: --tls-cert-file and --tls-key-file must be set together")
	}

	pathPrefix := strings.TrimSuffix(cfg.pathPrefix, "/")
	if pathPrefix != "" && (!strings.HasPrefix(pathPrefix, "/") || pathPrefix == "/" || path.Clean(pathPrefix) != pathPrefix) {
		return fmt.Errorf("invalid --path-prefix: %q is not a clean absolute path", cfg.pathPrefix)
	}

	if cfg.maxConcurrentDirectFetches < 0 {
		return fmt.Errorf("invalid --max-concurrent-direct-fetches: %d must not be negative", cfg.maxConcurrentDirectFetches)
	}

	for _, timeout := range []struct {
		name  string
		value time.Duration
	}{
		{"read-header-timeout", cfg.readHeaderTimeout},
		{"idle-timeout", cfg.idleTimeout},
		{"fetch-timeout", cfg.fetchTimeout},
		{"connect-timeout", cfg.connectTimeout},
		{"shutdown-timeout", cfg.shutdownTimeout},
	} {
		if timeout.value < 0 {
			return fmt.Errorf("invalid --%s: %v must not be negative", timeout.name, timeout.value)
		}
	}

	return nil
}

// runServerCmd runs the server command.
func runServerCmd(cmd *cobra.Command, cfg *serverCmdConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: cfg.connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.insecure}
	transport.RegisterProtocol("file", http.NewFileTransport(httpDirFS{}))
	defer transport.CloseIdleConnections()

	g := &goproxy.Goproxy{
		Fetcher: &goproxy.GoFetcher{
			GoBin:                      cfg.goBin,
			MaxConcurrentDirectFetches: cfg.maxConcurrentDirectFetches,
			TempDir:                    cfg.tempDir,
			Transport:                  transport,
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

	server := &http.Server{
		Handler:                      newServerHandler(cfg, g),
		DisableGeneralOptionsHandler: true,
		ReadHeaderTimeout:            cfg.readHeaderTimeout,
		IdleTimeout:                  cfg.idleTimeout,
		ErrorLog:                     slog.NewLogLogger(logHandler, slog.LevelError),
		BaseContext:                  func(_ net.Listener) context.Context { return cmd.Context() },
	}

	stopCtx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	address := cfg.address
	if address == "" {
		address = ":http"
		if cfg.tlsCertFile != "" {
			address = ":https"
		}
	}
	listener, err := (&net.ListenConfig{}).Listen(stopCtx, "tcp", address)
	if err != nil {
		if stopCtx.Err() != nil {
			return nil
		}
		return err
	}
	defer listener.Close()

	serverErrCh := make(chan error, 1)
	go func() {
		if cfg.tlsCertFile != "" {
			serverErrCh <- server.ServeTLS(listener, cfg.tlsCertFile, cfg.tlsKeyFile)
		} else {
			serverErrCh <- server.Serve(listener)
		}
		stop()
	}()

	<-stopCtx.Done()
	select {
	case serverErr := <-serverErrCh:
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			return serverErr
		}
		return nil
	default:
	}

	shutdownCtx := context.Background()
	if cfg.shutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(shutdownCtx, cfg.shutdownTimeout)
		defer cancel()
	}
	shutdownErr := server.Shutdown(shutdownCtx)
	if serverErr := <-serverErrCh; serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		return serverErr
	}
	return shutdownErr
}

// newServerHandler creates a new [http.Handler] used by the server command.
func newServerHandler(cfg *serverCmdConfig, base http.Handler) http.Handler {
	prefix := strings.TrimSuffix(cfg.pathPrefix, "/")
	patternPrefix := (&url.URL{Path: prefix}).EscapedPath()
	handler := base
	if prefix != "" {
		handler = http.StripPrefix(prefix, handler)
	}
	mux := http.NewServeMux()
	mux.Handle(patternPrefix+"/", handler)
	mux.HandleFunc("GET "+patternPrefix+"/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	})
	handler = mux
	if cfg.fetchTimeout > 0 {
		handler = func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				ctx, cancel := context.WithTimeout(req.Context(), cfg.fetchTimeout)
				defer cancel()
				h.ServeHTTP(rw, req.WithContext(ctx))
			})
		}(handler)
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		if req.Method != http.MethodGet && req.Method != http.MethodHead || req.ContentLength != 0 {
			base.ServeHTTP(rw, req)
			return
		}
		handler.ServeHTTP(rw, req)
	})
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
