package integrationtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	cardsecretapp "github.com/dujiao-next/internal/modules/cardsecret/application"
	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cardsecretgormstore "github.com/dujiao-next/internal/modules/cardsecret/infrastructure/gormstore"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	"github.com/dujiao-next/internal/platform/database/gormdb"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const binCSVContent = `BIN,Brand,Type,Category,Issuer,IssuerPhone,IssuerUrl,isoCode2,isoCode3,CountryName
429531,VISA,DEBIT,CLASSIC,"SCOTT CREDIT UNION",+16183451000,https://www.scu.org,US,USA,"UNITED STATES"
411111,VISA,CREDIT,CLASSIC,"TEST BANK",,https://x.com,GB,GBR,"UNITED KINGDOM"
550000,MASTERCARD,PREPAID,GIFT,"GIFT CO",,https://x.com,CA,CAN,"CANADA"
601100,DISCOVER,DEBIT,CLASSIC,"D BANK",,https://x.com,AU,AUS,"AUSTRALIA"
999999,AMEX,CHARGE,PLATINUM,"AMEX",,https://x.com,DE,DEU,"GERMANY"
440000,VISA,DEBIT,"PREPAID CLASSIC","P BANK",,https://x.com,CA,CAN,"CANADA"
445555,VISA,DEBIT,PLATINUM,"N BANK",,https://x.com,US,USA,"UNITED STATES"
`

func setupBinServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:card_bin_service_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&cardsecretdomain.CardBin{},
		&productdomain.Product{},
		&productdomain.ProductSKU{},
		&cardsecretdomain.Batch{},
		&cardsecretdomain.Secret{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	gormdb.DB = db
	return db
}

func newBinService(db *gorm.DB) *cardsecretapp.Service {
	return cardsecretapp.NewService(cardsecretapp.ServiceOptions{
		Secrets:      cardsecretgormstore.New(db),
		Batches:      cardsecretgormstore.NewBatch(db),
		Transactions: cardsecretgormstore.New(db),
		Products:     productgormstore.NewProductStore(db),
		ProductSKUs:  productgormstore.NewSKUStore(db),
		Bins:         cardsecretgormstore.NewBin(db),
	})
}

func TestImportCardBinsParsesRealHeaderAndNormalizes(t *testing.T) {
	db := setupBinServiceTestDB(t)
	svc := newBinService(db)

	file := newCardSecretCSVFileHeader(t, binCSVContent)
	result, err := svc.ImportCardBins(cardsecretapp.ImportCardBinsInput{File: file})
	if err != nil {
		t.Fatalf("import card bins failed: %v", err)
	}
	if result.Total != 7 || result.Inserted != 7 {
		t.Fatalf("expected 7 imported bins, got total=%d inserted=%d", result.Total, result.Inserted)
	}

	expected := map[string]cardsecretdomain.CardBin{
		"429531": {BIN: "429531", Country: "US", Brand: cardsecretdomain.PickBrandVisa, CardType: cardsecretdomain.CardTypePD},
		"411111": {BIN: "411111", Country: "GB", Brand: cardsecretdomain.PickBrandVisa, CardType: cardsecretdomain.CardTypeC},
		"550000": {BIN: "550000", Country: "CA", Brand: cardsecretdomain.PickBrandMastercard, CardType: cardsecretdomain.CardTypeD},
		"601100": {BIN: "601100", Country: "AU", Brand: cardsecretdomain.PickBrandDiscover, CardType: cardsecretdomain.CardTypePD},
		"999999": {BIN: "999999", Country: "DE", Brand: cardsecretdomain.PickBrandAmex, CardType: cardsecretdomain.CardTypeC},
		"440000": {BIN: "440000", Country: "CA", Brand: cardsecretdomain.PickBrandVisa, CardType: cardsecretdomain.CardTypeD},
		"445555": {BIN: "445555", Country: "US", Brand: cardsecretdomain.PickBrandVisa, CardType: cardsecretdomain.CardTypePD},
	}
	rows, _, err := svc.ListCardBins(cardsecretcontract.CardBinFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list card bins failed: %v", err)
	}
	got := make(map[string]cardsecretdomain.CardBin, len(rows))
	for _, row := range rows {
		got[row.BIN] = row
	}
	for bin, want := range expected {
		row, ok := got[bin]
		if !ok {
			t.Fatalf("bin %s missing after import", bin)
		}
		if row.Country != want.Country || row.Brand != want.Brand || row.CardType != want.CardType {
			t.Errorf("bin %s mismatch: want %+v got %+v", bin, want, row)
		}
	}
}

