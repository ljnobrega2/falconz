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
//                              Se ausente, rotas /internal/* ficam desativadas (fail-closed).
//   - ASYNQ_REDIS_ADDR       — Redis para Asynq scheduler (default: localhost:6379).
//                              Se ausente, o scheduler de reconciliação não é iniciado.
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
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"service":"wallet","version":"2.0"}`))
	})

	// Rotas de double-write PHP → Go (desativadas se WALLET_INTERNAL_SECRET ausente).
	// Registradas no root "/" para não exigir autenticação JWT (auth via HMAC).
	if internalH != nil {
		handlers.RegisterInternalRoutes(r, internalH)
	}

	r.Route("/wp-json/tp-carteira/v1", func(r chi.Router) {
		// Auth
		r.Post("/auth/token", stub("POST /auth/token"))
		r.Post("/auth/token-from-portal-session", stub("POST /auth/token-from-portal-session"))

		// Webhook PIX — sem JWT (autenticado por HMAC-SHA256).
		r.Post("/pix/webhook", pixH.PostPixWebhook)
		// Alias compatível com o PHP (/webhook/pix).
		r.Post("/webhook/pix", pixH.PostPixWebhook)

		// Carteira — requer JWT.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthJWT)

			r.Get("/me", stub("GET /me"))
			r.Get("/saldo", walletH.GetSaldo)
			r.Get("/extrato", walletH.GetExtrato)
			r.Post("/recarregar", stub("POST /recarregar"))
			r.Get("/recarga/{recarga_id}/pix", stub("GET /recarga/{id}/pix"))

			// Operações internas de reserva (usadas pelo serviço ME e por jobs).
			r.Post("/carteira/reservar", walletH.PostReservar)
			r.Post("/carteira/debitar-reserva", walletH.PostDebitarReserva)
			r.Post("/carteira/creditar", walletH.PostCreditar)
			r.Post("/carteira/liberar-reserva", walletH.PostLiberarReserva)
		})

		// Admin — requer permissão manage_woocommerce (stub — implementar em Fase 3).
		r.Group(func(r chi.Router) {
			r.Get("/admin/usuario/{user_id}/saldo", stub("GET /admin/usuario/{id}/saldo"))
			r.Get("/admin/usuario/{user_id}/extrato", stub("GET /admin/usuario/{id}/extrato"))
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

func stub(route string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Info("[wallet] stub hit — implementar na Fase 2", "route", route)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"ok":false,"erro":"Rota ` + route + ` ainda não implementada no serviço Go (Fase 2).","hint":"Verificar openapi-tp-carteira-v1.yaml"}`))
	}
}
