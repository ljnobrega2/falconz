// Serviço Go de Labels + Melhor Envio — Senderzz Fase 4 (strangler fig).
//
// Responsabilidades:
//   - CRUD de etiquetas de envio via ME API (wc-melhor-envio/v1)
//   - CRIT-01: preço recalculado server-side via /me/shipment/calculate
//   - Jobs assíncronos via Asynq (Redis): geração de PDF, sync de rastreamento
//   - Cache de cotações em Postgres (wc_me_shipment_cache, TTL 10 min)
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL            — DSN Postgres (pgx format)
//   - ASYNQ_REDIS_ADDR        — endereço Redis p/ Asynq (ex: localhost:6379)
//   - JWT_SECRET              — mesmo que tpc_jwt_secret do WP (para AuthJWT)
//   - TRACKING_WEBHOOK_SECRET — HMAC-SHA256 p/ validar webhooks de rastreamento
//     Fail-closed: se vazio, POST /webhook/tracking retorna 503
//
// Variáveis opcionais:
//   - PORT                 — porta HTTP (padrão: 8084)
//   - ME_TOKEN             — OAuth token ME. Se vazio: aviso de log, CRUD de etiquetas
//     indisponível; /calculate ainda funciona para cotação
//   - ME_BASE_URL          — URL base ME API (padrão: https://melhorenvio.com.br/api/v2)
//   - PDF_STORAGE_DIR      — diretório local para PDFs (padrão: /var/senderzz/labels)
//   - WORKER_CONCURRENCY   — concorrência do worker Asynq (padrão: 4)
//   - WALLET_SERVICE_URL   — CRIT-01: base URL do wallet-service (ex: http://wallet:8081).
//     Se vazia, a reserva de saldo fica DESATIVADA (off-switch):
//     etiquetas são criadas sem débito (comportamento Fase 4).
//   - WALLET_INTERNAL_SECRET — CRIT-01: mesmo secret HMAC das rotas /internal/* do
//     wallet-service. Se WALLET_SERVICE_URL estiver setada e
//     este secret vazio → reserva falha fail-closed.
package main

import (
	"context"
	"crypto/hmac"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/db"
	"github.com/senderzz/labels-service/internal/handlers"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/jobs"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/middleware"
	"github.com/senderzz/labels-service/internal/wallet"
)

