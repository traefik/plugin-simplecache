package plugin_simplecache

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "should error if path is not valid",
			cfg:     &Config{Path: "/foo/bar", MaxExpiry: 300, Cleanup: 600},
			wantErr: true,
		},
		{
			name:    "should error if maxExpiry <= 1",
			cfg:     &Config{Path: os.TempDir(), MaxExpiry: 1, Cleanup: 600},
			wantErr: true,
		},
		{
			name:    "should error if cleanup <= 1",
			cfg:     &Config{Path: os.TempDir(), MaxExpiry: 300, Cleanup: 1},
			wantErr: true,
		},
		{
			name:    "should be valid",
			cfg:     &Config{Path: os.TempDir(), MaxExpiry: 300, Cleanup: 600},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(context.Background(), nil, test.cfg, "simplecache")

			if test.wantErr && err == nil {
				t.Fatal("expected error on bad regexp format")
			}
		})
	}
}

func TestCache_ServeHTTP(t *testing.T) {
	dir := createTempDir(t)

	next := func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Cache-Control", "max-age=20")
		rw.WriteHeader(http.StatusOK)
	}

	cfg := &Config{Path: dir, MaxExpiry: 10, Cleanup: 20, AddStatusHeader: true, ConsiderUrlQuery: false}

	c, err := New(context.Background(), http.HandlerFunc(next), cfg, "simplecache")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://localhost/some/path", nil)
	rw := httptest.NewRecorder()

	c.ServeHTTP(rw, req)

	if state := rw.Header().Get("Cache-Status"); state != "miss" {
		t.Errorf("unexpected cache state: want \"miss\", got: %q", state)
	}

	rw = httptest.NewRecorder()

	c.ServeHTTP(rw, req)

	if state := rw.Header().Get("Cache-Status"); state != "hit" {
		t.Errorf("unexpected cache state: want \"hit\", got: %q", state)
	}

	rw = httptest.NewRecorder()
	// Check that the same request with a different URL query hits the same cache entry
	c.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "http://localhost/some/path?queryParam=1", nil))

	if state := rw.Header().Get("Cache-Status"); state != "hit" {
		t.Errorf("unexpected cache state: want \"hit\", got: %q", state)
	}

}

func TestCache_ServeHTTP_ConsiderUrlQuery(t *testing.T) {
	dir := createTempDir(t)

	next := func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Cache-Control", "max-age=20")
		rw.WriteHeader(http.StatusOK)
	}

	cfg := &Config{Path: dir, MaxExpiry: 10, Cleanup: 20, AddStatusHeader: true, ConsiderUrlQuery: true}

	c, err := New(context.Background(), http.HandlerFunc(next), cfg, "simplecache")
	if err != nil {
		t.Fatal(err)
	}

	testUrl := "http://localhost/some/path"
	req := httptest.NewRequest(http.MethodGet, testUrl, nil)
	rw := httptest.NewRecorder()

	c.ServeHTTP(rw, req) // Add response to the cache
	rw = httptest.NewRecorder()
	c.ServeHTTP(rw, req)

	if state := rw.Header().Get("Cache-Status"); state != "hit" {
		t.Errorf("unexpected cache state: want \"hit\", got: %q", state)
	}

	rw = httptest.NewRecorder()
	c.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, testUrl+"?queryParam=1", nil))
	if state := rw.Header().Get("Cache-Status"); state != "miss" {
		t.Errorf("unexpected cache state: want \"miss\", got: %q", state)
	}

	rw = httptest.NewRecorder()
	c.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, testUrl+"?queryParam=2", nil))
	if state := rw.Header().Get("Cache-Status"); state != "miss" {
		t.Errorf("unexpected cache state: want \"miss\", got: %q", state)
	}
}

func createTempDir(tb testing.TB) string {
	tb.Helper()

	dir, err := ioutil.TempDir("./", "example")
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		if err = os.RemoveAll(dir); err != nil {
			tb.Fatal(err)
		}
	})

	return dir
}
