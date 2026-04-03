package cache

import (
	"context"
	"testing"
	"time"

	"github.com/lyp256/airouter/internal/config"
)

func TestNamespace(t *testing.T) {
	cfg := &config.CacheConfig{
		Enabled: true,
		Type:    "memory",
		Size:    1,
		TTL:     time.Minute,
	}
	c, _ := New(cfg)

	type User struct {
		ID   string
		Name string
	}

	ns := NewNamespace[*User](c, "user", time.Minute)
	ctx := context.Background()

	// Test Once
	u, err := ns.Once(ctx, "1", 0, func() (*User, error) {
		return &User{ID: "1", Name: "Test"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Test" {
		t.Errorf("expected Test, got %s", u.Name)
	}

	// Test Get
	u2, err := ns.Get(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if u2.Name != "Test" {
		t.Errorf("expected Test, got %s", u2.Name)
	}

	// Test Delete
	_ = ns.Delete(ctx, "1")
	_, err = ns.Get(ctx, "1")
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}
