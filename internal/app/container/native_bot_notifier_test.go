package container

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	fulfillmentcontract "github.com/dujiao-next/internal/modules/fulfillment/contract"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	notifybotapi "github.com/dujiao-next/internal/modules/telegram/notify/infrastructure/botapi"
)

// stubTokenResolver 总是返回固定 token。
type stubTokenResolver struct{ token string }

func (r stubTokenResolver) ResolveActiveBotToken() (string, error) { return r.token, nil }

// stubFulfStore 仅实现 GetByOrderID。
type stubFulfStore struct {
	byOrder map[uint]*fulfillmentdomain.Fulfillment
}

func (s stubFulfStore) Create(*fulfillmentdomain.Fulfillment) error { return nil }
func (s stubFulfStore) GetByOrderID(orderID uint) (*fulfillmentdomain.Fulfillment, error) {
	return s.byOrder[orderID], nil
}
func (s stubFulfStore) FindByOrderIDForUpdate(orderID uint) (*fulfillmentdomain.Fulfillment, bool, error) {
	f, ok := s.byOrder[orderID]
	return f, ok, nil
}

// stubOrderStore 嵌入接口避免实现全部方法，仅覆盖 GetByID。
type stubOrderStore struct {
	ordercontract.Store
	byID map[uint]*orderdomain.Order
}

func (s stubOrderStore) GetByID(id uint) (*orderdomain.Order, error) {
	return s.byID[id], nil
}

// captureTransport 捕获 sendDocument 请求并返回 Telegram 风格的 OK 响应。
type captureTransport struct {
	gotURL  string
	bodyBuf bytes.Buffer
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.gotURL = req.URL.String()
	// 保存 body 副本供校验
	t.bodyBuf.Reset()
	if req.Body != nil {
		_, _ = io.Copy(&t.bodyBuf, req.Body)
		req.Body.Close()
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Header:     make(http.Header),
	}, nil
}

func newNotifierWithCapture(t *testing.T, fulfStore fulfillmentcontract.Store, orderStore ordercontract.Store) (*nativeBotNotifier, *captureTransport) {
	t.Helper()
	tr := &captureTransport{}
	api := notifybotapi.NewWithHTTPClient(&http.Client{Transport: tr})
	n := &nativeBotNotifier{
		token:  stubTokenResolver{token: "TESTTOKEN"},
		api:    api,
		fulf:   fulfStore,
		orders: orderStore,
	}
	return n, tr
}

// TestNativeBotNotifierUsesChildIDForPayloadAndParentOrderNoForFilename 验证修复的核心：
// - 履约查询用子订单 ID（GetByOrderID(childID) 命中履约记录）
// - txt 文件名用父订单 OrderNo（与 bot「我的订单」展示一致）
func TestNativeBotNotifierUsesChildIDForPayloadAndParentOrderNoForFilename(t *testing.T) {
	const parentID uint = 100
	const childID uint = 101
	fulf := stubFulfStore{byOrder: map[uint]*fulfillmentdomain.Fulfillment{
		// 履约记录挂在子订单上（与生产一致：CreateAuto 在子订单上跑）
		childID: {ID: 1, OrderID: childID, Payload: "4111111111111111|12|2027|123\n4222222222222222|11|2027|456"},
		// 故意在父订单上也放一条，确保 notifier 不会误用父 ID 查
		parentID: {ID: 2, OrderID: parentID, Payload: "SHOULD-NOT-USE"},
	}}
	orders := &stubOrderStore{byID: map[uint]*orderdomain.Order{
		parentID: {OrderNo: "DJ-PARENT-001"},
		childID:  {OrderNo: "DJ-PARENT-001-1", ParentID: uintPtr(parentID)},
	}}
	n, tr := newNotifierWithCapture(t, fulf, orders)

	if err := n.EnqueueOrderFulfilled("999888", childID); err != nil {
		t.Fatalf("EnqueueOrderFulfilled: %v", err)
	}
	if tr.gotURL == "" {
		t.Fatal("expected a sendDocument request to be made")
	}
	if !strings.Contains(tr.gotURL, "botTESTTOKEN/sendDocument") {
		t.Errorf("unexpected url %q", tr.gotURL)
	}
	body := tr.bodyBuf.String()
	// 文件名应使用父订单 OrderNo，而非子订单 OrderNo（DJ-PARENT-001-1）
	if !strings.Contains(body, "卡密_DJ-PARENT-001.txt") {
		t.Errorf("expected filename to use parent OrderNo DJ-PARENT-001, body=%q", body)
	}
	if strings.Contains(body, "DJ-PARENT-001-1.txt") {
		t.Errorf("filename must NOT use child OrderNo, body=%q", body)
	}
	// payload 必须是子订单履约内容，不能是父订单上的 SHOULD-NOT-USE
	if !strings.Contains(body, "4111111111111111") {
		t.Errorf("expected child fulfillment payload in document, body=%q", body)
	}
	if strings.Contains(body, "SHOULD-NOT-USE") {
		t.Errorf("must not use parent-order fulfillment payload, body=%q", body)
	}
}

