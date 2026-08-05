package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
)

// Service 处理 Telegram webhook 更新并管理原生 webhook 生命周期。
type Service struct {
	config  contract.BotConfigReader
	tokens  contract.BotTokenResolver
	botapi  contract.BotAPIClient
	purchase *purchaseService
}

// NewService 构造 webhook 应用服务。
func NewService(config contract.BotConfigReader, tokens contract.BotTokenResolver, botapi contract.BotAPIClient) *Service {
	if config == nil {
		panic("telegram webhook service: config is nil")
	}
	if tokens == nil {
		panic("telegram webhook service: token resolver is nil")
	}
	if botapi == nil {
		panic("telegram webhook service: bot api client is nil")
	}
	return &Service{config: config, tokens: tokens, botapi: botapi}
}

// WithPurchase 注入 bot 内购买端口（可选）。不注入时 bot 内购买功能禁用。
func (s *Service) WithPurchase(ports contract.PurchasePorts) *Service {
	s.purchase = newPurchaseService(ports, s.botapi, func() string {
		cfg, err := s.config.GetTelegramBotConfig()
		if err != nil {
			return "zh-CN"
		}
		return resolveLocale(cfg.DefaultLocale)
	})
	return s
}

// HandleUpdate 处理单个 Telegram Update。
func (s *Service) HandleUpdate(ctx context.Context, update webhookdomain.Update) error {
	cfg, err := s.config.GetTelegramBotConfig()
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}

	token, err := s.tokens.ResolveActiveBotToken()
	if err != nil || token == "" {
		return err
	}

	// bot 内购买优先消费（/shop 命令与 shop:* 回调）
	if s.purchase != nil {
		handled, herr := s.purchase.handle(ctx, token, update)
		if herr != nil {
			return herr
		}
		if handled {
			return nil
		}
	}

	locale := resolveLocale(cfg.DefaultLocale)

	if update.CallbackQuery != nil {
		return s.handleCallbackQuery(ctx, token, cfg, locale, update.CallbackQuery)
	}
	if update.Message != nil {
		return s.handleMessage(ctx, token, cfg, locale, update.Message)
	}
	return nil
}

// ApplyWebhook 根据配置启用状态设置或删除 Telegram webhook。
func (s *Service) ApplyWebhook(ctx context.Context, webhookURL, secretToken string) error {
	cfg, err := s.config.GetTelegramBotConfig()
	if err != nil {
		return err
	}
	token, err := s.tokens.ResolveActiveBotToken()
	if err != nil {
		return err
	}
	if token == "" {
		return contract.ErrTokenUnavailable
	}

	// 未启用或未配置 webhook_url：删除 webhook、清空命令，标记 disabled。
	if !cfg.Enabled || strings.TrimSpace(webhookURL) == "" {
		_ = s.botapi.DeleteWebhook(ctx, token)
		_ = s.botapi.SetMyCommands(ctx, token, nil)
		return s.updateWebhookStatus(cfg, false, "", "disabled", nil)
	}

	// 启用且配置了 webhook_url：真正向 Telegram 注册 webhook 并注册命令。
	if err := s.botapi.SetWebhook(ctx, token, strings.TrimSpace(webhookURL), strings.TrimSpace(secretToken)); err != nil {
		return s.updateWebhookStatus(cfg, true, "", "set_webhook_failed", []string{err.Error()})
	}
	if err := s.botapi.SetMyCommands(ctx, token, builtinCommands()); err != nil {
		return s.updateWebhookStatus(cfg, true, "", "set_commands_failed", []string{err.Error()})
	}
	return s.updateWebhookStatus(cfg, true, "", "active", nil)
}

