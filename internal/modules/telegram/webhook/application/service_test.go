package application

import (
	"context"
	"testing"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
)

// --- service_test 专用 stub ---

type stubBotConfig struct {
	cfg    settingsmessaging.TelegramBotConfigSetting
	status settingsmessaging.TelegramBotRuntimeStatusSetting
	err    error
}

func (s *stubBotConfig) GetTelegramBotConfig() (settingsmessaging.TelegramBotConfigSetting, error) {
	return s.cfg, s.err
}
func (s *stubBotConfig) GetTelegramBotRuntimeStatus() (settingsmessaging.TelegramBotRuntimeStatusSetting, error) {
	return s.status, nil
}
func (s *stubBotConfig) UpdateTelegramBotRuntimeStatus(settingsmessaging.TelegramBotRuntimeStatusSetting) error {
	return nil
}

type stubTokenResolver struct{ token string }

func (s *stubTokenResolver) ResolveActiveBotToken() (string, error) { return s.token, nil }

// markupButtons 展平 inline_keyboard 取所有按钮文案与 callback_data/URL。
func markupButtons(mk inlineKeyboard) []inlineButton {
	var out []inlineButton
	for _, row := range mk.InlineKeyboard {
		out = append(out, row...)
	}
	return out
}

func sampleHelpConfig() settingsmessaging.TelegramBotConfigSetting {
	return settingsmessaging.TelegramBotConfigSetting{
		Enabled:       true,
		DefaultLocale: "zh-CN",
		Basic: settingsmessaging.TelegramBotBasicConfig{
			DisplayName: "测试Bot",
			SupportURL:  "https://help.example.com/support",
		},
		Help: settingsmessaging.TelegramBotHelpConfig{
			Enabled: true,
			Title:   settingsmessaging.LocalizedText{"zh-CN": "❓ 帮助中心"},
			Intro:   settingsmessaging.LocalizedText{"zh-CN": "这里是简介"},
			CenterHint: settingsmessaging.LocalizedText{
				"zh-CN": "还没解决？联系客服。",
			},
			SupportHint: settingsmessaging.LocalizedText{
				"zh-CN": "客服暂未配置，可稍后再试。",
			},
			// 故意打乱存储顺序，Order 为 3,1,2，验证渲染按 Order 升序
			Items: []settingsmessaging.TelegramBotHelpItem{
				{
					Key: "orders", Enabled: true, Order: 3,
					Summary: settingsmessaging.LocalizedText{"zh-CN": "📦 订单问题"},
					Title:   settingsmessaging.LocalizedText{"zh-CN": "📦 订单问题详情"},
					Content: settingsmessaging.LocalizedText{"zh-CN": "订单正文内容"},
				},
				{
					Key: "shop", Enabled: true, Order: 1,
					Summary: settingsmessaging.LocalizedText{"zh-CN": "🛍️ 怎么下单"},
					Title:   settingsmessaging.LocalizedText{"zh-CN": "🛍️ 怎么下单详情"},
					Content: settingsmessaging.LocalizedText{"zh-CN": "下单正文内容"},
				},
				{
					Key: "disabled", Enabled: false, Order: 2,
					Summary: settingsmessaging.LocalizedText{"zh-CN": "❌ 不应显示"},
					Title:   settingsmessaging.LocalizedText{"zh-CN": "❌ 不应显示"},
					Content: settingsmessaging.LocalizedText{"zh-CN": "❌ 不应显示"},
				},
				{
					Key: "wallet", Enabled: true, Order: 2,
					Summary: settingsmessaging.LocalizedText{"zh-CN": "💰 钱包充值"},
					Title:   settingsmessaging.LocalizedText{"zh-CN": "💰 钱包充值详情"},
					Content: settingsmessaging.LocalizedText{"zh-CN": "钱包正文内容"},
				},
			},
		},
	}
}

func newServiceForHelp(bot *fakeBotAPI, cfg settingsmessaging.TelegramBotConfigSetting) *Service {
	return NewService(&stubBotConfig{cfg: cfg}, &stubTokenResolver{token: "tok"}, bot)
}

