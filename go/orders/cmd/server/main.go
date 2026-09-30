// Serviço Go de Pedidos — Senderzz Fase 6 (strangler fig).
//
// Este serviço substitui o WooCommerce como sistema de gestão de pedidos.
// É a última fase antes do shutdown do WordPress.
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL    — DSN Postgres (pgx format)
//   - JWT_SECRET      — mesmo que tpc_jwt_secret do WP
//   - WEBHOOK_SECRET  — para validação HMAC de webhooks de pagamento
//   - WP_SALT_AUTH    — AUTH_SALT do WordPress (sessões de portal)
//
// Variáveis opcionais:
//   - PORT            — porta HTTP (default: 8086)
//   - REDIS_URL       — DSN Redis para Asynq (jobs de expiração e sync).
//     Se ausente, o scheduler e os workers de expiração/sync NÃO são iniciados
//     (apenas o HTTP server roda — fail-soft).
//   - ORDERS_CORS_ORIGINS — allowlist de origens CORS separada por vírgula
//     (ex.: "https://painel.senderzz.com,https://app.senderzz.com").
//     Vazio → nenhum Origin recebe cabeçalhos credenciados (fail-closed, AUDIT SEC-GO-CORS-REFLECT).
//
// Base path: /wp-json/senderzz/v1 (nginx proxeia sem rewrite)
// Health check: GET /health
//
// Fase 6 escopo:
//   - CRUD de pedidos com máquina de estados
//   - Pagamentos (COD, PIX stub, wallet stub)
//   - Migração de wp_wc_orders via pgloader
//   - Double-write reverso para WP via job ProcessStatusSync
//   - Exportação CSV
//
// Fase 7 (planejado):
//   - Integração NATS para eventos de status
//   - Chamadas reais ao wallet-service para PIX e débito de carteira
//   - Remoção do double-write após cutover completo
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/orders-service/internal/auth"
	"github.com/senderzz/orders-service/internal/db"
	"github.com/senderzz/orders-service/internal/handlers"
	"github.com/senderzz/orders-service/internal/httpx"
	"github.com/senderzz/orders-service/internal/jobs"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Inicializa o pool de conexões Postgres.
	pool, err := db.Connect(ctx)
	if err != nil {
		slog.Error("[orders] falha ao conectar ao banco", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Instancia os handlers com injeção de dependência.
	orderH := handlers.NewOrderHandler(pool)
	paymentH := handlers.NewPaymentHandler(pool)
	checkoutH := handlers.NewCheckoutHandler(pool)

	// ── Asynq: workers + scheduler de jobs de pedidos ────────────────────────
	// AUDIT CRON-orders-jobs-no-scheduler: os handlers ProcessOrderExpiry e
	// ProcessStatusSync existiam mas nenhum servidor Asynq os processava — pedidos
	// pending nunca expiravam e o double-write reverso nunca rodava.
	// Idêntico ao padrão de go/wallet/cmd/server/main.go.
	//
	// Fail-soft: sem REDIS_URL, o serviço sobe só com HTTP (sem expiração/sync).
	var asynqServer *asynq.Server
	var asynqScheduler *asynq.Scheduler
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		// FEAT-ORDERS-SCHED (AUDIT CRON-orders-jobs-no-scheduler):
		// Sem Redis (DEV — e prod sem Asynq) o caminho acima nunca sobe, então os
		// pedidos 'pending' NUNCA expiravam. Aqui rodamos a expiração com um
		// time.Ticker + goroutine (mesmo padrão de go/cron/cmd/server/main.go),
		// sem exigir Asynq/Redis. Fica no branch SEM Redis de propósito: quando
		// Redis está presente, o scheduler Asynq abaixo já dispara TaskOrderExpiry
		// (*/5) — não queremos as duas vias rodando o mesmo job e gerando erros
		// espúrios de "transição inválida" sobre pedidos já cancelados.
		slog.Warn("[orders] REDIS_URL ausente — scheduler Asynq desativado; expiração de pedidos rodará via ticker interno (sem Redis)")
		startExpiryTicker(ctx, pool)
	} else {
		redisOpt, err := asynq.ParseRedisURI(redisURL)
		if err != nil {
			slog.Error("[orders] REDIS_URL inválida — Asynq desativado", "err", err)
		} else {
			// Servidor de workers: processa a fila "orders".
			asynqServer = asynq.NewServer(redisOpt, asynq.Config{
				Concurrency: 4,
				Queues: map[string]int{
					"orders":  10,
					"default": 5,
				},
				// ErrorHandler: loga falhas de task para observabilidade (AUDIT CRON-...).
				ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
					slog.Error("[asynq] task falhou",
						"type", task.Type(),
						"err", err,
					)
				}),
			})

			// Registra os handlers de task. Os workers recebem o pool por closure.
			mux := asynq.NewServeMux()
			mux.HandleFunc(jobs.TaskOrderExpiry, func(ctx context.Context, t *asynq.Task) error {
				return jobs.ProcessOrderExpiry(ctx, pool, t)
			})
			mux.HandleFunc(jobs.TaskStatusSync, func(ctx context.Context, t *asynq.Task) error {
				return jobs.ProcessStatusSync(ctx, pool, t)
			})

			// Scheduler: dispara a expiração de pedidos a cada 5 minutos.
			// ProcessStatusSync é enfileirado sob demanda pela statemachine
			// (EnqueueStatusSync) — não tem cron próprio.
			asynqScheduler = asynq.NewScheduler(redisOpt, nil)
			if _, err := asynqScheduler.Register(
				"*/5 * * * *",
				asynq.NewTask(jobs.TaskOrderExpiry, nil,
					asynq.TaskID("orders:expiry:cron"),
					asynq.MaxRetry(3),
					asynq.Queue("orders"),
				),
			); err != nil {
				slog.Error("[orders] falha ao registrar schedule de expiração", "err", err)
				// Não fatal — o HTTP server continua operando.
			}

			go func() {
				slog.Info("[asynq] iniciando servidor de workers", "redis", redisURL)
				if err := asynqServer.Run(mux); err != nil {
					slog.Error("[asynq] servidor de workers encerrado com erro", "err", err)
				}
			}()
			go func() {
				slog.Info("[asynq] iniciando scheduler (expiração de pedidos a cada 5 min)")
				if err := asynqScheduler.Run(); err != nil {
					slog.Error("[asynq] scheduler encerrado com erro", "err", err)
				}
			}()
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	r := chi.NewRouter()

	// ── Middlewares globais ───────────────────────────────────────────────────
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(slogMiddleware)
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware)

	// ── Health (liveness) + Readiness ─────────────────────────────────────────
	// /health  → liveness + sanidade do banco (comportamento PRESERVADO: Ping → 200/503).
	// /readyz  → readiness explícita (pool.Ping → 200 pronto / 503 não-pronto). Separado
	//            de /health para que o orquestrador tire a instância do balanceador quando
	//            o Postgres cai SEM matar o processo. Mesmo padrão de go/affiliates.
	r.Get("/health", healthHandler(pool))
	r.Get("/readyz", readyzHandler(pool))

	// ── Checkout público (SEM JWT) ────────────────────────────────────────────
	// Endpoints chamados pela página de checkout nativa do produtor/afiliado.
	// Ficam na RAIZ do router (não sob /wp-json) — caminho literal /checkout-api/*.
	// Preço SEMPRE server-side (senderzz_checkout_links.display_value).
	//
	// P2-SEC rate-limit checkout: estas rotas são públicas (sem JWT). /checkout-api/freight
	// dispara uma cotação PAGA ao Melhor Envio por request — sem limite, vira vetor de
	// amplificação de custo + spam de pedidos. httprate.LimitByIP(30, time.Minute) aplica
	// um orçamento compartilhado de 30 req/min POR IP a toda a superfície /checkout-api/*.
	// NOTA: o store é in-memory por instância — atrás de N upstreams nginx o limite efetivo
	// é N×30 (aceitável para o objetivo de anti-amplificação; revisar se escalar horizontalmente).
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitByIP(30, time.Minute))

		r.Get("/checkout-api/offer", checkoutH.GetOffer)
		r.Get("/checkout-api/schedule", checkoutH.GetSchedule)
		// FEAT-FRETE: cotação de frete (ofertas tipo correio). Motoboy → options:[].
		r.Get("/checkout-api/freight", checkoutH.GetFreight)
		// FEAT-LINK-MISTO: resolve COD vs Expedição pelo CEP (link tipo='misto').
		r.Get("/checkout-api/resolve", checkoutH.GetResolveMode)
		r.Post("/checkout-api/order", checkoutH.PostOrder)
		// Rastreio público do pedido (cliente final, sem JWT) — resolve por code
		// (order_number SZ-NNNNNNN ou id em pedidos migrados).
		r.Get("/checkout-api/order/{code}", checkoutH.GetOrderTracking)
	})

	// API pública de pedidos (menu Integrações do portal) — auth por API key
	// (Authorization: Bearer), não JWT. Rate limit por IP como defesa básica
	// contra abuso; auth real é a própria chave (resolveAPIProducer).
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitByIP(60, time.Minute))
		r.Post("/api/v1/orders", checkoutH.PostAPIOrder)
		r.Get("/api/v1/zonas", checkoutH.GetAPIZonas)
	})

	// Reprocessar última tentativa falha (rede Docker só — nunca no gateway
	// público). portal-service chama depois de validar o produtor via JWT.
	r.Post("/internal/api-orders/reprocess-last", checkoutH.ReprocessLastAPIOrder)

	// ── Rotas do serviço Orders ───────────────────────────────────────────────
	r.Route("/wp-json/senderzz/v1", func(r chi.Router) {

		// Webhook de pagamento — sem JWT (autenticado por HMAC-SHA256).
		// Deve ficar fora do grupo JWT para receber callbacks do gateway.
		r.Post("/payments/webhook/{gateway}", paymentH.PostPaymentWebhook)

		// Rotas protegidas por JWT.
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthJWT)

			// ── Pedidos ───────────────────────────────────────────────────────
			// POST /orders — cria pedido
			r.Post("/orders", orderH.PostOrders)
			// GET /orders — lista pedidos do usuário (filtros + paginação)
			r.Get("/orders", orderH.GetOrders)
			// GET /orders/export — exportação CSV (antes do {id} para não conflitar)
			r.Get("/orders/export", orderH.GetOrdersExport)
			// GET /orders/{id} — detalhe do pedido com itens, endereço e histórico
			r.Get("/orders/{id}", orderH.GetOrder)
			// PATCH /orders/{id}/status — transição de status (statemachine)
			r.Patch("/orders/{id}/status", orderH.PatchOrderStatus)
			// POST /orders/{id}/cancel — atalho de cancelamento
			r.Post("/orders/{id}/cancel", orderH.PostOrderCancel)
			// GET /orders/{id}/meta — leitura de metadados
			r.Get("/orders/{id}/meta", orderH.GetOrderMeta)
			// POST /orders/{id}/meta — gravação de metadado
			r.Post("/orders/{id}/meta", orderH.PostOrderMeta)

			// ── Pagamentos ────────────────────────────────────────────────────
			// POST /orders/{id}/pay — inicia pagamento
			r.Post("/orders/{id}/pay", paymentH.PostOrderPay)
			// POST /orders/{id}/refund — reembolso (apenas admin)
			r.Post("/orders/{id}/refund", paymentH.PostOrderRefund)
		})
	})

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("[orders] servidor iniciado",
			"port", port,
			"base", "/wp-json/senderzz/v1",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("[orders] ListenAndServe falhou", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("[orders] sinal recebido, encerrando gracefully…")

	// AUDIT CRON-orders-jobs-no-scheduler: encerra Asynq antes do HTTP server.
	if asynqScheduler != nil {
		asynqScheduler.Shutdown()
	}
	if asynqServer != nil {
		asynqServer.Shutdown()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("[orders] shutdown forçado", "err", err)
	}
	slog.Info("[orders] servidor encerrado")
}

// ── Scheduler interno sem Redis (FEAT-ORDERS-SCHED) ────────────────────────────
//
// AUDIT CRON-orders-jobs-no-scheduler: jobs.ProcessOrderExpiry existia mas, sem
// Redis, nenhum scheduler o disparava — pedidos 'pending' nunca expiravam, cotas
// de agenda ficavam presas. Este ticker fecha o ciclo em DEV/prod-sem-Redis.
//
// Só ProcessOrderExpiry é varrido aqui. ProcessStatusSync é por-pedido (precisa de
// payload com order_id/wp_order_id/status, enfileirado sob demanda) — NÃO existe
// um critério de varredura periódica para ele sem inventar lógica nova; logo, fica
// de fora do ticker (ver report do achado).

// startExpiryTicker dispara jobs.ProcessOrderExpiry uma vez no startup e a cada
// intervalo (default 300s, override ORDERS_JOB_INTERVAL_SECONDS), até o ctx encerrar.
// Roda em goroutine própria para não bloquear o boot do HTTP server.
func startExpiryTicker(ctx context.Context, pool *pgxpool.Pool) {
	interval := ordersJobInterval()
	slog.Info("[orders/sched] ticker de expiração iniciado (sem Redis)",
		"interval_seconds", int(interval.Seconds()))

	go func() {
		// Roda já no startup — senão nada dispara nos primeiros N segundos.
		runExpiryJob(ctx, pool)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("[orders/sched] ticker de expiração encerrado (shutdown)")
				return
			case <-ticker.C:
				runExpiryJob(ctx, pool)
			}
		}
	}()
}

