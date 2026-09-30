// Serviço Go do CRON runner — Senderzz (site novo).
//
// O QUE FAZ: daemon de instância única que, em intervalo fixo, chama funções
// Postgres IDEMPOTENTES (criadas e testadas na DB viva em
// infra/postgres/230-fixes-v472-crons.sql) e grava observabilidade em
// senderzz_cron_status (status atual por nome) + senderzz_cron_runs (histórico).
//
// Antes daqui existia só o VIEWER (go/admin/.../cron_status.go) — nenhum runner
// de fato disparava os jobs. Este serviço fecha o ciclo.
//
// Variáveis de ambiente:
//   - DATABASE_URL           — DSN Postgres (pgx format) — OBRIGATÓRIA
//   - CRON_INTERVAL_SECONDS  — intervalo do ticker em segundos (default 300; inválido/≤0 ignorado)
//   - CRON_RUN_ONCE          — se "1", roda todos os jobs uma vez, imprime resumo e sai 0 (smoke test)
//
// PRINCÍPIO: jobs idempotentes — re-disparo é inofensivo, então os jobs rodam a cada
// tick (exceto os 2 jobs LGPD, com gate minInterval=24h: anonimização roda 1×/dia).
// Um job que falha NÃO derruba o runner nem bloqueia os outros (recover + log +
// continua). NUNCA logamos a DSN (carrega a senha).
//
// Crons de API EXTERNA (Melhor Envio reconcile, recarga PIX) NÃO são implementados
// aqui: em DEV não há token ME. Logamos um aviso honesto no startup e NÃO gravamos
// linhas de status falsas 'ok' para eles.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/cron-service/internal/dispatch"
)

// job — um cron a executar. Dois modos, MUTUAMENTE EXCLUSIVOS:
//   - SQL: `sql` é uma chamada idempotente que retorna um único int (count). É o
//     modo dos jobs originais — preservado intacto.
//   - Go:  `run` é uma função Go que recebe o pool e retorna (count, error). Usado
//     pelo dispatcher de webhooks (sz_webhook_dispatch), que precisa de transação,
//     HTTP e lógica que não cabe num SELECT. Quando `run != nil`, ele tem
//     precedência e `sql` é ignorado.
type job struct {
	name string // nome usado em senderzz_cron_status/_runs (o viewer faz merge por nome)
	sql  string // chamada SQL idempotente; deve retornar um único int

	// run — implementação Go opcional. Se != nil, runJob chama `run` em vez de `sql`.
	// Roda dentro do mesmo recover() dos jobs SQL: um panic vira erro, nunca derruba
	// o runner.
	run func(ctx context.Context, pool *pgxpool.Pool) (int, error)

	// minInterval — janela mínima entre execuções deste job. Zero (default dos 4
	// jobs originais) = roda a cada tick (idempotente, re-disparo inofensivo).
	// >0 = gate por last_run em senderzz_cron_status: pula se rodou há menos que isso.
	// Usado pelos jobs LGPD (sz_anonymize_old_pii e sz_anonymize_old_order_pii), que
	// devem rodar 1×/dia, não a cada tick — anonimização é cara/ruidosa no histórico e
	// não há ganho em repetir antes da janela de retenção (2 anos) avançar. RUN_ONCE
	// ignora o gate (force-run no smoke).
	minInterval time.Duration
}