func TestServiceSendHelpCenterRendersConfigAndOrder(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, sampleHelpConfig())

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			Text: "/help",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a help message, got none")
	}
	msg := bot.sent[len(bot.sent)-1]
	for _, want := range []string{"❓ 帮助中心", "这里是简介", "还没解决？", "客服暂未配置"} {
		if !containsStr(msg, want) {
			t.Fatalf("expected message to contain %q, got: %q", want, msg)
		}
	}
	// CenterHint 与 SupportHint 都应出现
	if !containsStr(msg, "客服暂未配置，可稍后再试。") {
		t.Fatalf("expected SupportHint in message, got: %q", msg)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected an inline keyboard of summary buttons")
	}
	btns := markupButtons(bot.markups[len(bot.markups)-1])
	// 按 Order：shop(1), wallet(2), orders(3)；disabled 不出现
	if len(btns) != 3 {
		t.Fatalf("expected 3 buttons (disabled excluded), got %d: %+v", len(btns), btns)
	}
	if btns[0].Text != "🛍️ 怎么下单" || btns[0].CallbackData != "help:detail:shop" {
		t.Fatalf("expected first button to be shop(Order1), got: %+v", btns[0])
	}
	if btns[1].Text != "💰 钱包充值" || btns[1].CallbackData != "help:detail:wallet" {
		t.Fatalf("expected second button to be wallet(Order2), got: %+v", btns[1])
	}
	if btns[2].Text != "📦 订单问题" || btns[2].CallbackData != "help:detail:orders" {
		t.Fatalf("expected third button to be orders(Order3), got: %+v", btns[2])
	}
}

func TestServiceSendHelpCenterDisabledFallback(t *testing.T) {
	cfg := settingsmessaging.TelegramBotConfigSetting{
		Enabled:       true,
		DefaultLocale: "zh-CN",
		Basic:         settingsmessaging.TelegramBotBasicConfig{DisplayName: "测试Bot"},
		Help:          settingsmessaging.TelegramBotHelpConfig{Enabled: false},
	}
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, cfg)

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			Text: "/help",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a message")
	}
	msg := bot.sent[len(bot.sent)-1]
	// Help 关闭时回退主菜单提示，不应包含帮助中心标题
	if containsStr(msg, "帮助中心") {
		t.Fatalf("expected main menu hint fallback (no help center), got: %q", msg)
	}
	if !containsStr(msg, "测试Bot") {
		t.Fatalf("expected mainMenuHint to mention bot display name, got: %q", msg)
	}
}

func TestServiceSendHelpCenterSkipsDisabledItems(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, sampleHelpConfig())

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			Text: "/help",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected a keyboard")
	}
	btns := markupButtons(bot.markups[len(bot.markups)-1])
	for _, b := range btns {
		if containsStr(b.Text, "不应显示") {
			t.Fatalf("disabled item should not appear in keyboard, got: %+v", b)
		}
	}
}

func TestServiceHelpDetailCallback(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, sampleHelpConfig())

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID:   "cb1",
			From: webhookdomain.User{ID: 9},
			Message: webhookdomain.Message{
				Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			},
			Data: "help:detail:shop",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a detail message")
	}
	msg := bot.sent[len(bot.sent)-1]
	if !containsStr(msg, "🛍️ 怎么下单详情") {
		t.Fatalf("expected item Title in detail, got: %q", msg)
	}
	if !containsStr(msg, "下单正文内容") {
		t.Fatalf("expected item Content in detail, got: %q", msg)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected a markup with back button")
	}
	btns := markupButtons(bot.markups[len(bot.markups)-1])
	// shop 未置 ShowSupportLink，因此无「联系客服」URL 按钮，只有「返回帮助中心」
	if len(btns) != 1 {
		t.Fatalf("expected 1 button (back only), got %d: %+v", len(btns), btns)
	}
	if btns[0].CallbackData != "help" {
		t.Fatalf("expected back-to-help button, got: %+v", btns[0])
	}
}

