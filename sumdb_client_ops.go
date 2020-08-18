package goproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// sumdbClientOps implements the `sumdb.ClientOps`.
type sumdbClientOps struct {
	loadOnce    sync.Once
	loadError   error
	key         []byte
	endpointURL *url.URL
	envGOPROXY  string
	envGOSUMDB  string
	httpClient  *http.Client
	errorLogger *log.Logger
}

// load loads the stuff of the sco up.
func (sco *sumdbClientOps) load() {
	sumdbParts := strings.Fields(sco.envGOSUMDB)
	if l := len(sumdbParts); l == 0 {
		sco.loadError = errors.New("missing GOSUMDB")
		return
	} else if l > 2 {
		sco.loadError = errors.New("invalid GOSUMDB: too many fields")
		return
	}

	if sumdbParts[0] == "sum.golang.google.cn" {
		sumdbParts[0] = "sum.golang.org"
		if len(sumdbParts) == 1 {
			sumdbParts = append(
				sumdbParts,
				"https://sum.golang.google.cn",
			)
		}
	}

	if sumdbParts[0] == "sum.golang.org" {
		sumdbParts[0] = "sum.golang.org" +
			"+033de0ae+Ac4zctda0e5eza+HJyk9SxEdh+s3Ux18htTTAD8OuAn8"
	}

	sco.key = []byte(sumdbParts[0])

	sumdbName := sumdbParts[0]
	if i := strings.Index(sumdbName, "+"); i >= 0 {
		sumdbName = sumdbName[:i]
	}

	if len(sumdbParts) == 1 {
		sumdbParts = append(sumdbParts, sumdbName)
	}

	sco.endpointURL, sco.loadError = parseRawURL(sumdbParts[1])
	if sco.loadError != nil {
		return
	}

	var proxyError error
	for goproxy := sco.envGOPROXY; goproxy != ""; {
		var (
			proxy           string
			fallBackOnError bool
		)

		if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
			proxy = goproxy[:i]
			fallBackOnError = goproxy[i] == '|'
			goproxy = goproxy[i+1:]
		} else {
			proxy = goproxy
			goproxy = ""
		}

		if proxy == "direct" || proxy == "off" {
			proxyError = nil
			break
		}

		proxyURL, err := parseRawURL(proxy)
		if err != nil {
			if fallBackOnError {
				proxyError = err
				continue
			}

			sco.loadError = err

			return
		}

		endpointURL := appendURL(proxyURL, "sumdb", sumdbName)

		if err := httpGet(
			context.Background(),
			sco.httpClient,
			appendURL(endpointURL, "/supported").String(),
			nil,
		); err != nil {
			if fallBackOnError || errors.Is(err, errNotFound) {
				proxyError = err
				continue
			}

			sco.loadError = err

			return
		}

		sco.endpointURL = endpointURL

		proxyError = nil

		break
	}

	if proxyError != nil && !errors.Is(proxyError, errNotFound) {
		sco.loadError = proxyError
	}
}

// ReadRemote implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	var buf bytes.Buffer
	if err := httpGet(
		context.Background(),
		sco.httpClient,
		appendURL(sco.endpointURL, path).String(),
		&buf,
	); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ReadConfig implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadConfig(file string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	if file == "key" {
		return sco.key, nil
	} else if strings.HasSuffix(file, "/latest") {
		return []byte{}, nil // Empty result means empty tree
	}

	return nil, fmt.Errorf("unknown config %s", file)
}

// WriteConfig implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) WriteConfig(file string, old, new []byte) error {
	sco.loadOnce.Do(sco.load)
	return sco.loadError
}

// ReadCache implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadCache(file string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	return nil, ErrCacheNotFound
}

// WriteCache implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) WriteCache(file string, data []byte) {
	sco.loadOnce.Do(sco.load)
}

// Log implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) Log(msg string) {
	sco.loadOnce.Do(sco.load)
}

// SecurityError implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) SecurityError(msg string) {
	sco.loadOnce.Do(sco.load)
	if sco.errorLogger != nil {
		sco.errorLogger.Print(msg)
	} else {
		log.Print(msg)
	}
}