// runExpiryJob executa um ciclo de ProcessOrderExpiry isolado por recover():
// um panic ou erro do job NÃO derruba o server. Reaproveita o handler Asynq
// existente passando um task real (nil-task entraria em panic no t.Payload()).
func runExpiryJob(ctx context.Context, pool *pgxpool.Pool) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("[orders/sched] panic em ProcessOrderExpiry — recuperado", "panic", rec)
		}
	}()

	start := time.Now()
	task := asynq.NewTask(jobs.TaskOrderExpiry, nil)
	if err := jobs.ProcessOrderExpiry(ctx, pool, task); err != nil {
		// O job já loga por-pedido; aqui registramos a falha agregada do ciclo.
		slog.Error("[orders/sched] ciclo de expiração retornou erro",
			"err", err, "duration_ms", time.Since(start).Milliseconds())
		return
	}
	slog.Info("[orders/sched] ciclo de expiração concluído",
		"duration_ms", time.Since(start).Milliseconds())
}

// ordersJobInterval lê ORDERS_JOB_INTERVAL_SECONDS; default 300s; inválido ou ≤0 ignorado.
func ordersJobInterval() time.Duration {
	const def = 300 * time.Second
	v := strings.TrimSpace(os.Getenv("ORDERS_JOB_INTERVAL_SECONDS"))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		slog.Warn("[orders/sched] ORDERS_JOB_INTERVAL_SECONDS inválido — usando default",
			"valor", v, "default_seconds", 300)
		return def
	}
	return time.Duration(n) * time.Second
}

