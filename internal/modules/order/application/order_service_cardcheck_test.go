package application

import (
	"fmt"
	"testing"
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	promotiondomain "github.com/dujiao-next/internal/modules/promotion/domain"
	promotiongormstore "github.com/dujiao-next/internal/modules/promotion/infrastructure/gormstore"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// buildCardCheckPricingOrderService 构造带 sqlite 商品数据的订单服务。
func buildCardCheckPricingOrderService(t *testing.T) (*OrderService, productdomain.Product, productdomain.ProductSKU) {
	t.Helper()
	dsn := fmt.Sprintf("file:cardcheck_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &promotiondomain.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := categorydomain.Category{
		Slug:      fmt.Sprintf("cc-cat-%d", now.UnixNano()),
		NameJSON:  jsonmap.JSON{"zh-CN": "测活分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := productdomain.Product{
		CategoryID:       category.ID,
		Slug:             fmt.Sprintf("cc-prod-%d", now.UnixNano()),
		TitleJSON:        jsonmap.JSON{"zh-CN": "测活商品"},
		PriceAmount:      money.FromDecimal(decimal.NewFromInt(10)),
		PurchaseType:     constants.ProductPurchaseMember,
		FulfillmentType:  constants.FulfillmentTypeAuto,
		CardCheckEnabled: true,
		CardCheckFee:     money.FromDecimal(decimal.NewFromInt(2)),
		IsActive:         true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	sku := productdomain.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     productdomain.DefaultSKUCode,
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductStore:    productgormstore.NewProductStore(db),
		ProductSKUStore: productgormstore.NewSKUStore(db),
		PromotionRepo:   promotiongormstore.New(db),
		ExpireMinutes:   15,
	})
	return svc, product, sku
}

func TestBuildOrderResultCardCheckFeeAppliedWhenOptedIn(t *testing.T) {
	svc, product, sku := buildCardCheckPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, CardCheckEnabled: true},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult error: %v", err)
	}
	if len(result.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(result.Plans))
	}
	item := result.Plans[0].Item
	if !item.CardCheckEnabled {
		t.Fatalf("expected card check enabled on order item")
	}
	if item.UnitPrice.String() != "12.00" {
		t.Fatalf("expected unit price 12.00 (10 + 2 fee), got %s", item.UnitPrice.String())
	}
	if item.OriginalUnitPrice.String() != "12.00" {
		t.Fatalf("expected original unit price 12.00, got %s", item.OriginalUnitPrice.String())
	}
	if !result.TotalAmount.Equal(decimal.NewFromInt(12)) {
		t.Fatalf("expected total 12.00, got %s", result.TotalAmount.String())
	}
}

func TestBuildOrderResultNoFeeWhenNotOptedIn(t *testing.T) {
	svc, product, sku := buildCardCheckPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, CardCheckEnabled: false},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult error: %v", err)
	}
	item := result.Plans[0].Item
	if item.CardCheckEnabled {
		t.Fatalf("expected card check disabled when user did not opt in")
	}
	if item.UnitPrice.String() != "10.00" {
		t.Fatalf("expected unit price 10.00 without fee, got %s", item.UnitPrice.String())
	}
	if !result.TotalAmount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected total 10.00, got %s", result.TotalAmount.String())
	}
}

func TestBuildOrderResultFeeIgnoredWhenProductUnsupported(t *testing.T) {
	dsn := fmt.Sprintf("file:cardcheck_unsupported_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &promotiondomain.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := categorydomain.Category{
		Slug:      fmt.Sprintf("cc-cat-u-%d", now.UnixNano()),
		NameJSON:  jsonmap.JSON{"zh-CN": "测活分类"},
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	// 商品未开启测活
	product := productdomain.Product{
		CategoryID:      category.ID,
		Slug:            fmt.Sprintf("cc-prod-u-%d", now.UnixNano()),
		TitleJSON:       jsonmap.JSON{"zh-CN": "普通商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	sku := productdomain.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     productdomain.DefaultSKUCode,
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductStore:    productgormstore.NewProductStore(db),
		ProductSKUStore: productgormstore.NewSKUStore(db),
		PromotionRepo:   promotiongormstore.New(db),
		ExpireMinutes:   15,
	})
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, CardCheckEnabled: true},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult error: %v", err)
	}
	item := result.Plans[0].Item
	if item.CardCheckEnabled {
		t.Fatalf("expected card check disabled when product unsupported")
	}
	if item.UnitPrice.String() != "10.00" {
		t.Fatalf("expected unit price 10.00 without fee, got %s", item.UnitPrice.String())
	}
}
