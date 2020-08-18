package goproxy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
