// Serviço Go da Carteira+PIX — Senderzz Fase 2 (strangler fig).
//
// Variáveis de ambiente obrigatórias:
//   - DATABASE_URL           — DSN Postgres (pgx format)
//   - JWT_SECRET             — mesmo que tpc_jwt_secret do WP
//   - WEBHOOK_SECRET         — mesmo que tpc_webhook_secret do WP
//   - WP_SALT_AUTH           — AUTH_SALT do WordPress
//
// Variáveis opcionais:
//   - PORT                   — porta HTTP (default: 8081)
//   - ME_API_URL             — Melhor Envio API base (default: https://melhorenvio.com.br/api/v2)
//   - ME_TOKEN               — token OAuth ME
//   - WALLET_INTERNAL_SECRET — secret HMAC para rotas /internal/* (double-write PHP→Go).
//     Se ausente, rotas /internal/* ficam desativadas (fail-closed).
//   - ASYNQ_REDIS_ADDR       — Redis para Asynq scheduler (default: localhost:6379).
//     Se ausente, o scheduler de reconciliação não é iniciado.
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
	"github.com/senderzz/wallet-service/internal/db"
	"github.com/senderzz/wallet-service/internal/handlers"
	"github.com/senderzz/wallet-service/internal/jobs"
	"github.com/senderzz/wallet-service/internal/melhorenvio"
	"github.com/senderzz/wallet-service/internal/middleware"
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
		slog.Error("[wallet] falha ao conectar ao banco", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Instancia os handlers com injeção de dependência.
	walletH := handlers.NewWalletHandler(pool)
	pixH := handlers.NewPixHandler(pool)
	authH := handlers.NewAuthHandler(pool)

	// Cliente Melhor Envio (M-01): emissão de PIX via POST /me/balance.
	// Sem ME_TOKEN, GerarPix retorna erro claro (não panic) — ver melhorenvio.NewClient.
	meClient := melhorenvio.NewClient()
	if !meClient.HasToken() {
		slog.Warn("[wallet] ME_TOKEN ausente — emissão de PIX (/recarregar) retornará 503 até configurar")
	}
	recargaH := handlers.NewRecargaHandler(pool, meClient)

	// Instancia o handler de double-write (PHP → Go).
	// Se WALLET_INTERNAL_SECRET não estiver configurado, internalH = nil
	// e as rotas /internal/* ficam desativadas (fail-closed por design).
	internalH, err := handlers.NewInternalHandler(pool)
	if err != nil {
		slog.Error("[wallet] falha ao inicializar handler interno", "err", err)
		os.Exit(1)
	}
	if internalH != nil {
		slog.Info("[wallet] double-write PHP→Go ativado (rotas /internal/*)")
	}

	// ── Asynq: scheduler de reconciliação diária ─────────────────────────────
	// Só inicia se ASYNQ_REDIS_ADDR estiver configurado.
	redisAddr := os.Getenv("ASYNQ_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	reconcileTask := jobs.NewReconcileTask(pool)
	estornoReconcileTask := jobs.NewEstornoReconcileTask(pool)
	var asynqServer *asynq.Server
	var asynqScheduler *asynq.Scheduler

	redisOpt := asynq.RedisClientOpt{Addr: redisAddr}

	// Configura o servidor Asynq para processar tarefas de reconciliação.
	asynqServer = asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 2,
		Queues: map[string]int{
			"critical": 10,
			"default":  5,
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			slog.Error("[asynq] tarefa falhou",
				"type", task.Type(),
				"err", err,
			)
		}),
	})

	// Registra o handler de reconciliação no servidor Asynq.
	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeReconcile, reconcileTask.ProcessReconcile)
	mux.HandleFunc(jobs.TypeCreditarEstornosVencidos, estornoReconcileTask.ProcessCreditarEstornosVencidos)

	// Configura o scheduler para disparar reconciliação diariamente às 06:00 UTC
	// (03:00 BRT — janela de baixo tráfego na operação Senderzz).
	asynqScheduler = asynq.NewScheduler(redisOpt, nil)
	if _, err := asynqScheduler.Register(
		"0 6 * * *",
		asynq.NewTask(jobs.TypeReconcile, nil,
			asynq.TaskID("wallet:reconcile:daily"),
			asynq.MaxRetry(2),
			asynq.Queue("critical"),
		),
	); err != nil {
		slog.Error("[tpc_reconcile] falha ao registrar schedule", "err", err)
		// Não fatal — o servidor HTTP continua operando sem o scheduler.
	}

	// Estornos pendentes de cancelamento de expedição: sem sinal em tempo real
	// da ME (ver estorno_reconcile.go), credita automaticamente o que passar
	// da janela de segurança de 24h. Roda a cada hora — folga curta o bastante
	// pra não acumular muito atraso além da própria janela de 24h.
	if _, err := asynqScheduler.Register(
		"0 * * * *",
		asynq.NewTask(jobs.TypeCreditarEstornosVencidos, nil,
			asynq.TaskID("wallet:creditar-estornos-vencidos:hourly"),
			asynq.MaxRetry(2),
			asynq.Queue("critical"),
		),
	); err != nil {
		slog.Error("[tpc_estorno_reconcile] falha ao registrar schedule", "err", err)
	}

	// Inicia o servidor Asynq em background.
	go func() {
		slog.Info("[asynq] iniciando servidor de workers", "redis", redisAddr)
		if err := asynqServer.Run(mux); err != nil {
			slog.Error("[asynq] servidor encerrado com erro", "err", err)
		}
	}()

	// Inicia o scheduler Asynq em background.
	go func() {
		slog.Info("[asynq] iniciando scheduler (reconciliação diária 06:00 UTC)")
		if err := asynqScheduler.Run(); err != nil {
			slog.Error("[asynq] scheduler encerrado com erro", "err", err)
		}
	}()

	// ── Router HTTP ───────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	// RequestLogger substitui chimw.Logger (texto) por slog JSON estruturado e
	// injeta no contexto um logger pré-anotado com request_id (middleware.LoggerFrom).
	// DEVE ficar antes de Recoverer para registrar o acesso mesmo em panic→500.
	r.Use(middleware.RequestLogger)
	// Recoverer (slog, com request_id) substitui chimw.Recoverer: panic→500 JSON
	// no mesmo contrato dos demais erros + log ERROR com stack. NÃO muda negócio.
	r.Use(middleware.Recoverer)

	// /health = liveness puro: o processo está de pé? Sempre 200 (não toca no banco).
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"wallet","version":"2.0"}`))
	})

	// /readyz = readiness: o serviço está apto a receber tráfego? Faz pool.Ping
	// com timeout curto — 200 se o banco responde, 503 caso contrário. Separado de
	// /health para que orquestradores (k8s/compose) tirem a instância do balanceador
	// quando o Postgres cai SEM matar o processo (que /health continua reportando vivo).
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, pingCancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer pingCancel()
		w.Header().Set("Content-Type", "application/json")
		if err := pool.Ping(pingCtx); err != nil {
			slog.Error("[wallet] readiness check falhou — banco inacessível", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ok":false,"service":"wallet","erro":"banco indisponível"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true,"service":"wallet","version":"2.0"}`))
	})

	// Rotas de double-write PHP → Go (desativadas se WALLET_INTERNAL_SECRET ausente).
	// Registradas no root "/" para não exigir autenticação JWT (auth via HMAC).
	if internalH != nil {
		handlers.RegisterInternalRoutes(r, internalH)
	}

	r.Route("/wp-json/tp-carteira/v1", func(r chi.Router) {
		// Auth — públicas (sem JWT). M-02: port FIEL de rest-api.php:311-420.
		r.Post("/auth/token", authH.PostAuthToken)
		r.Post("/auth/token-from-portal-session", authH.PostAuthTokenFromPortalSession)

		// Webhook PIX — sem JWT (autenticado por HMAC-SHA256).
		r.Post("/pix/webhook", pixH.PostPixWebhook)
		// Alias compatível com o PHP (/webhook/pix).
		r.Post("/webhook/pix", pixH.PostPixWebhook)

		// Carteira — requer JWT.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthJWT)

			r.Get("/me", authH.GetMe)
			r.Get("/saldo", walletH.GetSaldo)
			r.Get("/extrato", walletH.GetExtrato)
			// M-01: recarga PIX (port FIEL de tpc_endpoint_recarregar / tpc_endpoint_pix_status).
			r.Post("/recarregar", recargaH.PostRecarregar)
			r.Get("/recarga/{recarga_id}/pix", recargaH.GetRecargaPix)
			// "Já paguei" — fail-closed: só registra a intenção e devolve prazo,
			// NÃO confirma/credita (port FIEL de tpc_endpoint_pix_ja_paguei, pix.php:469).
			r.Post("/pix/{recarga_id}/ja-paguei", recargaH.PostPixJaPaguei)

			// SEC-GO-01: reservar/debitar-reserva/creditar/liberar-reserva NÃO podem
			// viver sob JWT de usuário — creditam/debitam a carteira do PRÓPRIO caller
			// com valor + referência (idempotência) do body → mint de dinheiro.
			// São operações SERVIÇO-A-SERVIÇO. Quando o fluxo de reserva for ligado
			// (orders payments.go, TODO Fase 7), expor SOMENTE via /internal/* (HMAC),
			// lendo user_id do corpo HMAC-confiável — nunca do JWT do usuário.
			// O crédito de serviço já existe em /internal/transacoes (InserirTransacao).
		})

		// Admin — visão de QUALQUER usuário por user_id (port FIEL de
		// tpc_endpoint_admin_saldo / tpc_endpoint_admin_extrato, rest-api.php:280-289).
		// Auth: AuthAdminJWT — análogo fiel ao current_user_can('manage_woocommerce')
		// do PHP (admin humano logado). Fail-closed: secret ausente → 503.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthAdminJWT(pool))

			r.Get("/admin/usuario/{user_id}/saldo", walletH.GetSaldoAdmin)
			r.Get("/admin/usuario/{user_id}/extrato", walletH.GetExtratoAdmin)
		})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("[wallet] iniciando", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("[wallet] falha ao iniciar", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("[wallet] encerrando...")

	// Encerra o Asynq de forma graciosa antes do HTTP server.
	asynqScheduler.Shutdown()
	asynqServer.Shutdown()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("[wallet] erro no shutdown", "err", err)
	}
	slog.Info("[wallet] encerrado.")
}
