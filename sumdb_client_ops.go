package goproxy

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// sumdbClientOps implements the `sumdb.ClientOps`.
type sumdbClientOps struct {
	endpointURL *url.URL
	envGOPROXY  string
	envGOSUMDB  string
	errorLogger *log.Logger

	loadOnce  sync.Once
	loadError error
}

// load loads the stuff of the sco up.
func (sco *sumdbClientOps) load() {
	sumdbName := sco.envGOSUMDB
	if i := strings.Index(sumdbName, "+"); i >= 0 {
		sumdbName = sumdbName[:i]
	}

	for _, proxy := range strings.Split(sco.envGOPROXY, ",") {
		if proxy == "direct" || proxy == "off" {
			break
		}

		var proxyURL *url.URL
		proxyURL, sco.loadError = parseRawURL(proxy)
		if sco.loadError != nil {
			return
		}

		endpointURL := appendURL(proxyURL, "sumdb", sumdbName)
		operationURL := appendURL(endpointURL, "/supported")

		var res *http.Response
		res, sco.loadError = http.Get(operationURL.String())
		if sco.loadError != nil {
			return
		}
		defer res.Body.Close()

		var b []byte
		b, sco.loadError = ioutil.ReadAll(res.Body)
		if sco.loadError != nil {
			return
		}

		switch res.StatusCode {
		case http.StatusOK:
		case http.StatusBadRequest:
			sco.loadError = fmt.Errorf("%s", b)
			return
		case http.StatusNotFound, http.StatusGone:
			continue
		default:
			sco.loadError = fmt.Errorf(
				"GET %s: %s: %s",
				redactedURL(operationURL),
				res.Status,
				b,
			)
			return
		}

		sco.endpointURL = endpointURL

		return
	}

	sumdbURL := sco.envGOSUMDB
	if i := strings.Index(sumdbURL, " "); i > 0 {
		sumdbURL = sumdbURL[i+1:]
	} else {
		sumdbURL = sumdbName
	}

	var endpointURL *url.URL
	endpointURL, sco.loadError = parseRawURL(sumdbURL)
	if sco.loadError != nil {
		return
	}

	sco.endpointURL = endpointURL
}

// ReadRemote implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	operationURL := appendURL(sco.endpointURL, path)

	res, err := http.Get(operationURL.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound, http.StatusGone:
		return nil, fmt.Errorf("%s", b)
	default:
		return nil, fmt.Errorf(
			"GET %s: %s: %s",
			redactedURL(operationURL),
			res.Status,
			b,
		)
	}

	return b, nil
}

// ReadConfig implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadConfig(file string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	if file == "key" {
		return []byte(sco.envGOSUMDB), nil
	}

	if strings.HasSuffix(file, "/latest") {
		// Empty result means empty tree.
		return []byte{}, nil
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