func TestCreateCardSecretBatchAnnotatesFromBinLibrary(t *testing.T) {
	db := setupBinServiceTestDB(t)
	svc := newBinService(db)

	file := newCardSecretCSVFileHeader(t, binCSVContent)
	if _, err := svc.ImportCardBins(cardsecretapp.ImportCardBinsInput{File: file}); err != nil {
		t.Fatalf("import card bins failed: %v", err)
	}

	product := &productdomain.Product{
		CategoryID:      1,
		Slug:            "card-secret-pick-annotate",
		TitleJSON:       jsonmap.JSON{"zh-CN": "挑卡商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(20)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	sku := &productdomain.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     productdomain.DefaultSKUCode,
		PriceAmount: money.FromDecimal(decimal.NewFromInt(20)),
		IsActive:    true,
	}
	if err := db.Create(sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	_, _, err := svc.CreateCardSecretBatch(cardsecretapp.CreateCardSecretBatchInput{
		ProductID: product.ID,
		SKUID:     sku.ID,
		Secrets: []string{
			"4295310100100010|12|2027|123",
			"9999990100100010|12|2027|123",
			"NOT-A-CARD",
		},
		Source: constants.CardSecretSourceManual,
		AdminID: 1,
	})
	if err != nil {
		t.Fatalf("create card secret batch failed: %v", err)
	}

	rows, _, err := cardsecretgormstore.New(db).List(cardsecretcontract.ListFilter{
		ProductID: product.ID,
		SKUID:     sku.ID,
		PageSize:  50,
	})
	if err != nil {
		t.Fatalf("list secrets failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(rows))
	}
	bySecret := map[string]cardsecretdomain.Secret{}
	for _, row := range rows {
		bySecret[row.Secret] = row
	}
	visa := bySecret["4295310100100010|12|2027|123"]
	if visa.Country != "US" || visa.Brand != cardsecretdomain.PickBrandVisa || visa.CardType != cardsecretdomain.CardTypePD {
		t.Errorf("visa card annotation mismatch: %+v", visa)
	}
	amex := bySecret["9999990100100010|12|2027|123"]
	if amex.Country != "DE" || amex.Brand != cardsecretdomain.PickBrandAmex || amex.CardType != cardsecretdomain.CardTypeC {
		t.Errorf("amex card annotation mismatch: %+v", amex)
	}
	unmatched := bySecret["NOT-A-CARD"]
	if unmatched.Country != "" || unmatched.Brand != "" || unmatched.CardType != "" {
		t.Errorf("unmatched card should stay unannotated: %+v", unmatched)
	}
}

func TestListAvailableByProductFilteredMatchesPickFilter(t *testing.T) {
	db := setupBinServiceTestDB(t)
	svc := newBinService(db)

	file := newCardSecretCSVFileHeader(t, binCSVContent)
	if _, err := svc.ImportCardBins(cardsecretapp.ImportCardBinsInput{File: file}); err != nil {
		t.Fatalf("import card bins failed: %v", err)
	}

	product := &productdomain.Product{
		CategoryID:      1,
		Slug:            "card-secret-pick-filter",
		TitleJSON:       jsonmap.JSON{"zh-CN": "挑卡商品"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(20)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}
	sku := &productdomain.ProductSKU{
		ProductID:   product.ID,
		SKUCode:     productdomain.DefaultSKUCode,
		PriceAmount: money.FromDecimal(decimal.NewFromInt(20)),
		IsActive:    true,
	}
	if err := db.Create(sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	secrets := []string{
		"4295310100100010|12|2027|123", // US visa PD
		"5500000100100010|12|2027|123", // CA mastercard D
		"6011000100100010|12|2027|123", // AU discover PD
	}
	if _, _, err := svc.CreateCardSecretBatch(cardsecretapp.CreateCardSecretBatchInput{
		ProductID: product.ID,
		SKUID:     sku.ID,
		Secrets:   secrets,
		Source:    constants.CardSecretSourceManual,
		AdminID:   1,
	}); err != nil {
		t.Fatalf("create card secret batch failed: %v", err)
	}

	store := cardsecretgormstore.New(db)
	rows, err := store.ListAvailableByProductFiltered(product.ID, sku.ID, cardsecretcontract.PickFilter{
		Country: "US",
	}, 10)
	if err != nil {
		t.Fatalf("filtered list failed: %v", err)
	}
	if len(rows) != 1 || rows[0].Country != "US" {
		t.Fatalf("expected only the US card, got %d", len(rows))
	}

	count, err := store.CountAvailableByProductFiltered(product.ID, sku.ID, cardsecretcontract.PickFilter{
		Country:   "CA",
		Brands:    []string{cardsecretdomain.PickBrandMastercard},
		CardTypes: []string{cardsecretdomain.CardTypeD},
	})
	if err != nil {
		t.Fatalf("filtered count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 CA mastercard D, got %d", count)
	}

	attrs, err := store.CountPickAttrs(product.ID)
	if err != nil {
		t.Fatalf("count pick attrs failed: %v", err)
	}
	if len(attrs) != 3 {
		t.Fatalf("expected 3 pick attr rows, got %d", len(attrs))
	}
}

// TestCountByBinHeadAggregatesByFirstDigit 验证按卡号首位聚合库存。
func TestCountByBinHeadAggregatesByFirstDigit(t *testing.T) {
	db := setupBinServiceTestDB(t)
	store := cardsecretgormstore.New(db)
	product := &productdomain.Product{CategoryID: 1, Slug: "bin-head-prod", TitleJSON: jsonmap.JSON{"zh-CN": "x"}, PriceAmount: money.FromDecimal(decimal.NewFromInt(1)), PurchaseType: constants.ProductPurchaseMember, FulfillmentType: constants.FulfillmentTypeAuto, IsActive: true}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	now := time.Now()
	secrets := []cardsecretdomain.Secret{
		{ProductID: product.ID, Secret: "a", Status: cardsecretdomain.StatusAvailable, BinPrefix: "411111", CreatedAt: now},
		{ProductID: product.ID, Secret: "b", Status: cardsecretdomain.StatusAvailable, BinPrefix: "422222", CreatedAt: now},
		{ProductID: product.ID, Secret: "c", Status: cardsecretdomain.StatusAvailable, BinPrefix: "511111", CreatedAt: now},
		{ProductID: product.ID, Secret: "d", Status: cardsecretdomain.StatusAvailable, BinPrefix: "611111", CreatedAt: now},
		{ProductID: product.ID, Secret: "e", Status: cardsecretdomain.StatusAvailable, BinPrefix: "622222", CreatedAt: now},
		{ProductID: product.ID, Secret: "f", Status: cardsecretdomain.StatusAvailable, BinPrefix: "", CreatedAt: now}, // 空 bin_prefix 应被跳过
	}
	for i := range secrets {
		if err := db.Create(&secrets[i]).Error; err != nil {
			t.Fatalf("create secret: %v", err)
		}
	}
	heads, err := store.CountByBinHead(product.ID)
	if err != nil {
		t.Fatalf("CountByBinHead: %v", err)
	}
	got := map[string]int64{}
	for _, h := range heads {
		got[h.Head] = h.Total
	}
	if got["4"] != 2 {
		t.Errorf("head 4: want 2, got %d", got["4"])
	}
	if got["5"] != 1 {
		t.Errorf("head 5: want 1, got %d", got["5"])
	}
	if got["6"] != 2 {
		t.Errorf("head 6: want 2, got %d", got["6"])
	}

	// 首位前缀 LIKE 匹配：4 头应匹配 2 张。
	count4, err := store.CountAvailableByProductFiltered(product.ID, 0, cardsecretcontract.PickFilter{BinPrefix: "4"})
	if err != nil {
		t.Fatalf("count head 4: %v", err)
	}
	if count4 != 2 {
		t.Errorf("LIKE 4%%: want 2, got %d", count4)
	}
	// 6 位精确匹配：411111 应匹配 1 张。
	countExact, err := store.CountAvailableByProductFiltered(product.ID, 0, cardsecretcontract.PickFilter{BinPrefix: "411111"})
	if err != nil {
		t.Fatalf("count exact: %v", err)
	}
	if countExact != 1 {
		t.Errorf("exact 411111: want 1, got %d", countExact)
	}
}
