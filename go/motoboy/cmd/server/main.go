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
	"runtime/debug"
	"strings"
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

	// Middlewares globais. Ordem importa:
	//   RequestID → injeta o request_id no contexto (correlaciona todos os logs).
	//   RealIP    → resolve o IP real atrás do nginx.
	//   slog      → access-log estruturado (inclui request_id + status final).
	//   recover   → rede de proteção: panic → 500 + log estruturado (stack +
	//               request_id). Substitui middleware.Recoverer do chi (que só
	//               despeja o stack cru em stderr, sem request_id) por um recover
	//               próprio para a stack cair no MESMO pipeline slog/JSON. Fica
	//               DEPOIS de slog para que o status 500 seja capturado no access
	//               log, e antes do CORS/handlers para cobrir todo o resto.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(slogMiddleware)
	r.Use(recoverMiddleware)
	r.Use(securityHeadersMiddleware)
	r.Use(corsMiddleware)

	// Rotas internas de double-write (PHP → Go) — protegidas por HMAC.
	if internalH != nil {
		handlers.RegisterInternalRoutes(r, internalH)
	}

	// Health check (fora do prefixo WP) — usado pelo nginx e health checks do K8s.
	// Mantido FIEL ao comportamento histórico (já consumido por nginx/K8s): faz
	// ping no banco. Não altero sua lógica para não arriscar o gate de roteamento
	// do nginx que pode depender dele.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ok", "service": "motoboy"})
	})

	// Readiness probe (fora do prefixo WP) — separado de /health para o contrato
	// de observabilidade de arquitetura: readiness = "estou apto a RECEBER
	// tráfego?". Responde 200 só quando o pool de conexões consegue alcançar o
	// Postgres (pool.Ping); 503 caso contrário, para o orquestrador (K8s
	// readinessProbe) tirar o pod do balanceador enquanto o banco está
	// inacessível, sem matar o processo (liveness). Mesmo contrato httpx das
	// demais respostas.
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			slog.Warn("[readyz] banco inacessível", "err", err, "request_id", middleware.GetReqID(r.Context()))
			httpx.WriteErr(w, http.StatusServiceUnavailable, "banco inacessível")
			return
		}
		httpx.WriteOK(w, map[string]any{"status": "ready", "service": "motoboy"})
	})

	// Todas as 41 rotas sob o prefixo canônico do WP REST.
	// nginx proxeia /wp-json/sz-motoboy/v1/* → Go sem rewrite.
	r.Route("/wp-json/sz-motoboy/v1", func(r chi.Router) {

		// ── Auth (público) ────────────────────────────────────────────────────
		r.Post("/login", loginH.Login)
		// /login/verificar é PÚBLICO no WP (sz_mb_api_login_verificar): recebe o
		// telefone e retorna {nome, tem_senha}. NÃO exige X-MB-Token.
		r.Post("/login/verificar", loginH.LoginVerificar)
		r.Post("/login/definir-senha", loginH.LoginDefinirSenha)
		// /login/autenticar = login por senha (pin_hash) — port FIEL de
		// sz_mb_api_login_autenticar(). NÃO é alias de OTP (corrige a divergência
		// de auth WP×Go: o PWA do motoboy autentica por telefone+senha).
		r.Post("/login/autenticar", loginH.LoginAutenticar)
		// OTP é uma adição do serviço Go (não existe no WP); mantido registrado.
		r.Post("/otp/solicitar", authH.OTPSolicitar)
		r.Post("/otp/confirmar", authH.OTPValidar)

		// ── Motoboy (requer X-MB-Token) ───────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(authMotoboy)

			r.Post("/motoboy/trocar-senha", loginH.TrocarSenha)
			r.Get("/motoboy/lote", loteH.Lote)
			r.Get("/motoboy/token/validar", authH.TokenValidar)
			r.Post("/motoboy/iniciar-rota", rotaH.IniciarRota)
			r.Post("/motoboy/a-caminho", rotaH.ACaminho)
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

		// ── OL / Operador Logístico (requer portal_session + role operator) ──
		// SEC-GO-02: antes aceitava QUALQUER sessão portal (cliente/afiliado/
		// produtor podiam mudar status/trocar motoboy de qualquer pedido).
		r.Group(func(r chi.Router) {
			r.Use(authPortal)
			r.Use(auth.RequireRole("operator"))

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

		// /dispensar-cpf requer portal session + role operator (SEC-GO-08).
		r.With(authPortal, auth.RequireRole("operator")).Post("/dispensar-cpf", opsH.DispensarCPF)
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

// recoverMiddleware é a rede de proteção contra panics: captura qualquer panic
// nos handlers/middlewares internos, responde 500 e registra o erro de forma
// ESTRUTURADA (slog/JSON) com o request_id e o stack trace. Diferente do
// middleware.Recoverer do chi (que escreve o stack cru em stderr, fora do
// pipeline slog e sem request_id), aqui o panic vira uma linha JSON
// correlacionável às demais linhas da mesma requisição.
//
// http.ErrAbortHandler é repropagado (não é um panic de erro: o servidor o usa
// para abortar a resposta deliberadamente).
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec) // não é erro: deixa o servidor abortar a resposta.
				}
				slog.Error("[panic] handler entrou em pânico",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", middleware.GetReqID(r.Context()),
					"stack", string(debug.Stack()),
				)
				// Best-effort: se nada foi escrito ainda, devolve 500 no contrato httpx.
				httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

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

// originAllowed verifica o Origin contra a allowlist em ALLOWED_ORIGINS (CSV).
// AUDIT-2026-06-18 (Onda 1): antes o CORS ecoava QUALQUER Origin com
// Allow-Credentials:true — qualquer site podia fazer requisições autenticadas
// por cookie de sessão (CSRF nas rotas /ol/*). Agora só ecoa origens permitidas.
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

// securityHeadersMiddleware injeta headers de segurança em TODA resposta.
//
// Defense-in-depth (P2). Em PRODUÇÃO o ingress é o nginx (infra/nginx/
// security-headers.conf), que já emite o conjunto canônico para o browser;
// o Go fica em 127.0.0.1, inalcançável direto. Estes headers cobrem o caminho
// de DEV (Vite/cloudflared) onde o serviço Go pode ser alcançado sem passar
// pelo nginx, e servem como rede de segurança se algum dia o gateway for
// reconfigurado.
//
// Valores alinhados com infra/nginx/security-headers.conf. nginx `add_header`
// APPENDA (não substitui o header do upstream, salvo proxy_hide_header): se
// ambos forem emitidos, o browser aplica a INTERSEÇÃO da política — benigno
// aqui, pois esta API só devolve JSON consumido via fetch (CSP não governa
// JSON). Diferença proposital: a CSP aqui é a mínima de API (default-src
// 'none'), pois este serviço nunca devolve HTML/JS/CSS que precise de
// 'self'/'unsafe-inline'.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Evita MIME-sniffing de respostas JSON.
		h.Set("X-Content-Type-Options", "nosniff")
		// Anti-clickjacking (legado) + frame-ancestors na CSP abaixo.
		h.Set("X-Frame-Options", "SAMEORIGIN")
		// Não vazar a URL completa (pode conter IDs) para terceiros.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// API JSON: bloqueia qualquer carregamento de recurso e embedding.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		// Desabilita APIs sensíveis do browser nas respostas deste serviço.
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		// HSTS — força HTTPS no domínio (e subdomínios) por 1 ano.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware permite requisições cross-origin apenas de origens na allowlist.
// Sem dependência externa — stdlib puro.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-MB-Token, X-Alan-Token, X-Senderzz-Token, X-Request-ID")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
