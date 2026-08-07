package application

import (
	"strings"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
)

// ErrTokenUnavailable 表示没有可用的 Bot Token。
var ErrTokenUnavailable = contract.ErrTokenUnavailable

func resolveLocale(defaultLocale string) string {
	locale := strings.TrimSpace(defaultLocale)
	if locale == "" {
		return "zh-CN"
	}
	return locale
}

// localizedText 按语言取多语言文本，缺失时回退到 default_locale，再回退到第一个非空值。
func localizedText(lt settingsmessaging.LocalizedText, locale string) string {
	if len(lt) == 0 {
		return ""
	}
	if v, ok := lt[locale]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	for _, lang := range []string{"zh-CN", "zh-TW", "en-US"} {
		if v, ok := lt[lang]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	for _, v := range lt {
		if v := strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// builtinCommands 返回 Bot 菜单命令。
func builtinCommands() []contract.BotCommand {
	return []contract.BotCommand{
		{Command: "start", Description: "开始使用 / 欢迎语"},
		{Command: "shop", Description: "在线购买"},
		{Command: "help", Description: "帮助中心"},
		{Command: "menu", Description: "打开主菜单"},
	}
}

// isBuiltinMenuKey 判断是否为内置菜单 key。
func isBuiltinMenuKey(key string) bool {
	for _, k := range settingsmessaging.BuiltinTelegramBotMenuKeysOrder {
		if k == key {
			return true
		}
	}
	return false
}

// builtinMenuHint 返回内置菜单 key 的提示文本 key（此处返回空，由具体动作决定）。
func builtinMenuHint(key string) settingsmessaging.LocalizedText {
	switch key {
	case "shop_home":
		return settingsmessaging.LocalizedText{"zh-CN": "请访问商城主页选购", "zh-TW": "請造訪商城首頁選購", "en-US": "Visit the shop to browse"}
	case "my_orders":
		return settingsmessaging.LocalizedText{"zh-CN": "请在商城个人中心查看订单", "zh-TW": "請在商城個人中心查看訂單", "en-US": "View orders in your account"}
	case "my_wallet":
		return settingsmessaging.LocalizedText{"zh-CN": "请在商城个人中心查看钱包", "zh-TW": "請在商城個人中心查看錢包", "en-US": "View wallet in your account"}
	case "affiliate":
		return settingsmessaging.LocalizedText{"zh-CN": "请在商城个人中心查看推广返利", "zh-TW": "請在商城個人中心查看推廣返利", "en-US": "View affiliate in your account"}
	case "gift_card":
		return settingsmessaging.LocalizedText{"zh-CN": "请在商城兑换礼品卡", "zh-TW": "請在商城兌換禮品卡", "en-US": "Redeem gift cards in the shop"}
	case "switch_language":
		return settingsmessaging.LocalizedText{"zh-CN": "在商城可切换语言", "zh-TW": "在商城可切換語言", "en-US": "Switch language in the shop"}
	case "contact_support":
		return settingsmessaging.LocalizedText{"zh-CN": "请联系客服获取帮助", "zh-TW": "請聯繫客服獲取幫助", "en-US": "Contact support for help"}
	}
	return nil
}

// resolveMenuAction 解析菜单项动作，返回动作 URL/命令（空表示内置无跳转）。
func resolveMenuAction(cfg settingsmessaging.TelegramBotConfigSetting, key string) string {
	for _, item := range cfg.Menu.Items {
		if !item.Enabled || item.Key != key {
			continue
		}
		if item.Action.Type == "url" || item.Action.Type == "web_app" || item.Action.Type == "command" {
			return strings.TrimSpace(item.Action.Value)
		}
		return "builtin"
	}
	return ""
}

// mainMenuHint 返回主菜单提示文本。
func mainMenuHint(cfg settingsmessaging.TelegramBotConfigSetting, locale string) string {
	name := strings.TrimSpace(cfg.Basic.DisplayName)
	if name == "" {
		name = "Bot"
	}
	intro := localizedText(cfg.Basic.Description, locale)
	switch locale {
	case "zh-TW":
		if intro != "" {
			return name + "\n" + intro + "\n\n輸入 /help 查看說明，/menu 開啟主選單。"
		}
		return name + "\n\n輸入 /help 查看說明，/menu 開啟主選單。"
	case "en-US":
		if intro != "" {
			return name + "\n" + intro + "\n\nType /help for help, /menu for the main menu."
		}
		return name + "\n\nType /help for help, /menu for the main menu."
	default:
		if intro != "" {
			return name + "\n" + intro + "\n\n输入 /help 查看帮助，/menu 打开主菜单。"
		}
		return name + "\n\n输入 /help 查看帮助，/menu 打开主菜单。"
	}
}

// inlineButton 是内联键盘按钮。
type inlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
	WebApp *webAppInfo `json:"web_app,omitempty"`
}

type webAppInfo struct {
	URL string `json:"url"`
}

// inlineKeyboard 是内联键盘。
type inlineKeyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

// replyKeyboardButton 是 reply 键盘按钮（从输入框弹出，点一下即发送文字）。
type replyKeyboardButton struct {
	Text string `json:"text"`
}

// replyKeyboard 是 Telegram ReplyKeyboardMarkup：从输入框弹出自定义键盘。
type replyKeyboard struct {
	Keyboard        [][]replyKeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool                   `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool                   `json:"one_time_keyboard,omitempty"`
}

// replyKeyboardRemove 移除 reply 键盘（回复到普通输入框）。
type replyKeyboardRemove struct {
	RemoveKeyboard bool `json:"remove_keyboard"`
}

// buildInlineKeyboard 根据配置生成内联键盘。
func buildInlineKeyboard(cfg settingsmessaging.TelegramBotConfigSetting, locale string) inlineKeyboard {
	rows := make([][]inlineButton, 0)
	row := make([]inlineButton, 0, 2)
	for _, item := range cfg.Menu.Items {
		if !item.Enabled {
			continue
		}
		label := localizedText(item.Label, locale)
		if label == "" {
			continue
		}
		btn := inlineButton{Text: label}
		switch item.Action.Type {
		case "url":
			if u := strings.TrimSpace(item.Action.Value); u != "" {
				btn.URL = u
			} else {
				btn.CallbackData = item.Key
			}
		case "web_app":
			if u := strings.TrimSpace(item.Action.Value); u != "" {
				btn.WebApp = &webAppInfo{URL: u}
			} else {
				btn.CallbackData = item.Key
			}
		case "command":
			cmd := strings.TrimSpace(item.Action.Value)
			if cmd != "" {
				btn.CallbackData = cmd
			} else {
				btn.CallbackData = item.Key
			}
		default:
			btn.CallbackData = item.Key
		}
		row = append(row, btn)
		if len(row) == 2 {
			rows = append(rows, row)
			row = make([]inlineButton, 0, 2)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	return inlineKeyboard{InlineKeyboard: rows}
}