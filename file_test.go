package plugin_simplecache

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testCacheKey = "GETlocalhost:8080/test/path"

func TestFileCache(t *testing.T) {
	dir := createTempDir(t)

	fc, err := newFileCache(dir, time.Second)
	if err != nil {
		t.Errorf("unexpected newFileCache error: %v", err)
	}

	_, err = fc.Get(testCacheKey)
	if err == nil {
		t.Error("unexpected cache content")
	}

	cacheContent := []byte("some random cache content that should be exact")

	err = fc.Set(testCacheKey, cacheContent, time.Second)
	if err != nil {
		t.Errorf("unexpected cache set error: %v", err)
	}

	got, err := fc.Get(testCacheKey)
	if err != nil {
		t.Errorf("unexpected cache get error: %v", err)
	}

	if !bytes.Equal(got, cacheContent) {
		t.Errorf("unexpected cache content: want %s, got %s", cacheContent, got)
	}
}

func TestFileCache_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatal(r)
		}
	}()

	dir := createTempDir(t)

	fc, err := newFileCache(dir, time.Second)
	if err != nil {
		t.Errorf("unexpected newFileCache error: %v", err)
	}

	cacheContent := []byte("some random cache content that should be exact")

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for {
			got, _ := fc.Get(testCacheKey)
			if got != nil && !bytes.Equal(got, cacheContent) {
				panic(fmt.Errorf("unexpected cache content: want %s, got %s", cacheContent, got))
			}

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	go func() {
		defer wg.Done()

		for {
			err = fc.Set(testCacheKey, cacheContent, time.Second)
			if err != nil {
				panic(fmt.Errorf("unexpected cache set error: %w", err))
			}

			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	wg.Wait()
}

func TestPathMutex(t *testing.T) {
	pm := &pathMutex{lock: map[string]*fileLock{}}

	mu := pm.MutexAt("sometestpath")
	mu.Lock()

	var (
		wg     sync.WaitGroup
		locked uint32
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		mu := pm.MutexAt("sometestpath")
		mu.Lock()
		defer mu.Unlock()

		atomic.AddUint32(&locked, 1)
	}()

	// locked should be 0 as we already have a lock on the path.
	if atomic.LoadUint32(&locked) != 0 {
		t.Error("unexpected second lock")
	}

	mu.Unlock()

	wg.Wait()

	if l := len(pm.lock); l > 0 {
		t.Errorf("unexpected lock length: want 0, got %d", l)
	}
}

func BenchmarkFileCache_Get(b *testing.B) {
	dir := createTempDir(b)

	fc, err := newFileCache(dir, time.Minute)
	if err != nil {
		b.Errorf("unexpected newFileCache error: %v", err)
	}

	_ = fc.Set(testCacheKey, []byte("some random cache content that should be exact"), time.Minute)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = fc.Get(testCacheKey)
	}
}
