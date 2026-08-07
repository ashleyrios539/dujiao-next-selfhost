package application

import (
	"context"
	"sort"
	"strings"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
)

// sendHelpCenter 渲染帮助中心列表：Title + Intro + 各条目 Summary（可点进详情的 inline 按钮，按 Order 排序）+ CenterHint + SupportHint。
// cfg.Help.Enabled=false 时回退为普通主菜单提示（与原行为一致）。
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

	// 按 Order 升序稳定排序后，把每条 Summary 做成可点进详情的按钮。
	ordered := make([]settingsmessaging.TelegramBotHelpItem, len(cfg.Help.Items))
	copy(ordered, cfg.Help.Items)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })

	rows := make([][]inlineButton, 0)
	row := make([]inlineButton, 0, 2)
	for _, item := range ordered {
		if !item.Enabled {
			continue
		}
		summary := localizedText(item.Summary, locale)
		if summary == "" || strings.TrimSpace(item.Key) == "" {
			continue
		}
		row = append(row, inlineButton{Text: summary, CallbackData: "help:detail:" + item.Key})
		if len(row) == 2 {
			rows = append(rows, row)
			row = make([]inlineButton, 0, 2)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	if hint := localizedText(cfg.Help.CenterHint, locale); hint != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(hint)
	}
	if hint := localizedText(cfg.Help.SupportHint, locale); hint != "" {
		sb.WriteString("\n")
		sb.WriteString(hint)
	}

	opts := contract.SendMessageOptions{DisableWebPagePreview: true}
	if len(rows) > 0 {
		opts.ReplyMarkup = inlineKeyboard{InlineKeyboard: rows}
	}
	return s.botapi.SendMessage(ctx, token, chatID, sb.String(), opts)
}

// sendHelpDetail 渲染单条帮助条目详情：Title + Content，可选「联系客服」URL 按钮（当 ShowSupportLink 且配置了 SupportURL），以及「返回帮助中心」按钮。
func (s *Service) sendHelpDetail(ctx context.Context, token, chatID string, cfg settingsmessaging.TelegramBotConfigSetting, locale, key string) error {
	key = strings.TrimSpace(key)
	if key == "" || !cfg.Help.Enabled {
		return s.sendHelpCenter(ctx, token, chatID, cfg, locale)
	}

	var item *settingsmessaging.TelegramBotHelpItem
	for i := range cfg.Help.Items {
		if cfg.Help.Items[i].Key == key {
			item = &cfg.Help.Items[i]
			break
		}
	}
	if item == nil || !item.Enabled {
		return s.botapi.SendMessage(ctx, token, chatID, localizedText(purchaseTexts["help.item_disabled"], locale), contract.SendMessageOptions{DisableWebPagePreview: true})
	}

	var sb strings.Builder
	if title := localizedText(item.Title, locale); title != "" {
		sb.WriteString(title)
		sb.WriteString("\n\n")
	}
	if content := localizedText(item.Content, locale); content != "" {
		sb.WriteString(content)
	}

	// ShowSupportLink 但未配置 SupportURL：补一句 SupportHint 作为兜底。
	supportURL := strings.TrimSpace(cfg.Basic.SupportURL)
	if item.ShowSupportLink && supportURL == "" {
		if hint := localizedText(cfg.Help.SupportHint, locale); hint != "" {
			sb.WriteString("\n\n")
			sb.WriteString(hint)
		}
	}

	rows := make([][]inlineButton, 0, 2)
	var actionRow []inlineButton
	if item.ShowSupportLink && supportURL != "" {
		actionRow = append(actionRow, inlineButton{Text: localizedText(purchaseTexts["help.contact_support"], locale), URL: supportURL})
	}
	actionRow = append(actionRow, inlineButton{Text: localizedText(purchaseTexts["help.back"], locale), CallbackData: "help"})
	rows = append(rows, actionRow)

	opts := contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: inlineKeyboard{InlineKeyboard: rows}}
	return s.botapi.SendMessage(ctx, token, chatID, sb.String(), opts)
}
