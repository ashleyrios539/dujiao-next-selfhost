package cardcheck

import (
	"context"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/google/uuid"
)

// CheckCards 批量检测卡片存活状态。
//
// 流程：
//  1. 发起 get_card 批量检测任务；
//  2. 轮询 /api/history/by_uuid 收集结果；
//  3. 无论是否收集完毕都调用 xiaofei 结束任务（未检测到的卡自动退点）。
//
// 结果与入参 cards 一一对应（无法精确匹配时按顺序兜底）。
func (c *Client) CheckCards(ctx context.Context, kami, interfaceID, country string, cards []Card, opts Options) []Result {
	if len(cards) == 0 {
		return nil
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultOptions().PollInterval
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultOptions().Timeout
	}

	taskID := uuid.NewString()
	startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startCancel()
	if err := c.startCheck(startCtx, kami, interfaceID, country, cards, taskID); err != nil {
		logger.Warnw("checkdx_start_failed", "task_id", taskID, "cards", len(cards), "error", err)
		c.stopTask(ctx, kami, taskID)
		return nil
	}
	logger.Infow("checkdx_task_started", "task_id", taskID, "cards", len(cards), "interface", interfaceID)

	deadline := time.Now().Add(opts.Timeout)
	var results []Result
	for time.Now().Before(deadline) {
		lines, err := c.fetchHistory(ctx, kami, taskID)
		if err != nil {
			logger.Warnw("checkdx_poll_failed", "task_id", taskID, "error", err)
		} else if len(lines) > 0 {
			results = mergeResults(results, lines, cards)
			if len(results) >= len(cards) {
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

	c.stopTask(ctx, kami, taskID)
	if len(results) < len(cards) {
		logger.Warnw("checkdx_partial_results",
			"task_id", taskID,
			"expected", len(cards),
			"got", len(results),
		)
	}
	return results
}

// mergeResults 把新拉取的结果行并入已收集结果，按卡号匹配，兜底顺序分配。
func mergeResults(current []Result, lines []string, cards []Card) []Result {
	cardIndex := make(map[string]int, len(cards))
	for index, card := range cards {
		cardIndex[card.Number] = index
	}
	assigned := make([]bool, len(cards))
	byIndex := make(map[int]Result, len(current))
	for _, result := range current {
		if index, found := cardIndex[result.Card.Number]; found && !assigned[index] {
			assigned[index] = true
			byIndex[index] = result
		}
	}

	nextSlot := 0
	for _, line := range lines {
		status := parseStatus(line)
		if status == "" {
			continue
		}
		index := -1
		if card, ok := matchCardByLine(line, cards); ok {
			if found, exists := cardIndex[card.Number]; exists {
				index = found
			}
		}
		if index < 0 {
			for nextSlot < len(cards) && assigned[nextSlot] {
				nextSlot++
			}
			if nextSlot >= len(cards) {
				continue
			}
			index = nextSlot
		}
		if assigned[index] {
			continue
		}
		assigned[index] = true
		byIndex[index] = Result{Card: cards[index], Status: status, Raw: line}
	}

	results := make([]Result, 0, len(byIndex))
	for index := range cards {
		if result, found := byIndex[index]; found {
			results = append(results, result)
		}
	}
	return results
}

// matchCardByLine 尝试从结果行中识别对应的卡片。
// 依次尝试完整卡号、前4+后4（适配掩码 4111****1111）。
func matchCardByLine(line string, cards []Card) (Card, bool) {
	for _, card := range cards {
		if card.Number != "" && strings.Contains(line, card.Number) {
			return card, true
		}
	}
	for _, card := range cards {
		if len(card.Number) < 8 {
			continue
		}
		head := card.Number[:4]
		tail := card.Number[len(card.Number)-4:]
		if strings.Contains(line, head) && strings.Contains(line, tail) {
			return card, true
		}
	}
	return Card{}, false
}

// parseStatus 从结果行首词识别存活状态。
func parseStatus(line string) Status {
	first := strings.ToLower(firstWord(strings.TrimSpace(line)))
	switch {
	case strings.HasPrefix(first, "live"):
		return StatusLive
	case strings.HasPrefix(first, "dead"):
		return StatusDead
	case strings.HasPrefix(first, "unknown"):
		return StatusUnknown
	}
	return ""
}

func firstWord(line string) string {
	if index := strings.IndexAny(line, " \t"); index >= 0 {
		return line[:index]
	}
	return line
}
