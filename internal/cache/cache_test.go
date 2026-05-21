package cache

import (
	"context"
	"testing"
	"time"

	"github.com/lyp256/airouter/internal/config"
)

func TestCacheOnce(t *testing.T) {
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

	ctx := context.Background()
	key := "user:1"

	var u *User
	err := c.Once(ctx, key, &u, time.Minute, func() (interface{}, error) {
		return &User{ID: "1", Name: "Test"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Test" {
		t.Errorf("expected Test, got %s", u.Name)
	}

	var u2 *User
	err = c.Get(ctx, key, &u2)
	if err != nil {
		t.Fatal(err)
	}
	if u2.Name != "Test" {
		t.Errorf("expected Test, got %s", u2.Name)
	}

	_ = c.Delete(ctx, key)
	err = c.Get(ctx, key, &u2)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}
