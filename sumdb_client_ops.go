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
	endpoint    string
	envGOPROXY  string
	envGOSUMDB  string
	errorLogger *log.Logger

	loadOnce  sync.Once
	loadError error
}

// load loads the stuff of the sco up.
func (sco *sumdbClientOps) load() {
	host := sco.envGOSUMDB
	if i := strings.Index(host, "+"); i >= 0 {
		host = host[:i]
	}

	for _, goproxy := range strings.Split(sco.envGOPROXY, ",") {
		goproxy = strings.TrimSpace(goproxy)
		if goproxy == "" {
			continue
		}

		if goproxy == "direct" || goproxy == "off" {
			break
		}

		endpoint := fmt.Sprintf("%s/sumdb/%s", goproxy, host)
		url := fmt.Sprint(endpoint, "/supported")

		var res *http.Response
		if res, sco.loadError = http.Get(url); sco.loadError != nil {
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
				url,
				res.Status,
				b,
			)
			return
		}

		sco.endpoint = endpoint

		return
	}

	var hostURL *url.URL
	if hostURL, sco.loadError = url.Parse(host); sco.loadError != nil {
		return
	}

	if hostURL.Scheme == "" {
		hostURL.Scheme = "https"
	}

	sco.endpoint = hostURL.String()
}

// ReadRemote implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	if sco.loadOnce.Do(sco.load); sco.loadError != nil {
		return nil, sco.loadError
	}

	url := fmt.Sprint(sco.endpoint, path)

	res, err := http.Get(url)
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
		return nil, fmt.Errorf("GET %s: %s: %s", url, res.Status, b)
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