// ── Probes de saúde (liveness/readiness) ──────────────────────────────────────
//
// OBSERVABILIDADE: /readyz é o probe canônico de readiness (pool.Ping → 200/503),
// separado do /health. Os handlers recebem um dbPinger (não o *pgxpool.Pool concreto)
// para permitir exercitar ambos os ramos (banco OK / banco fora) sem um Postgres real.

// dbPinger é a superfície mínima que os probes precisam do pool de conexões.
// *pgxpool.Pool satisfaz a interface (método Ping(context.Context) error).
type dbPinger interface {
	Ping(ctx context.Context) error
}

// healthHandler é o probe de liveness/sanidade — comportamento PRESERVADO do closure
// inline anterior (Ping → 200/503). Mantido para nginx/probes já configurados.
func healthHandler(p dbPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{
			"status":  "ok",
			"service": "orders",
			"version": "6.0",
			"fase":    "6 — substitui WooCommerce orders",
		})
	}
}

// readyzHandler é o probe de readiness — readiness = pool.Ping com timeout curto →
// 200 (pronto) / 503 (banco inacessível). Loga em ERROR quando não-pronto.
func readyzHandler(p dbPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := p.Ping(pingCtx); err != nil {
			slog.Error("[orders] readiness check falhou — banco inacessível", "err", err)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "not_ready: banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ready", "service": "orders"})
	}
}

