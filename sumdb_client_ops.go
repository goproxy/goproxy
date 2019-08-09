package goproxy

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

// sumdbClientOps implements the `sumdb.ClientOps`.
type sumdbClientOps struct {
	envGOSUMDB  string
	errorLogger *log.Logger
}

// ReadRemote implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadRemote(path string) ([]byte, error) {
	host := sco.envGOSUMDB
	if i := strings.Index(host, "+"); i >= 0 {
		host = host[:i]
	}

	var url string
	if strings.HasPrefix(host, "https://") {
		url = fmt.Sprint(host, path)
	} else {
		url = fmt.Sprint("https://", host, path)
	}

	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// ReadConfig implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) ReadConfig(file string) ([]byte, error) {
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
func (*sumdbClientOps) WriteConfig(file string, old, new []byte) error {
	return nil
}

// ReadCache implements the `sumdb.ClientOps`.
func (*sumdbClientOps) ReadCache(file string) ([]byte, error) {
	return nil, ErrCacheNotFound
}

// WriteCache implements the `sumdb.ClientOps`.
func (*sumdbClientOps) WriteCache(file string, data []byte) {}

// Log implements the `sumdb.ClientOps`.
func (*sumdbClientOps) Log(msg string) {}

// SecurityError implements the `sumdb.ClientOps`.
func (sco *sumdbClientOps) SecurityError(msg string) {
	if sco.errorLogger != nil {
		sco.errorLogger.Print(msg)
	} else {
		log.Print(msg)
	}
}