// jobs — jobs idempotentes. Nomes FIXADOS pela task:
//   - job 3 (cleanup) usa "senderzz_db_cleanup" (casa com o catálogo de 18 crons do viewer)
//   - os demais não têm entrada exata no catálogo; usamos nomes claros (não aparecem
//     na tela hardcoded, mas a tabela de runs registra mesmo assim — isso é esperado).
//
// LGPD (DOIS jobs de retenção, ambos minInterval=24h — 1 execução/dia):
//
//   - sz_anonymize_old_pii (250-fixes-v472-retention.sql): mascara PII de TELEMETRIA
//     (ip/user_agent/actor_email) de sessões expiradas e da trilha de acesso a PII
//     com > 2 anos. NUNCA deleta linha nem toca dado financeiro. Observação: a própria
//     função SQL já insere 1 linha em senderzz_cron_runs (auto-log, duration_ms=0)
//     ANTES do record() do runner — o record() é necessário do mesmo jeito porque é
//     ele que faz o UPSERT em senderzz_cron_status (a função não toca o status).
//     Resultado: 2 linhas de histórico por execução (auto-log da função + run do
//     runner) — comportamento pré-existente da função, que vive fora de go/cron.
//
//   - sz_anonymize_old_order_pii (401-fix-auditoria-infra.sql): mascara PII de PEDIDO/
//     ENTREGA (nome/telefone/email em sz_order_addresses e dest_nome/dest_telefone em
//     sz_motoboy_pedidos) de pedidos FINALIZADOS há > 2 anos (LGPD Art.15/16),
//     respeitando guarda fiscal — NUNCA deleta linha nem toca valor financeiro.
//     Idempotente (não re-mascara já-anonimizado). Diferente da função acima, esta NÃO
//     auto-loga em senderzz_cron_runs (só dois UPDATEs + RETURN), então gera exatamente
//     1 linha de histórico, a do record() do runner.
var jobs = []job{
	{name: "sz_cod_release_due", sql: "SELECT sz_cod_release_due()"},
	{name: "sz_affiliate_release_due", sql: "SELECT sz_affiliate_release_due()"},
	{name: "senderzz_db_cleanup", sql: "SELECT sz_cleanup_expired_sessions()"},
	{name: "sz_cancel_preagendados_vencidos", sql: "SELECT sz_cancel_preagendados_vencidos()"},
	{name: "sz_motoboy_financial_overdue_due", sql: "SELECT sz_motoboy_financial_overdue_due()"},
	// REF-PAYOUT 462 — recompensa de indicação (idempotente, UNIQUE ref). Credita 1×
	// por pedido concluído de produtor indicado (COD 2,5% / frete 1%).
	{name: "sz_referral_payout", sql: "SELECT sz_referral_payout()"},
	// AUDIT-2026-07-31 (dono): liquida reembolso confirmado por remessa (ME
	// cancelou a etiqueta) ou por prazo vencido (12h fallback, quando não dá
	// pra verificar — motoboy/COD sem etiqueta ME, ou ME nunca confirmou).
	{name: "sz_process_refund_requests_due", sql: "SELECT sz_process_refund_requests_due()"},
	{name: "sz_anonymize_old_pii", sql: "SELECT sz_anonymize_old_pii()", minInterval: 24 * time.Hour},
	{name: "sz_anonymize_old_order_pii", sql: "SELECT sz_anonymize_old_order_pii()", minInterval: 24 * time.Hour},
	// AUDIT-LGPD-CHECKOUT-2026-06-24 #A2 (474-webhook-log-retention.sql): expurga
	// senderzz_webhook_log > 30 dias — payload guarda PII de cliente/entrega verbatim
	// (LGPD Art.16). DELETE idempotente; auto-loga 1 linha no histórico via record().
	{name: "sz_purge_webhook_log", sql: "SELECT sz_purge_webhook_log()", minInterval: 24 * time.Hour},

	// Dispatcher do outbox de webhooks (Go, não SQL). Consome sz_webhook_outbox e
	// entrega via POST aos webhooks do produtor. GATE de produção interno
	// (webhook_dispatch_enabled, default '0'): com a flag desligada não faz nenhum
	// POST. minInterval=1min — não inundar endpoints; SKIP LOCKED previne dupla
	// entrega entre ticks sobrepostos.
	{name: "sz_webhook_dispatch", run: dispatch.Dispatch, minInterval: time.Minute},
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// SIGINT/SIGTERM → cancela o contexto e sai limpo.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := connect(ctx)
	if err != nil {
		slog.Error("[senderzz_cron] falha ao conectar ao banco", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Aviso honesto: crons de API externa NÃO rodam aqui (sem token ME em DEV).
	slog.Info("[senderzz_cron] crons de API externa NÃO executados (Melhor Envio reconcile, recarga PIX) — sem token ME em DEV; nenhuma linha de status falsa será gravada para eles")

	// ── Modo RUN-ONCE (smoke test) ───────────────────────────────────────────────
	// Roda os jobs uma vez, imprime um resumo por job e sai 0. O status por linha já
	// comunica sucesso/falha, então não saímos com código != 0 mesmo se um job falhar.
	if os.Getenv("CRON_RUN_ONCE") == "1" {
		slog.Info("[senderzz_cron] CRON_RUN_ONCE=1 — execução única (smoke test)")
		// force=true: ignora o gate minInterval — o smoke PRECISA rodar e provar o job
		// mesmo que ele já tenha rodado hoje (senão o resumo vem vazio e nada é provado).
		results := runAll(ctx, pool, true)
		for _, r := range results {
			fmt.Printf("RUN_ONCE | %-32s | count=%-4d | status=%-7s | %dms\n",
				r.name, r.count, r.status, r.durationMs)
		}
		os.Exit(0)
	}

	// ── Modo daemon ──────────────────────────────────────────────────────────────
	interval := intervalFromEnv()
	slog.Info("[senderzz_cron] iniciando daemon", "interval_seconds", int(interval.Seconds()), "jobs", len(jobs))

	// Roda uma vez já no startup — senão nada dispara nos primeiros N segundos.
	// force=false: o gate minInterval vale no daemon (LGPD só roda 1×/dia).
	runAll(ctx, pool, false)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("[senderzz_cron] sinal recebido, encerrando...")
			return
		case <-ticker.C:
			runAll(ctx, pool, false)
		}
	}
}

