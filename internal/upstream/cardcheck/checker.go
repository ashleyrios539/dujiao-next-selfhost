package cardcheck

import (
	"context"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
)

// CheckCards 批量检测卡片存活状态。
//
// 流程：
//  1. 提交 POST /v1/submit 批量检测任务，拿到 task_id；
//  2. 轮询 GET /v1/result 收集结果，直到 status=done 或超时；
//  3. 无论是否完成都调用 POST /v1/cancel 结束任务（未检测到的卡自动退点）。
//
// 结果与入参 cards 一一对应（无法精确匹配时按返回顺序兜底）。
func (c *Client) CheckCards(ctx context.Context, kami, interfaceID string, cards []Card, opts Options) []Result {
	if len(cards) == 0 {
		return nil
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultOptions().PollInterval
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultOptions().Timeout
	}

	lines := make([]string, 0, len(cards))
	for _, card := range cards {
		lines = append(lines, card.Format())
	}

	startCtx, startCancel := context.WithTimeout(ctx, 30*time.Second)
	defer startCancel()
	taskID, err := c.submit(startCtx, kami, interfaceID, lines)
	if err != nil {
		logger.Warnw("checkdx_submit_failed", "cards", len(cards), "interface", interfaceID, "error", err)
		return nil
	}
	logger.Infow("checkdx_task_started", "task_id", taskID, "cards", len(cards), "interface", interfaceID)

	deadline := time.Now().Add(opts.Timeout)
	var items []ResultItem
	for time.Now().Before(deadline) {
		resp, err := c.fetchResults(ctx, kami, taskID, 0)
		if err != nil {
			logger.Warnw("checkdx_poll_failed", "task_id", taskID, "error", err)
		} else if resp != nil {
			items = mergeItems(items, resp.Results)
			if resp.Status == "done" || len(items) >= len(cards) {
				break
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			deadline = time.Now()
		case <-time.After(opts.PollInterval):
		}
	}

	c.cancelTask(ctx, kami, taskID)
	results := matchResults(items, cards)
	if len(results) < len(cards) {
		logger.Warnw("checkdx_partial_results",
			"task_id", taskID,
			"expected", len(cards),
			"got", len(results),
		)
	}
	return results
}

// mergeItems 合并两次轮询返回的结果条目，按卡号去重。
func mergeItems(current, incoming []ResultItem) []ResultItem {
	seen := make(map[string]bool, len(current)+len(incoming))
	merged := make([]ResultItem, 0, len(current)+len(incoming))
	for _, item := range append(append([]ResultItem(nil), current...), incoming...) {
		key := strings.TrimSpace(item.Card)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	return merged
}

// matchResults 把 API 返回的结果映射到入参卡片，返回按入参顺序对齐的结果。
// 匹配策略：完整卡号 → 前4+后4（适配掩码 4111****1111）→ 按返回顺序兜底。
func matchResults(items []ResultItem, cards []Card) []Result {
	assigned := make([]bool, len(cards))
	byIndex := make(map[int]Result, len(items))

	for _, item := range items {
		index := indexOfCard(item.Card, cards, assigned)
		if index < 0 {
			for next := 0; next < len(cards); next++ {
				if !assigned[next] {
					index = next
					break
				}
			}
		}
		if index < 0 || assigned[index] {
			continue
		}
		assigned[index] = true
		byIndex[index] = Result{
			Card:   cards[index],
			Status: parseVerdict(item.Verdict),
			Raw:    item.Raw,
		}
	}

	results := make([]Result, 0, len(byIndex))
	for index := range cards {
		if result, found := byIndex[index]; found {
			results = append(results, result)
		}
	}
	return results
}

// indexOfCard 按完整卡号或前4+后4掩码查找入参卡片下标，已分配的下标跳过。
func indexOfCard(number string, cards []Card, assigned []bool) int {
	digits := digitsOnly(number)
	for index, card := range cards {
		if assigned[index] || card.Number == "" {
			continue
		}
		if card.Number == digits {
			return index
		}
	}
	for index, card := range cards {
		if assigned[index] || len(card.Number) < 8 || len(digits) < 8 {
			continue
		}
		if strings.HasPrefix(digits, card.Number[:4]) && strings.HasSuffix(digits, card.Number[len(card.Number)-4:]) {
			return index
		}
	}
	return -1
}

// parseVerdict 把 /v1/result 返回的 verdict 映射为内部状态。
func parseVerdict(verdict string) Status {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "live":
		return StatusLive
	case "dead":
		return StatusDead
	case "unknown":
		return StatusUnknown
	}
	return ""
}
