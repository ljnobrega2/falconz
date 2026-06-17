// Package handlers — endpoint admin para a carteira dos motoboys.
//
// Espelha sz_mb_tab_carteiras() em includes/motoboy/admin.php:1837 (PHP legado)
// sobre Postgres. Cada motoboy acumula "ganhos" (sz_motoboy_ganhos) gerados por
// pedido entregue/frustrado; o pagamento (sz_motoboy_pagamentos) consome esses
// ganhos do mais antigo para o mais novo (FIFO) atualizando valor_pago e status.
//
// Surface:
//
//	GET  /motoboy-carteira/summary             — KPIs globais (disp/conf/pago)
//	GET  /motoboy-carteira?q=&limit=200        — lista por motoboy + saldos + qtds
//	POST /motoboy-carteira/{motoboy_id}/pagamento
//	     body: {valor_pago, data_pagamento, obs}
//	     → transação: insere pagamento + alimenta valor_pago dos ganhos (FIFO)
//	GET  /motoboy-carteira/{motoboy_id}/historico?limit=150
//	     → JOIN ganhos × pedidos × pagamentos, com saldo aberto e recebido cliente
//	POST /motoboy-carteira/sync
//	     → reconstrói sz_motoboy_ganhos + sz_motoboy_fechamento a partir de sz_motoboy_pedidos
//
// Notas:
//   - "valor_pago" pode não existir em sz_motoboy_ganhos (instalações antigas).
//     A coluna é auto-ALTERed pelo PHP; aqui usamos COALESCE em todas as queries
//     e dependemos de tableExists só para tabelas raiz. Coluna ausente vira 500
//     de degradação aceitável — o admin será orientado a rodar a migração PHP.
//   - O pagamento é envolto em pgx.Tx com SELECT FOR UPDATE para evitar dois
//     pagamentos concorrentes consumirem o mesmo ganho (paridade com o lock
//     pessimista usado em cod_saques.go ApproveAffiliate).
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyCarteiraHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// Summary — KPIs globais agregados sobre todos os motoboys ativos.
type MotoboyCarteiraSummary struct {
	TotalDisponivel  float64 `json:"total_disponivel"`
	TotalAConciliar  float64 `json:"total_a_conciliar"`
	TotalPago        float64 `json:"total_pago"`
}

// MotoboyCarteiraRow — linha por motoboy na listagem principal.
type MotoboyCarteiraRow struct {
	MotoboyID        int64   `json:"motoboy_id"`
	MotoboyNome      string  `json:"motoboy_nome"`
	Telefone         string  `json:"telefone"`
	ZonaNome         string  `json:"zona_nome"`
	SaldoDisponivel  float64 `json:"saldo_disponivel"`
	SaldoAConciliar  float64 `json:"saldo_a_conciliar"`
	SaldoPago        float64 `json:"saldo_pago"`
	QtdDisponivel    int64   `json:"qtd_disponivel"`
	QtdPendente      int64   `json:"qtd_pendente"`
	QtdPago          int64   `json:"qtd_pago"`
}

