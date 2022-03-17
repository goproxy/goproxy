package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/goproxy/goproxy"
)

func main() {
	app := cli.App{
		Name:   "goproxy",
		Usage:  "run local goproxy",
		Action: run,
		Before: before,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "listen", Aliases: []string{"l"}, Value: ":8080", Usage: "address to listen to"},
			&cli.StringFlag{Name: "cache-dir", Aliases: []string{"c"}, Value: "", Usage: "cache directory (none if empty)"},
			&cli.StringFlag{Name: "proxy-path-prefix", Value: "", Usage: "base prefix of all request paths"},

			&cli.StringFlag{Name: "tls-cert", Value: "", Usage: "TLS cert file to listen as HTTPS (http if empty)"},
			&cli.StringFlag{Name: "tls-key", Value: "", Usage: "TLS key file to listen as HTTPS (http if empty)"},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

func before(c *cli.Context) error {
	// setup logger and other globad staff here

	return nil
}

func run(c *cli.Context) error {
	p := &goproxy.Goproxy{
		ErrorLogger: log.Default(), // setup once, use everywhere
	}

	if pp := c.String("proxy-path-prefix"); pp != "" {
		log.Printf("use path prefix: %v", pp)

		p.PathPrefix = pp
	}

	if d := c.String("cache-dir"); d != "" {
		log.Printf("use cache dir: %v", d)

		p.Cacher = goproxy.DirCacher(d)
	}

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Printf("handle request: %v", req.RequestURI)

		p.ServeHTTP(w, req)
	})

	listenAddr := c.String("listen")

	log.Printf("listen address: %v", listenAddr)

	var err error
	if cf, kf := c.String("tls-cert"), c.String("tls-key"); cf != "" || kf != "" {
		err = http.ListenAndServeTLS(listenAddr, cf, kf, h)
	} else {
		err = http.ListenAndServe(listenAddr, h)
	}

	if err != nil {
		return errors.Wrap(err, "listen and serve")
	}

	return nil
}
