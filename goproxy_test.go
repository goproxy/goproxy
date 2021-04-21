package goproxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