// HistoricoRow — linha do extrato detalhado de um motoboy.
type HistoricoRow struct {
	ID              int64   `json:"id"`
	Data            string  `json:"data"`
	PedidoID        int64   `json:"pedido_id"`
	WcOrderID       int64   `json:"wc_order_id"`
	Tipo            string  `json:"tipo"`
	Valor           float64 `json:"valor"`
	PagoNesteGanho  float64 `json:"pago_neste_ganho"`
	SaldoAberto     float64 `json:"saldo_aberto"`
	DataSaque       *string `json:"data_saque"`
	Status          string  `json:"status"`
	RecebidoCliente float64 `json:"recebido_cliente"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists igual ao padrão de audit.go / cod_saques.go.
func (h *MotoboyCarteiraHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// parseMotoboyIDParam lê chi URLParam "motoboy_id" como int64 positivo.
func parseMotoboyIDParam(r *http.Request) (int64, bool) {
	s := chi.URLParam(r, "motoboy_id")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// clampLimit aplica clamp [1, max] com default fallback.
func clampLimit(raw string, def, max int) int {
	n, _ := strconv.Atoi(raw)
	if n <= 0 {
		n = def
	}
	if n > max {
		n = max
	}
	return n
}

// ─── Summary ─────────────────────────────────────────────────────────────

// Summary retorna KPIs globais.
// GET /motoboy-carteira/summary
//
//	total_disponivel = SUM(GREATEST(valor - valor_pago, 0)) WHERE status='disponivel'
//	total_a_conciliar = SUM(valor) WHERE status='pendente'
//	total_pago = SUM(valor_pago) (todos os status)
func (h *MotoboyCarteiraHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := MotoboyCarteiraSummary{}

	// Tabela raiz ausente = retorna zeros sem erro (paridade com audit.go).
	if !h.tableExists(ctx, "sz_motoboy_ganhos") {
		httpx.JSON(w, 200, out)
		return
	}

	err := h.Pool.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE
				WHEN status='disponivel'
				THEN GREATEST(COALESCE(valor,0) - COALESCE(valor_pago,0), 0)
				ELSE 0 END), 0) AS total_disponivel,
			COALESCE(SUM(CASE
				WHEN status='pendente'
				THEN COALESCE(valor,0)
				ELSE 0 END), 0) AS total_a_conciliar,
			COALESCE(SUM(COALESCE(valor_pago,0)), 0) AS total_pago
		 FROM sz_motoboy_ganhos`).Scan(&out.TotalDisponivel, &out.TotalAConciliar, &out.TotalPago)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, out)
}

// ─── List ────────────────────────────────────────────────────────────────

