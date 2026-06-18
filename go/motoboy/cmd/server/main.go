// Serviço Go do módulo Motoboy — Senderzz Fase 1 (strangler fig).
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL   — DSN Postgres (pgx format)
//   - REDIS_URL      — DSN Redis para Asynq (ex: redis://localhost:6379)
//   - WP_SALT_AUTH   — AUTH_SALT do WordPress (para validação de sessões portal)
//   - ALAN_TOKEN     — token estático do expedidor (Alan/expedição)
//
// Variáveis opcionais:
//   - PORT           — porta HTTP (default: 8080)
//
// Base path: /wp-json/sz-motoboy/v1 (nginx proxeia sem rewrite)
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

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/db"
	"github.com/senderzz/motoboy-service/internal/handlers"
	"github.com/senderzz/motoboy-service/internal/httpx"
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
	loteH := &handlers.LoteHandler{Pool: pool}
	trackingH := &handlers.TrackingHandler{Pool: pool}
	zonaH := &handlers.ZonaHandler{
		Pool:  pool,
		Cache: handlers.NewCEPCache(),
	}
	olH := &handlers.OLHandler{Pool: pool}
	rotaH := &handlers.RotaHandler{Pool: pool}
	authH := &handlers.MobAuthHandler{Pool: pool}
	loginH := &handlers.LoginHandler{Pool: pool}
	alanH := &handlers.AlanHandler{Pool: pool}
	opsH := &handlers.MotoboOpsHandler{Pool: pool}
	internalH, err := handlers.NewInternalHandler(pool)
	if err != nil {
		slog.Warn("[main] double-write desativado (MOTOBOY_INTERNAL_SECRET ausente)")
	}

	// ── Middlewares de auth (reutilizados em múltiplos grupos) ────────────────
	authMotoboy := auth.AuthMotoboy(pool)
	authAlan := auth.AuthAlan()
	authPortal := auth.AuthPortal(pool)

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middlewares globais.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(slogMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Rotas internas de double-write (PHP → Go) — protegidas por HMAC.
	if internalH != nil {
		handlers.RegisterInternalRoutes(r, internalH)
	}

	// Health check (fora do prefixo WP) — usado pelo nginx e health checks do K8s.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ok", "service": "motoboy"})
	})

	// Todas as 41 rotas sob o prefixo canônico do WP REST.
	// nginx proxeia /wp-json/sz-motoboy/v1/* → Go sem rewrite.
	r.Route("/wp-json/sz-motoboy/v1", func(r chi.Router) {

		// ── Auth (público) ────────────────────────────────────────────────────
		r.Post("/login", loginH.Login)
		// /login/verificar requer X-MB-Token — aplica middleware inline.
		r.With(authMotoboy).Post("/login/verificar", loginH.LoginVerificar)
		r.Post("/login/definir-senha", loginH.LoginDefinirSenha)
		// /login/autenticar é alias legado de /otp/confirmar (compat PWA antigo).
		r.Post("/login/autenticar", authH.OTPValidar)
		r.Post("/otp/solicitar", authH.OTPSolicitar)
		r.Post("/otp/confirmar", authH.OTPValidar)

		// ── Motoboy (requer X-MB-Token) ───────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(authMotoboy)

			r.Post("/motoboy/trocar-senha", loginH.TrocarSenha)
			r.Get("/motoboy/lote", loteH.Lote)
			r.Get("/motoboy/token/validar", authH.TokenValidar)
			r.Post("/motoboy/iniciar-rota", rotaH.IniciarRota)
			r.Post("/motoboy/devolver-qr", opsH.DevolverQR)
			r.Post("/motoboy/ping", opsH.Ping)
			r.Post("/motoboy/entregar", rotaH.Entregar)
			r.Post("/motoboy/frustrar", rotaH.Frustrar)
			r.Get("/motoboy/fechamento", opsH.Fechamento)
			r.Post("/motoboy/confirmar-repasse", opsH.ConfirmarRepasse)
			r.Get("/motoboy/pendentes-confirmacao", opsH.PendentesConfirmacao)
			r.Post("/motoboy/comprovante", opsH.Comprovante)
			r.Get("/motoboy/comprovantes/{order_id}", opsH.Comprovantes)
			r.Post("/motoboy/push-subscribe", opsH.PushSubscribe)
		})

		// ── Wallet (requer X-MB-Token) ────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(authMotoboy)

			r.Get("/wallet/saldo", opsH.WalletSaldo)
			r.Get("/wallet/historico", opsH.WalletHistorico)
			r.Get("/wallet/bancario", opsH.WalletBancarioGet)
			r.Post("/wallet/bancario", opsH.WalletBancarioPost)
		})

		// ── Alan / Expedição (requer X-Alan-Token) ────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(authAlan)

			r.Get("/alan/localizacao", alanH.Localizacao)
			r.Get("/alan/historico/{motoboy_id}", alanH.Historico)
			r.Get("/alan/etiquetas", alanH.Etiquetas)
			r.Post("/alan/push-subscribe", alanH.PushSubscribe)
			r.Get("/alan/pedidos", alanH.Pedidos)
			r.Post("/alan/embalar", alanH.Embalar)
			r.Post("/alan/confirmar-fechamento", alanH.ConfirmarFechamento)
			r.Get("/alan/dashboard", alanH.Dashboard)
		})

		// ── OL / Operador Logístico (requer portal_session) ──────────────────
		r.Group(func(r chi.Router) {
			r.Use(authPortal)

			r.Post("/ol/mudar-status", olH.MudarStatus)
			r.Post("/ol/trocar-motoboy", olH.TrocarMotoboy)
			r.Get("/ol/motoboys-do-dia", olH.MotoboysDoDia)
			r.Get("/ol/motoboys", olH.Motoboys)
			r.Get("/ol/pedido-historico", opsH.PedidoHistorico)
		})

		// ── Tracking (público, sem auth) ──────────────────────────────────────
		// GET  /tracking/{order_id} — IMPLEMENTADO
		r.Get("/tracking/{order_id}", trackingH.GetTracking)
		// POST /tracking/{order_id}/reagendar — stub V-SEC-03 (ver TODO no handler)
		r.Post("/tracking/{order_id}/reagendar", trackingH.Reagendar)

		// ── Público / sem auth ────────────────────────────────────────────────
		// GET  /zona-cep?cep=XXXXXXXX — IMPLEMENTADO
		r.Get("/zona-cep", zonaH.GetZonaCEP)
		r.Get("/link-expedicao", opsH.LinkExpedicao)

		// /dispensar-cpf requer portal session — middleware aplicado inline.
		r.With(authPortal).Post("/dispensar-cpf", opsH.DispensarCPF)
	})

	// ── Servidor HTTP ─────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("[main] servidor iniciado", "port", port, "base", "/wp-json/sz-motoboy/v1")
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

// corsMiddleware permite requisições cross-origin do PWA e do portal SPA.
// Ecoa o Origin do request (necessário ao usar Allow-Credentials: true —
// wildcard "*" é rejeitado pelos navegadores com credenciais).
// Sem dependência externa — stdlib puro.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-MB-Token, X-Alan-Token, X-Senderzz-Token, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
