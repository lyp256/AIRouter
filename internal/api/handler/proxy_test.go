package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/internal/service"
	"github.com/lyp256/airouter/pkg/openai"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIsSSEMetadataOnly(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "event only",
			in:   "event: response.created",
			want: true,
		},
		{
			name: "event with data frame",
			in:   "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}",
			want: false,
		},
		{
			name: "data only",
			in:   "data: {\"type\":\"content_block_delta\"}",
			want: false,
		},
		{
			name: "comment only",
			in:   ": ping",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSSEMetadataOnly([]byte(tt.in)); got != tt.want {
				t.Fatalf("isSSEMetadataOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogUsageRecordsProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.UsageLog{}); err != nil {
		t.Fatalf("migrate usage_logs: %v", err)
	}

	handler := &ProxyHandler{db: db, logger: zap.NewNop()}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("request_id", "req-1")

	modelCfg := &model.Model{
		Name:        "gpt-test",
		InputPrice:  1000,
		OutputPrice: 2000,
	}
	selection := &service.UpstreamSelection{
		Upstream:    &model.Upstream{ID: "up-1"},
		Provider:    &model.Provider{Type: "openai_response"},
		ProviderKey: &model.ProviderKey{ID: "pk-1"},
	}
	usage := &openai.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}

	handler.logUsage(ctx, selection, modelCfg, "claude", usage, 10, 4, 12, "success", 200, "")

	var logged model.UsageLog
	if err := db.First(&logged).Error; err != nil {
		t.Fatalf("read usage log: %v", err)
	}
	if logged.Protocol != "claude" {
		t.Fatalf("protocol = %q, want claude", logged.Protocol)
	}
	if logged.ProviderType != "openai_response" {
		t.Fatalf("provider_type = %q, want openai_response", logged.ProviderType)
	}
}
