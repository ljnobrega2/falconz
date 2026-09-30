package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/httpx"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/middleware"
	"github.com/shopspring/decimal"
)

// GetMEBalance retorna o saldo atual da conta ME da plataforma.
// GET /wp-json/wc-melhor-envio/v1/balance
// Qualquer usuário autenticado pode consultar (saldo é da plataforma, não por produtor).
func (h *LabelHandler) GetMEBalance(w http.ResponseWriter, r *http.Request) {
	bal, err := h.me.GetBalance(r.Context())
	if err != nil {
		slog.Error("[senderzz_labels] GetMEBalance: erro", "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "erro ao consultar saldo ME: "+err.Error())
		return
	}
	httpx.WriteOK(w, map[string]any{"ok": true, "balance": bal})
}

// meBalanceCheckpointKey — senderzz_options.name da baseline de saldo ME usada
// pela reconciliação (ver ReconcileMEChargesTick).
const meBalanceCheckpointKey = "me_balance_checkpoint"

// confirmPendingCharge credita UMA charge pendente se o saldo real da ME já subiu
// o suficiente ACIMA do checkpoint (baseline) pra cobrir o valor. Idempotente
// (UPDATE ... WHERE status='pending'). Retorna true se confirmou/já estava paga.
//
// AUDIT-2026-07-27: o painel do Melhor Envio só oferece o evento "Atualização
// das etiquetas criadas e editadas" no webhook configurável (confirmado por print
// do dono) — NÃO existe evento de saldo/pagamento pra assinar. Confirmação tem
// que ser ATIVA: comparar saldo real da conta ME contra uma baseline.
//
// AUDIT-2026-07-28 (v2 — fix definitivo): a v1 comparava saldo real contra SOMA
// de recargas confirmadas menos gasto em etiquetas (wc_me_labels.price não
// canceladas) — mas etiquetas CANCELADAS no nosso banco às vezes NÃO são
// estornadas de verdade pela ME (achado ao vivo: 3 etiquetas canceladas do
// pedido 1633, R$77,97, nunca voltaram pro saldo real — gap batia exatamente
// com o buraco entre o saldo real e o esperado). Modelar gasto categoria por
// categoria é uma corrida sem fim contra qualquer jeito da ME cobrar sem a
// gente saber. Fix real: LARGA de modelar gasto. checkpoint = último saldo real
// conhecido em que não sobrava nada pendente pra explicar. unaccounted = saldo
// real ATUAL − checkpoint (mede só o que MUDOU desde a última reconciliação boa,
// não importa a causa). Ao confirmar uma charge, o checkpoint AVANÇA pelo valor
// confirmado — absorve automaticamente qualquer gasto real acontecido no meio,
// sem precisar saber a causa.
func confirmPendingCharge(ctx context.Context, db *pgxpool.Pool, chargeID, producerID int64, amount decimal.Decimal, checkpoint *decimal.Decimal, liveBalance decimal.Decimal) (bool, error) {
	unaccounted := liveBalance.Sub(*checkpoint)
	if unaccounted.LessThan(amount) {
		return false, nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE wc_me_balance_charges SET status = 'paid', updated_at = NOW()
		  WHERE id = $1 AND status = 'pending'`,
		chargeID,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// Corrida: outra chamada já confirmou entre o SELECT e aqui. Idempotente.
		return true, nil
	}

	var novoSaldo decimal.Decimal
	if err := tx.QueryRow(ctx,
		`UPDATE tpc_carteira SET saldo = saldo + $1 WHERE user_id = $2 RETURNING saldo`,
		amount, producerID,
	).Scan(&novoSaldo); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO tpc_transacoes (user_id, tipo, valor, saldo_apos, descricao, referencia, status, actor_id)
		 VALUES ($1, 'credito', $2, $3, 'Recarga PIX (Melhor Envio) confirmada', $4, 'confirmado', $1)
		 ON CONFLICT (user_id, referencia, tipo) DO NOTHING`,
		producerID, amount, novoSaldo, "me-charge-"+strconv.FormatInt(chargeID, 10),
	); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}

	*checkpoint = checkpoint.Add(amount)
	if _, err := db.Exec(ctx,
		`INSERT INTO senderzz_options (name, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		meBalanceCheckpointKey, checkpoint.StringFixed(2),
	); err != nil {
		slog.Error("[senderzz_labels] falha ao persistir checkpoint de saldo ME", "err", err)
	}
	slog.Info("[senderzz_labels] recarga ME confirmada por reconciliação ativa",
		"charge_id", chargeID, "producer_id", producerID, "amount", amount.StringFixed(2),
		"novo_checkpoint", checkpoint.StringFixed(2))
	return true, nil
}

// confirmPendingEstorno credita UM estorno de cancelamento de etiqueta pendente
// (tpc_transacoes status='pendente', referencia 'cancel_label_%'). Sempre
// credita o valor FIXO que foi debitado do produtor no checkout (amount, vindo
// de tpc_transacoes.valor) — nunca o preço/reembolso real da ME, que é
// tipicamente diferente (preço fechado pro produtor vs custo real da remessa).
// O caller (ReconcileEstornosTick) só chama isso depois de confirmar por
// remessa (GetShipmentStatus) que aquele shipment específico está
// status=="canceled" com canceled_at preenchido na ME.
func confirmPendingEstorno(ctx context.Context, db *pgxpool.Pool, txID, userID int64, amount decimal.Decimal) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE tpc_transacoes SET status = 'confirmado'
		  WHERE id = $1 AND tipo = 'credito' AND status = 'pendente'`,
		txID,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// Já confirmado (ops manual ou janela de segurança de 24h no wallet-service).
		return true, nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tpc_carteira SET saldo = saldo + $2::numeric WHERE user_id = $1`,
		userID, amount,
	); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tpc_transacoes SET saldo_apos = (SELECT saldo FROM tpc_carteira WHERE user_id = $1) WHERE id = $2`,
		userID, txID,
	); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}

	slog.Info("[senderzz_labels] estorno de cancelamento confirmado por remessa (ME confirmou canceled_at)",
		"tx_id", txID, "user_id", userID, "amount", amount.StringFixed(2))
	return true, nil
}

