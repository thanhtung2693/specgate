package repo

import (
	"testing"
	"time"
)

func TestCache_PutGet(t *testing.T) {
	t.Parallel()
	c := NewLRUCache(1024, 5*time.Minute)
	c.Put("key1", []byte("value1"))

	val, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(val) != "value1" {
		t.Errorf("got %q", string(val))
	}
}

func TestCache_Miss(t *testing.T) {
	t.Parallel()
	c := NewLRUCache(1024, 5*time.Minute)

	_, ok := c.Get("missing")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCache_Eviction(t *testing.T) {
	t.Parallel()
	c := NewLRUCache(100, 5*time.Minute)
	c.Put("a", make([]byte, 60))
	c.Put("b", make([]byte, 60))

	_, ok := c.Get("a")
	if ok {
		t.Fatal("expected 'a' to be evicted")
	}
	_, ok = c.Get("b")
	if !ok {
		t.Fatal("expected 'b' to be present")
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	t.Parallel()
	c := NewLRUCache(1024, 1*time.Millisecond)
	c.Put("key", []byte("val"))

	time.Sleep(5 * time.Millisecond)
	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestCache_Delete(t *testing.T) {
	t.Parallel()
	c := NewLRUCache(1024, 5*time.Minute)
	c.Put("key", []byte("val"))
	c.Delete("key")

	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected cache miss after delete")
	}
}

func TestMetaCache_PutGet(t *testing.T) {
	t.Parallel()
	c := NewMetaCache(100, 5*time.Minute)
	meta := &FileMeta{Path: "main.go", Size: 100, BlobSHA: "abc"}
	c.Put("key", meta)

	got, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.BlobSHA != "abc" {
		t.Errorf("BlobSHA = %q", got.BlobSHA)
	}
}

func TestMetaCache_TTLExpiry(t *testing.T) {
	t.Parallel()
	c := NewMetaCache(100, 1*time.Millisecond)
	c.Put("key", &FileMeta{Path: "x"})
	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected miss after TTL")
	}
}