// internalSecretMiddleware exige X-Internal-Secret == LABELS_INTERNAL_SECRET
// nas rotas /internal/*, se a env estiver configurada. Comparação em tempo
// constante (hmac.Equal) contra timing attack. Opt-in: env vazia = passa
// direto (preserva comportamento atual até o secret ser configurado nos dois
// lados — admin-service precisa enviar o mesmo header).
func internalSecretMiddleware(next http.Handler) http.Handler {
	secret := strings.TrimSpace(os.Getenv("LABELS_INTERNAL_SECRET"))
	if secret == "" {
		slog.Warn("[senderzz_labels] LABELS_INTERNAL_SECRET ausente — rotas /internal/* sem secret compartilhado (só isolamento de rede)")
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Secret")
		if len(got) != len(secret) || !hmac.Equal([]byte(got), []byte(secret)) {
			slog.Warn("[senderzz_labels] /internal/*: X-Internal-Secret inválido/ausente", "path", r.URL.Path, "ip", r.RemoteAddr)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── Banco de dados ───────────────────────────────────────────────────────
	pool, err := db.Connect(ctx)
	if err != nil {
		slog.Error("[senderzz_labels] falha ao conectar ao banco", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// ── Cliente Melhor Envio ─────────────────────────────────────────────────
	// Fail-closed parcial: ME_TOKEN vazio → aviso de log + continua.
	// CRUD de etiquetas falhará com 401 na ME API; /calculate ainda funciona.
	meClient := me.NewMEClient()

	// ── Cliente wallet-service (CRIT-01: reserva de saldo) ───────────────────
	// Off-switch: WALLET_SERVICE_URL vazia → reserva desativada (Enabled()=false),
	// etiqueta criada sem débito (comportamento Fase 4). Ativa → fail-closed:
	// falha na reserva aborta a emissão (nunca etiqueta que não pode cobrar).
	walletClient := wallet.NewClient()

	// ── Cliente Asynq (Redis) ────────────────────────────────────────────────
	// Lê REDIS_URL (redis://host:port/db) para honrar o banco correto.
	// ASYNQ_REDIS_ADDR sobreescreve o host:port se definido explicitamente.
	asynqAddr, asynqDB := parseRedisURL(os.Getenv("REDIS_URL"))
	if explicit := os.Getenv("ASYNQ_REDIS_ADDR"); explicit != "" {
		asynqAddr = explicit
	}
	redisOpt := asynq.RedisClientOpt{Addr: asynqAddr, DB: asynqDB}
	slog.Info("[senderzz_labels] Asynq Redis configurado", "addr", asynqAddr, "db", asynqDB)

	asynqClient := asynq.NewClient(redisOpt)
	defer asynqClient.Close()

	// ── Worker Asynq — goroutine separada, mesmo binário ───────────────────
	// HTTP server + Asynq worker no mesmo processo (Fase 4).
	// Em produção de alta escala, separar em dois deployments:
	//   cmd/server/main.go  — apenas HTTP
	//   cmd/worker/main.go  — apenas Asynq worker
	concurrency := 4
	if c := os.Getenv("WORKER_CONCURRENCY"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			concurrency = n
		}
	}

	asynqServer := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: concurrency,
		Queues: map[string]int{
			"critical": 10,
			"default":  5,
		},
		// Retry com backoff exponencial (padrão Asynq).
		// Cada job define MaxRetry individualmente (GeneratePDF=5, SyncTracking=10).
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			slog.Error("[senderzz_labels] worker: erro no job",
				"type", task.Type(),
				"err", err,
			)
		}),
	})

	labelWorker := jobs.NewLabelWorker(pool, meClient)

	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeGeneratePDF, labelWorker.ProcessGeneratePDF)
	mux.HandleFunc(jobs.TypeSyncTracking, labelWorker.ProcessSyncTracking)
	mux.HandleFunc(jobs.TypeBackfillTracking, labelWorker.ProcessBackfillTracking)

	// Inicia o worker em goroutine separada.
	go func() {
		slog.Info("[senderzz_labels] worker Asynq iniciando",
			"concurrency", concurrency,
			"redis_addr", asynqAddr,
			"redis_db", asynqDB,
		)
		if err := asynqServer.Run(mux); err != nil {
			slog.Error("[senderzz_labels] worker Asynq encerrado com erro", "err", err)
		}
	}()

	// Goroutine de sincronização periódica de rastreamento (a cada 5 min,
	// AUDIT-2026-07-31 dono: "acompanhado de 5 em 5 minutos, não podemos ter
	// atraso" — era 10min). Enfileira SyncTracking para toda etiqueta ATIVA
	// com tracking_code (status draft/released/posted — AUDIT-2026-07-31:
	// antes só pegava status='posted', deixando etiquetas em 'released' com
	// tracking_code já emitido (ex.: pedido #1665) SEM NUNCA sincronizar —
	// só avançavam se o webhook nativo da ME chegasse, e não chegou), e
	// BackfillTracking para reconciliar códigos ausentes ou provisórios.
	// Primeira execução imediata: grava o estado atual das etiquetas existentes
	// sem esperar o primeiro intervalo do ticker após um deploy.
	enqueuePendingTrackingSync(ctx, pool, asynqClient)
	enqueueTrackingBackfill(ctx, pool, asynqClient)
	go func() {
		syncTicker := time.NewTicker(5 * time.Minute)
		defer syncTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-syncTicker.C:
				enqueuePendingTrackingSync(ctx, pool, asynqClient)
				enqueueTrackingBackfill(ctx, pool, asynqClient)
			}
		}
	}()

	// Poll de recarga PIX (a cada 1 min): confirma sozinho quaisquer charges
	// pendentes cujo valor já apareça no saldo real da ME — o painel ME não
	// oferece webhook de pagamento (só "Atualização das etiquetas"), então sem
	// isso a confirmação dependia do produtor clicar "Já paguei" manualmente
	// (AUDIT-2026-07-27). Idempotente (ver ReconcileMEChargesTick).
	go func() {
		pixTicker := time.NewTicker(1 * time.Minute)
		defer pixTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pixTicker.C:
				n, err := handlers.ReconcileMEChargesTick(ctx, pool, meClient, 0)
				if err != nil {
					slog.Error("[senderzz_labels] poll recarga ME falhou", "err", err)
				} else if n > 0 {
					slog.Info("[senderzz_labels] poll recarga ME confirmou charges", "count", n)
				}
			}
		}
	}()

	// Poll de estorno de cancelamento (a cada 1 min, AUDIT-2026-07-28): "se
	// houver diferença no saldo pra mais no valor exato da etiqueta, libera
	// reembolso" — ME não expõe status/webhook de reembolso confirmado (só
	// documenta "estorno em até 12h" sem campo consultável), então a detecção é
	// por dedução: saldo real da conta ME subiu → bate contra o rastro de
	// estornos pendentes (mesmo checkpoint do poll de recarga acima, é o mesmo
	// saldo de conta sendo explicado). O que não bater dentro da janela de
	// segurança de 24h é creditado incondicionalmente por estorno_reconcile.go
	// (wallet-service) — esta reconciliação ativa é só o caminho RÁPIDO/exato.
	go func() {
		estornoTicker := time.NewTicker(1 * time.Minute)
		defer estornoTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-estornoTicker.C:
				n, err := handlers.ReconcileEstornosTick(ctx, pool, meClient)
				if err != nil {
					slog.Error("[senderzz_labels] poll estorno de cancelamento falhou", "err", err)
				} else if n > 0 {
					slog.Info("[senderzz_labels] poll estorno confirmou por saldo real", "count", n)
				}
			}
		}
	}()

	// ── Handlers HTTP ────────────────────────────────────────────────────────
	labelH := handlers.NewLabelHandler(pool, meClient, asynqClient, walletClient)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8084"
	}

	r := chi.NewRouter()
	// OBSERVABILIDADE: RequestID PRIMEIRO — slogMiddleware/recoverMiddleware leem
	// chimw.GetReqID; sem RequestID antes deles o request_id sai vazio.
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	// AUDIT GO-HDR-01: cabeçalhos de segurança (nosniff/X-Frame/CSP/HSTS/Referrer/
	// Permissions) em TODA resposta — logo após RealIP para embrulhar os demais
	// middlewares e cobrir /health, /readyz e preflights. Defense-in-depth atrás do
	// nginx nas RESPOSTAS Go (não no HTML do painel). Ver internal/httpx/security_headers.go.
	r.Use(httpx.SecurityHeaders())
	// slogMiddleware substitui chimw.Logger (texto) por log estruturado JSON com
	// request_id — mesmo padrão de go/orders e go/portal.
	r.Use(slogMiddleware)
	// recoverMiddleware: panic → 500 JSON + log estruturado com request_id.
	// Substitui chimw.Recoverer (que loga stack em texto e não emite nosso
	// envelope {"ok":false}). Continua sendo o ÚNICO recover em volta dos handlers.
	r.Use(recoverMiddleware)

	// Liveness — sem auth, estática. Usada por load balancers e Docker healthcheck.
	// NÃO toca o banco: responde 200 enquanto o processo estiver de pé. A prontidão
	// (dependências OK) é responsabilidade de /readyz.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"labels","version":"4.0"}`))
	})

	// Readiness — sem auth. Diferente de /health: valida a dependência crítica
	// (pool Postgres via Ping). 200 quando pronto para receber tráfego; 503 quando
	// o banco está inacessível (load balancer/k8s tira o pod de rotação sem matá-lo).
	// Ping com timeout curto próprio para não pendurar o probe se o banco travar.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer pingCancel()
		w.Header().Set("Content-Type", "application/json")
		if err := pool.Ping(pingCtx); err != nil {
			slog.Warn("[senderzz_labels] /readyz: banco inacessível",
				"err", err,
				"request_id", chimw.GetReqID(r.Context()),
			)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false,"service":"labels","ready":false,"erro":"banco inacessível"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"service":"labels","ready":true}`))
	})

	// Rotas internas — somente rede Docker interna (sem JWT de usuário).
	// AUDIT-2026-07-30 HIGH: isolamento de rede era a ÚNICA defesa (confirmado
	// hoje: portas 127.0.0.1-only, sem exposição de host) — mas sem defesa em
	// profundidade nenhum vazamento de rede/proxy reverso mal configurado no
	// futuro fica coberto. Adiciona secret compartilhado (X-Internal-Secret vs
	// LABELS_INTERNAL_SECRET), mesmo padrão HMAC-secret do wallet-service.
	// Opt-in por enquanto (se a env não estiver setada, comportamento não muda —
	// evita quebrar o admin-service em produção até o secret ser configurado dos
	// dois lados); uma vez setada nos dois serviços, passa a exigir o header.
	r.Route("/internal", func(r chi.Router) {
		r.Use(internalSecretMiddleware)
		r.Get("/labels/margin-report", labelH.InternalMarginReport)
		r.Post("/labels/{id}/enqueue-generate", labelH.InternalEnqueueGenerate)
		// GET /internal/me/carriers — catálogo ME (transportadoras+serviços) p/ admin-service.
		r.Get("/me/carriers", labelH.InternalListCarriers)
		// FEAT-IMPRIMIR-SEPARA (2026-07-28): print/declaração em lote (admin-service proxy).
		r.Post("/labels/print-batch", labelH.InternalPrintBatch)
		r.Get("/labels/{id}/pdf-file", labelH.InternalServePDF)
		// AUDIT-2026-07-30: reconciliação manual sz_orders.status ↔ status real na ME
		// (fallback pro caso do /webhook/me falhar ou não ter sido reenviado).
		r.Get("/me-status/mismatches", labelH.InternalMEStatusMismatches)
		r.Post("/me-status/sync/{order_id}", labelH.InternalMEStatusSync)
		// AUDIT-2026-07-30 #6: consulta ao vivo (read-only) pro rastreio público.
		r.Get("/me-status/live/{order_id}", labelH.InternalMEStatusLive)
		// AUDIT-2026-07-30 #8: timestamps reais por etapa (created/paid/generated/posted/delivered).
		r.Get("/me-status/events/{order_id}", labelH.InternalMEStatusEvents)
	})

	// Rotas do labels-service — espelham o namespace PHP wc-melhor-envio/v1.
	r.Route("/wp-json/wc-melhor-envio/v1", func(r chi.Router) {
		// GET /calculate — cotação de frete (protegido por JWT).
		// CRIT-01: apenas informativo para o checkout. POST /labels recalcula antes de criar.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthJWT)
			r.Get("/calculate", labelH.GetCalculate)
		})

		// CRUD de etiquetas — requer JWT.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthJWT)

			// GET /labels?order_id=N&status=draft&limit=50
			r.Get("/labels", labelH.GetLabels)

			// POST /labels — cria etiqueta com CRIT-01 (recalcula preço).
			r.Post("/labels", labelH.PostLabel)

			// Rotas com segmento literal ANTES das rotas com {id} para que o chi
			// radix trie priorize static segments ao construir a árvore.
			// GET  /labels/prepare/{order_id} — preview do payload montado do banco.
			// POST /labels/emit/{order_id}    — emissão 1-click (assemble + emit).
			// order_id = sz_orders.id (ID portal).
			r.Get("/labels/prepare/{order_id}", labelH.PrepareOrderLabel)
			r.Post("/labels/emit/{order_id}", labelH.EmitOrderLabel)

			// GET /labels/{id} — detalhes + rastreamento ao vivo.
			r.Get("/labels/{id}", labelH.GetLabel)

			// DELETE /labels/{id} — cancela etiqueta (ME + banco).
			r.Delete("/labels/{id}", labelH.DeleteLabel)
		})

		// Rotas de saldo ME — requerem JWT.
		// GET  /balance         — saldo atual da conta ME da plataforma.
		// POST /balance/pix     — produtor gera PIX para recarregar conta ME.
		// GET  /balance/history — histórico de recargas do produtor.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthJWT)
			r.Get("/balance", labelH.GetMEBalance)
			r.Post("/balance/pix", labelH.PostBalancePix)
			r.Post("/balance/pix/{id}/confirm", labelH.PostBalancePixConfirm)
			r.Get("/balance/history", labelH.GetBalanceHistory)
		})

		// Webhook de rastreamento — sem JWT, autenticado por HMAC-SHA256.
		// Recebe eventos de status de transportadoras (ME + carriers integrados).
		// Fail-closed: TRACKING_WEBHOOK_SECRET vazio → 503.
		r.Post("/webhook/tracking", labelH.PostTrackingWebhook)

		// Webhook NATIVO do Melhor Envio (painel Área Dev → Webhooks). Público.
		// GET / POST-vazio = teste do painel (ACK 200). Eventos reais validam
		// x-me-signature = base64(HMAC-SHA256(body, ME_WEBHOOK_SECRET)).
		// URL pública: /wp-json/wc-melhor-envio/v1/webhook/me
		r.Post("/webhook/me", labelH.PostMEWebhook)
		r.Get("/webhook/me", labelH.PostMEWebhook)
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // 60s — GenerateLabel pode ser lento
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("[senderzz_labels] servidor HTTP iniciando", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("[senderzz_labels] falha ao iniciar servidor", "err", err)
			os.Exit(1)
		}
	}()

	// Aguarda sinal de encerramento (SIGINT/SIGTERM).
	<-ctx.Done()
	slog.Info("[senderzz_labels] encerrando...")

	// Graceful shutdown: 15s para conexões HTTP ativas e worker Asynq.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()

	// Para o worker Asynq antes do HTTP para não aceitar novos jobs durante shutdown.
	asynqServer.Shutdown()
	slog.Info("[senderzz_labels] worker Asynq encerrado.")

	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("[senderzz_labels] erro no shutdown HTTP", "err", err)
	}
	slog.Info("[senderzz_labels] encerrado.")
}

// ── Middlewares de observabilidade ────────────────────────────────────────────

// slogMiddleware registra cada requisição com slog estruturado (JSON), incluindo
// o request_id propagado por chimw.RequestID. Substitui chimw.Logger (saída de
// texto) — mesmo padrão de go/orders e go/portal. NÃO altera lógica de negócio:
// apenas observa o request e mede a latência.
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

// parseRedisURL extrai addr (host:port) e DB number de uma REDIS_URL no formato
// redis://host:port/db. Retorna "localhost:6379" e 0 em caso de erro ou URL vazia.
func parseRedisURL(rawURL string) (addr string, db int) {
	addr = "localhost:6379"
	db = 0
	if rawURL == "" {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	if host != "" {
		addr = host + ":" + port
	}
	if len(u.Path) > 1 {
		if n, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/")); err == nil {
			db = n
		}
	}
	return
}

// enqueuePendingTrackingSync enfileira SyncTracking para etiquetas com
// tracking_code e status posted. Chamada periodicamente pelo goroutine de sync.
func enqueuePendingTrackingSync(ctx context.Context, pool *pgxpool.Pool, client *asynq.Client) {
	rows, err := pool.Query(ctx,
		`SELECT id, tracking_code FROM wc_me_labels
		 WHERE tracking_code IS NOT NULL
		   AND status IN ('draft', 'released', 'posted')
		 LIMIT 200`)
	if err != nil {
		slog.Warn("[senderzz_labels] sync tracking periódico: erro ao consultar", "err", err)
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int64
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			continue
		}
		if err := jobs.EnqueueSyncTracking(client, id, code); err != nil {
			slog.Warn("[senderzz_labels] sync tracking periódico: erro ao enfileirar",
				"label_id", id, "err", err)
		} else {
			n++
		}
	}
	if n > 0 {
		slog.Info("[senderzz_labels] sync tracking periódico: jobs enfileirados", "count", n)
	}
}

// enqueueTrackingBackfill enfileira BackfillTracking para etiquetas
// postadas/liberadas. Além de preencher códigos ausentes, reconcilia a
// transição da Jadlog para o tracking definitivo.
func enqueueTrackingBackfill(ctx context.Context, pool *pgxpool.Pool, client *asynq.Client) {
	rows, err := pool.Query(ctx,
		`SELECT id, me_shipment_id FROM wc_me_labels
		 WHERE status IN ('posted', 'released')
		   AND me_shipment_id IS NOT NULL AND me_shipment_id <> ''
		 LIMIT 100`)
	if err != nil {
		slog.Warn("[senderzz_labels] backfill tracking periódico: erro ao consultar", "err", err)
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int64
		var shipmentID string
		if err := rows.Scan(&id, &shipmentID); err != nil {
			continue
		}
		if err := jobs.EnqueueBackfillTracking(client, id, shipmentID); err != nil {
			slog.Warn("[senderzz_labels] backfill tracking periódico: erro ao enfileirar",
				"label_id", id, "err", err)
		} else {
			n++
		}
	}
	if n > 0 {
		slog.Info("[senderzz_labels] backfill tracking periódico: jobs enfileirados", "count", n)
	}
}

// recoverMiddleware captura panics nos handlers, loga com slog estruturado
// (request_id incluído) e responde 500 no envelope padrão {"ok":false,"erro":...}.
// Substitui chimw.Recoverer (stack em texto, sem nosso envelope) e continua sendo
// o ÚNICO recover em volta dos handlers. Re-propaga http.ErrAbortHandler conforme
// o contrato do net/http (abort silencioso da conexão, não é um panic real).
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// http.ErrAbortHandler é semântico (aborta a conexão) — não logar
				// como panic e re-propagar para o net/http tratar.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("[senderzz_labels] panic recuperado no handler (HTTP 500)",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", chimw.GetReqID(r.Context()),
					"stack", string(debug.Stack()),
				)
				httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