func TestServiceHelpDetailShowSupportLinkWithURL(t *testing.T) {
	cfg := sampleHelpConfig()
	// 把 support 项加进来并开启 ShowSupportLink，SupportURL 已配置
	cfg.Help.Items = append(cfg.Help.Items, settingsmessaging.TelegramBotHelpItem{
		Key: "support", Enabled: true, Order: 4, ShowSupportLink: true,
		Summary: settingsmessaging.LocalizedText{"zh-CN": "💬 联系客服"},
		Title:   settingsmessaging.LocalizedText{"zh-CN": "💬 联系客服详情"},
		Content: settingsmessaging.LocalizedText{"zh-CN": "联系客服正文"},
	})
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, cfg)

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID:   "cb1",
			From: webhookdomain.User{ID: 9},
			Message: webhookdomain.Message{
				Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			},
			Data: "help:detail:support",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 || len(bot.markups) == 0 {
		t.Fatalf("expected message + markup")
	}
	btns := markupButtons(bot.markups[len(bot.markups)-1])
	var support, back *inlineButton
	for i := range btns {
		if btns[i].URL != "" {
			support = &btns[i]
		}
		if btns[i].CallbackData == "help" {
			back = &btns[i]
		}
	}
	if support == nil {
		t.Fatalf("expected a support URL button (SupportURL configured), got: %+v", btns)
	}
	if support.URL != "https://help.example.com/support" {
		t.Fatalf("expected URL button to point to SupportURL, got: %s", support.URL)
	}
	if !containsStr(support.Text, "联系客服") {
		t.Fatalf("expected support button label, got: %q", support.Text)
	}
	if back == nil {
		t.Fatalf("expected a back-to-help button alongside support URL, got: %+v", btns)
	}
}

func TestServiceHelpDetailShowSupportLinkNoURL(t *testing.T) {
	cfg := sampleHelpConfig()
	cfg.Basic.SupportURL = "" // 未配置客服链接
	cfg.Help.Items = append(cfg.Help.Items, settingsmessaging.TelegramBotHelpItem{
		Key: "support", Enabled: true, Order: 4, ShowSupportLink: true,
		Summary: settingsmessaging.LocalizedText{"zh-CN": "💬 联系客服"},
		Title:   settingsmessaging.LocalizedText{"zh-CN": "💬 联系客服详情"},
		Content: settingsmessaging.LocalizedText{"zh-CN": "联系客服正文"},
	})
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, cfg)

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID:   "cb1",
			From: webhookdomain.User{ID: 9},
			Message: webhookdomain.Message{
				Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			},
			Data: "help:detail:support",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a detail message")
	}
	msg := bot.sent[len(bot.sent)-1]
	// ShowSupportLink 但无 URL → 正文补 SupportHint 兜底
	if !containsStr(msg, "客服暂未配置，可稍后再试。") {
		t.Fatalf("expected SupportHint fallback when SupportURL empty, got: %q", msg)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected markup")
	}
	btns := markupButtons(bot.markups[len(bot.markups)-1])
	for _, b := range btns {
		if b.URL != "" {
			t.Fatalf("no URL button should appear when SupportURL empty, got: %+v", b)
		}
	}
}

func TestServiceHelpDetailUnknownKey(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newServiceForHelp(bot, sampleHelpConfig())

	if err := svc.HandleUpdate(context.Background(), webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID:   "cb1",
			From: webhookdomain.User{ID: 9},
			Message: webhookdomain.Message{
				Chat: webhookdomain.Chat{ID: 1, Type: "private"},
			},
			Data: "help:detail:nonexistent",
		},
	}); err != nil {
		t.Fatalf("HandleUpdate err: %v", err)
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a fallback message")
	}
	msg := bot.sent[len(bot.sent)-1]
	if !containsStr(msg, "该条目已停用") {
		t.Fatalf("expected disabled/unknown item hint, got: %q", msg)
	}
}
