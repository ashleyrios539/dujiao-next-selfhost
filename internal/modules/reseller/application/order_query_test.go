package application

import (
	"errors"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestNeutralProfitStatusUnavailableWhenIneligible(t *testing.T) {
	status := neutralProfitStatus(resellerdomain.OrderSnapshot{
		ProfitEligible: false,
		ProfitAmount:   money.FromDecimal(decimal.NewFromInt(10)),
	}, orderdomain.Order{Status: constants.OrderStatusPaid}, nil)
	if status != resellercontract.ProfitStatusUnavailable {
		t.Fatalf("expected unavailable, got %s", status)
	}
}

func TestMaskBuyerEmail(t *testing.T) {
	if got := maskBuyerEmail("ashang@example.com"); got != "a***@example.com" {
		t.Fatalf("unexpected mask: %s", got)
	}
}

func TestOrderQueryServiceRejectsInactiveProfile(t *testing.T) {
	svc := NewOrderQueryService(orderQueryStoreStub{profile: &resellerdomain.Profile{
		ID:     1,
		UserID: 9,
		Status: resellerdomain.ProfileStatusDisabled,
	}})
	_, _, err := svc.ListUserOrders(9, resellercontract.OrderListInput{Page: 1, PageSize: 10})
	if err != resellercontract.ErrProfileInactive {
		t.Fatalf("expected profile inactive, got %v", err)
	}
}

type orderQueryStoreStub struct {
	profile *resellerdomain.Profile
}

func (s orderQueryStoreStub) GetProfileByUserID(userID uint) (*resellerdomain.Profile, error) {
	return s.profile, nil
}
func (s orderQueryStoreStub) GetProfileByID(id uint) (*resellerdomain.Profile, error) {
	return s.profile, nil
}
func (s orderQueryStoreStub) ListOrderSnapshotsByReseller(filter resellercontract.OrderSnapshotListFilter) ([]resellercontract.OrderSnapshotRow, int64, error) {
	return nil, 0, nil
}
func (s orderQueryStoreStub) StatsOrderSnapshotsByReseller(filter resellercontract.OrderSnapshotListFilter) (resellercontract.OrderStatsRow, error) {
	return resellercontract.OrderStatsRow{}, nil
}
func (s orderQueryStoreStub) GetOrderSnapshotByResellerOrderNo(resellerID uint, orderNo string) (*resellercontract.OrderSnapshotRow, error) {
	return nil, nil
}

func TestResolveChannelPriceModeUsesChannelPriceAmount(t *testing.T) {
	setting := resellerdomain.ProductSetting{
		PricingMode:        resellerdomain.PricingModeChannelPrice,
		ChannelPriceAmount: money.FromDecimal(decimal.NewFromInt(80)),
	}
	price, rule, err := ResolveUnitAmount(nil, &setting, nil, decimal.NewFromInt(100))
	if err != nil {
		t.Fatalf("resolve channel price failed: %v", err)
	}
	if rule.Mode != resellerdomain.PricingModeChannelPrice {
		t.Fatalf("expected channel_price rule mode, got %s", rule.Mode)
	}
	if !price.Equal(decimal.NewFromInt(80)) {
		t.Fatalf("expected channel price 80, got %s", price.String())
	}
}

func TestValidateChannelUnitAmountAllowsBelowRetailButNotBelowCost(t *testing.T) {
	sku := &productdomain.ProductSKU{CostPriceAmount: money.FromDecimal(decimal.NewFromInt(70))}
	// 渠道价 80 低于零售底价（如 100）是批发进货的常态，应通过。
	if err := ValidateChannelUnitAmount(sku, decimal.NewFromInt(80)); err != nil {
		t.Fatalf("channel price above cost should pass, got %v", err)
	}
	// 低于成本价应被拒绝。
	if err := ValidateChannelUnitAmount(sku, decimal.NewFromInt(69)); !errors.Is(err, resellercontract.ErrPriceBelowBase) {
		t.Fatalf("expected below-cost rejection, got %v", err)
	}
	// 非正数应被拒绝。
	if err := ValidateChannelUnitAmount(sku, decimal.Zero); !errors.Is(err, resellercontract.ErrPriceBelowBase) {
		t.Fatalf("expected non-positive rejection, got %v", err)
	}
	// 无成本价（成本为 0）时允许渠道价低于零售底价。
	if err := ValidateChannelUnitAmount(nil, decimal.NewFromInt(50)); err != nil {
		t.Fatalf("channel price without cost should pass, got %v", err)
	}
}
