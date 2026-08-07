package integrationtest

import (
	"strconv"
	"testing"

	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestProductServiceListPublicIncludesChildProductsForParentCategory(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	parent := categorydomain.Category{
		Slug:     "games",
		NameJSON: jsonmap.JSON{"zh-CN": "games"},
	}
	child := categorydomain.Category{
		ParentID: 1,
		Slug:     "steam",
		NameJSON: jsonmap.JSON{"zh-CN": "steam"},
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent category failed: %v", err)
	}
	child.ParentID = parent.ID
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category failed: %v", err)
	}

	parentProduct := productdomain.Product{
		CategoryID:  parent.ID,
		Slug:        "parent-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "parent-product"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	childProduct := productdomain.Product{
		CategoryID:  child.ID,
		Slug:        "child-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "child-product"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	if err := db.Create(&parentProduct).Error; err != nil {
		t.Fatalf("create parent product failed: %v", err)
	}
	if err := db.Create(&childProduct).Error; err != nil {
		t.Fatalf("create child product failed: %v", err)
	}

	products, total, err := svc.Read.ListPublic(strconv.FormatUint(uint64(parent.ID), 10), "", 1, 20)
	if err != nil {
		t.Fatalf("list public products failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}
}

func TestProductServiceListPublicSortOrderDescending(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "sort-test",
		NameJSON: jsonmap.JSON{"zh-CN": "sort-test"},
		IsActive: true,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	high := productdomain.Product{
		CategoryID:  category.ID,
		Slug:        "high-sort-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "high"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
		SortOrder:   100,
	}
	low := productdomain.Product{
		CategoryID:  category.ID,
		Slug:        "low-sort-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "low"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
		SortOrder:   1,
	}
	if err := db.Create(&high).Error; err != nil {
		t.Fatalf("create high sort product failed: %v", err)
	}
	if err := db.Create(&low).Error; err != nil {
		t.Fatalf("create low sort product failed: %v", err)
	}

	rows, total, err := svc.Read.ListPublic("", "", 1, 20)
	if err != nil {
		t.Fatalf("list public products failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 products, got %d", len(rows))
	}
	if rows[0].Slug != "high-sort-product" || rows[1].Slug != "low-sort-product" {
		t.Fatalf("expected high sort_order first, got %s then %s", rows[0].Slug, rows[1].Slug)
	}
}

func TestProductServiceListPublicSortsSKUsDescending(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "sku-sort-test",
		NameJSON: jsonmap.JSON{"zh-CN": "sku-sort-test"},
		IsActive: true,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := productdomain.Product{
		CategoryID:  category.ID,
		Slug:        "sku-order-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "sku-order-product"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
		SortOrder:   0,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	high := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "HIGH",
		SpecValuesJSON: jsonmap.JSON{},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      100,
	}
	low := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "LOW",
		SpecValuesJSON: jsonmap.JSON{},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      1,
	}
	if err := db.Create(&high).Error; err != nil {
		t.Fatalf("create high sort sku failed: %v", err)
	}
	if err := db.Create(&low).Error; err != nil {
		t.Fatalf("create low sort sku failed: %v", err)
	}

	rows, total, err := svc.Read.ListPublic("", "", 1, 20)
	if err != nil {
		t.Fatalf("list public products failed: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("expected exactly 1 product, total=%d len=%d", total, len(rows))
	}
	if len(rows[0].SKUs) != 2 {
		t.Fatalf("expected 2 skus, got %d", len(rows[0].SKUs))
	}
	if rows[0].SKUs[0].SKUCode != "HIGH" || rows[0].SKUs[1].SKUCode != "LOW" {
		t.Fatalf("expected high sort_order sku first, got %s then %s", rows[0].SKUs[0].SKUCode, rows[0].SKUs[1].SKUCode)
	}
}

func TestProductServiceGetAdminByIDIncludesInactiveSKUs(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	product := productdomain.Product{
		CategoryID:  1,
		Slug:        "admin-all-skus-product",
		TitleJSON:   jsonmap.JSON{"zh-CN": "admin-all-skus-product"},
		PriceAmount: money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:    true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	activeSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "ACTIVE",
		SpecValuesJSON: jsonmap.JSON{},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      10,
	}
	inactiveSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "INACTIVE",
		SpecValuesJSON: jsonmap.JSON{},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(20)),
		IsActive:       false,
		SortOrder:      1,
	}
	if err := db.Create(&activeSKU).Error; err != nil {
		t.Fatalf("create active sku failed: %v", err)
	}
	if err := db.Create(&inactiveSKU).Error; err != nil {
		t.Fatalf("create inactive sku failed: %v", err)
	}
	inactiveSKU.IsActive = false
	if err := db.Save(&inactiveSKU).Error; err != nil {
		t.Fatalf("persist inactive sku failed: %v", err)
	}

	got, err := svc.Read.GetAdminByID(strconv.FormatUint(uint64(product.ID), 10))
	if err != nil {
		t.Fatalf("get admin product failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected product, got nil")
	}
	if len(got.SKUs) != 2 {
		t.Fatalf("expected 2 skus for admin detail, got %d", len(got.SKUs))
	}
	if got.SKUs[0].SKUCode != "ACTIVE" || !got.SKUs[0].IsActive {
		t.Fatalf("expected first sku to be active ACTIVE, got %+v", got.SKUs[0])
	}
	if got.SKUs[1].SKUCode != "INACTIVE" || got.SKUs[1].IsActive {
		t.Fatalf("expected second sku to be inactive INACTIVE, got %+v", got.SKUs[1])
	}
}

