// Serviço Go de Afiliados + Carteira COD — Senderzz Fase 3 (strangler fig).
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL               — DSN Postgres (pgx format)
//   - JWT_SECRET                 — mesmo que tpc_jwt_secret do WP (para AuthPortalJWT)
//   - WP_SALT_AUTH               — AUTH_SALT do WordPress (para AuthPortalSession, fallback)
//
// Variáveis opcionais:
//   - PORT                       — porta HTTP (default: 8083)
//   - AFFILIATES_INTERNAL_SECRET — HMAC secret para double-write do PHP (se ausente, /internal desativado)
//
// Base path: /wp-json/senderzz/v1 (nginx proxeia sem rewrite)
// Health check: GET /health
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

	"github.com/senderzz/affiliates-service/internal/auth"
	"github.com/senderzz/affiliates-service/internal/db"
	"github.com/senderzz/affiliates-service/internal/handlers"
	"github.com/senderzz/affiliates-service/internal/httpx"
)

func main() {
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

	// ── Handlers ──────────────────────────────────────────────────────────────
	affiliatesH := &handlers.AffiliatesHandler{Pool: pool}
	codH := &handlers.CODHandler{Pool: pool}

	// double-write sempre inicializado; secret vazio → 503 dentro dos handlers (fail-closed).
	internalH, err := handlers.NewInternalHandler(pool)
	if err != nil {
		slog.Error("[main] erro ao inicializar double-write", "err", err)
		os.Exit(1)
	}

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middlewares globais.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(slogMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Rotas internas de double-write (PHP → Go) — protegidas por HMAC.
	// Registradas fora do prefixo /wp-json para simplificar a config do nginx.
	// Handler sempre ativo; secret vazio → 503 dentro dos handlers (fail-closed).
	handlers.RegisterInternalRoutes(r, internalH)

	// Health check (fora do prefixo WP) — usado pelo nginx e health checks do K8s.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ok", "service": "affiliates"})
	})

	// Middleware de auth JWT para portal (principal esquema desta fase).
	// AuthPortalSession disponível via auth.AuthPortalSession(pool) como fallback.
	jwtAuth := auth.AuthPortalJWT

	// Todas as rotas sob o prefixo canônico do WP REST.
	// nginx proxeia /wp-json/senderzz/v1/* → Go sem rewrite.
	r.Route("/wp-json/senderzz/v1", func(r chi.Router) {

		// ── Afiliados (requer JWT Bearer) ─────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(jwtAuth)

			// Lista vínculos do usuário autenticado (produtor ou afiliado).
			// Parâmetro opcional: ?status=pending|active|paused|revoked
			r.Get("/affiliates", affiliatesH.List)

			// Afiliado solicita vínculo com produtor+produto.
			// Body: {produtor_id, produto_id}
			r.Post("/affiliates/request", affiliatesH.Request)

			// Listagem de convites pendentes do produtor.
			r.Get("/affiliates/invites", affiliatesH.ListInvites)

			// Produtor cria convite (token 64-hex, expira 7d).
			// Body: {email}
			r.Post("/affiliates/invites", affiliatesH.CreateInvite)

			// Produtor revoga convite pendente.
			r.Delete("/affiliates/invites/{token}", affiliatesH.RevokeInvite)

			// Produtor aprova solicitação pendente.
			// Body opcional: {comissao_pct}
			r.Post("/affiliates/{id}/approve", affiliatesH.Approve)

			// Produtor revoga vínculo ativo.
			r.Post("/affiliates/{id}/revoke", affiliatesH.Revoke)

			// Lista comissões. Parâmetros opcionais: ?status=&limit=
			r.Get("/affiliates/commissions", affiliatesH.ListCommissions)

			// Resumo de comissões agrupado por status.
			r.Get("/affiliates/commissions/summary", affiliatesH.CommissionsSummary)

			// Lista links de checkout do afiliado autenticado.
			r.Get("/affiliates/links", affiliatesH.ListLinks)

			// Afiliado cria link de checkout rastreado.
			// Body: {affiliate_id, produto_id}
			r.Post("/affiliates/links", affiliatesH.CreateLink)

			// Desativa link de checkout.
			r.Delete("/affiliates/links/{id}", affiliatesH.DeactivateLink)

			// ── Carteira COD ─────────────────────────────────────────────────
			// Saldo atual da carteira COD.
			r.Get("/cod/saldo", codH.GetSaldo)

			// Histórico de movimentações COD. Parâmetros opcionais: ?limit=&tipo=
			r.Get("/cod/extrato", codH.GetExtrato)

			// Solicita antecipação de saldo COD.
			// Body opcional: {valor, descricao}
			r.Post("/cod/anticipate", codH.PostAnticipate)
		})
	})

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("[main] servidor iniciado", "port", port, "base", "/wp-json/senderzz/v1")
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

// ── Middlewares inline ────────────────────────────────────────────────────────

// slogMiddleware registra cada requisição com slog (sem dependência externa).
func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("[http]",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// corsMiddleware permite requisições cross-origin do portal SPA.
// Ecoa o Origin do request (necessário ao usar Allow-Credentials: true).
// Sem dependência externa — stdlib puro.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Senderzz-Token, X-Internal-Sig, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
