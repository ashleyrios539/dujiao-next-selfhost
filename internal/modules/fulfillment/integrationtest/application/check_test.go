package application_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/dujiao-next/internal/modules/fulfillment/application"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	fulfillmentgormstore "github.com/dujiao-next/internal/modules/fulfillment/infrastructure/gormstore"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingscontract "github.com/dujiao-next/internal/modules/settings/contract"
	cardcheck "github.com/dujiao-next/internal/upstream/cardcheck"

	"github.com/dujiao-next/internal/constants"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cardsecretgormstore "github.com/dujiao-next/internal/modules/cardsecret/infrastructure/gormstore"
	"github.com/dujiao-next/internal/platform/database/gormdb"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var checkTestCounter uint64

func setupCardCheckTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:fulfillment_check_test_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), atomic.AddUint64(&checkTestCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&orderdomain.Order{},
		&orderdomain.OrderItem{},
		&fulfillmentdomain.Fulfillment{},
		&cardsecretdomain.Secret{},
		&cardsecretdomain.Batch{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	gormdb.DB = db
	return db
}

// scriptedChecker 按卡号脚本返回测活结果，记录调用次数。
type scriptedChecker struct {
	liveNumbers map[string]bool
	calls       int
}

func (c *scriptedChecker) CheckCards(_ context.Context, _, _ string, cards []cardcheck.Card, _ cardcheck.Options) []cardcheck.Result {
	results := make([]cardcheck.Result, 0, len(cards))
	for _, card := range cards {
		status := cardcheck.StatusDead
		if c.liveNumbers[card.Number] {
			status = cardcheck.StatusLive
		}
		results = append(results, cardcheck.Result{Card: card, Status: status})
	}
	c.calls++
	return results
}

// fakeSettingStore 最小 settings 仓库，仅承载 card_check_config。
type fakeSettingStore struct {
	values map[string]jsonmap.JSON
}

func (f *fakeSettingStore) GetByKey(key string) (jsonmap.JSON, bool, error) {
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeSettingStore) Upsert(key string, value jsonmap.JSON) (jsonmap.JSON, error) {
	f.values[key] = value
	return value, nil
}

func cardCheckEnabledSetting(bufferPercent int) *fakeSettingStore {
	return &fakeSettingStore{values: map[string]jsonmap.JSON{
		constants.SettingKeyCardCheckConfig: jsonmap.JSON{
			"enabled":                   true,
			"kami":                      "CheckDx_test",
			"interface":                 "post5",
			"buffer":                    bufferPercent,
			"timeout_seconds":           60,
			"poll_interval_millis":      2000,
		},
	}}
}

func createCardCheckOrder(t *testing.T, db *gorm.DB, orderNo string, quantity int) *orderdomain.Order {
	t.Helper()
	now := time.Now()
	order := &orderdomain.Order{
		OrderNo:                 orderNo,
		UserID:                  1,
		Status:                  constants.OrderStatusPaid,
		Currency:                "CNY",
		OriginalAmount:          money.FromDecimal(decimal.NewFromInt(int64(quantity * 10))),
		DiscountAmount:          money.FromDecimal(decimal.Zero),
		PromotionDiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:             money.FromDecimal(decimal.NewFromInt(int64(quantity * 10))),
		WalletPaidAmount:        money.FromDecimal(decimal.Zero),
		OnlinePaidAmount:        money.FromDecimal(decimal.NewFromInt(int64(quantity * 10))),
		RefundedAmount:          money.FromDecimal(decimal.Zero),
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	item := &orderdomain.OrderItem{
		OrderID:            order.ID,
		ProductID:          10,
		SKUID:              0,
		TitleJSON:          jsonmap.JSON{"zh-CN": "测活商品"},
		UnitPrice:          money.FromDecimal(decimal.NewFromInt(10)),
		Quantity:           quantity,
		TotalPrice:         money.FromDecimal(decimal.NewFromInt(int64(quantity * 10))),
		FulfillmentType:    constants.FulfillmentTypeAuto,
		CardCheckEnabled:   true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create order item failed: %v", err)
	}
	return order
}

func createAvailableCardSecrets(t *testing.T, db *gorm.DB, numbers ...string) {
	t.Helper()
	now := time.Now()
	for _, number := range numbers {
		secret := &cardsecretdomain.Secret{
			ProductID: 10,
			SKUID:     0,
			Secret:    number + "|01|28|123",
			Status:    cardsecretdomain.StatusAvailable,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Create(secret).Error; err != nil {
			t.Fatalf("create card secret failed: %v", err)
		}
	}
}

func buildCardCheckService(db *gorm.DB, checker CardChecker, settingStore settingscontract.Store) *Service {
	return New(Options{
		OrderStore:       ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
		FulfillmentStore: fulfillmentgormstore.New(db),
		SettingService:   settingsapp.NewService(settingStore),
		CardSecretStore:  cardsecretgormstore.New(db),
		CardChecker:      checker,
	})
}

func TestCreateAutoCardCheckStopsWhenEnoughLive(t *testing.T) {
	db := setupCardCheckTestDB(t)
	createAvailableCardSecrets(t, db,
		"4111111111110001", "4111111111110002", "4111111111110003",
	)
	order := createCardCheckOrder(t, db, "FULFILL-CHECK-STOP", 2)

	checker := &scriptedChecker{liveNumbers: map[string]bool{
		"4111111111110001": true,
		"4111111111110002": true,
		"4111111111110003": true,
	}}
	svc := buildCardCheckService(db, checker, cardCheckEnabledSetting(0))

	result, err := svc.CreateAuto(order.ID)
	if err != nil {
		t.Fatalf("create auto fulfillment failed: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (stop once enough live)", checker.calls)
	}
	liveLines := strings.Split(strings.TrimSpace(result.Payload), "\n")
	if len(liveLines) != 2 {
		t.Fatalf("payload lines = %d, want 2, payload: %s", len(liveLines), result.Payload)
	}

	var orderAfter orderdomain.Order
	if err := db.First(&orderAfter, order.ID).Error; err != nil {
		t.Fatalf("query order failed: %v", err)
	}
	if orderAfter.Status != constants.OrderStatusCompleted {
		t.Fatalf("order status want completed got %s", orderAfter.Status)
	}
}

func TestCreateAutoCardCheckKeepsCheckingByRatio(t *testing.T) {
	db := setupCardCheckTestDB(t)
	createAvailableCardSecrets(t, db,
		"4111111111110001", "4111111111110002", "4111111111110003",
		"4111111111110004", "4111111111110005",
	)
	order := createCardCheckOrder(t, db, "FULFILL-CHECK-RATIO", 2)

	// 第 1 轮批量 3 张：0001 活，0002/0003 死；第 2 轮按剩余数量(1)+50% 再取 2 张：0004/0005 活。
	checker := &scriptedChecker{liveNumbers: map[string]bool{
		"4111111111110001": true,
		"4111111111110004": true,
		"4111111111110005": true,
	}}
	svc := buildCardCheckService(db, checker, cardCheckEnabledSetting(50))

	result, err := svc.CreateAuto(order.ID)
	if err != nil {
		t.Fatalf("create auto fulfillment failed: %v", err)
	}
	if checker.calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (round 1 short, round 2 fills)", checker.calls)
	}
	liveLines := strings.Split(strings.TrimSpace(result.Payload), "\n")
	if len(liveLines) != 2 {
		t.Fatalf("payload lines = %d, want 2, payload: %s", len(liveLines), result.Payload)
	}
	if !strings.Contains(result.Payload, "4111111111110001") {
		t.Fatalf("payload should contain first live card: %s", result.Payload)
	}

	var deadCount int64
	if err := db.Model(&cardsecretdomain.Secret{}).
		Where("status = ? AND secret LIKE ?", cardsecretdomain.StatusInvalid, "%0002%").
		Count(&deadCount).Error; err != nil {
		t.Fatalf("count invalid dead card failed: %v", err)
	}
	if deadCount != 1 {
		t.Fatalf("dead card 0002 invalid count = %d, want 1", deadCount)
	}

	var orderAfter orderdomain.Order
	if err := db.First(&orderAfter, order.ID).Error; err != nil {
		t.Fatalf("query order failed: %v", err)
	}
	if orderAfter.Status != constants.OrderStatusCompleted {
		t.Fatalf("order status want completed got %s", orderAfter.Status)
	}
}

func TestCreateAutoCardCheckAbortsWhenUpstreamDown(t *testing.T) {
	db := setupCardCheckTestDB(t)
	createAvailableCardSecrets(t, db,
		"4111111111110001", "4111111111110002", "4111111111110003",
	)
	order := createCardCheckOrder(t, db, "FULFILL-CHECK-DOWN", 2)

	checker := &nilChecker{}
	svc := buildCardCheckService(db, checker, cardCheckEnabledSetting(0))

	if _, err := svc.CreateAuto(order.ID); err == nil {
		t.Fatalf("expected fulfillment failure when upstream returns no results")
	}

	var invalidCount int64
	if err := db.Model(&cardsecretdomain.Secret{}).
		Where("status = ?", cardsecretdomain.StatusInvalid).
		Count(&invalidCount).Error; err != nil {
		t.Fatalf("count invalid failed: %v", err)
	}
	if invalidCount != 0 {
		t.Fatalf("invalid count = %d, want 0 (must not clear stock when upstream down)", invalidCount)
	}
}

// nilChecker 模拟测活上游不可用（返回空结果）。
type nilChecker struct{}

func (c *nilChecker) CheckCards(context.Context, string, string, []cardcheck.Card, cardcheck.Options) []cardcheck.Result {
	return nil
}
