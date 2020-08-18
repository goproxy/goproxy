package goproxy

import (
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
