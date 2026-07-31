package cardcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
)

// Client 是 CheckDx 测活 API 的 HTTP 客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// Option 定制 Client 构造。
type Option func(*Client)

// WithBaseURL 覆盖 API 根地址。
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/"); trimmed != "" {
			c.baseURL = trimmed
		}
	}
}

// WithHTTPClient 注入自定义 HTTP 客户端（测试友好）。
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

// New 创建 CheckDx 客户端。
func New(opts ...Option) *Client {
	client := &Client{
		baseURL: DefaultBaseURL,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// VerifyResponse 卡密验证结果。
type VerifyResponse struct {
	KamiNum *float64 `json:"kami_num"`
	Result  string   `json:"result"`
}

// Verify 验证卡密并返回剩余点数。
func (c *Client) Verify(ctx context.Context, kami string) (float64, error) {
	var out VerifyResponse
	if err := c.post(ctx, "/api/verify", map[string]interface{}{"cardCode": kami}, &out); err != nil {
		return 0, err
	}
	if out.KamiNum == nil {
		if strings.TrimSpace(out.Result) != "" {
			return 0, fmt.Errorf("checkdx verify failed: %s", out.Result)
		}
		return 0, fmt.Errorf("checkdx verify failed: invalid response")
	}
	return *out.KamiNum, nil
}

// GetPostResponse 可用接口列表。
type GetPostResponse struct {
	InterfaceOptions []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"interfaceOptions"`
}

// ListInterfaces 获取当前可用的测活接口（站点）。
func (c *Client) ListInterfaces(ctx context.Context) ([]InterfaceOption, error) {
	var out GetPostResponse
	if err := c.get(ctx, "/api/get_post", &out); err != nil {
		return nil, err
	}
	options := make([]InterfaceOption, 0, len(out.InterfaceOptions))
	for _, item := range out.InterfaceOptions {
		options = append(options, InterfaceOption{Label: item.Label, Value: item.Value})
	}
	return options, nil
}

// getCardRequest 发起批量测活的请求体。
type getCardRequest struct {
	CardNumbers       string `json:"cardNumbers"`
	SelectedInterface string `json:"selectedInterface"`
	UUID              string `json:"UUID"`
	Kami              string `json:"kami"`
	Len               int    `json:"len"`
	SelectedCountry   string `json:"selectedCountry"`
	FrontendVersion   string `json:"frontend_version"`
}

type getCardResponse struct {
	Text    string   `json:"text"`
	Dianshu *float64 `json:"dianshu"`
}

// startCheck 发起批量测活任务。
func (c *Client) startCheck(ctx context.Context, kami, interfaceID, country string, cards []Card, taskID string) error {
	lines := make([]string, 0, len(cards))
	for _, card := range cards {
		lines = append(lines, card.Format())
	}
	body := getCardRequest{
		CardNumbers:       strings.Join(lines, "@@@@"),
		SelectedInterface: interfaceID,
		UUID:              taskID,
		Kami:              kami,
		Len:               len(cards),
		SelectedCountry:   country,
		FrontendVersion:   DefaultFrontendVersion,
	}
	var out getCardResponse
	if err := c.post(ctx, "/api/get_card", body, &out); err != nil {
		return err
	}
	if strings.TrimSpace(out.Text) != "ok" {
		return fmt.Errorf("checkdx get_card rejected: %s", strings.TrimSpace(out.Text))
	}
	return nil
}

type historyResponse struct {
	Results []struct {
		Status string `json:"status"`
	} `json:"results"`
}

// fetchHistory 拉取某任务已产出的检测结果。
func (c *Client) fetchHistory(ctx context.Context, kami, taskID string) ([]string, error) {
	var out historyResponse
	if err := c.post(ctx, "/api/history/by_uuid", map[string]interface{}{"uuid": taskID, "kami": kami}, &out); err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(out.Results))
	for _, item := range out.Results {
		lines = append(lines, strings.TrimSpace(item.Status))
	}
	return lines, nil
}

type stopTaskResponse struct {
	RefundedCount  string `json:"_退回条数"`
	UpdatedBalance string `json:"_更新后"`
	CompletedCount string `json:"_实际已完成"`
	Text           string `json:"text"`
}

// stopTask 结束任务并退回未检测卡片的点数。
func (c *Client) stopTask(ctx context.Context, kami, taskID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out stopTaskResponse
	if err := c.post(reqCtx, "/api/xiaofei", map[string]interface{}{
		"uuid":   taskID,
		"status": "结束",
		"kami":   kami,
	}, &out); err != nil {
		logger.Warnw("checkdx_stop_task_failed", "task_id", taskID, "error", err)
		return
	}
	logger.Infow("checkdx_task_stopped",
		"task_id", taskID,
		"refunded", out.RefundedCount,
		"completed", out.CompletedCount,
		"balance", out.UpdatedBalance,
	)
}

// doRequest 执行 JSON 请求并解码响应。
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("checkdx marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("checkdx build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("checkdx request %s: %w", path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("checkdx read response %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("checkdx %s returned status %d: %s", path, resp.StatusCode, truncate(payload, 300))
	}
	if out != nil && len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("checkdx decode response %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body interface{}, out interface{}) error {
	return c.doRequest(ctx, http.MethodPost, path, body, out)
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	return c.doRequest(ctx, http.MethodGet, path, nil, out)
}

func truncate(raw []byte, max int) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > max {
		return text[:max] + "..."
	}
	return text
}