// SyncRuntimeStatus 校验 token 并更新运行时状态。
func (s *Service) SyncRuntimeStatus(ctx context.Context) error {
	cfg, err := s.config.GetTelegramBotConfig()
	if err != nil {
		return err
	}
	token, err := s.tokens.ResolveActiveBotToken()
	if err != nil {
		_ = s.updateWebhookStatus(cfg, false, "", "token_unavailable", []string{err.Error()})
		return err
	}
	if token == "" {
		return s.updateWebhookStatus(cfg, false, "", "no_token", nil)
	}

	info, err := s.botapi.GetMe(ctx, token)
	if err != nil {
		return s.updateWebhookStatus(cfg, false, "", "get_me_failed", []string{err.Error()})
	}
	botVersion := "native-webhook"
	if info != nil && info.UserName != "" {
		botVersion = "native-webhook@" + info.UserName
	}
	return s.updateWebhookStatus(cfg, true, botVersion, "active", nil)
}

func (s *Service) updateWebhookStatus(cfg settingsmessaging.TelegramBotConfigSetting, connected bool, botVersion, webhookStatus string, warnings []string) error {
	current, _ := s.config.GetTelegramBotRuntimeStatus()
	status := settingsmessaging.TelegramBotRuntimeStatusSetting{
		Connected:        connected,
		LastSeenAt:       current.LastSeenAt,
		BotVersion:       botVersion,
		WebhookStatus:    webhookStatus,
		MachineCode:      "native",
		LicenseStatus:    "native",
		LicenseExpiresAt: "",
		Warnings:         append([]string(nil), warnings...),
		ConfigVersion:    cfg.ConfigVersion,
		LastConfigSyncAt: current.LastConfigSyncAt,
	}
	return s.config.UpdateTelegramBotRuntimeStatus(status)
}

func (s *Service) handleMessage(ctx context.Context, token string, cfg settingsmessaging.TelegramBotConfigSetting, locale string, msg *webhookdomain.Message) error {
	if msg == nil {
		return nil
	}
	// 仅私聊与群组处理；非文本直接忽略
	if !msg.IsPrivateChat() && !msg.IsGroupChat() {
		return nil
	}
	text := strings.TrimSpace(msg.Text)
	chatID := fmt.Sprintf("%d", msg.Chat.ID)

	// /start 命令或首次进入：发送欢迎语
	if text == "/start" || strings.HasPrefix(text, "/start ") {
		if cfg.Welcome.Enabled {
			if welcome := localizedText(cfg.Welcome.Message, locale); welcome != "" {
				return s.botapi.SendMessage(ctx, token, chatID, welcome, contract.SendMessageOptions{
					DisableWebPagePreview: true,
					ReplyMarkup:           s.startKeyboard(locale),
				})
			}
		}
		return s.botapi.SendMessage(ctx, token, chatID, mainMenuHint(cfg, locale), contract.SendMessageOptions{
			DisableWebPagePreview: true,
			ReplyMarkup:           s.startKeyboard(locale),
		})
	}

	// /help 命令：发送帮助中心
	if text == "/help" || strings.HasPrefix(text, "/help ") {
		return s.sendHelpCenter(ctx, token, chatID, cfg, locale)
	}

	// /menu 命令：发送内联菜单
	if text == "/menu" || strings.HasPrefix(text, "/menu ") {
		return s.sendInlineMenu(ctx, token, chatID, cfg, locale)
	}

	// 其他文本：仅在私聊回复主菜单提示，群组不打扰
	if msg.IsPrivateChat() {
		return s.botapi.SendMessage(ctx, token, chatID, mainMenuHint(cfg, locale), contract.SendMessageOptions{DisableWebPagePreview: true})
	}
	return nil
}

