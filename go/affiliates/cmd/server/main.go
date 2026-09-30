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
	"strings"
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
	// P2-SEC carteira-fantasma: CODHandler (rotas /cod/*) desregistrado — lia
	// senderzz_cod_wallet/senderzz_cod_ledger (0 linhas, fonte ERRADA). A fonte
	// canônica de saldo COD é sz_cod_wallet_transactions. Manter essas rotas criava
	// uma 3ª fonte-de-verdade contraditória. O tipo handlers.CODHandler permanece
	// no pacote (não instanciado aqui) até ser migrado para a tabela correta.

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
	// AUDIT GO-HDR-01: cabeçalhos de segurança (nosniff/X-Frame/CSP/HSTS/Referrer/
	// Permissions) em TODA resposta — logo após RealIP para embrulhar Recoverer/CORS/
	// handlers e cobrir /health, /readyz e preflights. Defense-in-depth atrás do nginx
	// nas RESPOSTAS Go (não no HTML do painel). Ver internal/httpx/security_headers.go.
	r.Use(httpx.SecurityHeaders())
	r.Use(slogMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Rotas internas de double-write (PHP → Go) — protegidas por HMAC.
	// Registradas fora do prefixo /wp-json para simplificar a config do nginx.
	// Handler sempre ativo; secret vazio → 503 dentro dos handlers (fail-closed).
	handlers.RegisterInternalRoutes(r, internalH)

	// Health check (fora do prefixo WP) — usado pelo nginx e health checks do K8s.
	//
	// OBSERVABILIDADE: dois probes com semânticas distintas (padrão liveness/readiness):
	//   - /health  → liveness + sanidade do banco. Mantido EXATAMENTE como antes
	//                (faz Ping) para não quebrar probes/nginx já configurados.
	//   - /readyz  → readiness explícita. 200 quando o pool responde Ping; 503 quando
	//                o banco está inacessível. É o probe canônico para "pronto para
	//                receber tráfego" (drena o pod do load balancer sem matá-lo).
	r.Get("/health", healthHandler(pool))
	r.Get("/readyz", readyzHandler(pool))

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

			// FEAT-RBAC-2026-06-21: código de indicação PERMANENTE do usuário da
			// sessão (link fixo /r/{code}). Gera lazy no 1º acesso. Caminho ESTÁTICO
			// (/affiliates/referral) — não colide com /affiliates/{id}/... pois o
			// segmento literal "referral" é mais específico que o param {id}.
			r.Get("/affiliates/referral", affiliatesH.Referral)

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
			// P2-SEC carteira-fantasma: rotas /cod/saldo, /cod/extrato e
			// /cod/anticipate REMOVIDAS. Liam senderzz_cod_wallet/senderzz_cod_ledger
			// (0 linhas, fonte ERRADA) — a fonte canônica é sz_cod_wallet_transactions.
			// Re-registrar só após o CODHandler apontar para a tabela correta.
		})

		// ── Rotas PÚBLICAS (SEM JWT) ──────────────────────────────────────────
		// FEAT-RBAC-2026-06-21: o link fixo de indicação é clicado por um prospect
		// DESLOGADO, então o resolver NÃO pode carregar jwtAuth. Grupo irmão do
		// grupo autenticado, no mesmo prefixo /wp-json/senderzz/v1.
		//
		// ROTEAMENTO (decisivo): o gateway FALK (infra/falk/gateway.conf:16)
		// roteia ESTE serviço por ^/wp-json/senderzz/v1/(affiliates|cod) — então
		// o resolver PRECISA ficar sob /affiliates/ para chegar aqui (um path
		// /referral/* cairia no catch-all → orders-service). Por isso:
		//   /affiliates/referral/{code}  (público, resolve dono)
		//   /affiliates/referral         (autenticado, código próprio)
		// São rotas chi distintas (1 segmento extra) → sem colisão com
		// /affiliates/{id}/... (o literal "referral" é mais específico que {id}).
		// O caminho pretty falklog.com.br/r/{code} é mapeado pelo gateway/front
		// sobre este endpoint — não é satisfeito no Go.
		r.Group(func(r chi.Router) {
			r.Get("/affiliates/referral/{code}", affiliatesH.ResolveReferral)
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

// ── Probes de saúde (liveness/readiness) ──────────────────────────────────────

// dbPinger é a superfície mínima que os probes precisam do pool de conexões.
// *pgxpool.Pool satisfaz a interface (método Ping(context.Context) error).
// Extrair a interface permite testar healthHandler/readyzHandler sem um banco real.
type dbPinger interface {
	Ping(ctx context.Context) error
}

// healthHandler é o probe de liveness/sanidade — comportamento idêntico ao antigo
// closure inline (Ping → 200/503). Mantido para nginx/probes já configurados.
func healthHandler(p dbPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ok", "service": "affiliates"})
	}
}

// readyzHandler é o probe de readiness — separado de /health.
// readiness = pool.Ping → 200 (pronto) / 503 (banco inacessível).
func readyzHandler(p dbPinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := p.Ping(r.Context()); err != nil {
			slog.Warn("[readyz] banco inacessível — não pronto", "err", err)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "not_ready: banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ready", "service": "affiliates"})
	}
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

// corsAllowedOrigins lê AFFILIATES_CORS_ORIGINS uma vez (lista separada por vírgula).
// Ex.: "https://painel.senderzz.com,https://app.senderzz.com".
// Vazio → nenhum origin é permitido para requisições credenciais (fail-closed).
func corsAllowedOrigins() map[string]struct{} {
	raw := os.Getenv("AFFILIATES_CORS_ORIGINS")
	allowed := make(map[string]struct{})
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = struct{}{}
		}
	}
	return allowed
}

// SEC-GO-01: CORS com allowlist explícita (CWE-942).
// Antes o middleware ecoava QUALQUER Origin do request junto com
// Access-Control-Allow-Credentials: true — qualquer site malicioso podia fazer
// leituras cross-origin credenciadas. Agora só ecoa o Origin (e habilita
// credentials) quando ele está na allowlist AFFILIATES_CORS_ORIGINS.
// Origens fora da lista não recebem cabeçalhos credenciados de CORS.
// Comportamento dos clientes Bearer (SPA atual) preservado: o token Bearer não
// é anexado automaticamente pelo browser, então o aperto não os quebra; basta
// listar a(s) origem(ns) do painel em AFFILIATES_CORS_ORIGINS.
func corsMiddleware(next http.Handler) http.Handler {
	allowed := corsAllowedOrigins()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Set("Vary", "Origin")

		_, ok := allowed[origin]
		if origin != "" && ok {
			// Origin confiável — ecoa explicitamente e libera credenciais.
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Senderzz-Token, X-Internal-Sig, X-Request-ID")
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