// jobResult — resultado de um job, usado no resumo do RUN-ONCE.
type jobResult struct {
	name       string
	count      int
	status     string // ok | error
	durationMs int64
}

// runAll executa todos os jobs em sequência. Cada job é isolado: uma falha (erro
// SQL ou panic) é capturada, registrada e não impede os demais.
// force=true ignora o gate minInterval (usado no RUN_ONCE/smoke).
func runAll(ctx context.Context, pool *pgxpool.Pool, force bool) []jobResult {
	results := make([]jobResult, 0, len(jobs))
	for _, j := range jobs {
		// Gate de janela mínima: jobs com minInterval>0 (LGPD) só rodam se passou
		// tempo suficiente desde o último last_run. No RUN_ONCE (force) sempre roda.
		if !force && j.minInterval > 0 && jobRecentlyRan(ctx, pool, j) {
			slog.Info("[senderzz_cron] job dentro da janela mínima, pulando (sem gravar status)",
				"name", j.name, "min_interval_h", int(j.minInterval.Hours()))
			continue // não grava nada: preserva o último 'ok' real e não repolui o histórico
		}
		results = append(results, runJob(ctx, pool, j))
	}
	return results
}

// jobRecentlyRan diz se o job rodou há MENOS que seu minInterval, lendo last_run de
// senderzz_cron_status. Sem linha / last_run NULL = roda (1ª vez). Em caso de erro de
// leitura, fail-open (roda): a função SQL é idempotente, então rodar de novo é seguro,
// e é melhor anonimizar PII a mais do que pular silenciosamente por um erro transitório.
func jobRecentlyRan(ctx context.Context, pool *pgxpool.Pool, j job) bool {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var lastRun *time.Time
	err := pool.QueryRow(qctx,
		`SELECT last_run FROM senderzz_cron_status WHERE name = $1`, j.name,
	).Scan(&lastRun)
	if err != nil {
		// pgx.ErrNoRows ou qualquer outro erro → fail-open (não bloqueia a execução).
		return false
	}
	if lastRun == nil {
		return false
	}
	return time.Since(*lastRun) < j.minInterval
}

