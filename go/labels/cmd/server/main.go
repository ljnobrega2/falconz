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
//                               Fail-closed: se vazio, POST /webhook/tracking retorna 503
//
// Variáveis opcionais:
//   - PORT                 — porta HTTP (padrão: 8084)
//   - ME_TOKEN             — OAuth token ME. Se vazio: aviso de log, CRUD de etiquetas
//                            indisponível; /calculate ainda funciona para cotação
//   - ME_BASE_URL          — URL base ME API (padrão: https://melhorenvio.com.br/api/v2)
//   - PDF_STORAGE_DIR      — diretório local para PDFs (padrão: /var/senderzz/labels)
//   - WORKER_CONCURRENCY   — concorrência do worker Asynq (padrão: 4)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/hibiken/asynq"
	"github.com/senderzz/labels-service/internal/db"
	"github.com/senderzz/labels-service/internal/handlers"
	"github.com/senderzz/labels-service/internal/jobs"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/middleware"
)

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

	// ── Cliente Asynq (Redis) ────────────────────────────────────────────────
	redisAddr := os.Getenv("ASYNQ_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisOpt := asynq.RedisClientOpt{Addr: redisAddr}

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

	// Inicia o worker em goroutine separada.
	go func() {
		slog.Info("[senderzz_labels] worker Asynq iniciando",
			"concurrency", concurrency,
			"redis_addr", redisAddr,
		)
		if err := asynqServer.Run(mux); err != nil {
			slog.Error("[senderzz_labels] worker Asynq encerrado com erro", "err", err)
		}
	}()

	// ── Handlers HTTP ────────────────────────────────────────────────────────
	labelH := handlers.NewLabelHandler(pool, meClient, asynqClient)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8084"
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// Health check — sem auth, usada por load balancers e Docker healthcheck.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"labels","version":"4.0"}`))
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

			// GET /labels/{id} — detalhes + rastreamento ao vivo.
			r.Get("/labels/{id}", labelH.GetLabel)

			// DELETE /labels/{id} — cancela etiqueta (ME + banco).
			r.Delete("/labels/{id}", labelH.DeleteLabel)
		})

		// Webhook de rastreamento — sem JWT, autenticado por HMAC-SHA256.
		// Recebe eventos de status de transportadoras (ME + carriers integrados).
		// Fail-closed: TRACKING_WEBHOOK_SECRET vazio → 503.
		r.Post("/webhook/tracking", labelH.PostTrackingWebhook)
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
