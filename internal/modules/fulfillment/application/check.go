package application

import (
	"context"
	"time"

	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
	cardcheck "github.com/dujiao-next/internal/upstream/cardcheck"
	"github.com/dujiao-next/internal/logger"
)

// CardChecker 交付前测活端口，由 internal/upstream/cardcheck.Client 实现。
type CardChecker interface {
	CheckCards(ctx context.Context, kami, interfaceID string, cards []cardcheck.Card, opts cardcheck.Options) []cardcheck.Result
}

// runCardCheck 对订单内启用测活的商品执行交付前测活。
//
// 返回：
//   - byKey：每个商品 key 的活卡候选（仅活卡）；
//   - deadIDs：需要标记为失效的卡密 id（死卡、未知、未解析）。
func (s *Service) runCardCheck(ctx context.Context, order *orderdomain.Order, checkEnabled map[uint]bool, cfg settingsintegration.CardCheckConfig) (map[string][]cardsecretdomain.Secret, []uint, error) {
	byKey := make(map[string][]cardsecretdomain.Secret)
	var deadIDs []uint

	reservedRows, err := s.cardSecretStore.ListByOrderAndStatus(order.ID, cardsecretdomain.StatusReserved)
	if err != nil {
		return nil, nil, err
	}
	reservedByKey := make(map[string][]cardsecretdomain.Secret)
	for _, reserved := range reservedRows {
		key := orderdomain.ItemKey(reserved.ProductID, reserved.SKUID)
		reservedByKey[key] = append(reservedByKey[key], reserved)
	}

	opts := cardcheck.Options{
		PollInterval: time.Duration(cfg.PollMillis) * time.Millisecond,
		Timeout:      time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	for _, item := range order.Items {
		if !checkEnabled[item.ProductID] {
			continue
		}
		key := orderdomain.ItemKey(item.ProductID, item.SKUID)
		candidates := append([]cardsecretdomain.Secret(nil), reservedByKey[key]...)
		if len(candidates) < item.Quantity+cfg.Buffer {
			need := item.Quantity + cfg.Buffer - len(candidates)
			available, err := s.cardSecretStore.ListAvailableByProduct(item.ProductID, item.SKUID, need)
			if err != nil {
				return nil, nil, err
			}
			candidates = append(candidates, available...)
		}
		if len(candidates) == 0 {
			byKey[key] = nil
			continue
		}

		var cards []cardcheck.Card
		secretByNumber := make(map[string]cardsecretdomain.Secret)
		var unparsed []cardsecretdomain.Secret
		for _, secret := range candidates {
			card, ok := cardcheck.ParseCard(secret.Secret)
			if !ok {
				unparsed = append(unparsed, secret)
				continue
			}
			if _, exists := secretByNumber[card.Number]; exists {
				continue
			}
			cards = append(cards, card)
			secretByNumber[card.Number] = secret
		}
		for _, secret := range unparsed {
			deadIDs = append(deadIDs, secret.ID)
		}
		if len(cards) == 0 {
			byKey[key] = nil
			continue
		}

		results := s.cardChecker.CheckCards(ctx, cfg.Kami, cfg.Interface, cards, opts)

		// 仅对 CheckDx 明确给出非活状态的卡片标记失效；
		// 未返回结果（API 故障/维护/超时）的卡片保持原状态，交由重试或人工处理，避免误清库存。
		var live []cardsecretdomain.Secret
		for _, result := range results {
			secret, found := secretByNumber[result.Card.Number]
			if !found {
				continue
			}
			if result.Status == cardcheck.StatusLive {
				live = append(live, secret)
			} else {
				deadIDs = append(deadIDs, secret.ID)
			}
		}
		if len(results) < len(cards) {
			logger.Warnw("fulfillment_card_check_incomplete",
				"order_id", order.ID,
				"product_id", item.ProductID,
				"expected", len(cards),
				"got", len(results),
			)
		}
		byKey[key] = live
	}

	return byKey, deadIDs, nil
}

// markSecretsInvalid 把指定卡密标记为失效。
func (s *Service) markSecretsInvalid(ids []uint) {
	if len(ids) == 0 || s.cardSecretStore == nil {
		return
	}
	if _, err := s.cardSecretStore.BatchUpdateStatus(ids, cardsecretdomain.StatusInvalid, time.Now()); err != nil {
		logger.Warnw("fulfillment_mark_card_secret_invalid_failed", "ids", ids, "error", err)
	}
}
