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

// buildPickPricingOrderService 构造带挑卡加价表（sqlite）的订单服务。
func buildPickPricingOrderService(t *testing.T) (*OrderService, productdomain.Product, productdomain.ProductSKU) {
	t.Helper()
	dsn := fmt.Sprintf("file:pick_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &promotiondomain.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := categorydomain.Category{
		Slug:      fmt.Sprintf("pick-cat-%d", now.UnixNano()),
		NameJSON:  jsonmap.JSON{"zh-CN": "挑卡分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := productdomain.Product{
		CategoryID:       category.ID,
		Slug:             fmt.Sprintf("pick-prod-%d", now.UnixNano()),
		TitleJSON:        jsonmap.JSON{"zh-CN": "挑卡商品"},
		PriceAmount:      money.FromDecimal(decimal.NewFromInt(10)),
		PurchaseType:     constants.ProductPurchaseMember,
		FulfillmentType:  constants.FulfillmentTypeAuto,
		PickEnabled:      true,
		PickPrices:       jsonmap.JSON{"visa": "1.00", "mastercard": "2.00", "D": "0.50", "head4": "3.00"},
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

func TestBuildOrderResultPickSurchargeAppliedAndSnapshotted(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{
				ProductID:     product.ID,
				SKUID:         sku.ID,
				Quantity:      2,
				PickCountry:   "US",
				PickBrands:    []string{"visa"},
				PickCardTypes: []string{"D"},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult error: %v", err)
	}
	if len(result.Plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(result.Plans))
	}
	item := result.Plans[0].Item
	// 基础 10 + 品牌 visa 1.00 + 种类 D 0.50 = 11.50
	if item.UnitPrice.String() != "11.50" {
		t.Fatalf("expected unit price 11.50, got %s", item.UnitPrice.String())
	}
	if item.OriginalUnitPrice.String() != "11.50" {
		t.Fatalf("expected original unit price 11.50, got %s", item.OriginalUnitPrice.String())
	}
	if item.PickCountry != "US" {
		t.Fatalf("expected pick country US, got %q", item.PickCountry)
	}
	if len(item.PickBrands) != 1 || item.PickBrands[0] != "visa" {
		t.Fatalf("unexpected pick brands: %v", item.PickBrands)
	}
	if !result.TotalAmount.Equal(decimal.RequireFromString("23.00")) {
		t.Fatalf("expected total 23.00, got %s", result.TotalAmount.String())
	}
}

func TestBuildOrderResultPickSurchargeMaxPerGroup(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{
				ProductID:     product.ID,
				SKUID:         sku.ID,
				Quantity:      1,
				PickCountry:   "US",
				PickBrands:    []string{"visa", "mastercard"},
				PickCardTypes: []string{},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult error: %v", err)
	}
	item := result.Plans[0].Item
	// 品牌组多选只按最大值计一次：max(visa 1.00, mastercard 2.00) = 2.00
	if item.UnitPrice.String() != "12.00" {
		t.Fatalf("expected unit price 12.00, got %s", item.UnitPrice.String())
	}
}

func TestBuildOrderResultPickCountryRequiredWhenEnabled(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBrands: []string{"visa"}},
		},
	})
	if err != ErrProductPickModeRequired {
		t.Fatalf("expected ErrProductPickModeRequired, got %v", err)
	}
}

func TestBuildOrderResultPickCountryInvalid(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickCountry: "USA"},
		},
	})
	if err != ErrProductPickCountryInvalid {
		t.Fatalf("expected ErrProductPickCountryInvalid, got %v", err)
	}
}

func TestBuildOrderResultPickBrandInvalid(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickCountry: "US", PickBrands: []string{"invalid_brand"}},
		},
	})
	if err != ErrProductPickBrandInvalid {
		t.Fatalf("expected ErrProductPickBrandInvalid, got %v", err)
	}
}

func TestBuildOrderResultPickRejectedWhenProductUnsupported(t *testing.T) {
	dsn := fmt.Sprintf("file:pick_unsupported_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &promotiondomain.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := categorydomain.Category{
		Slug:      fmt.Sprintf("pick-cat-u-%d", now.UnixNano()),
		NameJSON:  jsonmap.JSON{"zh-CN": "普通分类"},
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}
	product := productdomain.Product{
		CategoryID:      category.ID,
		Slug:            fmt.Sprintf("pick-prod-u-%d", now.UnixNano()),
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
	_, err = svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickCountry: "US"},
		},
	})
	if err != ErrProductPickNotSupported {
		t.Fatalf("expected ErrProductPickNotSupported, got %v", err)
	}
}

