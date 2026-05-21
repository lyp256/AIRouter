package service

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/lyp256/airouter/internal/cache"
	"github.com/lyp256/airouter/internal/config"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
)

func TestSelectUpstreamKeepsProviderKeyWithCache(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Model{}, &model.Provider{}, &model.ProviderKey{}, &model.Upstream{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now()
	provider := model.Provider{ID: "provider-1", Name: "openai", Type: "openai_compatible", BaseURL: "https://example.test", Enabled: true, CreatedAt: now, UpdatedAt: now}
	providerKey := model.ProviderKey{ID: "provider-key-1", ProviderID: provider.ID, Name: "primary", Key: "sk-live-test", Status: "active", CreatedAt: now, UpdatedAt: now}
	upstream := model.Upstream{ID: "upstream-1", ModelID: "model-1", ProviderID: provider.ID, ProviderKeyID: providerKey.ID, ProviderModel: "gpt-test", Weight: 1, Enabled: true, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := db.Create(&providerKey).Error; err != nil {
		t.Fatalf("create provider key: %v", err)
	}
	if err := db.Create(&upstream).Error; err != nil {
		t.Fatalf("create upstream: %v", err)
	}

	c, err := cache.New(&config.CacheConfig{Enabled: true, Type: "memory", Size: 1, TTL: time.Minute})
	if err != nil {
		t.Fatalf("create cache: %v", err)
	}
	selector := NewUpstreamSelector(db, c)

	first, err := selector.SelectUpstream("model-1")
	if err != nil {
		t.Fatalf("first select: %v", err)
	}
	second, err := selector.SelectUpstream("model-1")
	if err != nil {
		t.Fatalf("second select: %v", err)
	}
	if first.RawKey != providerKey.Key {
		t.Fatalf("first RawKey = %q, want %q", first.RawKey, providerKey.Key)
	}
	if second.RawKey != providerKey.Key {
		t.Fatalf("second RawKey = %q, want %q", second.RawKey, providerKey.Key)
	}
}
