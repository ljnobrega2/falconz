// Serviço Go do Portal V2 — Senderzz Fase 5 (strangler fig).
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL   — DSN Postgres (pgx format), ex: postgres://user:pass@localhost:5432/senderzz
//   - JWT_SECRET     — secret HS256 para emissão/validação de tokens do portal
//   - WP_SALT_AUTH   — AUTH_SALT do WordPress (para validar sessões PHP legadas)
//
// Variáveis opcionais:
//   - PORT           — porta HTTP (default: 8085)
//   - REDIS_URL      — DSN Redis para Asynq (default: redis://localhost:6379)
//     Se ausente, worker de webhooks não é iniciado (modo sem filas).
//
// Base path: /wp-json/senderzz/v1/portal
// Health check: GET /health
//
// Fail-closed:
//
//	JWT_SECRET vazio   → 503 em todas as rotas autenticadas.
//	WP_SALT_AUTH vazio → 503 em todas as rotas autenticadas.
//	DATABASE_URL vazio → exit(1) no startup.
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
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/db"
	"github.com/senderzz/portal-service/internal/handlers"
	"github.com/senderzz/portal-service/internal/httpx"
	"github.com/senderzz/portal-service/internal/jobs"
)

func main() {
	// Configura slog JSON (compatível com Cloud Logging / Datadog).
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── Secrets fail-closed (visibilidade no boot) ────────────────────────────
	// NÃO derruba o processo: o comportamento autoritativo continua sendo o 503
	// por requisição (auth.AuthPortalJWT) quando JWT_SECRET/WP_SALT_AUTH estão
	// vazios. Aqui só logamos um ERRO claro e visível no startup para que a
	// configuração ausente apareça nos logs/alertas antes do primeiro 503.
	// Nunca logamos o valor do secret. // SEC-SECRETS-BOOT-VISIBILITY
	for _, name := range []string{"JWT_SECRET", "WP_SALT_AUTH"} {
		if os.Getenv(name) == "" {
			slog.Error("[main] secret crítico ausente — rotas autenticadas responderão 503",
				"secret", name)
		}
	}

	// ── Banco de dados ────────────────────────────────────────────────────────
	pool, err := db.Connect(ctx)
	if err != nil {
		slog.Error("[main] falha ao conectar banco", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// ── Asynq (opcional — worker de webhooks) ─────────────────────────────────
	var asynqClient *asynq.Client
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	redisOpt, redisErr := asynq.ParseRedisURI(redisURL)
	if redisErr != nil {
		slog.Warn("[main] REDIS_URL inválida — worker de webhooks desativado", "err", redisErr)
	} else {
		asynqClient = asynq.NewClient(redisOpt)
		defer asynqClient.Close()
		// Inicia worker de webhooks em goroutine separada.
		// AUDIT-2026-06-18: startWebhookWorker exige o tipo concreto asynq.RedisClientOpt,
		// mas asynq.ParseRedisURI retorna a interface RedisConnOpt — assert para redis://
		// simples (caso cluster/sentinel, o worker fica desativado sem derrubar o server).
		if clientOpt, ok := redisOpt.(asynq.RedisClientOpt); ok {
			go startWebhookWorker(ctx, clientOpt, pool)
		} else {
			slog.Warn("[main] REDIS_URL não é cliente simples — worker de webhooks desativado")
		}
	}

	// Variável mantida para uso futuro (EnqueueWebhookDispatch nos handlers).
	_ = asynqClient

	// ── Handlers ──────────────────────────────────────────────────────────────
	authH := &handlers.AuthHandler{Pool: pool}
	webhookH := &handlers.WebhookHandler{Pool: pool}
	integrationsH := &handlers.IntegrationsHandler{Pool: pool, OrdersServiceURL: os.Getenv("ORDERS_SERVICE_URL")}
	settingsH := &handlers.SettingsHandler{Pool: pool}
	docChangeH := &handlers.DocumentChangeHandler{Pool: pool}

	// Handlers de seção do Portal V2 — construção idêntica a WebhookHandler
	// (somente Pool). Cada um faz o seu próprio user-scoping pela sessão.
	ordersH := &handlers.OrdersHandler{Pool: pool}
	walletH := &handlers.WalletHandler{Pool: pool}
	walletExpeditionH := &handlers.WalletexpeditionHandler{Pool: pool}
	productsH := &handlers.ProductsHandler{Pool: pool}
	vitrineH := &handlers.VitrineHandler{Pool: pool}
	motoboyH := &handlers.MotoboyHandler{Pool: pool}
	motoboysDiaH := &handlers.MotoboysdiaHandler{Pool: pool}
	expedicaoH := &handlers.ExpedicaoHandler{Pool: pool}
	freightH := &handlers.FreightHandler{Pool: pool}
	linksH := &handlers.LinksHandler{Pool: pool}
	localidadesH := &handlers.LocalidadesHandler{Pool: pool}
	stockH := &handlers.StockHandler{Pool: pool}
	reportsH := &handlers.ReportsHandler{Pool: pool}
	affiliatesH := &handlers.AffiliatesHandler{Pool: pool}
	affiliateDashboardH := &handlers.AffiliateDashboardHandler{Pool: pool} // FEAT-PORTAL-SALES
	affiliateWalletH := &handlers.AffiliateWalletHandler{Pool: pool}       // saque + antecipação da comissão do afiliado
	usersH := &handlers.UsersHandler{Pool: pool}
	supportH := &handlers.SupportHandler{Pool: pool}
	// FEAT-PORTAL / FEAT-NOTIF — novos handlers (sessões, estoque do produtor, push).
	sessionsH := &handlers.SessionsHandler{Pool: pool}
	stockProducerH := &handlers.StockProducerHandler{Pool: pool}
	notifH := &handlers.NotifHandler{Pool: pool}
	// LGPD — canal PÚBLICO do titular (Art. 18). Cliente final/recebedor/motoboy
	// sem conta exercem direitos por aqui (sem auth). // AUDIT LGPD-no-data-subject-channel
	dataRequestH := &handlers.DataRequestHandler{Pool: pool}

	// Middleware de autenticação JWT (injetado nas rotas protegidas).
	requireAuth := auth.AuthPortalJWT(pool)

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middlewares globais.
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(slogMiddleware)
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware)

	// Health check — fora do prefixo WP, usado pelo nginx e K8s.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ok", "service": "portal"})
	})

	// Readiness — separado de /health: sinaliza se o serviço está PRONTO para
	// receber tráfego (pool de DB respondendo). Fora de qualquer grupo de auth.
	// 200 = pronto; 503 = não pronto (o orquestrador segura o tráfego). // READYZ
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			slog.Warn("[readyz] banco inacessível", "err", err)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "não pronto: banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ready", "service": "portal"})
	})

	// ── Push notifications (namespace sz-notif) ───────────────────────────────── // FEAT-NOTIF
	// SEC-RBAC (auditoria 2026-06-18): AGORA AUTENTICADO (requireAuth). Antes era
	// público e lia o user_id do body/query → IDOR (assinar/ler/sobrescrever push de
	// outro usuário). O dono passa a vir SEMPRE da sessão (u.WPUserID), espelhando o
	// PHP autoritativo (sz_notif_rest_get_user). Ambos os fronts já autenticam: o PWA
	// envia o cookie sz_portal_token (credentials:'same-origin'), o React anexa Bearer.
	r.Route("/wp-json/sz-notif/v1", func(r chi.Router) {
		r.Use(requireAuth)
		r.Post("/subscribe", notifH.Subscribe)
		r.Post("/unsubscribe", notifH.Unsubscribe)
		r.Get("/prefs", notifH.GetPrefs)
		r.Post("/prefs", notifH.SavePrefs)
	})

	// Todas as rotas sob o prefixo canônico do WP REST.
	// nginx proxeia /wp-json/senderzz/v1/portal/* → Go sem rewrite.
	r.Route("/wp-json/senderzz/v1", func(r chi.Router) {

		// ── Rotas públicas (sem autenticação) ─────────────────────────────────
		// POST /portal/login — credenciais → partial_token + 2FA
		r.Post("/portal/login", authH.Login)
		// POST /portal/login/2fa — partial_token + código → JWT completo
		r.Post("/portal/login/2fa", authH.Login2FA)
		// POST /portal/forgot-password — gera token e envia e-mail de reset
		r.Post("/portal/forgot-password", authH.ForgotPassword)
		// POST /portal/reset-password — valida token e redefine senha
		r.Post("/portal/reset-password", authH.ResetPassword)

		// LGPD — CANAL PÚBLICO do titular (Art. 18). Sem auth de propósito: o
		// cliente final/recebedor/motoboy NÃO tem conta no portal e ainda assim
		// pode exercer direitos (acesso, correção, exclusão, portabilidade,
		// revogação, oposição, info de compartilhamento). Identifica o titular
		// pelo e-mail; abre protocolo (status 'recebido'); rate-limit por IP.
		// AUDIT LGPD-no-data-subject-channel
		r.Post("/portal/data-request", dataRequestH.DataRequest)

		// ── Rotas protegidas (requer JWT válido + sessão no banco) ────────────
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)

			// Auth
			r.Post("/portal/logout", authH.Logout)
			r.Post("/portal/refresh", authH.Refresh)
			r.Get("/portal/me", authH.Me)

			// Sessões ativas (list / revoke-all exceto a atual). // FEAT-PORTAL
			r.Get("/portal/sessions", sessionsH.List)
			r.Post("/portal/sessions/revoke-all", sessionsH.RevokeAll)

			// Webhooks.
			// Atenção à ordem: /webhooks/clear-history ANTES de /webhooks/{id}
			// para que chi não interprete "clear-history" como {id}.
			r.Get("/portal/webhooks", webhookH.List)
			r.Post("/portal/webhooks", webhookH.Create)
			r.Post("/portal/webhooks/clear-history", webhookH.ClearHistory)
			r.Delete("/portal/webhooks/{id}", webhookH.Delete)
			r.Get("/portal/webhooks/{id}/history", webhookH.History)
			// Disparo síncrono: teste + reenvio do último payload. // FEAT-PORTAL
			r.Post("/portal/webhooks/{id}/test", webhookH.Test)
			r.Post("/portal/webhooks/{id}/resend", webhookH.Resend)

			// Integrações
			r.Get("/portal/integrations", integrationsH.List)
			r.Post("/portal/integrations/toggle", integrationsH.Toggle)
			r.Delete("/portal/integrations/logs", integrationsH.ClearLogs)
			// Rotate secret / reprocess: 501 (token + pipeline de recebimento não migrados).
			r.Post("/portal/integrations/rotate", integrationsH.Rotate)
			r.Post("/portal/integrations/reprocess", integrationsH.Reprocess)
			// Mapeamento de campos: persiste em integrations.mapping_json (JSONB). // FEAT-PORTAL
			r.Post("/portal/integrations/mapping", integrationsH.SaveMapping)

			// Configurações
			r.Get("/portal/settings", settingsH.Get)
			r.Post("/portal/settings", settingsH.Update)
			r.Post("/portal/settings/2fa", settingsH.Toggle2FA)
			// CHECKOUT-BRANDING (white-label v1): logo + cor primária do checkout do
			// produtor (senderzz_portal_user_meta). GET = autenticado; POST = produtor.
			r.Get("/portal/settings/brand", settingsH.GetBrand)
			r.Post("/portal/settings/brand", settingsH.UpdateBrand)
			r.Post("/portal/settings/brand/upload", settingsH.UploadPublicImage)
			// Conta: alterar e-mail de login e senha (espelha settings.php). // FEAT-PORTAL
			r.Post("/portal/account/email", settingsH.ChangeEmail)
			r.Post("/portal/account/password", settingsH.ChangePassword)
			// FEAT-DOC-CHANGE-2026-07-03: troca CPF⇄CNPJ do titular (aprovação do admin).
			r.Get("/portal/account/document-change", docChangeH.Get)
			r.Post("/portal/account/document-change", docChangeH.Create)
			// LGPD: exclusão/anonimização da PRÓPRIA conta (id da sessão; nunca
			// arbitrário). Anonimiza PII + soft-flag; preserva o ledger financeiro.
			// AUDIT LGPD-no-data-deletion-api
			r.Post("/portal/account/delete", settingsH.DeleteAccount)
			// LGPD: direito de acesso/portabilidade (Art. 18) — "baixar meus dados".
			// Exporta perfil (sem password_hash), settings, integrations (sem secrets),
			// pedidos (resumo), afiliações e sessões ativas (sem token raw). Registra
			// a ação em senderzz_pii_access_log (action='export', subject_type='self').
			r.Get("/portal/account/export", settingsH.AccountExport)
			// LGPD: consentimento versionado (aceite de política/termo). Idempotente
			// (ON CONFLICT DO NOTHING) — re-aceitar a mesma versão devolve o accepted_at.
			r.Post("/portal/account/consent", settingsH.AccountConsent)
			// LGPD: revogação de consentimento (Art. 8º §5º). Seta revoked_at=NOW()
			// na linha do PRÓPRIO titular (mantém a linha — NUNCA deleta). Idempotente.
			r.Post("/portal/account/consent/revoke", settingsH.AccountConsentRevoke)

			// Pedidos
			// Atenção à ordem: /orders/export.csv ANTES de /orders/{id}
			// para que chi não interprete "export.csv" como {id}.
			r.Get("/portal/orders", ordersH.List)
			r.Get("/portal/orders/export.csv", ordersH.ExportCSV) // FEAT-PORTAL
			r.Get("/portal/orders/{id}", ordersH.Detail)
			// Mutações do pedido motoboy escopadas ao AFILIADO dono (affiliate_id =
			// u.WPUserID). {id} = sz_orders.id (mesma convenção do Detail). 404 se não-dono.
			r.Post("/portal/orders/{id}/reagendar", ordersH.ReagendarAfiliado)
			r.Post("/portal/orders/{id}/reagendar-clone", ordersH.ReagendarCloneAfiliado)
			r.Post("/portal/orders/{id}/cancelar", ordersH.CancelarAfiliado)

			// Carteira
			r.Get("/portal/wallet/summary", walletH.Summary)
			r.Get("/portal/wallet/history", walletH.History)
			r.Get("/portal/wallet/future", walletH.Future)
			r.Get("/portal/wallet/withdrawals", walletH.Withdrawals)
			r.Get("/portal/wallet/accounts", walletH.Accounts)
			r.Post("/portal/wallet/accounts", walletH.AddAccount) // cadastrar conta PIX (Settings → Saques)
			r.Post("/portal/wallet/withdraw", walletH.Withdraw)
			r.Post("/portal/wallet/anticipate", walletH.Anticipate)
			// Transferência p/ carteira de frete (produtor, aprovação admin). FEAT-WFT
			r.Post("/portal/wallet/freight-transfer", walletH.FreightTransfer)
			r.Get("/portal/wallet/freight-transfers", walletH.FreightTransfers)
			// Ganho EFETIVAMENTE CREDITADO (lifetime) p/ a barra de gamificação "rumo a
			// R$ 1 mi" da sidebar (ProgressTier). Comissão líquida creditada p/ afiliado/
			// cliente; COD recebido na carteira p/ produtor; 0 p/ operador/demais.
			r.Get("/portal/earnings-credited", walletH.EarningsCredited)

			// Carteira do AFILIADO — saque + antecipação da PRÓPRIA comissão (espelho
			// do COD do produtor, sobre o ledger senderzz_affiliate_transactions).
			// Escopo SEMPRE pelo afiliado da sessão (afiliado_id = wp_user_id) — nunca
			// do cliente/body. Gate de role afiliado dentro do handler. // FEAT-AFF-SAQUE
			r.Post("/portal/affiliate-wallet/withdraw", affiliateWalletH.WithdrawAffiliate)
			r.Post("/portal/affiliate-wallet/anticipate", affiliateWalletH.AnticipateAffiliate)

			// Carteira de Expedição (TPC — saldo pré-pago de frete; read-only aqui).
			// Recarga PIX (generate_pix/check_pix) segue no WP/admin-ajax nesta fase.
			r.Get("/portal/wallet-expedition/summary", walletExpeditionH.Summary)
			r.Get("/portal/wallet-expedition/history", walletExpeditionH.History)

			// Produtos.
			// Atenção à ordem: /products/cds ANTES de /products/{id}
			// para que chi não interprete "cds" como {id}.
			r.Get("/portal/products", productsH.List)
			r.Post("/portal/products", productsH.Create) // FEAT-PORTAL-SALES — criar produto (só produtor)
			r.Get("/portal/products/cds", productsH.CDs)
			r.Get("/portal/products/{id}", productsH.Detail)
			r.Put("/portal/products/{id}", productsH.Update) // FEAT-PORTAL-SALES — editar produto (só produtor)
			r.Post("/portal/products/{id}/image", productsH.SaveImage)
			r.Delete("/portal/products/{id}", productsH.Delete)             // FEAT-PORTAL-SALES — soft-delete (só produtor)
			r.Get("/portal/products/{id}/variations", productsH.Variations) // FEAT-PORTAL
			r.Get("/portal/products/{id}/checkouts", productsH.Checkouts)   // checkouts vinculados ao produto (escopo por dono)
			r.Post("/portal/products/{id}/vitrine-description", productsH.SaveVitrineDescription)
			r.Post("/portal/products/{id}/vitrine-toggle", productsH.VitrineToggle)

			// Vitrine
			r.Get("/portal/vitrine", vitrineH.List)
			r.Post("/portal/vitrine/affiliate", vitrineH.Affiliate)
			// Cancela um pedido de afiliação PENDENTE do próprio usuário (#72). Estática
			// ANTES de qualquer rota /portal/vitrine/{...} parametrizada (não há, mas a
			// convenção do projeto é registrar estáticas primeiro).
			r.Post("/portal/vitrine/affiliate/cancel", vitrineH.CancelAffiliation)

			// Motoboy (Cash On Delivery)
			// Atenção à ordem: /motoboy/bulk ANTES de /motoboy/{id}/*
			// (rota estática vs. parametrizada — chi casa a primeira que registrar).
			r.Get("/portal/motoboy", motoboyH.List)
			r.Post("/portal/motoboy/bulk", motoboyH.BulkAction) // FEAT-PORTAL
			r.Post("/portal/motoboy/{id}/cancelar", motoboyH.Cancelar)
			r.Post("/portal/motoboy/{id}/reagendar", motoboyH.Reagendar)
			// Reagendar por CLONE (só frustrado) — produtor dono. {id} = wc_order_id.
			r.Post("/portal/motoboy/{id}/reagendar-clone", motoboyH.ReagendarClone)

			// Motoboys — dia: KPIs por motoboy do dia (user-scoped). Read-only.
			// No grupo geral (NÃO no AuthRole("operator")): o handler re-escopa por
			// dono e serve produtor/afiliado — sob operator-only o produtor levaria 403.
			r.Get("/portal/motoboys-dia", motoboysDiaH.List)

			// Expedição
			r.Get("/portal/expedicao", expedicaoH.List)
			// Bip/SKU na embalagem (modalidade Expedição operada pelo OL): valida o
			// SKU bipado contra os itens do pedido (sz_order_items.sku) e registra a
			// leitura em sz_pack_scans (context='expedicao'). 422 se não conferir —
			// NÃO marca embalado (isso segue no WP-AJAX via hook de carteira TPC).
			r.Post("/portal/expedicao/bipar", expedicaoH.Bipar)
			// Aprovar (green-light do produtor) — nativo Go, substitui admin-ajax.php
			// morto (nonce nunca mintado pelo SPA). Sem efeito de carteira/etiqueta.
			r.Post("/portal/expedicao/{id}/approve", expedicaoH.Approve)
			r.Post("/portal/expedicao/{id}/cancel", expedicaoH.Cancel)

			// Estoque (custódia física — read-only; espelha stock.php).
			r.Get("/portal/stock", stockH.List)
			// Estoque REAL do produtor (sz_stock — distinto do /portal/stock acima). // FEAT-PORTAL
			r.Get("/portal/producer-stock", stockProducerH.List)
			// Remessas produtor→CD (FEAT-STOCK-SHIPMENTS — paridade senderzz-stock-shipments.php)
			r.Get("/portal/producer-stock/shipments", stockProducerH.ListShipments)
			r.Post("/portal/producer-stock/shipments", stockProducerH.CreateShipment)
			r.Post("/portal/producer-stock/shipments/{id}/send", stockProducerH.SendShipment)

			// Frete (transportadoras preferidas/bloqueadas)
			r.Get("/portal/freight", freightH.Get)
			r.Post("/portal/freight/preferred", freightH.SavePreferred)
			r.Post("/portal/freight/blocked", freightH.SaveBlocked)

			// Links de checkout
			r.Get("/portal/links", linksH.List)
			r.Post("/portal/links", linksH.Create) // FEAT-PORTAL-SALES — gerar oferta/checkout (só produtor)
			r.Post("/portal/links/{id}/affiliate-toggle", linksH.AffiliateToggle)
			r.Post("/portal/links/{id}/commission", linksH.CommissionUpdate)
			r.Post("/portal/links/{id}/banner", linksH.BannerUpdate)
			r.Delete("/portal/links/{id}", linksH.Delete)

			// Localidades (Áreas de operação — CDs/regiões + zonas atendidas)
			r.Get("/portal/localidades", localidadesH.List)
			r.Get("/portal/localidades/{id}/zonas", localidadesH.Zonas)

			// Relatórios (read-only — métricas agregadas COD/Expedição do período)
			r.Get("/portal/reports", reportsH.List)

			// Afiliados — programa de afiliados do produtor (aprovar/recusar/comissão,
			// config padrão/auto-approve) + visão "Minha afiliação" do usuário.
			// Atenção à ordem: rotas estáticas (default-commission, auto-approve,
			// invites) ANTES de /affiliates/{id} para o chi não casar como {id}.
			r.Get("/portal/affiliates", affiliatesH.List)
			r.Get("/portal/affiliates/referral", affiliatesH.Referral)           // #71 — link de indicação FIXO/PERMANENTE do usuário (/r/{code})
			r.Get("/portal/affiliates/dashboard", affiliateDashboardH.Dashboard) // FEAT-PORTAL-SALES (KPIs de comissão do afiliado)
			r.Get("/portal/affiliates/export.csv", affiliatesH.ExportCSV)        // FEAT-PORTAL (comissões)
			r.Post("/portal/affiliates/default-commission", affiliatesH.DefaultCommission)
			r.Post("/portal/affiliates/auto-approve", affiliatesH.AutoApprove)
			r.Post("/portal/affiliates/per-affiliate-override", affiliatesH.PerAffiliateOverride)
			r.Post("/portal/affiliates/invites", affiliatesH.CreateInvite)
			r.Post("/portal/affiliates/invites/redeem", affiliatesH.RedeemInvite) // FEAT-INVITES: fecha o ciclo (token → vínculo)
			r.Delete("/portal/affiliates/invites/{id}", affiliatesH.RevokeInvite)
			r.Post("/portal/affiliates/{id}/approve", affiliatesH.Approve)
			r.Post("/portal/affiliates/{id}/reject", affiliatesH.Reject)
			r.Post("/portal/affiliates/{id}/commission", affiliatesH.Commission)
			r.Delete("/portal/affiliates/{id}", affiliatesH.Delete)

			// Usuários (equipe / sub-usuários) — só o usuário principal gerencia
			// (gate isManager: parent_user_id NULL/0). Subconta → 403.
			r.Get("/portal/users", usersH.List)
			r.Post("/portal/users", usersH.Create)
			r.Delete("/portal/users/{id}", usersH.Delete)

			// Suporte (Tickets) — escopado por portal_user_id; serve produtor e
			// afiliado. Espelha src/Portal/Portal_Page.php (ajax_tickets_*).
			r.Get("/portal/support/tickets", supportH.List)
			r.Post("/portal/support/tickets", supportH.Create)
			r.Get("/portal/support/tickets/{id}", supportH.Detail)
			r.Post("/portal/support/tickets/{id}/messages", supportH.SendMessage)
			r.Post("/portal/support/tickets/{id}/close", supportH.Close)

			// ── Rotas restritas por role ───────────────────────────────────────
			// Apenas operadores logísticos (OL) acessam rotas admin do portal.
			r.Group(func(r chi.Router) {
				r.Use(auth.AuthRole("operator"))
				r.Get("/portal/admin/status", func(w http.ResponseWriter, r *http.Request) {
					httpx.WriteOK(w, map[string]any{"role": "operator", "acesso": "ok"})
				})
			})
		})
	})

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8085"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("[main] servidor portal iniciado",
			"port", port,
			"base", "/wp-json/senderzz/v1/portal",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("[main] ListenAndServe falhou", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("[main] sinal recebido, encerrando gracefully…")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("[main] shutdown forçado", "err", err)
	}
	slog.Info("[main] servidor encerrado")
}

