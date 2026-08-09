package container

import (
	catalogmappingbootstrap "github.com/dujiao-next/internal/bootstrap/catalogmapping"
	telegrambroadcast "github.com/dujiao-next/internal/bootstrap/telegrambroadcast"
	"github.com/dujiao-next/internal/logger"
	apicredentialapp "github.com/dujiao-next/internal/modules/apicredential/application"
	auditlogapp "github.com/dujiao-next/internal/modules/auditlog/application"
	channelclientapp "github.com/dujiao-next/internal/modules/channelclient/application"
	contentapp "github.com/dujiao-next/internal/modules/content/application"
	localfilestore "github.com/dujiao-next/internal/modules/content/infrastructure/filestore/local"
	contentgormstore "github.com/dujiao-next/internal/modules/content/infrastructure/gormstore"
	dashboardapp "github.com/dujiao-next/internal/modules/dashboard/application"
	downstreamcallbackapp "github.com/dujiao-next/internal/modules/downstreamcallback/application"
	downstreamcallbackcontract "github.com/dujiao-next/internal/modules/downstreamcallback/contract"
	downstreamcallbackclient "github.com/dujiao-next/internal/modules/downstreamcallback/infrastructure/callbackclient"
	downstreamcallbackcredentialreader "github.com/dujiao-next/internal/modules/downstreamcallback/infrastructure/credentialreader"
	downstreamcallbackorderreader "github.com/dujiao-next/internal/modules/downstreamcallback/infrastructure/orderreader"
	downstreamcallbackqueue "github.com/dujiao-next/internal/modules/downstreamcallback/infrastructure/queueadapter"
	notificationapp "github.com/dujiao-next/internal/modules/notification/application"
	notificationasyncqueue "github.com/dujiao-next/internal/modules/notification/infrastructure/asyncqueue"
	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentqueue "github.com/dujiao-next/internal/modules/payment/infrastructure/queueadapter"
	procurementapp "github.com/dujiao-next/internal/modules/procurement/application"
	procurementmapping "github.com/dujiao-next/internal/modules/procurement/infrastructure/mappingreader"
	procurementnotification "github.com/dujiao-next/internal/modules/procurement/infrastructure/notificationadapter"
	procurementorder "github.com/dujiao-next/internal/modules/procurement/infrastructure/orderreader"
	procurementqueue "github.com/dujiao-next/internal/modules/procurement/infrastructure/queueadapter"
	procurementupstream "github.com/dujiao-next/internal/modules/procurement/infrastructure/upstreamgateway"
	reconciliationapp "github.com/dujiao-next/internal/modules/reconciliation/application"
	reconciliationnotification "github.com/dujiao-next/internal/modules/reconciliation/infrastructure/notificationadapter"
	reconciliationprocurement "github.com/dujiao-next/internal/modules/reconciliation/infrastructure/procurementreader"
	reconciliationqueue "github.com/dujiao-next/internal/modules/reconciliation/infrastructure/queueadapter"
	reconciliationupstream "github.com/dujiao-next/internal/modules/reconciliation/infrastructure/upstreamreader"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	broadcastapp "github.com/dujiao-next/internal/modules/telegram/broadcast/application"
	notifyapp "github.com/dujiao-next/internal/modules/telegram/notify/application"
	webhookapp "github.com/dujiao-next/internal/modules/telegram/webhook/application"
	webhookcontract "github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	notifybotapi "github.com/dujiao-next/internal/modules/telegram/notify/infrastructure/botapi"
	webhookinfra "github.com/dujiao-next/internal/modules/telegram/webhook/infrastructure"
	"github.com/dujiao-next/internal/platform/database/gormdb"
)

