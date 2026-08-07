package container

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// newOrderServiceDB 构造内存 SQLite + OrderService（仅 OrderStore）。
func newOrderServiceDB(t *testing.T) (*orderapp.OrderService, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:tg_order_reader_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&orderdomain.Order{}, &orderdomain.OrderItem{}, &fulfillmentdomain.Fulfillment{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	svc := orderapp.NewOrderService(orderapp.OrderServiceOptions{
		OrderStore: ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
	})
	return svc, db
}

// TestTelegramGetOrderByOrderNoAggregatesChildFulfillment 验证「我的订单」详情聚合子订单履约 payload。
// 场景：履约记录建在子订单上（自动发货 CreateAuto 在子订单上跑），父订单 Fulfillment 为空，
// bot GetOrderByOrderNo 必须聚合子订单 payload 才能在「我的订单」详情显示卡密。
func TestTelegramGetOrderByOrderNoAggregatesChildFulfillment(t *testing.T) {
	svc, db := newOrderServiceDB(t)
	now := time.Now()
	// 父订单（completed，无 Fulfillment）
	parent := &orderdomain.Order{
		OrderNo:        "DJ-PARENT-002",
		Status:         constants.OrderStatusCompleted,
		Currency:       "USD",
		TotalAmount:    money.FromDecimal(decimal.NewFromFloat(0.10)),
		OriginalAmount: money.FromDecimal(decimal.NewFromFloat(0.10)),
		PaidAt:         &now,
		CreatedAt:      now,
		UpdatedAt:      now,
		UserID:         7,
	}
	if err := db.Create(parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	// 子订单（completed，Fulfillment 挂在其上）
	child := &orderdomain.Order{
		OrderNo:        "DJ-PARENT-002-1",
		Status:         constants.OrderStatusCompleted,
		Currency:       "USD",
		TotalAmount:    money.FromDecimal(decimal.NewFromFloat(0.10)),
		OriginalAmount: money.FromDecimal(decimal.NewFromFloat(0.10)),
		PaidAt:         &now,
		CreatedAt:      now,
		UpdatedAt:      now,
		UserID:         7,
		ParentID:       uintPtr(parent.ID),
	}
	if err := db.Create(child).Error; err != nil {
		t.Fatalf("create child: %v", err)
	}
	// 履约记录挂在子订单上（与生产一致）
	fulf := &fulfillmentdomain.Fulfillment{
		OrderID: child.ID,
		Type:    constants.FulfillmentTypeAuto,
		Status:  "delivered",
		Payload: "4111111111111111|12|2027|123\n4222222222222222|11|2027|456",
	}
	if err := db.Create(fulf).Error; err != nil {
		t.Fatalf("create fulfillment: %v", err)
	}

	ports := &telegramPurchasePorts{orders: svc, locale: "zh-CN"}
	detail, err := ports.GetOrderByOrderNo(context.Background(), 7, "DJ-PARENT-002")
	if err != nil {
		t.Fatalf("GetOrderByOrderNo: %v", err)
	}
	if detail.Fulfillment == nil || detail.Fulfillment.Payload == "" {
		t.Fatalf("expected aggregated child fulfillment payload, got nil/empty: %+v", detail.Fulfillment)
	}
	if !strings.Contains(detail.Fulfillment.Payload, "4111111111111111") {
		t.Errorf("expected child payload in detail, got: %s", detail.Fulfillment.Payload)
	}
}

// TestTelegramGetOrderByOrderNoPrefersParentFulfillment 验证父订单自身有履约时优先用父的（不聚合子）。
func TestTelegramGetOrderByOrderNoPrefersParentFulfillment(t *testing.T) {
	svc, db := newOrderServiceDB(t)
	now := time.Now()
	parent := &orderdomain.Order{
		OrderNo:        "DJ-PARENT-003",
		Status:         constants.OrderStatusCompleted,
		Currency:       "USD",
		TotalAmount:    money.FromDecimal(decimal.NewFromFloat(0.10)),
		OriginalAmount: money.FromDecimal(decimal.NewFromFloat(0.10)),
		PaidAt:         &now,
		CreatedAt:      now,
		UpdatedAt:      now,
		UserID:         8,
	}
	if err := db.Create(parent).Error; err != nil {
		t.Fatalf("create parent: %v", err)
	}
	fulf := &fulfillmentdomain.Fulfillment{
		OrderID: parent.ID,
		Type:    constants.FulfillmentTypeAuto,
		Status:  "delivered",
		Payload: "PARENT-CARD-DATA",
	}
	if err := db.Create(fulf).Error; err != nil {
		t.Fatalf("create fulfillment: %v", err)
	}
	ports := &telegramPurchasePorts{orders: svc, locale: "zh-CN"}
	detail, err := ports.GetOrderByOrderNo(context.Background(), 8, "DJ-PARENT-003")
	if err != nil {
		t.Fatalf("GetOrderByOrderNo: %v", err)
	}
	if detail.Fulfillment == nil {
		t.Fatal("expected parent fulfillment, got nil")
	}
	if !strings.Contains(detail.Fulfillment.Payload, "PARENT-CARD-DATA") {
		t.Errorf("expected parent payload, got: %s", detail.Fulfillment.Payload)
	}
}