// startWebhookWorker inicia o servidor Asynq para processar tarefas de disparo de webhook.
// Executado em goroutine separada — encerra quando ctx for cancelado.
func startWebhookWorker(ctx context.Context, redisOpt asynq.RedisClientOpt, pool *pgxpool.Pool) {
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 10,
		Queues: map[string]int{
			"webhooks": 10,
			"default":  5,
		},
	})

	// Registra o handler de disparo de webhook no mux Asynq.
	dispatcher := jobs.NewWebhookDispatcher(pool)
	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeWebhookDispatch, dispatcher.ProcessTask)

	slog.Info("[worker] worker de webhooks iniciado")

	// srv.Start é bloqueante — executa em goroutine já.
	go func() {
		if err := srv.Start(mux); err != nil {
			slog.Error("[worker] falha ao iniciar worker de webhooks", "err", err)
		}
	}()

	<-ctx.Done()
	srv.Shutdown()
	slog.Info("[worker] worker de webhooks encerrado")
}

// ── Middlewares inline ────────────────────────────────────────────────────────

// slogMiddleware registra cada requisição com slog estruturado.
func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("[http]",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", chimw.GetReqID(r.Context()),
		)
	})
}

// originAllowed verifica o Origin contra a allowlist em ALLOWED_ORIGINS (CSV).
// AUDIT-2026-06-18 (Onda 1): antes o CORS ecoava QUALQUER Origin com
// Allow-Credentials:true — qualquer site podia fazer requisições autenticadas
// por cookie de sessão (CSRF). Agora só ecoa origens explicitamente permitidas.
func originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" && strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// corsMiddleware permite requisições cross-origin apenas de origens na allowlist.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, X-Senderzz-Token, X-Request-ID, Cookie")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
