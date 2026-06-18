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
//   - REDIS_URL       — DSN Redis para Asynq (jobs de expiração e sync)
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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/senderzz/orders-service/internal/auth"
	"github.com/senderzz/orders-service/internal/db"
	"github.com/senderzz/orders-service/internal/handlers"
	"github.com/senderzz/orders-service/internal/httpx"
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

	// ── Health check ─────────────────────────────────────────────────────────
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{
			"status":  "ok",
			"service": "orders",
			"version": "6.0",
			"fase":    "6 — substitui WooCommerce orders",
		})
	})

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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("[orders] shutdown forçado", "err", err)
	}
	slog.Info("[orders] servidor encerrado")
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

// corsMiddleware permite requisições cross-origin do portal SPA e integrações externas.
// Ecoa o Origin do request (necessário com Allow-Credentials: true —
// wildcard "*" é rejeitado pelos navegadores com credenciais).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Authorization, X-Senderzz-Token, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