// ── Middlewares inline ────────────────────────────────────────────────────────

// slogMiddleware registra cada requisição com slog estruturado E injeta no context
// um *slog.Logger já decorado com request_id, para que os handlers de negócio
// (checkout/pagamento) logem correlacionados à requisição (ver httpx.LoggerFrom).
func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := chimw.GetReqID(r.Context())

		// OBSERVABILIDADE: logger por-requisição decorado com request_id, propagado
		// via context. Os handlers o obtêm com httpx.LoggerFrom(ctx) — assim as linhas
		// `[checkout] …` / `[payments] …` carregam o mesmo request_id da linha `[http]`.
		reqLogger := slog.Default().With("request_id", reqID)
		r = r.WithContext(httpx.WithLogger(r.Context(), reqLogger))

		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("[http]",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", reqID,
		)
	})
}

// corsAllowedOrigins lê ORDERS_CORS_ORIGINS uma vez (lista separada por vírgula).
// Ex.: "https://painel.senderzz.com,https://app.senderzz.com".
// Vazio → nenhum origin é permitido para requisições credenciais (fail-closed).
func corsAllowedOrigins() map[string]struct{} {
	raw := os.Getenv("ORDERS_CORS_ORIGINS")
	allowed := make(map[string]struct{})
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = struct{}{}
		}
	}
	return allowed
}

// corsMiddleware permite requisições cross-origin do portal SPA e integrações externas.
//
// AUDIT SEC-GO-CORS-REFLECT (CWE-942): antes o middleware ecoava QUALQUER Origin do
// request junto com Access-Control-Allow-Credentials: true (e usava "*" quando vazio) —
// qualquer site malicioso podia fazer leituras cross-origin credenciadas. Agora só
// ecoa o Origin (e habilita credentials) quando ele consta na allowlist explícita
// ORDERS_CORS_ORIGINS — mesmo padrão de go/affiliates. Origens fora da lista NÃO
// recebem cabeçalhos credenciados de CORS. Clientes Bearer (SPA) seguem funcionando:
// o token não é anexado automaticamente pelo browser, então o aperto não os quebra.
func corsMiddleware(next http.Handler) http.Handler {
	allowed := corsAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")

		if _, ok := allowed[origin]; origin != "" && ok {
			// Origin confiável — ecoa explicitamente e libera credenciais.
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, Authorization, X-Senderzz-Token, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == http.MethodOptions {
			// Preflight: 204 mesmo para origens não permitidas (sem cabeçalhos CORS = browser bloqueia).
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