// ReconcileEstornosTick varre estornos de cancelamento pendentes (mais antigo
// primeiro) e confirma, POR REMESSA INDIVIDUAL, os que a ME já marcou como
// canceled+canceled_at (reembolso concluído do lado da ME). Substitui o antigo
// modelo de checkpoint de saldo agregado — aquele ficava cego sempre que gasto
// legítimo com novas etiquetas empurrava o saldo real abaixo do checkpoint
// entre duas verificações (achado ao vivo: 4 estornos do produtor 52 ficaram
// presos mesmo com a ME já tendo confirmado os 4 cancelamentos, porque outras
// etiquetas emitidas no meio tempo consumiram mais saldo do que os estornos
// devolveram). Checar por remessa é atribuível e não sofre esse blind spot —
// não depende nem mexe no checkpoint agregado (esse continua só pra recargas
// PIX em ReconcileMEChargesTick). Chamado a cada minuto (main.go); o que não
// bater aqui (ME lenta/indisponível) é coberto pela rede de segurança
// incondicional de 24h no wallet-service (estorno_reconcile.go).
func ReconcileEstornosTick(ctx context.Context, db *pgxpool.Pool, meClient *me.MEClient) (int, error) {
	rows, err := db.Query(ctx,
		`SELECT t.id, t.user_id, t.valor, l.me_shipment_id
		   FROM tpc_transacoes t
		   JOIN wc_me_labels l ON l.id = split_part(t.referencia, 'cancel_label_', 2)::bigint
		  WHERE t.tipo = 'credito' AND t.status = 'pendente' AND t.referencia LIKE 'cancel_label_%'
		  ORDER BY t.id ASC`,
	)
	if err != nil {
		return 0, err
	}
	type pendingEstorno struct {
		id, userID   int64
		valor        decimal.Decimal
		meShipmentID string
	}
	var pending []pendingEstorno
	for rows.Next() {
		var p pendingEstorno
		if err := rows.Scan(&p.id, &p.userID, &p.valor, &p.meShipmentID); err == nil {
			pending = append(pending, p)
		}
	}
	rows.Close()

	confirmed := 0
	for _, p := range pending {
		if p.meShipmentID == "" {
			continue
		}
		status, err := meClient.GetShipmentStatus(ctx, p.meShipmentID)
		if err != nil {
			slog.Error("[senderzz_labels] ReconcileEstornosTick: erro ao consultar shipment", "tx_id", p.id, "shipment_id", p.meShipmentID, "err", err)
			continue
		}
		if status.Status != "canceled" || status.CanceledAt == "" {
			continue
		}
		ok, err := confirmPendingEstorno(ctx, db, p.id, p.userID, p.valor)
		if err != nil {
			slog.Error("[senderzz_labels] ReconcileEstornosTick: erro ao confirmar", "tx_id", p.id, "err", err)
			continue
		}
		if ok {
			confirmed++
		}
	}
	return confirmed, nil
}

