// Package plugin_simplecache is a plugin to cache responses to disk.
package plugin_simplecache

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var errCacheMiss = errors.New("cache miss")

type fileCache struct {
	path string
	pm   *pathMutex
}

func newFileCache(path string, vacuum time.Duration) (*fileCache, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid cache path: %w", err)
	}

	if !info.IsDir() {
		return nil, errors.New("path must be a directory")
	}

	fc := &fileCache{
		path: path,
		pm:   &pathMutex{lock: map[string]*fileLock{}},
	}

	go fc.vacuum(vacuum)

	return fc, nil
}

func (c *fileCache) vacuum(interval time.Duration) {
	timer := time.NewTicker(interval)
	defer timer.Stop()

	for range timer.C {
		_ = filepath.Walk(c.path, func(path string, info os.FileInfo, err error) error {
			switch {
			case err != nil:
				return err
			case info.IsDir():
				return nil
			}

			mu := c.pm.MutexAt(filepath.Base(path))
			mu.Lock()
			defer mu.Unlock()

			// Get the expiry.
			var t [8]byte
			f, err := os.Open(filepath.Clean(path))
			if err != nil {
				// Just skip the file in this case.
				return nil // nolint:nilerr // skip
			}
			if n, err := f.Read(t[:]); err != nil && n != 8 {
				return nil
			}
			_ = f.Close()

			expires := time.Unix(int64(binary.LittleEndian.Uint64(t[:])), 0)
			if !expires.Before(time.Now()) {
				return nil
			}

			// Delete the file.
			_ = os.Remove(path)
			return nil
		})
	}
}

func (c *fileCache) Get(key string) ([]byte, error) {
	mu := c.pm.MutexAt(key)
	mu.RLock()
	defer mu.RUnlock()

	p := keyPath(c.path, key)
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return nil, errCacheMiss
	}

	b, err := ioutil.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, fmt.Errorf("error reading file %q: %w", p, err)
	}

	expires := time.Unix(int64(binary.LittleEndian.Uint64(b[:8])), 0)
	if expires.Before(time.Now()) {
		_ = os.Remove(p)
		return nil, errCacheMiss
	}

	return b[8:], nil
}

func (c *fileCache) Set(key string, val []byte, expiry time.Duration) error {
	mu := c.pm.MutexAt(key)
	mu.Lock()
	defer mu.Unlock()

	p := keyPath(c.path, key)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("error creating file path: %w", err)
	}

	f, err := os.OpenFile(filepath.Clean(p), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	defer func() {
		_ = f.Close()
	}()

	timestamp := uint64(time.Now().Add(expiry).Unix())

	var t [8]byte

	binary.LittleEndian.PutUint64(t[:], timestamp)

	if _, err = f.Write(t[:]); err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	if _, err = f.Write(val); err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	return nil
}

func keyHash(key string) [4]byte {
	h := crc32.Checksum([]byte(key), crc32.IEEETable)

	var b [4]byte

	binary.LittleEndian.PutUint32(b[:], h)

	return b
}

func keyPath(path, key string) string {
	h := keyHash(key)
	key = strings.NewReplacer("/", "-", ":", "_").Replace(key)

	return filepath.Join(
		path,
		hex.EncodeToString(h[0:1]),
		hex.EncodeToString(h[1:2]),
		hex.EncodeToString(h[2:3]),
		hex.EncodeToString(h[3:4]),
		key,
	)
}

type pathMutex struct {
	mu   sync.Mutex
	lock map[string]*fileLock
}

func (m *pathMutex) MutexAt(path string) *fileLock {
	m.mu.Lock()
	defer m.mu.Unlock()

	if fl, ok := m.lock[path]; ok {
		fl.ref++
		return fl
	}

	fl := &fileLock{ref: 1}
	fl.cleanup = func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		fl.ref--
		if fl.ref == 0 {
			delete(m.lock, path)
		}
	}
	m.lock[path] = fl

	return fl
}

type fileLock struct {
	ref     int
	cleanup func()

	mu sync.RWMutex
}

func (l *fileLock) RLock() {
	l.mu.RLock()
}

func (l *fileLock) RUnlock() {
	l.mu.RUnlock()
	l.cleanup()
}

func (l *fileLock) Lock() {
	l.mu.Lock()
}

func (l *fileLock) Unlock() {
	l.mu.Unlock()
	l.cleanup()
}