func TestBuildOrderResultPickBinAccepted(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "414720"},
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.OrderItems[0].PickBin != "414720" {
		t.Fatalf("expected pick_bin 414720, got %q", result.OrderItems[0].PickBin)
	}
	if result.OrderItems[0].PickCountry != "" {
		t.Fatalf("expected empty pick_country for bin mode, got %q", result.OrderItems[0].PickCountry)
	}
}

func TestBuildOrderResultPickBinInvalid(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "4147ABC"},
		},
	})
	if err != ErrProductPickBinInvalid {
		t.Fatalf("expected ErrProductPickBinInvalid, got %v", err)
	}
}

func TestBuildOrderResultPickBinConflictWithCountry(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "414720", PickCountry: "US"},
		},
	})
	if err != ErrProductPickBinConflict {
		t.Fatalf("expected ErrProductPickBinConflict, got %v", err)
	}
}

// TestBuildOrderResultPickBinHeadSingleDigitWithCountryAllowed 验证 1 位首位挑卡（3头/4头/5头/6头）
// 可与国家共存，不触发 ErrProductPickBinConflict。
func TestBuildOrderResultPickBinHeadSingleDigitWithCountryAllowed(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	// PickBin="4"（4头）+ PickCountry="US" 应被允许，而非冲突。
	_, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "4", PickCountry: "US"},
		},
	})
	if err == ErrProductPickBinConflict {
		t.Fatalf("single-digit head BIN with country must NOT conflict, got %v", err)
	}
	// 其余错误（如无库存）可接受，只要不是冲突错误。
}

// TestBuildOrderResultPickBinHeadSurcharge 验证首位挑卡按 headN 取加价。
func TestBuildOrderResultPickBinHeadSurcharge(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	// 商品 PickPrices 含 head4=3.00；4头（PickBin="4"）应取 head4 加价。
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "4"},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult: %v", err)
	}
	item := result.Plans[0].Item
	// base 10.00 + head4 3.00 = 13.00
	if got := item.UnitPrice.String(); got != "13.00" {
		t.Fatalf("expected unit price 13.00, got %s", got)
	}
}

// TestBuildOrderResultPickHeadAndCardTypeSurcharge 验证网页端「挑卡种类」模式提交
// 首位(PickBin=1位)+国家+种类(PickCardTypes) 的组合加价：head4 3.00 + D 0.50 = base 10.00 + 3.50 = 13.50。
// 这正是网页端 useProductDetail 在 type 模式选中首位与 DEBIT 时的提交结构。
func TestBuildOrderResultPickHeadAndCardTypeSurcharge(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "4", PickCountry: "US", PickCardTypes: []string{"D"}},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult: %v", err)
	}
	item := result.Plans[0].Item
	// base 10.00 + head4 3.00 + D 0.50 = 13.50
	if got := item.UnitPrice.String(); got != "13.50" {
		t.Fatalf("expected unit price 13.50 (head4 + D), got %s", got)
	}
	if item.PickBin != "4" {
		t.Fatalf("expected pick_bin 4, got %q", item.PickBin)
	}
	if item.PickCountry != "US" {
		t.Fatalf("expected pick_country US, got %q", item.PickCountry)
	}
}

// TestBuildOrderResultPickHeadWithEmptyBrandsValid 验证首位挑卡提交空品牌列表
// 不报错（网页端 buildItemPayload 恒提交 pickBrands=[]），防止后端回归要求品牌。
func TestBuildOrderResultPickHeadWithEmptyBrandsValid(t *testing.T) {
	svc, product, sku := buildPickPricingOrderService(t)
	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{ProductID: product.ID, SKUID: sku.ID, Quantity: 1, PickBin: "4", PickCountry: "US", PickBrands: []string{}, PickCardTypes: []string{}},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult with empty brands: %v", err)
	}
	if result.Plans[0].Item.PickBin != "4" {
		t.Fatalf("expected pick_bin 4, got %q", result.Plans[0].Item.PickBin)
	}
}