// runJob executa um único job, mede a duração e grava a observabilidade.
// recover() garante que um panic vire erro e NUNCA derrube o runner.
func runJob(ctx context.Context, pool *pgxpool.Pool, j job) (res jobResult) {
	start := time.Now().UTC()
	res.name = j.name

	var (
		count  int
		jobErr error
	)

	func() {
		// Blindagem: panic no driver/SQL/run() vira erro, não crash do processo.
		defer func() {
			if rec := recover(); rec != nil {
				jobErr = fmt.Errorf("panic: %v", rec)
			}
		}()
		if j.run != nil {
			// Job Go (ex.: dispatcher de webhooks). Tem precedência sobre `sql`.
			count, jobErr = j.run(ctx, pool)
		} else {
			jobErr = pool.QueryRow(ctx, j.sql).Scan(&count)
		}
	}()

	res.durationMs = time.Since(start).Milliseconds()
	res.count = count

	var message string
	if jobErr != nil {
		res.status = "error"
		message = jobErr.Error()
		slog.Error("[senderzz_cron] job falhou", "name", j.name, "err", jobErr, "duration_ms", res.durationMs)
	} else {
		res.status = "ok"
		message = fmt.Sprintf("%d registros processados", count)
		slog.Info("[senderzz_cron] job ok", "name", j.name, "count", count, "duration_ms", res.durationMs)
	}

	record(pool, j.name, start, res.durationMs, res.status, message)
	return res
}

// record grava as duas linhas de observabilidade.
// Usa context.Background() com timeout curto (não o ctx do runner): no instante do
// shutdown o ctx do job está cancelado, mas ainda queremos que a última linha caia.
// Se a própria gravação falhar, apenas logamos — nunca derrubamos o runner.
func record(pool *pgxpool.Pool, name string, lastRun time.Time, durationMs int64, status, message string) {
	wctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// UPSERT status atual — espelha go/admin/.../cron_status.go (5 colunas).
	// NÃO tocamos em next_run: essa coluna é do SkipNext do viewer.
	_, err := pool.Exec(wctx,
		`INSERT INTO senderzz_cron_status
		   (name, last_run, last_status, last_message, last_duration_ms)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (name) DO UPDATE SET
		   last_run         = EXCLUDED.last_run,
		   last_status      = EXCLUDED.last_status,
		   last_message     = EXCLUDED.last_message,
		   last_duration_ms = EXCLUDED.last_duration_ms`,
		name, lastRun, status, message, durationMs)
	if err != nil {
		slog.Error("[senderzz_cron] falha ao gravar senderzz_cron_status", "name", name, "err", err)
	}

	// Linha de histórico.
	_, err = pool.Exec(wctx,
		`INSERT INTO senderzz_cron_runs (name, started_at, duration_ms, status, message)
		 VALUES ($1, $2, $3, $4, $5)`,
		name, lastRun, durationMs, status, message)
	if err != nil {
		slog.Error("[senderzz_cron] falha ao gravar senderzz_cron_runs", "name", name, "err", err)
	}
}

// intervalFromEnv lê CRON_INTERVAL_SECONDS; default 300s; inválido ou ≤0 ignorado.
func intervalFromEnv() time.Duration {
	const def = 300 * time.Second
	v := os.Getenv("CRON_INTERVAL_SECONDS")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		slog.Warn("[senderzz_cron] CRON_INTERVAL_SECONDS inválido, usando default", "valor", v, "default_seconds", 300)
		return def
	}
	return time.Duration(n) * time.Second
}

// connect cria e valida o pool Postgres via DATABASE_URL.
// Pool enxuto: este runner é instância única e roda os jobs em sequência — 2-4
// conexões bastam. Falha rápido se o banco estiver inacessível.
// NUNCA logamos a DSN (carrega a senha).
func connect(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("[senderzz_cron] DATABASE_URL não definida")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_cron] erro ao parsear DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 4
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("[senderzz_cron] falha ao criar pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("[senderzz_cron] banco inacessível: %w", err)
	}
	return pool, nil
}