func (s *Service) handleCallbackQuery(ctx context.Context, token string, cfg settingsmessaging.TelegramBotConfigSetting, locale string, cb *webhookdomain.CallbackQuery) error {
	if cb == nil {
		return nil
	}
	chatID := fmt.Sprintf("%d", cb.Message.Chat.ID)
	data := strings.TrimSpace(cb.Data)

	// 先应答回调，避免 Telegram 转圈
	alertText := ""
	switch data {
	case "help":
		alertText = ""
	case "menu":
		alertText = ""
	default:
		if isBuiltinMenuKey(data) {
			alertText = localizedText(builtinMenuHint(data), locale)
		}
	}
	_ = s.botapi.AnswerCallbackQuery(ctx, token, cb.ID, contract.AnswerCallbackOptions{Text: alertText})

	switch data {
	case "help":
		return s.sendHelpCenter(ctx, token, chatID, cfg, locale)
	case "menu":
		return s.sendInlineMenu(ctx, token, chatID, cfg, locale)
	default:
		if action := resolveMenuAction(cfg, data); action != "" {
			// 内置菜单项：已接入 bot 内能力（开始购物 / 我的钱包）的直接调用。
			if action == "builtin" && s.purchase != nil {
				if chatID64, err := strconv.ParseInt(chatID, 10, 64); err == nil {
					switch data {
					case "shop_home":
						return s.purchase.StartFromMenu(ctx, token, chatID64, cb.From)
					case "my_wallet":
						return s.purchase.ShowWallet(ctx, token, chatID64, cb.From)
					}
				}
			}
			// 其余内置动作：回复提示文本（去网页端查看等）。
			hint := mainMenuHint(cfg, locale)
			return s.botapi.SendMessage(ctx, token, chatID, hint, contract.SendMessageOptions{DisableWebPagePreview: true})
		}
	}
	return nil
}

// startKeyboard 欢迎语附带的快捷操作键盘（开始购物 / 卡头库存 / 我的钱包 / 我的订单 / 语言切换 / 帮助）。
func (s *Service) startKeyboard(locale string) inlineKeyboard {
	langLabel := "🌐 English"
	if strings.HasPrefix(resolveLocale(locale), "en") {
		langLabel = "🌐 中文"
	}
	rows := [][]inlineButton{
		{
			{Text: "🛍️ 开始购物", CallbackData: "shop:start"},
			{Text: "📦 卡头库存", CallbackData: "shop:binstock"},
		},
		{
			{Text: "💰 我的钱包", CallbackData: "shop:wallet"},
			{Text: "📋 我的订单", CallbackData: "shop:orders"},
		},
		{
			{Text: langLabel, CallbackData: "shop:lang"},
			{Text: "❓ 帮助中心", CallbackData: "help"},
		},
	}
	return inlineKeyboard{InlineKeyboard: rows}
}

func (s *Service) sendHelpCenter(ctx context.Context, token, chatID string, cfg settingsmessaging.TelegramBotConfigSetting, locale string) error {
	if !cfg.Help.Enabled {
		return s.botapi.SendMessage(ctx, token, chatID, mainMenuHint(cfg, locale), contract.SendMessageOptions{DisableWebPagePreview: true})
	}
	var sb strings.Builder
	if title := localizedText(cfg.Help.Title, locale); title != "" {
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}
	if intro := localizedText(cfg.Help.Intro, locale); intro != "" {
		sb.WriteString(intro)
		sb.WriteString("\n\n")
	}
	for _, item := range cfg.Help.Items {
		if !item.Enabled {
			continue
		}
		summary := localizedText(item.Summary, locale)
		if summary == "" {
			continue
		}
		sb.WriteString("• ")
		sb.WriteString(summary)
		sb.WriteString("\n")
	}
	if hint := localizedText(cfg.Help.CenterHint, locale); hint != "" {
		sb.WriteString("\n")
		sb.WriteString(hint)
	}
	return s.botapi.SendMessage(ctx, token, chatID, sb.String(), contract.SendMessageOptions{DisableWebPagePreview: true})
}

func (s *Service) sendInlineMenu(ctx context.Context, token, chatID string, cfg settingsmessaging.TelegramBotConfigSetting, locale string) error {
	markup := buildInlineKeyboard(cfg, locale)
	hint := mainMenuHint(cfg, locale)
	return s.botapi.SendMessage(ctx, token, chatID, hint, contract.SendMessageOptions{
		DisableWebPagePreview: true,
		ReplyMarkup:           markup,
	})
}