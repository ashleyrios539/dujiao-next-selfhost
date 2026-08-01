package cardcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
)

// Client 是 CheckDx /v1 API 的 HTTP 客户端。
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

// New 创建 CheckDx /v1 客户端。
func New(opts ...Option) *Client {
	client := &Client{
		baseURL: DefaultBaseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// BalanceResponse 余额查询响应。
type BalanceResponse struct {
	OK      bool   `json:"ok"`
	APIKey  string `json:"api_key"`
	Balance string `json:"balance"`
	Error   string `json:"error"`
}

// Verify 校验卡密并返回剩余点数。
// 对应 GET /v1/balance，卡密通过 X-Api-Key 请求头传递。
func (c *Client) Verify(ctx context.Context, kami string) (float64, error) {
	kami = strings.TrimSpace(kami)
	if kami == "" {
		return 0, fmt.Errorf("checkdx verify: empty kami")
	}
	var out BalanceResponse
	if err := c.doRequest(ctx, http.MethodGet, "/v1/balance", nil, &out, func(req *http.Request) {
		req.Header.Set("X-Api-Key", kami)
	}); err != nil {
		return 0, err
	}
	if !out.OK {
		if strings.TrimSpace(out.Error) != "" {
			return 0, fmt.Errorf("checkdx verify failed: %s", out.Error)
		}
		return 0, fmt.Errorf("checkdx verify failed: invalid response")
	}
	balance, err := strconv.ParseFloat(strings.TrimSpace(out.Balance), 64)
	if err != nil {
		return 0, fmt.Errorf("checkdx verify: invalid balance %q", out.Balance)
	}
	return balance, nil
}

// SubmitResponse 提交测活任务的响应。
type SubmitResponse struct {
	OK      bool     `json:"ok"`
	TaskID  string   `json:"task_id"`
	Total   int      `json:"total"`
	Balance string   `json:"balance"`
	Invalid []string `json:"invalid"`
	Error   string   `json:"error"`
}

// submitRequest 提交测活任务的请求体。
type submitRequest struct {
	APIKey      string   `json:"api_key"`
	Interface   string   `json:"interface"`
	Cards       []string `json:"cards"`
	ClientToken string   `json:"client_token,omitempty"`
}

// submit 发起批量测活任务并返回 task_id。
func (c *Client) submit(ctx context.Context, kami, interfaceID string, lines []string) (string, error) {
	req := submitRequest{APIKey: kami, Interface: interfaceID, Cards: lines}
	var out SubmitResponse
	if err := c.post(ctx, "/v1/submit", req, &out); err != nil {
		return "", err
	}
	if !out.OK {
		detail := strings.TrimSpace(out.Error)
		if len(out.Invalid) > 0 {
			detail += " invalid=[" + strings.Join(out.Invalid, ", ") + "]"
		}
		if detail == "" {
			detail = "unknown"
		}
		return "", fmt.Errorf("checkdx submit rejected: %s", detail)
	}
	return strings.TrimSpace(out.TaskID), nil
}

// ResultItem 单卡检测结果条目（来自 /v1/result）。
type ResultItem struct {
	Card    string `json:"card"`
	Verdict string `json:"verdict"`
	Raw     string `json:"raw"`
	Time    string `json:"time"`
}

// ResultResponse 轮询 /v1/result 的响应。
type ResultResponse struct {
	OK         bool         `json:"ok"`
	Status     string       `json:"status"` // running | done
	Total      int          `json:"total"`
	Done       int          `json:"done"`
	NextOffset int          `json:"next_offset"`
	Truncated  bool         `json:"truncated"`
	Results    []ResultItem `json:"results"`
	Error      string       `json:"error"`
}

// fetchResults 拉取任务结果。
func (c *Client) fetchResults(ctx context.Context, kami, taskID string, offset int) (*ResultResponse, error) {
	path := "/v1/result?task_id=" + url.QueryEscape(taskID)
	if offset > 0 {
		path += "&offset=" + strconv.Itoa(offset)
	}
	var out ResultResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &out, func(req *http.Request) {
		req.Header.Set("X-Api-Key", kami)
	}); err != nil {
		return nil, err
	}
	if !out.OK {
		if strings.TrimSpace(out.Error) != "" {
			return nil, fmt.Errorf("checkdx result failed: %s", out.Error)
		}
		return nil, fmt.Errorf("checkdx result failed: invalid response")
	}
	return &out, nil
}

// CancelResponse 结束任务的响应。
type CancelResponse struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

// cancelTask 结束任务，未检测到的卡片自动退点。
func (c *Client) cancelTask(ctx context.Context, kami, taskID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out CancelResponse
	if err := c.post(reqCtx, "/v1/cancel", map[string]interface{}{
		"api_key": kami,
		"task_id": taskID,
	}, &out); err != nil {
		logger.Warnw("checkdx_cancel_task_failed", "task_id", taskID, "error", err)
		return
	}
	logger.Infow("checkdx_task_cancelled",
		"task_id", taskID,
		"done", out.Done,
		"total", out.Total,
		"message", out.Message,
	)
}

// doRequest 执行 JSON 请求并解码响应。header 在发送前可选注入。
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, out interface{}, header func(*http.Request)) error {
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
	if header != nil {
		header(req)
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
	return c.doRequest(ctx, http.MethodPost, path, body, out, nil)
}

func truncate(raw []byte, max int) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > max {
		return text[:max] + "..."
	}
	return text
}