// loadMEBalanceCheckpoint lê (ou inicializa) a baseline de saldo ME persistida.
// Extraído de ReconcileMEChargesTick pra ser compartilhado com ReconcileEstornosTick.
func loadMEBalanceCheckpoint(ctx context.Context, db *pgxpool.Pool, liveBalance decimal.Decimal) (decimal.Decimal, error) {
	var checkpointRaw *string
	if err := db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`, meBalanceCheckpointKey,
	).Scan(&checkpointRaw); err != nil && err != pgx.ErrNoRows {
		return decimal.Zero, err
	}
	if checkpointRaw != nil && *checkpointRaw != "" {
		if v, errP := decimal.NewFromString(*checkpointRaw); errP == nil {
			return v, nil
		}
		return liveBalance, nil
	}
	_, _ = db.Exec(ctx,
		`INSERT INTO senderzz_options (name, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (name) DO NOTHING`,
		meBalanceCheckpointKey, liveBalance.StringFixed(2),
	)
	return liveBalance, nil
}

// ReconcileMEChargesTick varre wc_me_balance_charges pendentes (mais antiga
// primeiro) e confirma as que já cabem no saldo real da ME. onlyID>0 restringe a
// UMA charge (usado pelo clique "Já paguei"); onlyID=0 varre TODAS (usado pelo
// poll automático em main.go). Retorna quantas foram confirmadas nesta chamada.
func ReconcileMEChargesTick(ctx context.Context, db *pgxpool.Pool, meClient *me.MEClient, onlyID int64) (int, error) {
	liveBalance, err := meClient.GetBalance(ctx)
	if err != nil {
		return 0, err
	}

	// checkpoint = baseline persistida (senderzz_options). Bootstrap na ausência
	// (1ª execução pós-deploy desta versão): assume liveBalance ATUAL como
	// baseline — zero pendências assumidas resolvidas nesse instante; qualquer
	// recarga futura que fizer o saldo SUBIR além disso confirma normalmente.
	// Evita confirmar em massa por engano coisas antigas na primeira rodada.
	var checkpointRaw *string
	if err := db.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name = $1`, meBalanceCheckpointKey,
	).Scan(&checkpointRaw); err != nil && err != pgx.ErrNoRows {
		return 0, err
	}
	var checkpoint decimal.Decimal
	if checkpointRaw != nil && *checkpointRaw != "" {
		if v, errP := decimal.NewFromString(*checkpointRaw); errP == nil {
			checkpoint = v
		} else {
			checkpoint = liveBalance
		}
	} else {
		checkpoint = liveBalance
		_, _ = db.Exec(ctx,
			`INSERT INTO senderzz_options (name, value, updated_at) VALUES ($1, $2, NOW())
			 ON CONFLICT (name) DO NOTHING`,
			meBalanceCheckpointKey, checkpoint.StringFixed(2),
		)
	}

	// AUDIT-2026-07-28 CRÍTICO: achado ao vivo — cobrança PIX pendente há
	// SEMANAS (esquecida/abandonada) podia "casar" com dinheiro que chegou
	// AGORA de um produtor totalmente diferente (o algoritmo só sabe "saldo
	// subiu X, confirma a mais antiga pendente até bater"). Creditou R$35 no
	// Gabriel e R$68,32 no Lucas que não eram deles — dinheiro de outro
	// produtor foi parar na carteira errada. Corrigido manualmente uma vez;
	// trava daqui pra frente: só confirma automático cobrança criada nas
	// últimas 48h. Mais velha que isso é sinal de PIX abandonado/nunca pago —
	// vira revisão manual (admin confere com a ME antes de creditar).
	q := `SELECT id, producer_id, amount FROM wc_me_balance_charges
	       WHERE status = 'pending' AND created_at > NOW() - INTERVAL '48 hours'`
	args := []any{}
	if onlyID > 0 {
		q += ` AND id = $1`
		args = append(args, onlyID)
	}
	q += ` ORDER BY id ASC`

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	type pendingCharge struct {
		id, producerID int64
		amount         decimal.Decimal
	}
	var pending []pendingCharge
	for rows.Next() {
		var c pendingCharge
		if err := rows.Scan(&c.id, &c.producerID, &c.amount); err == nil {
			pending = append(pending, c)
		}
	}
	rows.Close()

	confirmed := 0
	for _, c := range pending {
		ok, err := confirmPendingCharge(ctx, db, c.id, c.producerID, c.amount, &checkpoint, liveBalance)
		if err != nil {
			slog.Error("[senderzz_labels] ReconcileMEChargesTick: erro ao confirmar", "charge_id", c.id, "err", err)
			continue
		}
		if ok {
			confirmed++
		}
	}
	return confirmed, nil
}

