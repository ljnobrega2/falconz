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
//                      Se ausente, worker de webhooks não é iniciado (modo sem filas).
//
// Base path: /wp-json/senderzz/v1/portal
// Health check: GET /health
//
// Fail-closed:
//   JWT_SECRET vazio   → 503 em todas as rotas autenticadas.
//   WP_SALT_AUTH vazio → 503 em todas as rotas autenticadas.
//   DATABASE_URL vazio → exit(1) no startup.
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
		go startWebhookWorker(ctx, redisOpt, pool)
	}

	// Variável mantida para uso futuro (EnqueueWebhookDispatch nos handlers).
	_ = asynqClient

	// ── Handlers ──────────────────────────────────────────────────────────────
	authH := &handlers.AuthHandler{Pool: pool}
	webhookH := &handlers.WebhookHandler{Pool: pool}
	integrationsH := &handlers.IntegrationsHandler{Pool: pool}
	settingsH := &handlers.SettingsHandler{Pool: pool}

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

	// Todas as rotas sob o prefixo canônico do WP REST.
	// nginx proxeia /wp-json/senderzz/v1/portal/* → Go sem rewrite.
	r.Route("/wp-json/senderzz/v1", func(r chi.Router) {

		// ── Rotas públicas (sem autenticação) ─────────────────────────────────
		// POST /portal/login — credenciais → partial_token + 2FA
		r.Post("/portal/login", authH.Login)
		// POST /portal/login/2fa — partial_token + código → JWT completo
		r.Post("/portal/login/2fa", authH.Login2FA)

		// ── Rotas protegidas (requer JWT válido + sessão no banco) ────────────
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)

			// Auth
			r.Post("/portal/logout", authH.Logout)
			r.Post("/portal/refresh", authH.Refresh)
			r.Get("/portal/me", authH.Me)

			// Webhooks.
			// Atenção à ordem: /webhooks/clear-history ANTES de /webhooks/{id}
			// para que chi não interprete "clear-history" como {id}.
			r.Get("/portal/webhooks", webhookH.List)
			r.Post("/portal/webhooks", webhookH.Create)
			r.Post("/portal/webhooks/clear-history", webhookH.ClearHistory)
			r.Delete("/portal/webhooks/{id}", webhookH.Delete)
			r.Get("/portal/webhooks/{id}/history", webhookH.History)

			// Integrações
			r.Get("/portal/integrations", integrationsH.List)
			r.Post("/portal/integrations/toggle", integrationsH.Toggle)
			r.Delete("/portal/integrations/logs", integrationsH.ClearLogs)

			// Configurações
			r.Get("/portal/settings", settingsH.Get)
			r.Post("/portal/settings", settingsH.Update)
			r.Post("/portal/settings/2fa", settingsH.Toggle2FA)

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

// corsMiddleware permite requisições cross-origin do portal SPA.
// Ecoa o Origin do request (necessário com Allow-Credentials: true —
// wildcard "*" é rejeitado pelos navegadores quando credenciais são enviadas).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, X-Senderzz-Token, X-Request-ID, Cookie")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