// TestNativeBotNotifierNoFulfillmentOnChildSkipsSilently 验证子订单无履约记录时静默跳过（不发空文件）。
func TestNativeBotNotifierNoFulfillmentOnChildSkipsSilently(t *testing.T) {
	fulf := stubFulfStore{byOrder: map[uint]*fulfillmentdomain.Fulfillment{}}
	orders := &stubOrderStore{byID: map[uint]*orderdomain.Order{
		1: {OrderNo: "DJ-X"},
	}}
	n, tr := newNotifierWithCapture(t, fulf, orders)
	if err := n.EnqueueOrderFulfilled("1", 1); err != nil {
		t.Fatalf("EnqueueOrderFulfilled: %v", err)
	}
	if tr.gotURL != "" {
		t.Errorf("expected no request when fulfillment missing, got %q", tr.gotURL)
	}
}

// TestNativeBotNotifierChildWithoutParentUsesOwnOrderNo 验证无父订单时用自身 OrderNo。
func TestNativeBotNotifierChildWithoutParentUsesOwnOrderNo(t *testing.T) {
	fulf := stubFulfStore{byOrder: map[uint]*fulfillmentdomain.Fulfillment{
		7: {ID: 1, OrderID: 7, Payload: "CARD-DATA"},
	}}
	orders := &stubOrderStore{byID: map[uint]*orderdomain.Order{
		7: {OrderNo: "DJ-SOLO"},
	}}
	n, tr := newNotifierWithCapture(t, fulf, orders)
	if err := n.EnqueueOrderFulfilled("2", 7); err != nil {
		t.Fatalf("EnqueueOrderFulfilled: %v", err)
	}
	if !strings.Contains(tr.bodyBuf.String(), "卡密_DJ-SOLO.txt") {
		t.Errorf("expected own OrderNo as filename, body=%q", tr.bodyBuf.String())
	}
}

// TestNativeBotNotifierNoTokenSkips 验证无 bot token 时静默跳过。
func TestNativeBotNotifierNoTokenSkips(t *testing.T) {
	fulf := stubFulfStore{byOrder: map[uint]*fulfillmentdomain.Fulfillment{
		1: {ID: 1, OrderID: 1, Payload: "X"},
	}}
	n := &nativeBotNotifier{
		token:  stubTokenResolver{token: ""},
		api:    notifybotapi.NewWithHTTPClient(&http.Client{Transport: &captureTransport{}}),
		fulf:   fulf,
		orders: &stubOrderStore{byID: map[uint]*orderdomain.Order{}},
	}
	if err := n.EnqueueOrderFulfilled("3", 1); err != nil {
		t.Fatalf("EnqueueOrderFulfilled: %v", err)
	}
}

func uintPtr(v uint) *uint { return &v }

// 兜底：context 仅用于超时，确保编译期用到，避免 import 被裁剪。
var _ = context.Background
