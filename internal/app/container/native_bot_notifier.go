package container

import (
	"context"
	"strings"
	"time"

	fulfillmentapp "github.com/dujiao-next/internal/modules/fulfillment/application"
	fulfillmentcontract "github.com/dujiao-next/internal/modules/fulfillment/contract"
	telegrambroadcast "github.com/dujiao-next/internal/bootstrap/telegrambroadcast"
	notifybotapi "github.com/dujiao-next/internal/modules/telegram/notify/infrastructure/botapi"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
)

// nativeBotNotifier 是 fulfillment 模块 BotNotifier 的 native 直发实现：
// 发货完成后直接用 Telegram Bot API 把卡密推送给用户私聊，替代外部 channel client 回调链路
// （原生 webhook 模式下外部回调没有消费端点，原队列通知会丢失）。
type nativeBotNotifier struct {
	token contract.BotTokenResolver
	api   *notifybotapi.Client
	fulf  fulfillmentcontract.Store
}

var _ fulfillmentapp.BotNotifier = (*nativeBotNotifier)(nil)

// EnqueueOrderFulfilled 查询交付卡密并用 bot 直发给用户的 Telegram 私聊。
func (n *nativeBotNotifier) EnqueueOrderFulfilled(telegramUserID string, orderID uint) error {
	if n == nil || n.token == nil || n.api == nil || n.fulf == nil {
		return nil
	}
	token, err := n.token.ResolveActiveBotToken()
	if err != nil || strings.TrimSpace(token) == "" {
		return nil
	}
	f, err := n.fulf.GetByOrderID(orderID)
	if err != nil || f == nil || strings.TrimSpace(f.Payload) == "" {
		return nil
	}
	chatID := strings.TrimSpace(telegramUserID)
	if chatID == "" {
		return nil
	}
	msg := "✅ 订单已发货，卡密如下：\n\n" + truncateFulfillmentPayload(f.Payload) +
		"\n\n📋 也可在 Bot 菜单「我的订单」中随时查看。"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return n.api.SendMessage(ctx, token, chatID, msg, notifybotapi.SendMessageOptions{})
}

// truncateFulfillmentPayload 截断卡密 payload 到 Telegram 单条消息安全长度。
func truncateFulfillmentPayload(payload string) string {
	const maxLen = 3500
	if len(payload) <= maxLen {
		return payload
	}
	return payload[:maxLen] + "\n…（已截断）"
}

// newNativeBotNotifier 构造 native 直发通知器（需在 ChannelClientService 就绪后调用）。
func newNativeBotNotifier(c *Container) *nativeBotNotifier {
	return &nativeBotNotifier{
		token: telegrambroadcast.NewBotTokenResolver(c.ChannelClientService),
		api:   notifybotapi.New(),
		fulf:  c.FulfillmentStore,
	}
}
