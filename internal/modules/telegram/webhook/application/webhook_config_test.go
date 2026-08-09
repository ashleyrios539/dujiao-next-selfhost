package application

import (
	"context"
	"errors"
	"testing"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
)

// failingWebhookBotAPI 在 fakeBotAPI 基础上记录 SetWebhook 调用并可注入失败。
type failingWebhookBotAPI struct {
	fakeBotAPI
	setWebhookCalls int
	failSetWebhook  bool
}

func (f *failingWebhookBotAPI) SetWebhook(_ context.Context, _, _, _ string) error {
	f.setWebhookCalls++
	if f.failSetWebhook {
		return errors.New("setWebhook failed")
	}
	return nil
}

func newWebhookServiceForTest(cfg settingsmessaging.TelegramBotConfigSetting, token string) (*Service, *failingWebhookBotAPI) {
	api := &failingWebhookBotAPI{}
	svc := NewService(&stubBotConfig{cfg: cfg}, &stubTokenResolver{token: token}, api)
	return svc, api
}

// DB 未配置 webhook 时，ApplyConfiguredWebhook 用 config 兜底值，且 CurrentSecret 同步为兜底 secret。
func TestApplyConfiguredWebhook_UsesConfigFallbackWhenDBEmpty(t *testing.T) {
	cfg := settingsmessaging.DefaultTelegramBotConfig()
	cfg.Enabled = true // webhook 只有启用时才会 SetWebhook
	svc, api := newWebhookServiceForTest(cfg, "token")
	svc.WithConfigFallback("https://cfg.example/webhook", "cfg-secret")

	if err := svc.ApplyConfiguredWebhook(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := svc.CurrentSecret(); got != "cfg-secret" {
		t.Fatalf("CurrentSecret = %q, want cfg-secret", got)
	}
	if api.setWebhookCalls != 1 {
		t.Fatalf("SetWebhook called %d times, want 1", api.setWebhookCalls)
	}
}

// DB 已配置 webhook 时优先用 DB 值（包括 secret），config 兜底被覆盖。
func TestApplyConfiguredWebhook_DBPriority(t *testing.T) {
	cfg := settingsmessaging.DefaultTelegramBotConfig()
	cfg.Enabled = true
	cfg.Webhook.URL = "https://db.example/webhook"
	cfg.Webhook.SecretToken = "db-secret"
	svc, _ := newWebhookServiceForTest(cfg, "token")
	svc.WithConfigFallback("https://cfg.example/webhook", "cfg-secret")

	if err := svc.ApplyConfiguredWebhook(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := svc.CurrentSecret(); got != "db-secret" {
		t.Fatalf("CurrentSecret = %q, want db-secret", got)
	}
}

// WithConfigFallback 初始化 runtimeSecret 时 DB 优先：DB 有 secret 就不该用 config 值。
func TestWithConfigFallback_SeedsRuntimeSecretFromDB(t *testing.T) {
	cfg := settingsmessaging.DefaultTelegramBotConfig()
	cfg.Webhook.SecretToken = "db-secret"
	svc, _ := newWebhookServiceForTest(cfg, "token")

	svc.WithConfigFallback("https://cfg.example/webhook", "cfg-secret")

	if got := svc.CurrentSecret(); got != "db-secret" {
		t.Fatalf("CurrentSecret = %q, want db-secret (DB 优先初始化)", got)
	}
}

// SetWebhook 失败时 runtimeSecret 保持旧值，避免入站校验与 Telegram 已注册的 webhook 分叉。
func TestApplyConfiguredWebhook_FailureKeepsRuntimeSecret(t *testing.T) {
	cfg := settingsmessaging.DefaultTelegramBotConfig()
	cfg.Enabled = true
	svc, api := newWebhookServiceForTest(cfg, "token")
	svc.WithConfigFallback("https://cfg.example/webhook", "cfg-secret")
	api.failSetWebhook = true

	if err := svc.ApplyConfiguredWebhook(context.Background()); err == nil {
		t.Fatalf("expected SetWebhook error")
	}
	if got := svc.CurrentSecret(); got != "cfg-secret" {
		t.Fatalf("CurrentSecret = %q, want cfg-secret (失败不应切换)", got)
	}
}

// bot 未启用时：删除 webhook、不调 SetWebhook；CurrentSecret 仍按兜底保留（启动空窗内不误放行）。
func TestApplyConfiguredWebhook_DisabledBotDoesNotSetWebhook(t *testing.T) {
	cfg := settingsmessaging.DefaultTelegramBotConfig() // Enabled=false
	svc, api := newWebhookServiceForTest(cfg, "token")
	svc.WithConfigFallback("https://cfg.example/webhook", "cfg-secret")

	if err := svc.ApplyConfiguredWebhook(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if api.setWebhookCalls != 0 {
		t.Fatalf("SetWebhook should not be called when disabled, got %d calls", api.setWebhookCalls)
	}
	if got := svc.CurrentSecret(); got != "cfg-secret" {
		t.Fatalf("CurrentSecret = %q, want cfg-secret", got)
	}
}
