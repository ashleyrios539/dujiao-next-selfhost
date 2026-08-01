package application

import (
	"context"
	"math"
	"time"

	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
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

// runCardCheck 对订单内启用测活的商品执行交付前测活（按轮迭代，凑够活卡即停）。
//
// 每轮按「当前需求数量 + 需求数量的缓冲比例」取卡检测：
//   - 活卡数量达到购买数量 → 停止；
//   - 未达到 → 按剩余数量继续按比例补检，直至凑够或可用卡耗尽；
//   - API 故障/维护导致某轮无任何结果时停止该商品，交由重试，避免误清库存。
//
// 返回：
//   - byKey：每个商品 key 的活卡候选（仅活卡，按检测轮次排序）；
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
		pickFilter := cardsecretcontract.PickFilter{
			Country:   item.PickCountry,
			Brands:    item.PickBrands,
			CardTypes: item.PickCardTypes,
		}
		pool := newCardCheckPool(s.cardSecretStore, item.ProductID, item.SKUID, pickFilter, reservedByKey[key])
		live, itemDead, checkErr := s.checkItemCard(ctx, item, pool, cfg, opts)
		if checkErr != nil {
			return nil, nil, checkErr
		}
		deadIDs = append(deadIDs, itemDead...)
		byKey[key] = live
	}

	return byKey, deadIDs, nil
}

// checkItemCard 对单个订单项按轮迭代测活，返回活卡与应标失效的卡密 id。
func (s *Service) checkItemCard(ctx context.Context, item orderdomain.OrderItem, pool *cardCheckPool, cfg settingsintegration.CardCheckConfig, opts cardcheck.Options) ([]cardsecretdomain.Secret, []uint, error) {
	var live []cardsecretdomain.Secret
	var deadIDs []uint
	needed := item.Quantity
	for needed > 0 {
		extra := cardCheckBufferExtra(needed, cfg.Buffer)
		batch, err := pool.take(needed + extra)
		if err != nil {
			return nil, nil, err
		}
		if len(batch) == 0 {
			break
		}

		cards, secretByNumber, unparsed := parseCandidateBatch(batch)
		for _, secret := range unparsed {
			deadIDs = append(deadIDs, secret.ID)
		}
		if len(cards) == 0 {
			continue
		}

		results := s.cardChecker.CheckCards(ctx, cfg.Kami, cfg.Interface, cards, opts)
		if len(results) == 0 {
			logger.Warnw("fulfillment_card_check_aborted",
				"product_id", item.ProductID,
				"sku_id", item.SKUID,
				"batch_cards", len(cards),
				"reason", "no_results_from_upstream",
			)
			break
		}
		if len(results) < len(cards) {
			logger.Warnw("fulfillment_card_check_incomplete",
				"product_id", item.ProductID,
				"sku_id", item.SKUID,
				"expected", len(cards),
				"got", len(results),
			)
		}

		roundLive := make([]cardsecretdomain.Secret, 0, len(results))
		for _, result := range results {
			secret, found := secretByNumber[result.Card.Number]
			if !found {
				continue
			}
			if result.Status == cardcheck.StatusLive {
				roundLive = append(roundLive, secret)
			} else {
				deadIDs = append(deadIDs, secret.ID)
			}
		}
		live = append(live, roundLive...)
		if len(live) >= item.Quantity {
			break
		}
		needed = item.Quantity - len(live)
	}
	if len(live) > item.Quantity {
		live = live[:item.Quantity]
	}
	return live, deadIDs, nil
}

// cardCheckBufferExtra 按缓冲比例（百分比）计算当前需求数量的额外补检张数（向上取整）。
func cardCheckBufferExtra(needed, percent int) int {
	if needed <= 0 || percent <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	return int(math.Ceil(float64(needed) * float64(percent) / 100.0))
}

// parseCandidateBatch 解析候选卡密为可检测卡片，返回：
//   - cards：可检测卡片（按卡号去重，重复卡号仅保留第一个）；
//   - secretByNumber：卡号 → 卡密；
//   - unparsed：无法解析的卡密。
func parseCandidateBatch(batch []cardsecretdomain.Secret) ([]cardcheck.Card, map[string]cardsecretdomain.Secret, []cardsecretdomain.Secret) {
	var cards []cardcheck.Card
	secretByNumber := make(map[string]cardsecretdomain.Secret)
	var unparsed []cardsecretdomain.Secret
	for _, secret := range batch {
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
	return cards, secretByNumber, unparsed
}

// cardCheckPool 为测活按需提供候选卡密：先消费本单已预留的卡，再按需从可用库存补充。
// 补充时按 id 去重，避免同一张卡被多轮重复取用。
type cardCheckPool struct {
	store     cardsecretcontract.Repository
	productID uint
	skuID     uint
	filter    cardsecretcontract.PickFilter
	queue     []cardsecretdomain.Secret
	seen      map[uint]bool
	availSeen int
}

func newCardCheckPool(store cardsecretcontract.Repository, productID, skuID uint, filter cardsecretcontract.PickFilter, reserved []cardsecretdomain.Secret) *cardCheckPool {
	pool := &cardCheckPool{
		store:     store,
		productID: productID,
		skuID:     skuID,
		filter:    filter,
		seen:      make(map[uint]bool),
	}
	for _, secret := range reserved {
		if pool.seen[secret.ID] {
			continue
		}
		pool.seen[secret.ID] = true
		pool.queue = append(pool.queue, secret)
	}
	return pool
}

// take 返回最多 n 张候选卡密；可用库存不足时返回已有的部分。
func (p *cardCheckPool) take(n int) ([]cardsecretdomain.Secret, error) {
	for len(p.queue) < n {
		need := n - len(p.queue)
		// 按 id 升序返回可用库存，前 availSeen 条大概率已被本池取过；
		// 请求 enough = need + availSeen 以越过已见过的部分，拿到 need 条新卡。
		enough := need + p.availSeen
		var rows []cardsecretdomain.Secret
		var err error
		if p.filter.Empty() {
			rows, err = p.store.ListAvailableByProduct(p.productID, p.skuID, enough)
		} else {
			rows, err = p.store.ListAvailableByProductFiltered(p.productID, p.skuID, p.filter, enough)
		}
		if err != nil {
			return nil, err
		}
		added := 0
		for _, row := range rows {
			if p.seen[row.ID] {
				continue
			}
			p.seen[row.ID] = true
			p.queue = append(p.queue, row)
			p.availSeen++
			added++
		}
		if added == 0 {
			break
		}
	}
	if len(p.queue) == 0 {
		return nil, nil
	}
	top := n
	if top > len(p.queue) {
		top = len(p.queue)
	}
	out := p.queue[:top]
	p.queue = p.queue[top:]
	return out, nil
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