// TestGetPublicBySlugForBotThenApplyAutoStockCounts 验证 bot 商品详情路径能拿到自动发货库存：
// GetPublicBySlugForBot 返回的商品 AutoStockAvailable(gorm:"-") 为 0（store 不填充），
// 必须在容器层再调用 ApplyAutoStockCounts 才会得到真实库存——回归 telegramPurchasePorts.GetProductBySlug 修复。
func TestGetPublicBySlugForBotThenApplyAutoStockCounts(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "bot-stock-cat", NameJSON: jsonmap.JSON{"zh-CN": "bot-stock-cat"}, IsActive: true,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "bot-stock-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "bot-stock-product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(1)),
		IsActive:        true,
		BotVisible:      true,
		FulfillmentType: constants.FulfillmentTypeAuto,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	sku := productdomain.ProductSKU{
		ProductID: product.ID, SKUCode: productdomain.DefaultSKUCode,
		SpecValuesJSON: jsonmap.JSON{}, PriceAmount: money.FromDecimal(decimal.NewFromInt(1)),
		IsActive: true,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku: %v", err)
	}
	// 插入 5 张可用卡密
	insertCardSecrets(t, db, product.ID, sku.ID, cardsecretdomain.StatusAvailable, 5)

	got, err := svc.Read.GetPublicBySlugForBot("bot-stock-product")
	if err != nil {
		t.Fatalf("GetPublicBySlugForBot: %v", err)
	}
	if got == nil {
		t.Fatal("expected product, got nil")
	}
	// store 不填充计算字段，直接拿到的 AutoStockAvailable 应为 0（这正是容器层修复要补的）。
	if got.AutoStockAvailable != 0 {
		t.Fatalf("pre-apply AutoStockAvailable want 0, got %d", got.AutoStockAvailable)
	}
	// 容器层修复：GetProductBySlug 取得 product 后再 ApplyAutoStockCounts。
	products := []productdomain.Product{*got}
	if err := svc.Read.ApplyAutoStockCounts(products); err != nil {
		t.Fatalf("ApplyAutoStockCounts: %v", err)
	}
	*got = products[0]
	if got.AutoStockAvailable != 5 {
		t.Fatalf("post-apply AutoStockAvailable want 5, got %d", got.AutoStockAvailable)
	}
}
