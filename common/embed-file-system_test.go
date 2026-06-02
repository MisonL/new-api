package common

import (
	"errors"
	"net/http"
	"os"
	"testing"
	"testing/fstest"

	"github.com/gin-contrib/static"
)

func TestEmbedFileSystemHidesReleaseMetadata(t *testing.T) {
	fs := &embedFileSystem{
		FileSystem: http.FS(fstest.MapFS{
			"new-api-release.json": {Data: []byte(`{"app":"new-api"}`)},
			"assets/app.js":        {Data: []byte("console.log('ok')")},
		}),
		hiddenFiles: map[string]struct{}{
			"new-api-release.json": {},
		},
	}

	if _, err := fs.Open("/new-api-release.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open(new-api-release.json) error = %v, want os.ErrNotExist", err)
	}
	if fs.Exists("", "/new-api-release.json") {
		t.Fatal("expected hidden release metadata to be absent from Exists")
	}
	for _, name := range []string{"//new-api-release.json", "/./new-api-release.json", "/assets/../new-api-release.json"} {
		if _, err := fs.Open(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Open(%q) error = %v, want os.ErrNotExist", name, err)
		}
		if fs.Exists("", name) {
			t.Fatalf("expected hidden release metadata alias %q to be absent from Exists", name)
		}
	}
	if !fs.Exists("", "/assets/app.js") {
		t.Fatal("expected non-hidden static asset to exist")
	}

	var _ static.ServeFileSystem = fs
}