// PostBalancePixConfirm — "Já paguei — verificar confirmação".
// POST /wp-json/wc-melhor-envio/v1/balance/pix/{id}/confirm
// Mantido pro clique manual (feedback imediato); o poll automático (main.go)
// já confirma sozinho em segundo plano, então o clique é redundante na maioria
// das vezes — mas cobre o caso do produtor pagar e verificar antes do próximo tick.
func (h *LabelHandler) PostBalancePixConfirm(w http.ResponseWriter, r *http.Request) {
	callerID := middleware.GetUserID(r.Context())
	role := middleware.GetRole(r.Context())
	ctx := r.Context()

	chargeID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || chargeID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	var producerID int64
	var status string
	err = h.db.QueryRow(ctx,
		`SELECT producer_id, status FROM wc_me_balance_charges WHERE id = $1`,
		chargeID,
	).Scan(&producerID, &status)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if producerID != callerID && role != "admin" {
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if status != "pending" {
		httpx.WriteOK(w, map[string]any{"ok": true, "status": status, "confirmed": status == "paid"})
		return
	}

	if _, err := ReconcileMEChargesTick(ctx, h.db, h.me, chargeID); err != nil {
		slog.Error("[senderzz_labels] PostBalancePixConfirm: reconcile falhou", "charge_id", chargeID, "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "erro ao consultar saldo ME: "+err.Error())
		return
	}

	var finalStatus string
	_ = h.db.QueryRow(ctx, `SELECT status FROM wc_me_balance_charges WHERE id = $1`, chargeID).Scan(&finalStatus)
	httpx.WriteOK(w, map[string]any{"ok": true, "status": finalStatus, "confirmed": finalStatus == "paid"})
}

// PostBalancePix gera um PIX para o produtor recarregar a conta ME da plataforma.
// POST /wp-json/wc-melhor-envio/v1/balance/pix
// Body: {"amount": "50.00"}
//
// Fluxo:
//  1. Chama ME API POST /me/balance → retorna QR PIX
//  2. Persiste em wc_me_balance_charges (rastreio por producer_id)
//  3. Retorna QR code ao frontend
func (h *LabelHandler) PostBalancePix(w http.ResponseWriter, r *http.Request) {
	producerID := middleware.GetUserID(r.Context())

	var req struct {
		Amount decimal.Decimal `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "payload inválido")
		return
	}
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "valor deve ser maior que zero")
		return
	}
	if req.Amount.LessThan(decimal.NewFromFloat(10)) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "valor mínimo de recarga é R$ 10,00")
		return
	}
	if req.Amount.GreaterThan(decimal.NewFromFloat(5000)) {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "valor máximo de recarga é R$ 5.000,00")
		return
	}

	charge, err := h.me.AddBalance(r.Context(), req.Amount)
	if err != nil {
		slog.Error("[senderzz_labels] PostBalancePix: AddBalance falhou",
			"producer_id", producerID, "amount", req.Amount.StringFixed(2), "err", err)
		httpx.WriteErr(w, http.StatusBadGateway, "erro ao gerar PIX ME: "+err.Error())
		return
	}

	// Persiste a recarga no banco para auditoria por produtor.
	var chargeID string
	if charge != nil {
		chargeID = charge.ID
	}
	var qrCode, qrImage, link, expiryStr string
	if charge != nil {
		qrCode = charge.QRCode
		qrImage = charge.QRCodeImage
		link = charge.Link
		expiryStr = charge.Expiry
	}
	var expiry *time.Time
	if expiryStr != "" {
		if t, err := time.Parse(time.RFC3339, expiryStr); err == nil {
			expiry = &t
		}
	}

	// AUDIT-2026-07-30 HIGH: erro do INSERT era descartado (`_ =`). AddBalance já
	// tinha criado o PIX REAL na ME nesse ponto — se o INSERT local falhasse
	// (blip de banco), o produtor recebia e pagava um QR real sem NENHUM registro
	// local, e ReconcileMEChargesTick nunca teria uma charge 'pending' pra
	// confirmar: o dinheiro subiria no saldo real da ME e seria silenciosamente
	// absorvido pelo avanço do checkpoint na próxima recarga de OUTRO produtor —
	// produtor pagou, nunca foi creditado. Fix: tenta de novo (blip transitório é
	// o caso comum); se persistir, loga ERROR com todos os dados da charge real
	// pra reconciliação manual (não bloqueia a resposta — o PIX já é real na ME,
	// recusar aqui só faria o produtor gerar um SEGUNDO PIX e piorar o problema).
	var dbID int64
	insert := func() error {
		return h.db.QueryRow(r.Context(),
			`INSERT INTO wc_me_balance_charges
			    (producer_id, me_charge_id, amount, status, qr_code, qr_code_image, link, expiry)
			 VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7)
			 RETURNING id`,
			producerID,
			nullStr(chargeID),
			req.Amount,
			nullStr(qrCode),
			nullStr(qrImage),
			nullStr(link),
			expiry,
		).Scan(&dbID)
	}
	if err := insert(); err != nil {
		if err2 := insert(); err2 != nil {
			slog.Error("[senderzz_labels] CRÍTICO: recarga PIX real criada na ME mas INSERT local falhou 2x — produtor vai pagar sem registro, exige reconciliação manual",
				"producer_id", producerID,
				"amount", req.Amount.StringFixed(2),
				"me_charge_id", chargeID,
				"qr_code", qrCode,
				"err", err2,
			)
		}
	}

	slog.Info("[senderzz_labels] recarga ME criada",
		"db_id", dbID,
		"producer_id", producerID,
		"amount", req.Amount.StringFixed(2),
		"charge_id", chargeID,
	)

	httpx.WriteOK(w, map[string]any{
		"ok":            true,
		"id":            dbID,
		"amount":        req.Amount,
		"qr_code":       qrCode,
		"qr_code_image": qrImage,
		"link":          link,
		"expiry":        expiryStr,
		"status":        "pending",
	})
}

// GetBalanceHistory lista as recargas ME do produtor autenticado.
// GET /wp-json/wc-melhor-envio/v1/balance/history
func (h *LabelHandler) GetBalanceHistory(w http.ResponseWriter, r *http.Request) {
	producerID := middleware.GetUserID(r.Context())

	rows, err := h.db.Query(r.Context(),
		`SELECT id, amount, status, expiry, created_at
		   FROM wc_me_balance_charges
		  WHERE producer_id = $1
		  ORDER BY created_at DESC
		  LIMIT 50`,
		producerID,
	)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar histórico")
		return
	}
	defer rows.Close()

	type row struct {
		ID        int64           `json:"id"`
		Amount    decimal.Decimal `json:"amount"`
		Status    string          `json:"status"`
		Expiry    *time.Time      `json:"expiry"`
		CreatedAt time.Time       `json:"created_at"`
	}
	var data []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Amount, &r.Status, &r.Expiry, &r.CreatedAt); err != nil {
			continue
		}
		data = append(data, r)
	}
	if data == nil {
		data = []row{}
	}
	httpx.WriteOK(w, map[string]any{"ok": true, "data": data})
}

// nullStr converte string vazia para nil (para NULLABLE colunas no Postgres).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
