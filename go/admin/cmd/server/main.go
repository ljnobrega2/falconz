// senderzz-admin — API admin para o painel UI.
//
// Endpoints (todos sob /wp-json/senderzz/v1/admin/):
//
//	POST /login         → email+senha → JWT
//	GET  /me            → usuário autenticado
//	GET  /dashboard     → KPIs
//	GET  /users         → lista portal_users
//	GET  /users/{id}    → detalhe
//	PUT  /users/{id}    → patch (nome/role/ativo/plano)
//	GET  /motoboys      → lista
//	POST /motoboys      → cria
//	PUT  /motoboys/{id} → update
//	DELETE /motoboys/{id} → delete
//	GET  /orders/motoboy → lista pedidos motoboy
//	GET  /wallet/carteiras
//	GET  /wallet/transacoes
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/db"
	"github.com/senderzz/admin-service/internal/handlers"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// CRIT: recusa iniciar sem segredo JWT — chave vazia aceita tokens forjados (HMAC HS256).
	if os.Getenv("ADMIN_JWT_SECRET") == "" && os.Getenv("JWT_SECRET") == "" {
		slog.Error("ADMIN_JWT_SECRET não configurado — recusando iniciar")
		os.Exit(1)
	}

	pool, err := db.New(ctx)
	if err != nil {
		slog.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	authH := &handlers.AuthHandler{Pool: pool}
	dashH := &handlers.DashboardHandler{Pool: pool}
	usersH := &handlers.UsersHandler{Pool: pool}
	motH := &handlers.MotoboysHandler{Pool: pool}
	ordH := &handlers.OrdersHandler{Pool: pool}
	walH := &handlers.WalletHandler{Pool: pool}
	pixH := &handlers.PixHandler{Pool: pool}
	cdsH := &handlers.CDsHandler{Pool: pool}
	affH := &handlers.AffiliatesHandler{Pool: pool}
	labH := &handlers.LabelsHandler{Pool: pool}
	logH := &handlers.LogsHandler{Pool: pool}
	toolH := &handlers.ToolsHandler{Pool: pool}
	auditH := &handlers.AuditHandler{Pool: pool}
	livroH := &handlers.CodLivroHandler{Pool: pool}
	saquesH := &handlers.CodSaquesHandler{Pool: pool}
	taxasH := &handlers.CodTaxasHandler{Pool: pool}
	tpcCliH := &handlers.TpcClientesHandler{Pool: pool}
	affWalH := &handlers.AffiliateWalletHandler{Pool: pool}
	onbH := &handlers.OnboardingHandler{Pool: pool}
	odH := &handlers.OrderDetailHandler{Pool: pool}
	mbCfgH := &handlers.MotoboyConfigHandler{Pool: pool}
	mbDashH := &handlers.MotoboyDashboardHandler{Pool: pool}
	mbCarH := &handlers.MotoboyCarteiraHandler{Pool: pool}
	mbFecH := &handlers.MotoboyFechamentoHandler{Pool: pool}
	tpcTxH := &handlers.TpcTransacoesHandler{Pool: pool}
	tpcCfgH := &handlers.TpcConfigHandler{Pool: pool}
	mntH := &handlers.MaintenanceHandler{Pool: pool}
	cronH := &handlers.CronStatusHandler{Pool: pool}
	audLogH := &handlers.AuditLogHandler{Pool: pool}
	affRulH := &handlers.AffiliateRulesHandler{Pool: pool}
	expIntH := &handlers.ExpedicaoIntegracoesHandler{Pool: pool}
	expWhH := &handlers.ExpedicaoWebhooksHandler{Pool: pool}
	notifH := &handlers.NotificacoesPwaHandler{Pool: pool}
	mbEtqH := &handlers.MotoboyEtiquetasHandler{Pool: pool}
	mbCompH := &handlers.MotoboyComprovantesHandler{Pool: pool}
	mbSaqH := &handlers.MotoboySaquesHandler{Pool: pool}
	mbCusH := &handlers.MotoboyCustodiaHandler{Pool: pool}
	mbConcH := &handlers.MotoboyConciliacaoHandler{Pool: pool}
	codProdH := &handlers.CodWalletProducerHandler{Pool: pool}
	codTxH := &handlers.CodWalletTransactionsHandler{Pool: pool}
	trkBrandH := &handlers.TrackingBrandHandler{Pool: pool}
	apiDocsH := &handlers.ApiDocsHandler{}
	pushH := &handlers.PushTecnicoHandler{Pool: pool}
	capsH := &handlers.CapabilitiesHandler{Pool: pool}
	metaNormH := &handlers.OrderMetaNormalizationHandler{Pool: pool}
	pwaH := &handlers.PwaConfigHandler{Pool: pool}
	bulkH := &handlers.BulkActionsHandler{Pool: pool}
	mbMapaH := &handlers.MotoboyMapaHandler{Pool: pool}
	zonasH := &handlers.ZonasHandler{Pool: pool}
	settingsH := &handlers.SettingsHandler{Pool: pool}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(20 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/wp-json/senderzz/v1/admin", func(r chi.Router) {
		r.Post("/login", authH.Login)

		// Onboarding público (1ª instalação — sem auth)
		r.Get("/onboarding/setup-status", onbH.SetupStatus)
		r.Post("/onboarding/setup/create-admin", onbH.CreateAdmin)

		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(pool))
			r.Get("/me", authH.Me)
			r.Get("/dashboard", dashH.Summary)
			r.Get("/dashboard/alerts", dashH.Alerts)
			r.Get("/dashboard/stopped-orders", dashH.StoppedOrders)

			r.Get("/users", usersH.List)
			r.Get("/users/search", tpcCfgH.GetUsersSearch)
			r.Get("/users/{id}", usersH.Get)
			r.Put("/users/{id}", usersH.Update)

			r.Get("/motoboys", motH.List)
			r.Post("/motoboys", motH.Create)
			r.Put("/motoboys/{id}", motH.Update)
			r.Delete("/motoboys/{id}", motH.Delete)
			r.Get("/motoboys/{id}/zonas", motH.GetZonas)

			r.Get("/orders/motoboy", ordH.ListMotoboy)
			r.Post("/orders/motoboy/{id}/audit-fix", ordH.AuditFix)

			r.Get("/wallet/carteiras", walH.ListCarteiras)
			r.Get("/wallet/transacoes", walH.ListTransacoes)

			r.Get("/pix", pixH.List)
			r.Post("/pix/verificar", pixH.Verificar)
			r.Get("/pix/reconcile-status", pixH.ReconcileStatus)
			r.Get("/pix/divergences", pixH.Divergences)
			r.Put("/pix/{id}/status", pixH.UpdateStatus)
			r.Get("/pix/{id}", pixH.Detail)

			r.Get("/cds", cdsH.List)
			r.Post("/cds", cdsH.Create)
			r.Put("/cds/{id}", cdsH.Update)
			r.Delete("/cds/{id}", cdsH.Delete)

			r.Get("/affiliates", affH.List)

			// Etiquetas Melhor Envio
			r.Get("/labels", labH.List)
			r.Get("/labels/kpis", labH.KPIs)
			r.Get("/labels/margin-report", labH.MarginReport)
			r.Get("/labels/margin-report/csv", labH.MarginReportCSV)
			r.Get("/labels/{id}/pdf-url", labH.PDFUrl)
			r.Post("/labels/{id}/generate", labH.Generate)
			r.Post("/labels/{id}/cancel", labH.Cancel)
			r.Post("/labels/{id}/reverse", labH.Reverse)

			r.Get("/logs/webhooks", logH.Webhooks)
			r.Get("/logs/integrations", logH.Integrations)
			r.Get("/logs/motoboy", logH.MoyboyAudit)

			r.Get("/tools/stats", toolH.Stats)
			r.Post("/tools/users", toolH.InsertUser)

			r.Get("/audit/counts", auditH.Counts)
			r.Get("/audit/problems", auditH.Problems)
			r.Post("/audit/fix-all", auditH.FixAll)
			r.Post("/audit/fix-order/{id}", auditH.FixOrder)
			r.Post("/affiliates/{id}/wallet-fix", auditH.FixAffiliateWallet)

			// Livro COD
			r.Get("/cod-livro/summary", livroH.Summary)
			r.Get("/cod-livro/orders", livroH.Orders)
			r.Get("/cod-livro/affiliates-summary", livroH.AffiliatesSummary)
			r.Get("/cod-livro/producers-summary", livroH.ProducersSummary)

			// Saques COD/Afiliado
			r.Get("/cod-saques/producer", saquesH.ListProducer)
			r.Get("/cod-saques/producer/overrides", saquesH.GetProducerOverrides)
			r.Post("/cod-saques/producer/overrides", saquesH.SetProducerOverrides)
			r.Post("/cod-saques/producer/{id}/mark-paid", saquesH.MarkProducerPaid)
			r.Post("/cod-saques/producer/{id}/reject", saquesH.RejectProducer)
			r.Post("/cod-saques/producer/{id}/upload-proof", saquesH.UploadProducerProof)
			r.Get("/cod-saques/affiliate", saquesH.ListAffiliate)
			r.Post("/cod-saques/affiliate/{id}/approve", saquesH.ApproveAffiliate)
			r.Post("/cod-saques/affiliate/{id}/reject", saquesH.RejectAffiliate)
			r.Get("/cod-saques/global-rules", saquesH.GetGlobalRules)
			r.Post("/cod-saques/global-rules", saquesH.SetGlobalRules)

			// Taxas de entrega COD
			r.Get("/cod-taxas/global", taxasH.GetGlobal)
			r.Post("/cod-taxas/global", taxasH.SaveGlobal)
			r.Get("/cod-taxas/motoboys", taxasH.GetMotoboys)
			r.Post("/cod-taxas/motoboys", taxasH.SaveMotoboys)
			r.Get("/cod-taxas/producers", taxasH.GetProducers)
			r.Post("/cod-taxas/producers", taxasH.SaveProducers)
			r.Get("/cod-taxas/affiliates", taxasH.GetAffiliates)
			r.Post("/cod-taxas/affiliates", taxasH.SaveAffiliates)

			// TPC clientes (carteira frete)
			r.Get("/tpc-clientes", tpcCliH.List)
			r.Get("/tpc-clientes/{user_id}", tpcCliH.Get)
			r.Post("/tpc-clientes/{user_id}/recarga", tpcCliH.CreateRecarga)
			r.Post("/tpc-clientes/{user_id}/cancelar-recarga/{recarga_id}", tpcCliH.CancelRecarga)
			r.Post("/tpc-clientes/reset-wallet-all", tpcCliH.ResetWalletAll)

			// Carteira de afiliados
			r.Get("/affiliates-wallet/summary", affWalH.Summary)
			r.Get("/affiliates-wallet", affWalH.List)
			r.Get("/affiliates-wallet/transaction-types", affWalH.TransactionTypes)
			r.Get("/affiliates-wallet/{id}/transactions", affWalH.Transactions)
			r.Post("/affiliates-wallet/{id}/wallet-fix", affWalH.WalletFix)
			r.Post("/affiliates-wallet/{id}/release-pending", affWalH.ReleasePending)

			// Onboarding requests (autenticado)
			r.Get("/onboarding/requests", onbH.List)
			r.Get("/onboarding/requests/{id}", onbH.Get)
			r.Post("/onboarding/requests", onbH.Create)
			r.Post("/onboarding/requests/{id}/approve", onbH.Approve)
			r.Post("/onboarding/requests/{id}/reject", onbH.Reject)

			// Order detail consolidado (motoboy + afiliado + label + audit)
			r.Get("/orders/{id}", odH.Get)
			r.Post("/orders/{id}/force-motoboy-status", odH.ForceMotoboyStatus)
			r.Post("/orders/{id}/note", odH.Note)
			r.Get("/orders/{id}/notes", odH.Notes)

			// Motoboy config + dashboard
			r.Get("/motoboy-config", mbCfgH.Get)
			r.Post("/motoboy-config", mbCfgH.Save)
			r.Get("/motoboy-dashboard", mbDashH.Dashboard)

			// Motoboy carteira (pagamentos)
			r.Get("/motoboy-carteira/summary", mbCarH.Summary)
			r.Get("/motoboy-carteira", mbCarH.List)
			r.Post("/motoboy-carteira/{motoboy_id}/pagamento", mbCarH.RegistrarPagamento)
			r.Get("/motoboy-carteira/{motoboy_id}/historico", mbCarH.Historico)
			r.Post("/motoboy-carteira/sync", mbCarH.Sync)

			// Motoboy fechamento diário
			r.Get("/motoboy-fechamento", mbFecH.List)
			r.Get("/motoboy-fechamento/summary", mbFecH.Summary)
			r.Post("/motoboy-fechamento/{id}/alan-confirmar", mbFecH.AlanConfirmar)
			r.Post("/motoboy-fechamento/{id}/alan-desconfirmar", mbFecH.AlanDesconfirmar)
			r.Post("/motoboy-fechamento/{id}/repasse-confirmar", mbFecH.RepasseConfirmar)
			r.Post("/motoboy-fechamento/{id}/repasse-desconfirmar", mbFecH.RepasseDesconfirmar)
			r.Post("/motoboy-fechamento/generate", mbFecH.Generate)
			r.Post("/motoboy-fechamento/sync-wallets", mbFecH.SyncWallets)

			// TPC transações
			r.Get("/tpc-transacoes", tpcTxH.List)
			r.Delete("/tpc-transacoes/{id}", tpcTxH.Delete)
			r.Post("/tpc-transacoes/verificar-pix", tpcTxH.VerificarPix)

			// TPC config
			r.Get("/tpc-config", tpcCfgH.Get)
			r.Post("/tpc-config", tpcCfgH.Save)
			r.Get("/tpc-config/wallet-owners", tpcCfgH.GetWalletOwners)
			r.Post("/tpc-config/wallet-owners", tpcCfgH.SaveWalletOwners)
			r.Post("/tpc-config/regenerate-secret", tpcCfgH.RegenerateSecret)
			r.Get("/tpc-config/me-balance", tpcCfgH.GetMEBalance)

			// Catálogo de classes de entrega (wallet-owners dropdown)
			r.Get("/shipping-classes", tpcCfgH.GetShippingClasses)


			// Maintenance mode
			r.Get("/maintenance", mntH.Get)
			r.Post("/maintenance", mntH.Save)
			r.Get("/maintenance/preview-data", mntH.PreviewData)

			// Cron status
			r.Get("/crons", cronH.List)
			r.Post("/crons/{name}/trigger", cronH.Trigger)
			r.Post("/crons/{name}/skip-next", cronH.SkipNext)
			r.Get("/crons/{name}/recent-runs", cronH.RecentRuns)

			// Audit log viewer
			r.Get("/audit-log", audLogH.List)
			r.Get("/audit-log/actions", audLogH.Actions)
			r.Get("/audit-log/stats", audLogH.Stats)
			r.Get("/audit-log/{id}", audLogH.Get)

			// Affiliate rules
			r.Get("/affiliate-rules", affRulH.Get)
			r.Post("/affiliate-rules", affRulH.Save)
			r.Get("/affiliate-rules/stats", affRulH.Stats)

			// Expedição integrações (markup)
			r.Get("/expedicao/markup", expIntH.GetMarkup)
			r.Post("/expedicao/markup", expIntH.SaveMarkup)
			r.Get("/expedicao/shipping-classes", expIntH.GetShippingClasses)
			r.Post("/expedicao/markup/preview", expIntH.PreviewMarkup)

			// Expedição webhooks (por classe)
			r.Get("/expedicao-webhooks", expWhH.List)
			r.Post("/expedicao-webhooks", expWhH.Create)
			r.Put("/expedicao-webhooks/{id}", expWhH.Update)
			r.Delete("/expedicao-webhooks/{id}", expWhH.Delete)
			r.Post("/expedicao-webhooks/{id}/test", expWhH.Test)
			r.Get("/expedicao-webhooks/{id}/logs", expWhH.Logs)
			r.Post("/expedicao-webhooks/{id}/reprocess", expWhH.Reprocess)
			r.Get("/expedicao-webhooks/sample-payload", expWhH.SamplePayload)
			r.Get("/expedicao-webhooks/available-events", expWhH.AvailableEvents)
			r.Get("/expedicao-webhooks/classes", expWhH.Classes)

			// Notificações PWA
			r.Get("/notificacoes-pwa/events", notifH.GetEvents)
			r.Get("/notificacoes-pwa/templates", notifH.GetTemplates)
			r.Post("/notificacoes-pwa/templates", notifH.SaveTemplates)
			r.Get("/notificacoes-pwa/status-map", notifH.GetStatusMap)
			r.Post("/notificacoes-pwa/status-map", notifH.SaveStatusMap)
			r.Get("/notificacoes-pwa/recipients", notifH.GetRecipients)
			r.Post("/notificacoes-pwa/recipients", notifH.SaveRecipients)
			r.Get("/notificacoes-pwa/admin-recipients", notifH.GetAdminRecipients)
			r.Post("/notificacoes-pwa/admin-recipients", notifH.SaveAdminRecipients)
			r.Get("/notificacoes-pwa/order-number-flags", notifH.GetOrderNumberFlags)
			r.Post("/notificacoes-pwa/order-number-flags", notifH.SaveOrderNumberFlags)
			r.Get("/notificacoes-pwa/variables", notifH.GetVariables)

			// Motoboy etiquetas/comprovantes/saques
			r.Get("/motoboy-etiquetas", mbEtqH.List)
			r.Get("/motoboy-comprovantes", mbCompH.List)
			r.Get("/motoboy-comprovantes/stats", mbCompH.Stats)
			r.Get("/motoboy-comprovantes/export-csv", mbCompH.ExportCSV)
			r.Get("/motoboy-comprovantes/{id}", mbCompH.Get)
			r.Delete("/motoboy-comprovantes/{id}", mbCompH.Delete)
			r.Get("/motoboy-saques", mbSaqH.List)
			r.Get("/motoboy-saques/summary", mbSaqH.Summary)

			// Motoboy custódia
			r.Get("/motoboy-custodia/summary", mbCusH.Summary)
			r.Get("/motoboy-custodia", mbCusH.List)
			r.Get("/motoboy-custodia/summary-by-motoboy", mbCusH.SummaryByMotoboy)
			r.Post("/motoboy-custodia/route-assist", mbCusH.RouteAssist)
			r.Post("/motoboy-custodia/return", mbCusH.Return)

			// Motoboy conciliação
			r.Get("/motoboy-conciliacao", mbConcH.List)
			r.Post("/motoboy-conciliacao/{pedido_id}/conciliar", mbConcH.Conciliar)

			// COD wallet (producer + transactions viewer)
			r.Get("/cod-wallet-producer/summary", codProdH.Summary)
			r.Get("/cod-wallet-producer", codProdH.List)
			r.Get("/cod-wallet-producer/{user_id}/accounts", codProdH.Accounts)
			r.Post("/cod-wallet-producer/{user_id}/release-pending", codProdH.ReleasePending)
			r.Get("/cod-wallet-transactions", codTxH.List)
			r.Get("/cod-wallet-transactions/types", codTxH.Types)
			r.Get("/cod-wallet-transactions/stats", codTxH.Stats)
			r.Get("/cod-wallet-transactions/export-csv", codTxH.ExportCSV)

			// Tracking brand (per-class)
			r.Get("/tracking-brand", trkBrandH.GetAll)
			r.Post("/tracking-brand", trkBrandH.SaveAll)
			r.Post("/tracking-brand/add-class", trkBrandH.AddClass)
			r.Delete("/tracking-brand/{class_id}", trkBrandH.DeleteClass)

			// API docs (estático)
			r.Get("/api-docs", apiDocsH.GetDocs)

			// Push técnico (VAPID)
			r.Get("/push-tecnico/status", pushH.GetStatus)
			r.Post("/push-tecnico/regenerate-vapid", pushH.RegenerateVapid)
			r.Post("/push-tecnico/test-send", pushH.TestSend)
			r.Get("/push-tecnico/logs", pushH.GetLogs)
			r.Post("/push-tecnico/logs/{id}/reprocess", pushH.ReprocessLog)

			// Capabilities viewer
			r.Get("/capabilities", capsH.GetCapabilities)
			r.Get("/capabilities/users", capsH.GetCapabilityUsers)

			// Order meta normalization
			r.Get("/order-meta/normalization-status", metaNormH.Status)
			r.Post("/order-meta/normalize", metaNormH.Normalize)
			r.Get("/order-meta/divergences", metaNormH.Divergences)
			r.Delete("/order-meta/divergence-log", metaNormH.ClearDivergenceLog)

			// PWA config
			r.Get("/pwa-config", pwaH.Get)
			r.Post("/pwa-config", pwaH.Save)
			r.Get("/pwa-config/test-manifest", pwaH.TestManifest)

			// Bulk actions (etiquetas em lote)
			r.Get("/bulk-actions/orders", bulkH.ListOrders)
			r.Post("/bulk-actions/generate-labels", bulkH.GenerateLabels)
			r.Get("/bulk-actions/queue-status", bulkH.QueueStatus)
			r.Get("/bulk-actions/shipping-classes", bulkH.ShippingClasses)

			// Motoboy mapa ao vivo
			r.Get("/motoboy-mapa/locations", mbMapaH.Locations)

			// Zonas de entrega
			r.Get("/zonas", zonasH.List)
			r.Get("/zonas/cep-check", zonasH.CepCheck)
			r.Get("/zonas/ceps", zonasH.Ceps)
			r.Get("/zonas/{id}", zonasH.Get)

			// Configurações gerais
			r.Get("/settings", settingsH.Get)
			r.Put("/settings", settingsH.Save)

			// Vínculos e comissões de afiliados
			r.Get("/affiliates/links", affH.Links)
			r.Get("/affiliates/commissions", affH.Commissions)

			// Motoboys do dia
			r.Get("/motoboys/dia", motH.Dia)
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8087"
	}
	srv := &http.Server{Addr: ":" + port, Handler: r, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	slog.Info("[admin] iniciando", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
