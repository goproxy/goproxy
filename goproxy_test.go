package goproxy

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	g := New()
	assert.Equal(t, "go", g.GoBinName)
	assert.Equal(t, os.Environ(), g.GoBinEnv)
	assert.Zero(t, g.GoBinMaxWorkers)
	assert.Equal(t, time.Minute, g.GoBinFetchTimeout)
	assert.Empty(t, g.PathPrefix)
	assert.Nil(t, g.Cacher)
	assert.Zero(t, g.CacherMaxCacheBytes)
	assert.Nil(t, g.ProxiedSUMDBs)
	assert.False(t, g.InsecureMode)
	assert.Nil(t, g.ErrorLogger)
	assert.NotNil(t, g.loadOnce)
	assert.NotNil(t, g.httpClient)
	assert.NotNil(t, g.goBinEnv)
	assert.Nil(t, g.goBinWorkerChan)
	assert.Nil(t, g.sumdbClient)
	assert.NotNil(t, g.proxiedSUMDBs)
}

func TestIsNotFoundError(t *testing.T) {
	assert.True(t, isNotFoundError(&notFoundError{errors.New("not found")}))
	assert.False(t, isNotFoundError(errors.New("some other error")))
}

func TestIsTimeoutError(t *testing.T) {
	assert.False(t, isTimeoutError(&url.Error{
		Err: errors.New("some other url error"),
	}))

	// ---

	assert.True(t, isTimeoutError(context.DeadlineExceeded))

	// ---

	assert.False(t, isTimeoutError(context.Canceled))

	// ---

	assert.True(t, isTimeoutError(errors.New("fetch timed out")))

	// ---

	assert.False(t, isTimeoutError(errors.New("some other error")))
}

func TestParseRawURL(t *testing.T) {
	u, err := parseRawURL("example.com")
	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Equal(t, "https://example.com", u.String())

	// ---

	u, err = parseRawURL("http://example.com")
	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Equal(t, "http://example.com", u.String())

	// ---

	u, err = parseRawURL("https://example.com")
	assert.NoError(t, err)
	assert.NotNil(t, u)
	assert.Equal(t, "https://example.com", u.String())

	// ---

	u, err = parseRawURL("\n")
	assert.Error(t, err)
	assert.Nil(t, u)

	// ---

	u, err = parseRawURL("scheme://example.com")
	assert.Error(t, err)
	assert.Nil(t, u)
}

func TestAppendURL(t *testing.T) {
	assert.Equal(t, "https://example.com/foobar", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "foobar").String())

	// ---

	assert.Equal(t, "https://example.com/foo/bar", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "foo", "bar").String())

	// ---

	assert.Equal(t, "https://example.com/foo/bar", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "", "foo", "", "bar").String())

	// ---

	assert.Equal(t, "https://example.com/foo/bar", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "foo/bar").String())

	// ---

	assert.Equal(t, "https://example.com/foo/bar", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "/foo/bar").String())

	// ---

	assert.Equal(t, "https://example.com/foo/bar/", appendURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}, "/foo/bar/").String())
}

func TestRedactedURL(t *testing.T) {
	assert.Equal(t, "https://example.com", redactedURL(&url.URL{
		Scheme: "https",
		Host:   "example.com",
	}))

	// ---

	assert.Equal(t, "https://user@example.com", redactedURL(&url.URL{
		Scheme: "https",
		User:   url.User("user"),
		Host:   "example.com",
	}))

	// ---

	assert.Equal(t, "https://user:xxxxx@example.com", redactedURL(&url.URL{
		Scheme: "https",
		User:   url.UserPassword("user", "password"),
		Host:   "example.com",
	}))
}

func TestStringSliceContains(t *testing.T) {
	assert.True(t, stringSliceContains([]string{"foo", "bar"}, "foo"))
	assert.False(t, stringSliceContains([]string{"foo", "bar"}, "foobar"))
}

func TestGlobsMatchPath(t *testing.T) {
	assert.True(t, globsMatchPath("foobar", "foobar"))

	// ---

	assert.True(t, globsMatchPath("foo", "foo/bar"))

	// ---

	assert.False(t, globsMatchPath("foo", "bar/foo"))

	// ---

	assert.False(t, globsMatchPath("foo", "foobar"))

	// ---

	assert.True(t, globsMatchPath("foo/bar", "foo/bar"))

	// ---

	assert.False(t, globsMatchPath("foo/bar", "foobar"))

	// ---

	assert.True(t, globsMatchPath("foo,bar", "foo"))

	// ---

	assert.True(t, globsMatchPath("foo,", "foo"))

	// ---

	assert.True(t, globsMatchPath(",bar", "bar"))

	// ---

	assert.False(t, globsMatchPath("", "foobar"))
}
