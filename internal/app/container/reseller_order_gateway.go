package container

import (
	"context"
	"errors"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	reseller "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
)

// orderWholesaleGateway 将分销批发采购端口适配到订单用例。
// 订单用例以主站代理中心批发采购租户上下文计价（渠道价），买家即分销商本人。
type orderWholesaleGateway struct {
	order *orderapp.OrderService
}

func newOrderWholesaleGateway(order *orderapp.OrderService) resellercontract.WholesaleOrderGateway {
	return &orderWholesaleGateway{order: order}
}

func mapWholesaleOrderItems(items []resellercontract.WholesaleItemInput) []orderapp.CreateOrderItem {
	out := make([]orderapp.CreateOrderItem, 0, len(items))
	for _, item := range items {
		out = append(out, orderapp.CreateOrderItem{
			ProductID:        item.ProductID,
			SKUID:            item.SKUID,
			Quantity:         item.Quantity,
			FulfillmentType:  item.FulfillmentType,
			CardCheckEnabled: item.CardCheckEnabled,
		})
	}
	return out
}

func (g orderWholesaleGateway) Preview(ctx context.Context, input resellercontract.WholesaleOrderInput) (*resellercontract.WholesalePreview, error) {
	if g.order == nil {
		return nil, errors.New("reseller wholesale order service unavailable")
	}
	preview, err := g.order.PreviewOrder(orderapp.CreateOrderInput{
		UserID:   input.UserID,
		Tenant:   input.Tenant,
		Items:    mapWholesaleOrderItems(input.Items),
		ClientIP: input.ClientIP,
	})
	if err != nil {
		return nil, err
	}
	out := &resellercontract.WholesalePreview{
		Currency:       preview.Currency,
		TotalAmount:    preview.TotalAmount.Decimal,
		OriginalAmount: preview.OriginalAmount.Decimal,
		Items:          make([]resellercontract.WholesalePreviewItem, 0, len(preview.Items)),
	}
	for _, item := range preview.Items {
		out.Items = append(out.Items, resellercontract.WholesalePreviewItem{
			ProductID:        item.ProductID,
			SKUID:            item.SKUID,
			Quantity:         item.Quantity,
			UnitPrice:        item.UnitPrice.Decimal,
			TotalPrice:       item.TotalPrice.Decimal,
			CardCheckEnabled: item.CardCheckEnabled,
		})
	}
	return out, nil
}

func (g orderWholesaleGateway) Create(ctx context.Context, input resellercontract.WholesaleOrderInput) (*resellercontract.WholesaleCreated, error) {
	if g.order == nil {
		return nil, errors.New("reseller wholesale order service unavailable")
	}
	order, err := g.order.CreateOrder(orderapp.CreateOrderInput{
		UserID:   input.UserID,
		Tenant:   input.Tenant,
		Items:    mapWholesaleOrderItems(input.Items),
		ClientIP: input.ClientIP,
	})
	if err != nil {
		return nil, err
	}
	return &resellercontract.WholesaleCreated{
		OrderID:     order.ID,
		OrderNo:     order.OrderNo,
		Currency:    order.Currency,
		TotalAmount: order.TotalAmount.Decimal,
	}, nil
}

// initResellerWholesale 装配批发采购网关与用例，需在 OrderService 就绪之后调用。
func (c *Container) initResellerWholesale() {
	c.ResellerWholesaleGateway = newOrderWholesaleGateway(c.OrderService)
	c.ResellerPurchaseService = reseller.NewPurchaseService(
		c.ResellerStore,
		c.ResellerProductSettingService,
		c.ResellerWholesaleGateway,
	)
}