// initIntegrationServices è£…é…é€šçŸ¥ã€ç«™ç‚¹å¯¹æŽ¥ã€æ”¯ä»˜ã€é‡‡è´­ã€æ¸ é“ä¸Ž Telegram é›†æˆã€‚
func (c *Container) initIntegrationServices() {
	c.UserLoginLogService = auditlogapp.NewUserLoginService(c.UserLoginLogRepo)
	c.AuthzAuditService = auditlogapp.NewAuthzService(c.AuthzAuditLogRepo)
	c.AdminLoginLogService = auditlogapp.NewAdminLoginService(c.AdminLoginLogRepo)
	c.NotificationLogService = notificationapp.NewLogService(c.NotificationLogRepo)
	c.DashboardService = dashboardapp.NewService(c.DashboardRepo, c.SettingService)
	telegramNotifyService := notifyapp.NewService(c.SettingService, c.Config.TelegramAuth, notifybotapi.New())
	c.NotificationService = notificationapp.NewService(
		c.SettingService,
		c.EmailSender,
		notificationasyncqueue.New(c.QueueClient),
		c.DashboardService,
		c.NotificationLogService,
		telegramNotifyService,
	)
	c.ApiCredentialService = apicredentialapp.NewService(c.ApiCredentialRepo)
	c.SiteConnectionService = siteconnectionapp.NewService(c.SiteConnectionRepo, c.Config.App.SecretKey, "uploads")
	mediaCore := contentapp.NewMediaService(
		contentgormstore.NewMediaStore(gormdb.DB),
		localfilestore.New(),
		contentapp.WarningLoggerFunc(logger.Warnw),
	)
	c.ContentMediaService = mediaCore
	productMappingService, err := catalogmappingbootstrap.New(catalogmappingbootstrap.Dependencies{
		Mappings:    c.ProductMappingRepo,
		SKUMappings: c.SKUMappingRepo,
		Products:    c.ProductRepo,
		SKUs:        c.ProductSKURepo,
		Categories:  c.CategoryRepo,
		Connections: c.SiteConnectionService,
		Media:       mediaCore,
	})
	if err != nil {
		logger.Errorw("provider_init_product_mapping_failed", "error", err)
		panic(err)
	}
	c.ProductMappingService = productMappingService
	c.ProductMappingService.SetCategoryCreator(c.CategoryService)
	c.ProductMappingService.SetSettings(c.SettingService)
	c.SiteConnectionService.SetMarkupReapplier(c.ProductMappingService)
	c.OrderService.SetProductMappingService(c.ProductMappingService)
	var downstreamQueue downstreamcallbackcontract.CallbackQueue
	if c.QueueClient != nil {
		downstreamQueue = downstreamcallbackqueue.New(c.QueueClient)
	}
	c.DownstreamCallbackService = downstreamcallbackapp.NewService(downstreamcallbackapp.Options{
		References:  c.DownstreamOrderRefRepo,
		Orders:      downstreamcallbackorderreader.New(c.OrderStore),
		Credentials: downstreamcallbackcredentialreader.New(c.ApiCredentialRepo),
		Queue:       downstreamQueue,
		Deliverer:   downstreamcallbackclient.New(),
	})
	c.PaymentService = paymentapp.NewPaymentService(paymentapp.PaymentServiceOptions{
		OrderStore:              c.OrderStore,
		ProductRepo:             c.ProductRepo,
		ProductSKURepo:          c.ProductSKURepo,
		PaymentStore:            c.PaymentStore,
		ChannelStore:            c.PaymentChannelStore,
		WalletRepo:              c.WalletRepo,
		UserStore:               c.UserStore,
		ExternalIdentityStore:   c.ExternalIdentityStore,
		Queue:                   paymentqueue.New(c.QueueClient),
		WalletService:           c.WalletService,
		SettingService:          c.SettingService,
		DefaultEmailConfig:      c.Config.Email,
		ExpireMinutes:           c.Config.Order.PaymentExpireMinutes,
		AffiliateService:        c.AffiliateService,
		NotificationService:     c.NotificationService,
		PaymentProviderRegistry: c.PaymentProviderRegistry,
		ResellerAccounting:      c.ResellerAccountingLedger,
	})
	c.ProcurementOrderService = procurementapp.NewService(procurementapp.Options{
		Repository:         c.ProcurementOrderRepo,
		Orders:             procurementorder.New(c.OrderStore),
		ProductMappings:    procurementmapping.NewProducts(c.ProductMappingRepo),
		SKUMappings:        procurementmapping.NewSKUs(c.SKUMappingRepo),
		Connections:        procurementupstream.New(c.SiteConnectionService),
		Queue:              procurementqueue.New(c.QueueClient),
		OrderLifecycle:     c.ProcurementOrderRepo.NewLifecycle(c.QueueClient, c.SettingService, c.Config.Email),
		DownstreamCallback: c.DownstreamCallbackService,
		BotNotifier:        c.FulfillmentService,
		Notifications:      procurementnotification.New(c.NotificationService),
	})
	c.ReconciliationService = reconciliationapp.NewService(reconciliationapp.Options{
		Jobs: c.ReconciliationJobRepo, Items: c.ReconciliationItemRepo,
		Procurements:  reconciliationprocurement.New(c.ProcurementOrderRepo),
		Upstream:      reconciliationupstream.New(c.SiteConnectionService),
		Queue:         reconciliationqueue.New(c.QueueClient),
		Notifications: reconciliationnotification.New(c.NotificationService),
	})
	c.ChannelClientService = channelclientapp.NewService(c.ChannelClientStore, c.Config.App.SecretKey)
	c.TelegramBroadcastService = broadcastapp.NewService(
		c.TelegramBroadcastRepo,
		telegrambroadcast.NewUserDirectory(c.ExternalIdentityStore),
		telegrambroadcast.NewBotTokenResolver(c.ChannelClientService),
		telegrambroadcast.NewDispatcher(c.QueueClient),
		telegramNotifyService,
	)

	c.TelegramWebhookService = webhookapp.NewService(
		c.SettingService,
		telegrambroadcast.NewBotTokenResolver(c.ChannelClientService),
		webhookinfra.NewBotAPIAdapter(notifybotapi.New()),
	)
	// config.yml / .env 的 telegram_webhook 作为网页未配置时的兜底，保证现有部署平滑迁移。
	c.TelegramWebhookService.WithConfigFallback(c.Config.TelegramWebhook.WebhookURL, c.Config.TelegramWebhook.SecretToken)
	// 注入 bot 内购买端口（复用商城下单/支付/钱包/商品/身份逻辑）。
	c.TelegramWebhookService.WithPurchase(webhookcontract.PurchasePorts{
		Catalog:     &telegramPurchasePorts{products: c.ProductReadService, cats: c.CategoryService, settings: c.SettingService},
		Orders:      &telegramPurchasePorts{orders: c.OrderService},
		Payments:    &telegramPurchasePorts{payments: c.PaymentService},
		Wallet:      &telegramPurchasePorts{wallet: c.WalletService},
		Identity:    &telegramPurchasePorts{auth: c.UserAuthService},
		Settings:    &telegramPurchasePorts{settings: c.SettingService},
		OrderReader: &telegramPurchasePorts{orders: c.OrderService},
		Recharge:    &telegramPurchasePorts{payments: c.PaymentService, wallet: c.WalletService},
	})
}