// List retorna saldos agregados por motoboy ativo.
// GET /motoboy-carteira?q=&limit=200
//
// Espelha o GROUP BY do PHP (admin.php:1901) — só motoboys ativos, com saldos
// e contagens por status. Filtro q é ILIKE sobre m.nome (case-insensitive).
func (h *MotoboyCarteiraHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboys") {
		httpx.JSON(w, 200, map[string]any{"items": []MotoboyCarteiraRow{}, "count": 0})
		return
	}

	q := r.URL.Query().Get("q")
	limit := clampLimit(r.URL.Query().Get("limit"), 200, 500)

	// JOIN em sz_motoboy_ganhos / sz_motoboy_zonas é LEFT — motoboys sem ganhos
	// aparecem com saldos zerados. ILIKE %% casa com qualquer nome quando q=''.
	rows, err := h.Pool.Query(ctx,
		`SELECT
			m.id,
			COALESCE(m.nome,''),
			COALESCE(m.telefone,''),
			COALESCE(z.nome,'') AS zona_nome,
			COALESCE(SUM(CASE
				WHEN g.status='disponivel'
				THEN GREATEST(COALESCE(g.valor,0) - COALESCE(g.valor_pago,0), 0)
				ELSE 0 END), 0) AS saldo_disponivel,
			COALESCE(SUM(CASE
				WHEN g.status='pendente'
				THEN COALESCE(g.valor,0)
				ELSE 0 END), 0) AS saldo_a_conciliar,
			COALESCE(SUM(COALESCE(g.valor_pago,0)), 0) AS saldo_pago,
			COUNT(CASE
				WHEN g.status='disponivel'
				 AND GREATEST(COALESCE(g.valor,0) - COALESCE(g.valor_pago,0), 0) > 0
				THEN 1 END) AS qtd_disponivel,
			COUNT(CASE WHEN g.status='pendente' THEN 1 END) AS qtd_pendente,
			COUNT(CASE WHEN COALESCE(g.valor_pago,0) > 0 THEN 1 END) AS qtd_pago
		 FROM sz_motoboys m
		 LEFT JOIN sz_motoboy_zonas z  ON z.id = m.zona_id
		 LEFT JOIN sz_motoboy_ganhos g ON g.motoboy_id = m.id
		 WHERE COALESCE(m.ativo, TRUE) = TRUE
		   AND ($1 = '' OR m.nome ILIKE '%' || $1 || '%')
		 GROUP BY m.id, m.nome, m.telefone, z.nome
		 ORDER BY m.nome ASC
		 LIMIT $2`, q, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []MotoboyCarteiraRow{}
	for rows.Next() {
		var row MotoboyCarteiraRow
		if err := rows.Scan(
			&row.MotoboyID, &row.MotoboyNome, &row.Telefone, &row.ZonaNome,
			&row.SaldoDisponivel, &row.SaldoAConciliar, &row.SaldoPago,
			&row.QtdDisponivel, &row.QtdPendente, &row.QtdPago,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		out = append(out, row)
	}
	httpx.JSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

// ─── Pagamento ───────────────────────────────────────────────────────────

// pagamentoBody — body esperado em POST /motoboy-carteira/{motoboy_id}/pagamento.
type pagamentoBody struct {
	ValorPago      float64 `json:"valor_pago"`
	DataPagamento  string  `json:"data_pagamento"` // YYYY-MM-DD; vazio = NOW()::date
	Obs            string  `json:"obs"`
}

// RegistrarPagamento aplica um pagamento parcial/total à carteira do motoboy.
//
// Transação atômica:
//  1. Lock pessimista: SELECT saldo disponível dos ganhos FOR UPDATE
//  2. Valida valor_pago <= saldo_disponivel (paridade com admin.php:1862)
//  3. INSERT em sz_motoboy_pagamentos com status='pago'
//  4. Itera ganhos disponiveis ORDER BY created_at ASC e consome FIFO:
//     aplicar = MIN(restante, valor - valor_pago);
//     valor_pago += aplicar; status='pago' quando totalmente quitado.
//  5. Commit
//
// POST /motoboy-carteira/{motoboy_id}/pagamento
func (h *MotoboyCarteiraHandler) RegistrarPagamento(w http.ResponseWriter, r *http.Request) {
	motoboyID, ok := parseMotoboyIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "motoboy_id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_ganhos") || !h.tableExists(ctx, "sz_motoboy_pagamentos") {
		httpx.Err(w, 503, "tables_missing", "tabelas de carteira motoboy ainda não migradas")
		return
	}

	var body pagamentoBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	// Saneamento: nunca negativo. Arredondamento para 2 casas espelha PHP.
	valor := roundCents(body.ValorPago)
	if valor <= 0 {
		httpx.Err(w, 400, "bad_request", "valor_pago deve ser > 0")
		return
	}

	// Data: ISO YYYY-MM-DD. Vazio = data atual no banco.
	dataPgto := body.DataPagamento
	if dataPgto != "" && !isISODate(dataPgto) {
		httpx.Err(w, 400, "bad_request", "data_pagamento deve estar no formato YYYY-MM-DD")
		return
	}

	// Observação: limite defensivo para não estourar coluna TEXT.
	obs := body.Obs
	if obs == "" {
		obs = "Pagamento registrado via painel admin (Go)."
	}
	if len(obs) > 1000 {
		obs = obs[:1000]
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Rollback é no-op após Commit bem-sucedido.
	defer tx.Rollback(ctx)

	// 1. Lock pessimista: carrega todos os ganhos disponíveis FOR UPDATE
	// e calcula o saldo disponível dentro do mesmo snapshot.
	type ganho struct {
		ID        int64
		Valor     float64
		ValorPago float64
	}
	ganhos := []ganho{}
	saldoDisp := 0.0

	rows, err := tx.Query(ctx,
		`SELECT id, COALESCE(valor,0), COALESCE(valor_pago,0)
		   FROM sz_motoboy_ganhos
		  WHERE motoboy_id = $1
		    AND status = 'disponivel'
		    AND GREATEST(COALESCE(valor,0) - COALESCE(valor_pago,0), 0) > 0
		  ORDER BY created_at ASC, id ASC
		  FOR UPDATE`, motoboyID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	for rows.Next() {
		var g ganho
		if err := rows.Scan(&g.ID, &g.Valor, &g.ValorPago); err != nil {
			rows.Close()
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		aberto := g.Valor - g.ValorPago
		if aberto < 0 {
			aberto = 0
		}
		saldoDisp += aberto
		ganhos = append(ganhos, g)
	}
	rows.Close()

	// 2. Valida: nunca aceitar pagamento maior que saldo (paridade PHP).
	saldoDisp = roundCents(saldoDisp)
	if valor > saldoDisp {
		httpx.Err(w, 409, "valor_excede_saldo",
			"valor_pago ("+strconv.FormatFloat(valor, 'f', 2, 64)+
				") excede o saldo disponível ("+strconv.FormatFloat(saldoDisp, 'f', 2, 64)+")")
		return
	}

	// 3. Insere pagamento. Status='pago' = registro fechado (não é fila de saque).
	var pagamentoID int64
	if dataPgto == "" {
		err = tx.QueryRow(ctx,
			`INSERT INTO sz_motoboy_pagamentos
			   (motoboy_id, valor_total, data_pagamento, status, obs)
			 VALUES ($1, $2, CURRENT_DATE, 'pago', $3)
			 RETURNING id`,
			motoboyID, valor, obs).Scan(&pagamentoID)
	} else {
		err = tx.QueryRow(ctx,
			`INSERT INTO sz_motoboy_pagamentos
			   (motoboy_id, valor_total, data_pagamento, status, obs)
			 VALUES ($1, $2, $3::date, 'pago', $4)
			 RETURNING id`,
			motoboyID, valor, dataPgto, obs).Scan(&pagamentoID)
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// 4. Consome ganhos FIFO. Aplica até esgotar `restante`.
	restante := valor
	ganhosAtualizados := 0
	aplicadoTotal := 0.0

	for _, g := range ganhos {
		if restante <= 0.0001 {
			break
		}
		aberto := roundCents(g.Valor - g.ValorPago)
		if aberto <= 0 {
			continue
		}
		aplicar := aberto
		if restante < aplicar {
			aplicar = restante
		}
		aplicar = roundCents(aplicar)
		novoPago := roundCents(g.ValorPago + aplicar)
		// Margem de 0,0001 para evitar problemas de float comparing == 0.
		novoStatus := "disponivel"
		if novoPago+0.0001 >= g.Valor {
			novoStatus = "pago"
		}
		_, err := tx.Exec(ctx,
			`UPDATE sz_motoboy_ganhos
			   SET valor_pago   = $1,
			       status       = $2,
			       pagamento_id = $3
			 WHERE id = $4`,
			novoPago, novoStatus, pagamentoID, g.ID)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		ganhosAtualizados++
		aplicadoTotal = roundCents(aplicadoTotal + aplicar)
		restante = roundCents(restante - aplicar)
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                 true,
		"pagamento_id":       pagamentoID,
		"ganhos_atualizados": ganhosAtualizados,
		"valor_aplicado":     aplicadoTotal,
	})
}

// ─── Histórico ───────────────────────────────────────────────────────────

// Historico devolve o extrato detalhado de um motoboy.
//
// JOIN sz_motoboy_ganhos × sz_motoboy_pedidos × sz_motoboy_pagamentos:
//   - tipo / status vêm do ganho;
//   - wc_order_id e pgto_* vêm do pedido;
//   - data_saque e status do pagamento vêm de sz_motoboy_pagamentos via pagamento_id;
//   - recebido_cliente = COALESCE(pgto_dinheiro+pgto_pix+pgto_cartao, 0).
//
// GET /motoboy-carteira/{motoboy_id}/historico?limit=150
func (h *MotoboyCarteiraHandler) Historico(w http.ResponseWriter, r *http.Request) {
	motoboyID, ok := parseMotoboyIDParam(r)
	if !ok {
		httpx.Err(w, 400, "bad_request", "motoboy_id inválido")
		return
	}
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_ganhos") {
		httpx.JSON(w, 200, map[string]any{"items": []HistoricoRow{}, "count": 0, "motoboy_id": motoboyID})
		return
	}

	limit := clampLimit(r.URL.Query().Get("limit"), 150, 500)

	// LEFT JOINs porque o pedido pode ter sido removido e o pagamento ainda
	// não existir para ganhos ainda disponíveis.
	rows, err := h.Pool.Query(ctx,
		`SELECT
			g.id,
			g.created_at::text AS data,
			COALESCE(g.pedido_id, 0),
			COALESCE(mp.wc_order_id, 0),
			COALESCE(g.tipo, ''),
			COALESCE(g.valor, 0),
			COALESCE(g.valor_pago, 0),
			GREATEST(COALESCE(g.valor,0) - COALESCE(g.valor_pago,0), 0) AS saldo_aberto,
			pg.data_pagamento::text AS data_saque,
			COALESCE(g.status, ''),
			COALESCE(mp.pgto_dinheiro, 0)
				+ COALESCE(mp.pgto_pix, 0)
				+ COALESCE(mp.pgto_cartao, 0) AS recebido_cliente
		 FROM sz_motoboy_ganhos g
		 LEFT JOIN sz_motoboy_pedidos    mp ON mp.id = g.pedido_id
		 LEFT JOIN sz_motoboy_pagamentos pg ON pg.id = g.pagamento_id
		 WHERE g.motoboy_id = $1
		 ORDER BY g.created_at DESC, g.id DESC
		 LIMIT $2`, motoboyID, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []HistoricoRow{}
	for rows.Next() {
		var hist HistoricoRow
		if err := rows.Scan(
			&hist.ID, &hist.Data, &hist.PedidoID, &hist.WcOrderID,
			&hist.Tipo, &hist.Valor, &hist.PagoNesteGanho, &hist.SaldoAberto,
			&hist.DataSaque, &hist.Status, &hist.RecebidoCliente,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		out = append(out, hist)
	}
	httpx.JSON(w, 200, map[string]any{
		"items":      out,
		"count":      len(out),
		"motoboy_id": motoboyID,
	})
}

// ─── Sync ────────────────────────────────────────────────────────────────

// Sync reconstrói os lançamentos de ganho (sz_motoboy_ganhos) e os fechamentos
// diários (sz_motoboy_fechamento) a partir de sz_motoboy_pedidos.
//
// Espelha as fases (b) e (c) de sz_mbw_sync_all_data() em motoboy-wallet.php:711.
// A fase (a) (backfill a partir de WP postmeta) é omitida — território WP.
//
// Lógica por fase:
//
//	(b) Ganhos:
//	  1. DELETE lançamentos pendentes obsoletos (pedido não existe, status errado,
//	     motoboy divergente).
//	  2. Para cada pedido entregue/frustrado: UPSERT sz_motoboy_ganhos.
//	     - valor entrega = sz_mbw_taxa_entrega_mb_{id} ou sz_mbw_taxa_entrega (18).
//	     - valor frustrado = valor_taxa_frustrado do pedido.
//	     - Se lançamento existe E está pago → preserva (não sobrescreve).
//	  3. Reconciliar status: 'pendente' → 'disponivel' para pedidos finalizados.
//
//	(c) Fechamento diário:
//	  UPSERT sz_motoboy_fechamento via GROUP BY por motoboy + cd + data_fechamento.
//	  Utiliza as mesmas colunas e lógica do PHP (total_dinheiro/pix/cartao/a_repassar).
//	  Exclui fechamentos sem correspondência atual (pedidos reatribuídos / cancelados).
//
// POST /motoboy-carteira/sync
func (h *MotoboyCarteiraHandler) Sync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Tabelas raiz obrigatórias. Ganhos pode não existir (só em MySQL) → 503.
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.Err(w, 503, "tables_missing", "tabela sz_motoboy_pedidos não migrada para Postgres")
		return
	}
	if !h.tableExists(ctx, "sz_motoboy_ganhos") {
		httpx.Err(w, 503, "tables_missing",
			"tabela sz_motoboy_ganhos ausente no Postgres (replicação ainda não ativa); "+
				"execute o sync pelo painel WP enquanto a migração não for concluída")
		return
	}

	// ── Fase (b): Ganhos ──────────────────────────────────────────────────

	// Lê taxa global de entrega (fallback 18.00, espelha sz_mbw_get_taxa_entrega).
	taxaGlobal := h.optionFloat(ctx, "sz_mbw_taxa_entrega", 18.00)

	// 1. Remove lançamentos pendentes obsoletos — paridade com o DELETE do PHP.
	//    Condições: pedido não existe, status saiu de entregue/frustrado,
	//    tipo não bate com status, ou motoboy diverge do responsável atual.
	delRes, err := h.Pool.Exec(ctx,
		`DELETE FROM sz_motoboy_ganhos g
		 USING sz_motoboy_pedidos ped
		 WHERE g.pedido_id = ped.id
		   AND g.status = 'pendente'
		   AND (
		         ped.status NOT IN ('entregue','frustrado')
		      OR (g.tipo = 'entrega'   AND ped.status <> 'entregue')
		      OR (g.tipo = 'frustrado' AND ped.status <> 'frustrado')
		      OR g.motoboy_id <> COALESCE(NULLIF(ped.baixa_motoboy_id,0), ped.motoboy_id)
		       )`)
	delOrfaos := int64(0)
	if err == nil {
		delOrfaos = delRes.RowsAffected()
	}

	// Também remove ganhos pendentes cujo pedido_id não existe mais.
	delRes2, err2 := h.Pool.Exec(ctx,
		`DELETE FROM sz_motoboy_ganhos g
		 WHERE g.status = 'pendente'
		   AND NOT EXISTS (
		       SELECT 1 FROM sz_motoboy_pedidos ped WHERE ped.id = g.pedido_id
		       )`)
	if err2 == nil {
		delOrfaos += delRes2.RowsAffected()
	}

	// 2. Lê todos os pedidos entregue/frustrado para UPSERT de ganhos.
	type pedidoRow struct {
		ID            int64
		MotoboyID     int64 // COALESCE(baixa_motoboy_id, motoboy_id)
		Status        string
		ValorTaxaFru  float64
		CreatedAt     string
		BaixaAt       *string
		TsEntregue    *string
		TsFrustrado   *string
	}

	pedRows, err := h.Pool.Query(ctx,
		`SELECT
			id,
			COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) AS mb_id,
			status,
			COALESCE(valor_taxa_frustrado, 0),
			created_at::text,
			baixa_at::text,
			ts_entregue::text,
			ts_frustrado::text
		 FROM sz_motoboy_pedidos
		 WHERE status IN ('entregue','frustrado')
		   AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) IS NOT NULL
		   AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) > 0`)
	if err != nil {
		httpx.Err(w, 500, "db_error", "pedidos query: "+err.Error())
		return
	}
	defer pedRows.Close()

	var pedidos []pedidoRow
	for pedRows.Next() {
		var p pedidoRow
		if err := pedRows.Scan(&p.ID, &p.MotoboyID, &p.Status, &p.ValorTaxaFru,
			&p.CreatedAt, &p.BaixaAt, &p.TsEntregue, &p.TsFrustrado); err != nil {
			pedRows.Close()
			httpx.Err(w, 500, "db_error", "pedidos scan: "+err.Error())
			return
		}
		pedidos = append(pedidos, p)
	}
	pedRows.Close()

	// Cache de taxa por motoboy (evita N queries ao senderzz_options).
	taxaCache := map[int64]float64{}
	getTaxa := func(mbID int64) float64 {
		if v, ok := taxaCache[mbID]; ok {
			return v
		}
		v := h.optionFloat(ctx, "sz_mbw_taxa_entrega_mb_"+strconv.FormatInt(mbID, 10), 0)
		if v <= 0 {
			v = taxaGlobal
		}
		taxaCache[mbID] = v
		return v
	}

	ganhosUpserted := 0
	for _, p := range pedidos {
		tipo := "entrega"
		if p.Status == "frustrado" {
			tipo = "frustrado"
		}

		// Valor: taxa de entrega (por motoboy ou global) para entregues;
		// valor_taxa_frustrado para frustrados (paridade PHP — não recalcula histórico).
		valor := 0.0
		if tipo == "entrega" {
			valor = getTaxa(p.MotoboyID)
		} else {
			valor = roundCents(p.ValorTaxaFru)
		}

		// created_at do ganho = baixa_at > ts_entregue/ts_frustrado > created_at.
		createdAt := strings.TrimSpace(safeStr(p.BaixaAt))
		if createdAt == "" || createdAt == "0000-00-00 00:00:00" {
			if tipo == "entrega" {
				createdAt = strings.TrimSpace(safeStr(p.TsEntregue))
			} else {
				createdAt = strings.TrimSpace(safeStr(p.TsFrustrado))
			}
		}
		if createdAt == "" || createdAt == "0000-00-00 00:00:00" {
			createdAt = p.CreatedAt
		}

		// Verifica se já existe um ganho para esse pedido/tipo.
		var existsID int64
		var existsStatus string
		_ = h.Pool.QueryRow(ctx,
			`SELECT id, COALESCE(status,'') FROM sz_motoboy_ganhos
			 WHERE pedido_id=$1 AND tipo=$2 LIMIT 1`,
			p.ID, tipo).Scan(&existsID, &existsStatus)

		if existsID > 0 {
			// Preserva lançamentos pagos — não sobrescreve valor já quitado.
			if existsStatus == "pago" {
				continue
			}
			_, err := h.Pool.Exec(ctx,
				`UPDATE sz_motoboy_ganhos SET
				   motoboy_id  = $1,
				   wc_order_id = (SELECT wc_order_id FROM sz_motoboy_pedidos WHERE id=$2 LIMIT 1),
				   valor       = $3,
				   created_at  = $4
				 WHERE id = $5`,
				p.MotoboyID, p.ID, roundCents(valor), createdAt, existsID)
			if err == nil {
				ganhosUpserted++
			}
		} else {
			_, err := h.Pool.Exec(ctx,
				`INSERT INTO sz_motoboy_ganhos
				   (motoboy_id, pedido_id, wc_order_id, tipo, valor, status, created_at)
				 VALUES
				   ($1, $2, (SELECT wc_order_id FROM sz_motoboy_pedidos WHERE id=$2 LIMIT 1), $3, $4, 'pendente', $5)
				 ON CONFLICT DO NOTHING`,
				p.MotoboyID, p.ID, tipo, roundCents(valor), createdAt)
			if err == nil {
				ganhosUpserted++
			}
		}
	}

	// 3. Promove lançamentos 'pendente' de pedidos já finalizados → 'disponivel'.
	//    (Paridade com sz_mbw_backfill_status_from_pedidos: pedidos entregue/frustrado
	//     já foram conciliados → o ganho deve ser disponivel para pagamento.)
	concilRes, _ := h.Pool.Exec(ctx,
		`UPDATE sz_motoboy_ganhos g SET status = 'disponivel'
		 FROM sz_motoboy_pedidos ped
		 WHERE g.pedido_id = ped.id
		   AND g.status = 'pendente'
		   AND ped.status IN ('entregue','frustrado')`)
	conciliados := int64(0)
	if concilRes != nil {
		conciliados = concilRes.RowsAffected()
	}

	// ── Fase (c): Fechamento diário ──────────────────────────────────────────

	fechamentos := 0
	if h.tableExists(ctx, "sz_motoboy_fechamento") {
		// Remove fechamentos fantasmas (pedidos reatribuídos / cancelados).
		_, _ = h.Pool.Exec(ctx,
			`DELETE FROM sz_motoboy_fechamento f
			 WHERE NOT EXISTS (
			     SELECT 1 FROM sz_motoboy_pedidos ped
			     WHERE ped.status IN ('entregue','frustrado')
			       AND COALESCE(NULLIF(ped.baixa_motoboy_id,0), ped.motoboy_id) = f.motoboy_id
			       AND DATE(COALESCE(ped.baixa_at, ped.ts_entregue, ped.ts_frustrado,
			                         ped.updated_at, ped.created_at)) = f.data_fechamento
			 )`)

		// Agrega fechamentos por motoboy + cd + data.
		type fechRow struct {
			MotoboyID       int64
			CDID            int64
			DataFechamento  string
			TotalPedidos    int64
			TotalEntregues  int64
			TotalFrustrados int64
			TotalDinheiro   float64
			TotalPix        float64
			TotalCartao     float64
			TotalARepassar  float64
		}
		fechRows, err := h.Pool.Query(ctx,
			`SELECT
				COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id)                             AS motoboy_id,
				COALESCE(NULLIF(cd_id,0), 1)                                                  AS cd_id,
				DATE(COALESCE(baixa_at, ts_entregue, ts_frustrado, updated_at, created_at))  AS data_fechamento,
				COUNT(*)                                                                       AS total_pedidos,
				COUNT(*) FILTER (WHERE status='entregue')                                     AS total_entregues,
				COUNT(*) FILTER (WHERE status='frustrado')                                    AS total_frustrados,
				COALESCE(SUM(pgto_dinheiro) FILTER (WHERE status='entregue'), 0)             AS total_dinheiro,
				COALESCE(SUM(pgto_pix)      FILTER (WHERE status='entregue'), 0)             AS total_pix,
				COALESCE(SUM(pgto_cartao)   FILTER (WHERE status='entregue'), 0)             AS total_cartao,
				COALESCE(SUM(pgto_dinheiro+pgto_pix+pgto_cartao) FILTER (WHERE status='entregue'), 0) AS total_a_repassar
			 FROM sz_motoboy_pedidos
			 WHERE status IN ('entregue','frustrado')
			   AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) IS NOT NULL
			   AND COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id) > 0
			 GROUP BY COALESCE(NULLIF(baixa_motoboy_id,0), motoboy_id),
			          COALESCE(NULLIF(cd_id,0), 1),
			          DATE(COALESCE(baixa_at, ts_entregue, ts_frustrado, updated_at, created_at))
			 HAVING DATE(COALESCE(baixa_at, ts_entregue, ts_frustrado, updated_at, created_at)) IS NOT NULL`)
		if err == nil {
			defer fechRows.Close()
			for fechRows.Next() {
				var f fechRow
				if err := fechRows.Scan(
					&f.MotoboyID, &f.CDID, &f.DataFechamento,
					&f.TotalPedidos, &f.TotalEntregues, &f.TotalFrustrados,
					&f.TotalDinheiro, &f.TotalPix, &f.TotalCartao, &f.TotalARepassar,
				); err != nil {
					continue
				}
				_, err := h.Pool.Exec(ctx,
					`INSERT INTO sz_motoboy_fechamento (
					   motoboy_id, cd_id, data_fechamento,
					   total_pedidos, total_entregues, total_frustrados,
					   total_dinheiro, total_pix, total_cartao, total_a_repassar,
					   repasse_confirmado, alan_confirmou, created_at, updated_at
					 ) VALUES (
					   $1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10,
					   false, false, NOW(), NOW()
					 )
					 ON CONFLICT (motoboy_id, data_fechamento) DO UPDATE SET
					   total_pedidos    = EXCLUDED.total_pedidos,
					   total_entregues  = EXCLUDED.total_entregues,
					   total_frustrados = EXCLUDED.total_frustrados,
					   total_dinheiro   = EXCLUDED.total_dinheiro,
					   total_pix        = EXCLUDED.total_pix,
					   total_cartao     = EXCLUDED.total_cartao,
					   total_a_repassar = EXCLUDED.total_a_repassar,
					   cd_id            = COALESCE(EXCLUDED.cd_id, sz_motoboy_fechamento.cd_id),
					   updated_at       = NOW()`,
					f.MotoboyID, f.CDID, f.DataFechamento,
					f.TotalPedidos, f.TotalEntregues, f.TotalFrustrados,
					f.TotalDinheiro, f.TotalPix, f.TotalCartao, f.TotalARepassar)
				if err == nil {
					fechamentos++
				}
			}
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                  true,
		"ganhos_removidos":    delOrfaos,
		"ganhos_upserted":     ganhosUpserted,
		"ganhos_conciliados":  conciliados,
		"fechamentos":         fechamentos,
	})
}

// optionFloat lê uma opção float de senderzz_options com fallback def.
// Espelha getOptionFloat de cod_taxas.go mas sem dependência cruzada de handler.
func (h *MotoboyCarteiraHandler) optionFloat(ctx context.Context, key string, def float64) float64 {
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	f, err2 := strconv.ParseFloat(s, 64)
	if err2 != nil || f < 0 {
		return def
	}
	if f == 0 {
		return def
	}
	return f
}

// safeStr retorna o conteúdo de um *string ou "" se nil.
func safeStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ─── Util numérico ───────────────────────────────────────────────────────

// roundCents arredonda para 2 casas decimais (padrão moeda BR). Evita ruído
// de float64 nas comparações de saldo (paridade com round(...,2) do PHP).
func roundCents(v float64) float64 {
	// Math.Round(half-away-from-zero) com escala 100.
	if v >= 0 {
		return float64(int64(v*100+0.5)) / 100
	}
	return float64(int64(v*100-0.5)) / 100
}

// isISODate valida YYYY-MM-DD sem alocar regex. Tolerante a anos < 1000.
func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
