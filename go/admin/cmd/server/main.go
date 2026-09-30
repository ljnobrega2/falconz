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
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/db"
	"github.com/senderzz/admin-service/internal/handlers"
	"github.com/senderzz/admin-service/internal/httpx"
	"github.com/senderzz/admin-service/internal/melhorenvio"
	adminwallet "github.com/senderzz/admin-service/internal/wallet"
)

// P0-03: monta a allowlist de origens CORS a partir de ALLOWED_ORIGINS (CSV).
// Espelha o padrão de portal/motoboy (env ALLOWED_ORIGINS). Antes o CORS usava
// AllowedOrigins:["*"], aceitando requisições autenticadas de qualquer site.
// Se ALLOWED_ORIGINS estiver vazio, cai num default seguro = origem do admin-ui
// (http://localhost:5173) + APP_BASE_URL e TUNNEL_URL quando presentes no env.
// Nunca retorna "*": origem não-listada não recebe Access-Control-Allow-Origin
// (o go-chi/cors omite o header → o browser rejeita a chamada cross-origin).
func allowedOriginsFromEnv() []string {
	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}

	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		add(o)
	}
	if len(out) > 0 {
		return out
	}

	// Default seguro (dev): origem do admin-ui + tunnel quando exportado no env.
	add("http://localhost:5173")
	add(os.Getenv("APP_BASE_URL"))
	add(os.Getenv("TUNNEL_URL"))
	return out
}

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
	walH := &handlers.WalletHandler{Pool: pool, WalletClient: adminwallet.NewClient()}
	pixH := &handlers.PixHandler{Pool: pool}
	cdsH := &handlers.CDsHandler{Pool: pool}
	affH := &handlers.AffiliatesHandler{Pool: pool}
	labH := &handlers.LabelsHandler{Pool: pool, LabelsServiceURL: os.Getenv("LABELS_SERVICE_URL")}
	carriersH := &handlers.CarriersHandler{Pool: pool, LabelsServiceURL: os.Getenv("LABELS_SERVICE_URL")}
	logH := &handlers.LogsHandler{Pool: pool}
	toolH := &handlers.ToolsHandler{Pool: pool}
	auditH := &handlers.AuditHandler{Pool: pool}
	livroH := &handlers.CodLivroHandler{Pool: pool}
	saquesH := &handlers.CodSaquesHandler{Pool: pool}
	freightTransfersH := &handlers.WalletFreightTransfersHandler{Pool: pool}
	taxasH := &handlers.CodTaxasHandler{Pool: pool}
	tpcCliH := &handlers.TpcClientesHandler{Pool: pool}
	affWalH := &handlers.AffiliateWalletHandler{Pool: pool}
	onbH := &handlers.OnboardingHandler{Pool: pool}
	docChgH := &handlers.DocumentChangeHandler{Pool: pool}
	odH := &handlers.OrderDetailHandler{Pool: pool}
	revH := &handlers.RevenueHandler{Pool: pool} // Faturamento FALKZ (senderzz_revenue)
	profitH := &handlers.OperationalProfitHandler{Pool: pool}
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
	expOrdersH := &handlers.ExpedicaoOrdersHandler{Pool: pool}
	freightQuotesH := &handlers.FreightQuotesHandler{Pool: pool, ME: melhorenvio.NewClient()}
	expWhH := &handlers.ExpedicaoWebhooksHandler{Pool: pool}
	orderWhH := &handlers.OrderWebhooksHandler{Pool: pool}
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
	pwaH := &handlers.PwaConfigHandler{Pool: pool}
	bulkH := &handlers.BulkActionsHandler{Pool: pool, LabelsServiceURL: os.Getenv("LABELS_SERVICE_URL")}
	bulkQH := &handlers.BulkQueuesHandler{Pool: pool} // ações em lote nas filas de decisão
	mbMapaH := &handlers.MotoboyMapaHandler{Pool: pool}
	zonasH := &handlers.ZonasHandler{Pool: pool}
	settingsH := &handlers.SettingsHandler{Pool: pool}
	prdH := &handlers.ProductsHandler{Pool: pool}
	prdApvH := &handlers.ProductApprovalHandler{Pool: pool} // fila de aprovação de produto
	supH := &handlers.SupportHandler{Pool: pool}            // área de suporte (tickets) do admin
	chkH := &handlers.CheckoutLinksHandler{Pool: pool}
	prodH := &handlers.ProducersHandler{Pool: pool}
	ubrH := &handlers.UsersByRoleHandler{Pool: pool}
	clientesH := &handlers.ClientesHandler{Pool: pool} // clientes + fonte de donos p/ produto
	stkH := &handlers.StockHandler{Pool: pool}
	stkShipH := &handlers.StockShipmentHandler{Pool: pool}
	cfgTaxasH := &handlers.ConfigTaxasHandler{Pool: pool} // regra de cálculo (taxas) — muda SÓ via admin
	dsrH := &handlers.DSRHandler{Pool: pool}              // canal do titular LGPD Art.18/19 (DPO)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// AUDIT GO-HDR-01: cabeçalhos de segurança (nosniff/X-Frame/CSP/HSTS/Referrer/
	// Permissions) em TODA resposta — logo após RealIP para embrulhar Recoverer/
	// CORS/handlers e cobrir /healthz e preflights. Defense-in-depth atrás do nginx;
	// no dev (nginx fora do caminho) é a fonte destes headers nas RESPOSTAS Go — não
	// no HTML do painel (servido pelo Vite). Ver internal/httpx/security_headers.go.
	r.Use(httpx.SecurityHeaders())
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(20 * time.Second))
	// P0-03: allowlist via ALLOWED_ORIGINS (CSV) em vez de "*". AllowCredentials
	// permanece false — o admin usa Bearer JWT (não cookie), então não há CSRF
	// por credencial implícita; mesmo assim só espelhamos origens conhecidas.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOriginsFromEnv(),
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

	// Imagens de produto — file-server estático PÚBLICO (fora do auth: aparecem no
	// checkout/vitrine/storefront, não há sessão). Servidas do mesmo diretório onde
	// o handler de upload grava (handlers.ProductImgDir(), env PRODUCT_IMG_UPLOAD_PATH).
	// A URL gravada em meta.image_url tem o prefixo /uploads/products/ (PRODUCT_IMG_UPLOAD_URL).
	r.Handle("/uploads/products/*", http.StripPrefix("/uploads/products/", http.FileServer(http.Dir(handlers.ProductImgDir()))))

	r.Route("/wp-json/senderzz/v1/admin", func(r chi.Router) {
		// P0-04: rate-limit por IP no /login (5 tentativas/15min) antes do handler.
		r.With(auth.LoginRateLimit()).Post("/login", authH.Login)

		// Onboarding público (1ª instalação — sem auth)
		r.Get("/onboarding/setup-status", onbH.SetupStatus)
		r.Post("/onboarding/setup/create-admin", onbH.CreateAdmin)
		// Cadastro público auto-serviço (cria solicitação pendente; admin aprova + define nível/RBAC).
		// Rate-limit por IP p/ evitar abuso de cadastro.
		r.With(auth.LoginRateLimit()).Post("/onboarding/signup", onbH.Signup)
		// REF-PAYOUT 463 — resolve referral_code do link /r/{code} → nome do indicador,
		// para a LP validar "Indicado por …" (público, sem auth, rate-limited).
		r.With(auth.LoginRateLimit()).Get("/onboarding/referral/{code}", onbH.ResolveReferral)
		// AUDIT-2026-06-22 forgot-pw — recuperação de senha (público, rate-limited
		// por IP). forgot SEMPRE 200 genérico (anti-enumeração); reset é fail-closed.
		r.With(auth.LoginRateLimit()).Post("/onboarding/forgot", onbH.ForgotPassword)
		r.With(auth.LoginRateLimit()).Post("/onboarding/reset", onbH.ResetPassword)

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

			// GET /orders/motoboy + as 3 MUTAÇÕES (reagendar/cancelar/reagendar-clone)
			// E o GET /zona-schedule movidos para o grupo DualAuth (admin OU portal)
			// abaixo — as mutações E o zona-schedule têm OWNERSHIP-GATE no handler
			// (produtor só pedido DELE; afiliado idem). audit-fix permanece ADMIN-ONLY aqui.
			r.Post("/orders/motoboy/{id}/audit-fix", ordH.AuditFix)

			r.Get("/wallet/carteiras", walH.ListCarteiras)
			r.Get("/wallet/transacoes", walH.ListTransacoes)
			r.Get("/wallet/estornos-pendentes", walH.ListEstornosPendentes)
			r.Post("/wallet/estornos-pendentes/{id}/confirmar", walH.PostConfirmarEstorno)

			// Faturamento FALKZ (livro-razão de receita)
			r.Get("/revenue/summary", revH.Summary)
			r.Get("/revenue", revH.List)
			r.Get("/operational-profit/cod", profitH.CODSummary)

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

			// GET /affiliates movido para o grupo DualAuth (admin OU portal) abaixo —
			// produtor vê só os afiliados DELE (escopo no servidor). Detail permanece admin-only.
			r.Get("/affiliates/{id}/detail", affH.Detail)
			r.Put("/affiliates/{id}", affH.Update) // CPF/telefone manuais (meta _billing_cpf/_billing_phone)

			// Produtores (senderzz_portal_users role='produtor')
			r.Get("/producers", prodH.List)
			r.Get("/producers/{user_id}/detail", prodH.Detail)
			r.Put("/producers/{user_id}", prodH.Update)    // CPF/telefone manuais + flag expedição
			r.Delete("/producers/{user_id}", prodH.Delete) // soft-delete (ativo=false) + cascade checkouts

			// Usuários por role (OLs / admins)
			r.Get("/users-by-role", ubrH.List)

			// Clientes (senderzz_portal_users role='cliente'). ?role=all|afiliado|produtor
			// reaproveita o endpoint como fonte de DONOS p/ o seletor de produto.
			r.Get("/clientes", clientesH.List)
			r.Delete("/clientes/{id}", clientesH.Delete) // soft-delete (ativo=false) cliente/afiliado + cascade checkouts

			// Etiquetas Melhor Envio
			r.Get("/labels", labH.List)
			r.Get("/labels/kpis", labH.KPIs)
			r.Get("/labels/margin-report", labH.MarginReport)
			r.Get("/labels/margin-report/csv", labH.MarginReportCSV)
			r.Get("/labels/{id}/pdf-url", labH.PDFUrl)
			r.Post("/labels/{id}/generate", labH.Generate)
			r.Post("/labels/{id}/cancel", labH.Cancel)
			r.Post("/labels/{id}/reverse", labH.Reverse)

			// Expedição: catálogo global de transportadoras (Melhor Envio).
			r.Get("/expedicao/carriers/live", carriersH.LiveCatalog)
			r.Get("/expedicao/carriers/catalog", carriersH.Catalog)
			r.Put("/expedicao/carriers/catalog", carriersH.SaveCatalog)

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
			r.Post("/cod-saques/affiliate/{id}/upload-proof", saquesH.UploadAffiliateProof)
			r.Get("/cod-saques/global-rules", saquesH.GetGlobalRules)
			r.Post("/cod-saques/global-rules", saquesH.SetGlobalRules)

			// Transferência Carteira COD → Carteira de Expedição (aprovação admin). FEAT-WFT
			// Aprovar é 2 passos: gera PIX real (ME) → admin paga → confirma pagamento.
			r.Get("/wallet-freight-transfers", freightTransfersH.List)
			r.Post("/wallet-freight-transfers/{id}/generate-pix", freightTransfersH.GeneratePix)
			r.Post("/wallet-freight-transfers/{id}/confirm-pix", freightTransfersH.ConfirmPix)
			r.Post("/wallet-freight-transfers/{id}/reject", freightTransfersH.Reject)

			// Taxas de entrega COD
			r.Get("/cod-taxas/global", taxasH.GetGlobal)
			r.Post("/cod-taxas/global", taxasH.SaveGlobal)
			r.Get("/cod-taxas/motoboys", taxasH.GetMotoboys)
			r.Post("/cod-taxas/motoboys", taxasH.SaveMotoboys)
			r.Get("/cod-taxas/producers", taxasH.GetProducers)
			r.Post("/cod-taxas/producers", taxasH.SaveProducers)
			r.Get("/cod-taxas/affiliates", taxasH.GetAffiliates)
			r.Post("/cod-taxas/affiliates", taxasH.SaveAffiliates)

			// Taxa de transação do produtor (sz_producer_transaction_fee_pct) — usada
			// no breakdown financeiro de OrderDetail (taxa_transacao_produtor = total × pct).
			r.Get("/producer-fee-config", taxasH.GetProducerFeeConfig)
			r.Post("/producer-fee-config", taxasH.SaveProducerFeeConfig)

			// Regra de cálculo (taxas) — DONO: "muda SÓ via admin". Lê/edita
			// sz_producer_transaction_fee_pct (4,99) e motoboy_repasse_padrao (18)
			// em senderzz_options. A taxa do afiliado (4,99% fixa) vai como
			// read-only. Como order_detail.go/sz_order_financeiro JÁ leem essas
			// options, alterar aqui reflete em todo o site sem deploy.
			r.Get("/config/taxas", cfgTaxasH.Get)
			r.Post("/config/taxas", cfgTaxasH.Save)
			r.Get("/config/taxas/produtores", cfgTaxasH.ListProducerFees)
			r.Post("/config/taxas/produtores", cfgTaxasH.SaveProducerFees)
			r.Delete("/config/taxas/produtores/{id}", cfgTaxasH.DeleteProducerFee)

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

			// FEAT-DOC-CHANGE-2026-07-03: aprovação de troca CPF⇄CNPJ do titular.
			r.Get("/document-changes", docChgH.List)
			r.Get("/document-changes/{id}", docChgH.Get)
			r.Get("/document-changes/{id}/attachment", docChgH.Attachment)
			r.Post("/document-changes/{id}/approve", docChgH.Approve)
			r.Post("/document-changes/{id}/reject", docChgH.Reject)

			// Order detail consolidado (motoboy + afiliado + label + audit).
			// GET /orders/{id} movido para o grupo DualAuth (admin OU portal) abaixo;
			// as mutações/notes permanecem ADMIN-ONLY aqui.
			r.Post("/orders/{id}/force-motoboy-status", odH.ForceMotoboyStatus)
			r.Post("/orders/{id}/financial-status", odH.UpdateFinancialStatus)
			r.Post("/orders/{id}/change-motoboy", odH.ChangeMotoboy)
			r.Post("/orders/{id}/upload-evidence", odH.UploadEvidence)
			r.Post("/orders/{id}/note", odH.Note)
			r.Get("/orders/{id}/notes", odH.Notes)
			// FEAT-IMPRIMIR-SEPARA (2026-07-28): imprimir a etiqueta ME já separa o
			// pedido automaticamente (status → embalado). Motoboy tem seu próprio fluxo.
			r.Post("/orders/{id}/mark-packed", odH.MarkPacked)
			r.Post("/orders/{id}/mark-collected", odH.MarkCollected)
			// FEAT-EXPEDICAO-ADMIN-APROVAR (2026-07-28): admin/operador aprova pedido
			// Expedição de QUALQUER produtor (espelha go/portal Approve).
			r.Post("/orders/{id}/approve", odH.Approve)
			r.Post("/orders/{id}/cancel-expedicao", odH.CancelExpedicao)
			r.Post("/orders/{id}/emit-label", odH.EmitLabelRetry)
			r.Get("/orders/{id}/freight-quotes", freightQuotesH.GetFreightQuotes)
			r.Post("/orders/{id}/force-carrier", odH.ForceCarrierEmit)
			r.Patch("/orders/{id}/fields", odH.EditOrderFields)
			r.Get("/orders/{id}/label-pdf", odH.LabelPDF)

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
			r.Get("/expedicao/orders", expOrdersH.List)
			r.Get("/expedicao/shipping-classes", expIntH.GetShippingClasses)
			r.Delete("/expedicao/shipping-classes/{id}", expIntH.DeleteShippingClass)
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
			r.Get("/order-webhooks/logs", orderWhH.ListLogs)
			r.Get("/order-webhooks/outbox-summary", orderWhH.OutboxSummary)
			r.Post("/order-webhooks/resend-stale", orderWhH.ResendStale)
			r.Post("/order-webhooks/resend-exhausted", orderWhH.ResendExhausted)

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
			r.Post("/cod-wallet-producer/{user_id}/anticipate", codProdH.AnticipateAmount)
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

			// PWA config
			r.Get("/pwa-config", pwaH.Get)
			r.Post("/pwa-config", pwaH.Save)
			r.Get("/pwa-config/test-manifest", pwaH.TestManifest)

			// Bulk actions (etiquetas em lote)
			r.Get("/bulk-actions/orders", bulkH.ListOrders)
			r.Post("/bulk-actions/generate-labels", bulkH.GenerateLabels)
			r.Post("/bulk-actions/motoboy-generate-labels", bulkH.GenerateMotoboyLabels)
			r.Get("/bulk-actions/queue-status", bulkH.QueueStatus)
			r.Get("/bulk-actions/shipping-classes", bulkH.ShippingClasses)
			// FEAT-IMPRIMIR-SEPARA (2026-07-28): individual e lote no mesmo endpoint.
			r.Post("/bulk-actions/print-batch", bulkH.PrintBatch)
			r.Get("/order-status-sync/mismatches", bulkH.MEStatusMismatches)
			r.Post("/order-status-sync/{order_id}/sync", bulkH.MEStatusSync)

			// Bulk actions nas FILAS DE DECISÃO (AUDIT UX persona Admin, Onda 2 P0):
			// multi-seleção + barra de ações em lote. Idempotente, transacional por
			// item, auth admin. body {ids:[], ...}. Ver bulk_queues.go.
			r.Post("/bulk-actions/cod-saques/producer/mark-paid", bulkQH.CodSaquesProducerMarkPaid)
			r.Post("/bulk-actions/cod-saques/producer/reject", bulkQH.CodSaquesProducerReject)
			r.Post("/bulk-actions/cod-saques/affiliate/approve", bulkQH.CodSaquesAffiliateApprove)
			r.Post("/bulk-actions/cod-saques/affiliate/reject", bulkQH.CodSaquesAffiliateReject)
			r.Post("/bulk-actions/onboarding/approve", bulkQH.OnboardingApprove)
			r.Post("/bulk-actions/onboarding/reject", bulkQH.OnboardingReject)
			r.Post("/bulk-actions/orders/cancel", bulkQH.OrdersCancel)
			r.Post("/bulk-actions/orders/reschedule", bulkQH.OrdersReschedule)

			// Motoboy mapa ao vivo
			r.Get("/motoboy-mapa/locations", mbMapaH.Locations)

			// Zonas de entrega
			r.Get("/zonas", zonasH.List)
			r.Post("/zonas", zonasH.Create)
			r.Get("/zonas/cep-check", zonasH.CepCheck)
			r.Get("/zonas/ceps", zonasH.Ceps)
			r.Post("/zonas/ceps", zonasH.CepsCreate)
			r.Put("/zonas/ceps/{id}", zonasH.CepsUpdate)
			r.Delete("/zonas/ceps/{id}", zonasH.CepsDelete)
			r.Get("/zonas/{id}", zonasH.Get)
			r.Put("/zonas/{id}", zonasH.Update)
			r.Delete("/zonas/{id}", zonasH.Delete)

			// Configurações gerais
			r.Get("/settings", settingsH.Get)
			r.Put("/settings", settingsH.Save)

			// Comissões de afiliados (a rota /affiliates/links foi removida —
			// links de checkout nunca são expostos no admin; o método Links()
			// em affiliates.go permanece dead-code intencional).
			r.Get("/affiliates/commissions", affH.Commissions)

			// Motoboys do dia
			r.Get("/motoboys/dia", motH.Dia)

			// Produtos (sz_products). GET /products movido para o grupo DualAuth abaixo
			// (produtor vê só os produtos dele). stats/CRUD permanecem admin-only.
			r.Get("/products/stats", prdH.Stats)
			r.Post("/products", prdH.Create)
			r.Post("/products/sync-from-orders", prdH.SyncFromOrders)
			// Upload de imagem de produto: recebe um arquivo (multipart: image),
			// valida por magic bytes (jpeg/png/webp) e devolve {url} pública. O front
			// guarda a URL em form.image_url → persiste em meta.image_url no save.
			// Os ARQUIVOS são servidos pelo file-server público /uploads/products/*
			// (registrado fora do auth, abaixo). Admin-only (cria/edita catálogo).
			r.Post("/products/upload-image", prdH.UploadImage)
			r.Put("/products/{id}", prdH.Update)
			r.Delete("/products/{id}", prdH.Delete)

			// Fila de APROVAÇÃO de produto (produto novo nasce 'a_aprovar')
			r.Get("/products/approval-queue", prdApvH.ApprovalQueue)
			r.Post("/products/{id}/approve", prdApvH.Approve)
			r.Post("/products/{id}/reject", prdApvH.Reject)

			// Suporte (tickets) — admin vê TODOS os chamados (somente leitura)
			r.Get("/support/tickets", supH.ListTickets)
			r.Get("/support/tickets/{id}", supH.TicketDetail)

			// Estoque (sz_stock + sz_stock_movements)
			r.Get("/stock", stkH.List)
			r.Post("/stock", stkH.Create)
			// FEAT-SKU-ESTOQUE — bipagem de ENTRADA: OL bipa 1 unidade (SKU) + digita qty.
			// SKU precisa existir em sz_products do produtor, senão 422 e NÃO credita.
			r.Post("/stock/bipar", stkH.Bipar)
			r.Post("/stock/{id}/ajuste", stkH.Adjust)
			r.Get("/stock/{id}/movements", stkH.Movements)

			// Remessas de estoque produtor→CD (FEAT-STOCK-SHIPMENTS — paridade
			// senderzz-stock-shipments.php; criação/despacho ficam no portal do produtor).
			r.Get("/stock-shipments", stkShipH.List)
			r.Get("/stock-shipments/{id}", stkShipH.Detail)
			r.Post("/stock-shipments/{id}/confirm", stkShipH.Confirm)
			r.Post("/stock-shipments/{id}/conclude", stkShipH.Conclude)

			// Links de Checkout — SOMENTE LEITURA + relatório.
			// DONO: "link afiliado nao criamos e automatico nao deve ter no admin" +
			// "admin nao precisa ter tela de todos os checkouts isso precisa ser
			// emitido via relatorio pra tirar do banco". Por isso:
			//   - leitura interna preservada (List/ListOffers; Commissions usa os dados);
			//   - escrita manual REMOVIDA (Create/Update/Delete → 410 Gone);
			//   - export.csv substitui a "tela de todos os checkouts".
			r.Get("/checkout-links", chkH.List)
			// Ofertas de checkout migradas (senderzz_checkout_links) — somente leitura.
			// Registrado antes de /{id} para o chi casar a rota literal "offers" primeiro.
			r.Get("/checkout-links/offers", chkH.ListOffers)
			// Relatório p/ tirar do banco (substitui a tela de todos os checkouts).
			r.Get("/checkout-links/export.csv", chkH.ExportCSV)
			// Escrita manual desativada — links de afiliado são automáticos (410 Gone).
			r.Post("/checkout-links", chkH.Create)
			r.Put("/checkout-links/{id}", chkH.Update)
			r.Delete("/checkout-links/{id}", chkH.Delete)

			// Canal do titular — LGPD Art. 18/19 (DPO). AUDIT-LGPD-2026-06-24 A3+B6:
			// a fila senderzz_data_subject_requests era WRITE-ONLY (portal só gravava);
			// agora o DPO LÊ (prazo ASC), TRATA (status/response/handled_by/fulfilled_at)
			// e LOCALIZA o titular COD por TEL/CPF (lookup) — pedido só por e-mail não
			// alcança o comprador COD. Admin-only (este grupo auth.Middleware), nunca
			// DualAuth. /lookup literal antes da rota param (house style).
			r.Get("/data-subject-requests", dsrH.List)
			r.Get("/data-subject-requests/lookup", dsrH.Lookup)
			r.Post("/data-subject-requests/{id}", dsrH.Update)
		})

		// FEAT-RBAC-ORDERS-2026-06-24 — TELAS DE PEDIDOS COMPARTILHADAS.
		// Grupo SEPARADO com DualAuth: aceita ADMIN token OU PORTAL token (produtor/
		// afiliado). O escopo por papel é feito NO SERVIDOR, pela identidade
		// AUTENTICADA (auth.ActorFromCtx), nunca por input do cliente:
		//   - admin    → vê/muta TUDO (comportamento idêntico ao auth.Middleware).
		//   - produtor → WHERE sz_orders.produtor_id = <portalUserID>.
		//   - afiliado → WHERE sz_orders.affiliate_id = <wpUserID>.
		// 4 rotas de LEITURA + 3 MUTAÇÕES de pedido motoboy entram aqui. As mutações têm
		// OWNERSHIP-GATE no handler (auth.ActorFromCtx): produtor/afiliado só muta pedido
		// DELE — resolvido de sz_orders pelo {id} da rota, NUNCA por input do cliente;
		// cross-user → 404 ANTES de qualquer escrita. NENHUMA rota admin sensível
		// (settings/saques/CRUD) é exposta a tokens de portal — elas permanecem no grupo
		// auth.Middleware acima (iss=senderzz-admin obrigatório).
		r.Group(func(r chi.Router) {
			r.Use(auth.DualAuth(pool))
			r.Get("/orders/motoboy", ordH.ListMotoboy)
			r.Get("/orders/{id}", odH.Get)
			r.Get("/products", prdH.List)
			r.Get("/affiliates", affH.List)
			// Mutações com ownership-gate (admin muta tudo; portal só o que é DELE).
			r.Post("/orders/motoboy/{id}/reagendar", ordH.Reagendar)
			r.Post("/orders/motoboy/{id}/reagendar-clone", ordH.ReagendarClone)
			r.Post("/orders/motoboy/{id}/cancelar", ordH.Cancelar)
			// LEITURA com ownership-gate (read-only): a cópia da tela no PORTAL precisa
			// da agenda da zona p/ desabilitar dias fora de funcionamento no picker de
			// reagendar. Mesmo gate das mutações (produtor/afiliado só pedido DELE → 404).
			r.Get("/orders/motoboy/{id}/zona-schedule", ordH.ZonaSchedule)
			// Visão global de Expedição para o operador logístico — mesma lista do
			// admin, sem abrir a tela financeira/configurável inteira.
			r.Get("/operator/expedicao/orders", expOrdersH.ListOperator)
			// PDF/ZIP de etiquetas e declarações já emitidas. O handler reforça o
			// gate admin/OL; produtor e afiliado recebem 403 mesmo sob DualAuth.
			r.Post("/operator/expedicao/print-batch", bulkH.PrintBatch)
			// Fila operacional de saques para o OL. Regras globais, overrides e
			// demais configurações financeiras permanecem exclusivamente Admin.
			r.Get("/operator/saques/producer", saquesH.ListProducerOperator)
			r.Post("/operator/saques/producer/{id}/mark-paid", saquesH.MarkProducerPaidOperator)
			r.Post("/operator/saques/producer/{id}/reject", saquesH.RejectProducerOperator)
			r.Post("/operator/saques/producer/{id}/upload-proof", saquesH.UploadProducerProofOperator)
			r.Get("/operator/saques/affiliate", saquesH.ListAffiliateOperator)
			r.Post("/operator/saques/affiliate/{id}/approve", saquesH.ApproveAffiliateOperator)
			r.Post("/operator/saques/affiliate/{id}/reject", saquesH.RejectAffiliateOperator)
			r.Post("/operator/saques/affiliate/{id}/upload-proof", saquesH.UploadAffiliateProofOperator)
			// Operador logístico — ações de embalagem + lista motoboys p/ dropdown.
			// Handler tem gate interno (OL + admin); produtor/afiliado → 403.
			r.Post("/bulk-actions/motoboy-generate-labels", bulkH.GenerateMotoboyLabels)
			r.Get("/motoboys/minimal", motH.Minimal)
			// Transição de status manual (Em Rota / Entregue / Frustrado) — admin + OL.
			// ForceMotoboyStatus tem ownership-gate interno; OL vê tudo, produtor/afiliado → 403.
			r.Post("/orders/{id}/force-motoboy-status", odH.ForceMotoboyStatus)
			r.Post("/orders/{id}/financial-status", odH.UpdateFinancialStatus)
			r.Post("/orders/{id}/upload-evidence", odH.UploadEvidence)
			// Lista etiquetas por data (GET /motoboy-etiquetas?date=&status=) — admin + OL p/ "Imprimir por data".
			r.Get("/motoboy-etiquetas", mbEtqH.List)
			// AUDIT-2026-07-31 (dono): mark-packed/mark-collected tinham gate interno
			// pra operador logístico (auth.ActorFromCtx) mas só estavam registrados
			// no grupo admin-only (linha ~396) — OL nunca alcançava o handler, o
			// middleware já barrava antes. Registra também aqui (DualAuth), mesmo
			// padrão de force-motoboy-status acima.
			r.Post("/orders/{id}/mark-packed", odH.MarkPacked)
			r.Post("/orders/{id}/mark-collected", odH.MarkCollected)
			// Tela de pedidos (mesma lista do admin) — OL precisa enxergar pra agir.
			r.Get("/bulk-actions/orders", bulkH.ListOrders)
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
