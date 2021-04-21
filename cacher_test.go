package goproxy

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirCacher(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "goproxy.TestDirCacher")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dirCacher := DirCacher(tempDir)

	rc, err := dirCacher.Get(nil, "a/b/c")
	assert.ErrorIs(t, err, os.ErrNotExist)

	err = dirCacher.Set(nil, "a/b/c", strings.NewReader("foobar"))
	assert.NoError(t, err)

	rc, err = dirCacher.Get(nil, "a/b/c")
	assert.NoError(t, err)
	defer rc.Close()

	b, err := ioutil.ReadAll(rc)
	assert.NoError(t, err)
	assert.Equal(t, "foobar", string(b))
}
