package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGetFilterOptionsUsesLoggedProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.UsageLog{}, &model.Upstream{}, &model.Provider{}); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	if err := db.Create(&model.UsageLog{
		ID:           "log-1",
		Model:        "gpt-test",
		Protocol:     "claude",
		ProviderType: "openai_response",
		Status:       "success",
		CreatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("insert usage log: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/stats/filter-options", nil)

	NewStatsHandler(db).GetFilterOptions(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Data FilterOptions `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.ProviderTypes) != 1 || body.Data.ProviderTypes[0] != "claude" {
		t.Fatalf("provider_types = %#v, want [claude]", body.Data.ProviderTypes)
	}
}
