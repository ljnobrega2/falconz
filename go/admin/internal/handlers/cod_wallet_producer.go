// Package handlers — endpoint admin para "Carteira COD do produtor".
//
// Espelha a tela administrativa montada em includes/senderzz-cod-wallet.php
// (PHP legado). Permite ao admin enxergar, por produtor, os três saldos COD
// (pending / available / paid 30d), a conta PIX padrão e a última movimentação,
// além de disparar liberação manual de transações pending vencidas.
//
// Tabelas envolvidas:
//   - sz_cod_wallet_transactions ........... livro razão por produtor
//   - sz_cod_withdraw_accounts ............. contas PIX cadastradas (até 3 por user)
//   - senderzz_portal_users ................ nome/email (JOIN por wp_user_id)
//
// Comentários em PT-BR para acompanhar o padrão do restante do módulo.
package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// CodWalletProducerHandler agrupa endpoints da tela "Carteira COD por produtor".
type CodWalletProducerHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// CodWalletProducerSummary — KPIs globais agregados em todos os produtores.
// O recorte "paid_30d" usa updated_at como proxy de quando o saque foi pago
// (a tabela não tem paid_at/completed_at — a transição PHP/Go faz UPDATE …
// SET status='paid', updated_at=NOW()).
type CodWalletProducerSummary struct {
	TotalPending   float64 `json:"total_pending"`
	TotalAvailable float64 `json:"total_available"`
	TotalPaid30d   float64 `json:"total_paid_30d"`
	ProducersCount int64   `json:"producers_count"`
}

// CodWalletProducerPix — sub-objeto exibido na linha da tabela para indicar
// a conta PIX padrão do produtor. Quando o produtor não tem conta cadastrada
// devolve nil (campo `pix_default: null` no JSON).
type CodWalletProducerPix struct {
	Holder  string `json:"holder"`
	Key     string `json:"key"`
	PixType string `json:"type"`
}

// CodWalletProducerRow — uma linha da listagem principal.
// `UltimaMovimentacao` é nullable (produtor pode nunca ter recebido COD).
//
// IMPORTANTE (id-space): `UserID` vem de sz_cod_wallet_transactions.user_id, que está no
// espaço wp_user_id (ex.: Gabriel Campos = 21). Já `PortalID` é senderzz_portal_users.id
// (ex.: Gabriel = 15) — é a chave canônica usada para casar com cod-livro/producers-summary
// (cujo producer_id = sz_orders.produtor_id = portal id). A tela unificada faz o merge por
// PortalID; sem ele, o mesmo produtor apareceria em duas linhas (dados duplicados).
type CodWalletProducerRow struct {
	UserID             int64                 `json:"user_id"`
	PortalID           int64                 `json:"portal_id"`
	Nome               string                `json:"nome"`
	Email              string                `json:"email"`
	SaldoPending       float64               `json:"saldo_pending"`
	SaldoAvailable     float64               `json:"saldo_available"`
	SaldoPaid30d       float64               `json:"saldo_paid_30d"`
	PixDefault         *CodWalletProducerPix `json:"pix_default"`
	UltimaMovimentacao *string               `json:"ultima_movimentacao"`
}

// CodWalletProducerAccount — linha de sz_cod_withdraw_accounts no drawer
// "contas PIX do produtor".
type CodWalletProducerAccount struct {
	ID         int64  `json:"id"`
	HolderName string `json:"holder_name"`
	HolderCPF  string `json:"holder_cpf"`
	PixType    string `json:"pix_type"`
	PixKey     string `json:"pix_key"`
	IsDefault  bool   `json:"is_default"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists — utilitário compartilhado com audit/affiliate_wallet; verifica
// se a tabela existe no schema public antes de rodar a query. Graceful
// degradation: se tabelas ainda não foram migradas do MySQL, devolve
// resposta vazia em vez de 500.
func (h *CodWalletProducerHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// ─── GET /cod-wallet-producer/summary ────────────────────────────────────

// Summary devolve os KPIs globais (pending / available / paid 30d) +
// número de produtores distintos que aparecem como user_id em
// sz_cod_wallet_transactions.
func (h *CodWalletProducerHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := CodWalletProducerSummary{}

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		// Tabela ainda não migrada — devolve zeros (sem erro).
		httpx.JSON(w, 200, out)
		return
	}

	// SUM com COALESCE para que tabela vazia devolva 0 em vez de NULL.
	// O recorte 30d usa updated_at (a tabela não tem paid_at — UPDATE seta
	// updated_at=NOW() quando o admin marca como paid).
	_ = h.Pool.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status='pending'   THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS total_pending,
			COALESCE(SUM(CASE WHEN status='available' THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS total_available,
			COALESCE(SUM(CASE
				WHEN status='paid' AND updated_at >= NOW() - INTERVAL '30 days'
				THEN net ELSE 0
			END), 0) AS total_paid_30d,
			COUNT(DISTINCT user_id) AS producers_count
		 FROM sz_cod_wallet_transactions`).
		Scan(&out.TotalPending, &out.TotalAvailable, &out.TotalPaid30d, &out.ProducersCount)

	httpx.JSON(w, 200, out)
}

// ─── GET /cod-wallet-producer?q=&limit=300 ───────────────────────────────

// List devolve uma linha por produtor que aparece em sz_cod_wallet_transactions.
// Faz LEFT JOIN com senderzz_portal_users (por wp_user_id) para resolver
// nome/email e com sz_cod_withdraw_accounts para anexar a conta PIX padrão.
//
// Filtro `q` casa email OU nome via ILIKE (substring case-insensitive).
func (h *CodWalletProducerHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	search := q.Get("q")

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.JSON(w, 200, map[string]any{"items": []CodWalletProducerRow{}})
		return
	}

	// As duas tabelas auxiliares são opcionais — se ainda não foram migradas
	// devolvemos a linha sem PIX/nome em vez de quebrar a query inteira.
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasAccts := h.tableExists(ctx, "sz_cod_withdraw_accounts")

	// Monta blocos opcionais de JOIN / SELECT em PT-BR.
	usersJoin := ""
	nomeExpr := "'' AS nome"
	emailExpr := "'' AS email"
	// portalIDExpr: id canônico do portal (senderzz_portal_users.id). Quando a tabela
	// de usuários não existe, cai no próprio user_id (já que não há como resolver).
	portalIDExpr := "agg.user_id AS portal_id"
	if hasUsers {
		usersJoin = `LEFT JOIN senderzz_portal_users p ON (p.wp_user_id = agg.user_id OR (COALESCE(p.wp_user_id,0) <= 0 AND -p.id = agg.user_id))`
		nomeExpr = `COALESCE(p.nome,'') AS nome`
		emailExpr = `COALESCE(p.email,'') AS email`
		portalIDExpr = `COALESCE(p.id, agg.user_id) AS portal_id`
	}

	// pix_default: pega a conta marcada is_default=1; se nenhuma estiver
	// marcada, o ORDER BY garante o id menor (a primeira cadastrada).
	pixHolder := "NULL::text"
	pixKey := "NULL::text"
	pixType := "NULL::text"
	if hasAccts {
		// FIX 42703: a tabela usa a coluna booleana `active` — não existe `status`.
		pixHolder = `(SELECT holder_name FROM sz_cod_withdraw_accounts
		              WHERE user_id = agg.user_id AND active = TRUE
		              ORDER BY is_default DESC, id ASC LIMIT 1)`
		pixKey = `(SELECT pix_key FROM sz_cod_withdraw_accounts
		           WHERE user_id = agg.user_id AND active = TRUE
		           ORDER BY is_default DESC, id ASC LIMIT 1)`
		pixType = `(SELECT pix_type FROM sz_cod_withdraw_accounts
		            WHERE user_id = agg.user_id AND active = TRUE
		            ORDER BY is_default DESC, id ASC LIMIT 1)`
	}

	// O filtro `q` precisa de senderzz_portal_users; se a tabela não existe,
	// devolve sem aplicar o WHERE (mas a UI ainda mostra a busca client-side).
	whereSearch := "TRUE"
	if hasUsers {
		whereSearch = `($1 = ''
			OR p.email ILIKE '%' || $1 || '%'
			OR p.nome  ILIKE '%' || $1 || '%')`
	}

	sql := `
		WITH agg AS (
			SELECT
				user_id,
				COALESCE(SUM(CASE WHEN status='pending'   THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS saldo_pending,
				COALESCE(SUM(CASE WHEN status='available' THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS saldo_available,
				COALESCE(SUM(CASE
					WHEN status='paid' AND updated_at >= NOW() - INTERVAL '30 days'
					THEN net ELSE 0
				END), 0) AS saldo_paid_30d,
				MAX(created_at) AS ultima_movimentacao
			FROM sz_cod_wallet_transactions
			GROUP BY user_id
		)
		SELECT
			agg.user_id,
			` + portalIDExpr + `,
			` + nomeExpr + `,
			` + emailExpr + `,
			agg.saldo_pending,
			agg.saldo_available,
			agg.saldo_paid_30d,
			` + pixHolder + ` AS pix_holder,
			` + pixKey + `    AS pix_key,
			` + pixType + `   AS pix_type,
			agg.ultima_movimentacao::text AS ultima_movimentacao
		FROM agg
		` + usersJoin + `
		WHERE ` + whereSearch + `
		ORDER BY (agg.saldo_pending + agg.saldo_available) DESC, agg.user_id DESC
		LIMIT $2`

	rows, err := h.Pool.Query(ctx, sql, search, limit)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []CodWalletProducerRow{}
	for rows.Next() {
		var x CodWalletProducerRow
		var pixHolderV, pixKeyV, pixTypeV *string
		if err := rows.Scan(
			&x.UserID, &x.PortalID, &x.Nome, &x.Email,
			&x.SaldoPending, &x.SaldoAvailable, &x.SaldoPaid30d,
			&pixHolderV, &pixKeyV, &pixTypeV,
			&x.UltimaMovimentacao,
		); err != nil {
			continue
		}
		// Só anexa pix_default se pelo menos a key foi resolvida.
		if pixKeyV != nil && *pixKeyV != "" {
			x.PixDefault = &CodWalletProducerPix{
				Holder:  strDeref(pixHolderV),
				Key:     *pixKeyV,
				PixType: strDeref(pixTypeV),
			}
		}
		out = append(out, x)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// strDeref devolve "" para *string nil (evita imprimir <nil> no JSON).
func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ─── GET /cod-wallet-producer/{user_id}/accounts ─────────────────────────

// Accounts lista as contas PIX cadastradas para um produtor. Usado no drawer
// "contas PIX". Filtra active = TRUE — contas inativas não aparecem (não
// há UI para reativar do lado admin nessa tela).
func (h *CodWalletProducerHandler) Accounts(w http.ResponseWriter, r *http.Request) {
	uidStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || userID == 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_cod_withdraw_accounts") {
		httpx.JSON(w, 200, map[string]any{"items": []CodWalletProducerAccount{}})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id,
		        COALESCE(holder_name,''),
		        COALESCE(holder_cpf,''),
		        COALESCE(pix_type,''),
		        COALESCE(pix_key,''),
		        COALESCE(is_default, false) IS TRUE
		 FROM sz_cod_withdraw_accounts
		 WHERE user_id = $1 AND active = TRUE
		 ORDER BY is_default DESC, id ASC`, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []CodWalletProducerAccount{}
	for rows.Next() {
		var a CodWalletProducerAccount
		if err := rows.Scan(&a.ID, &a.HolderName, &a.HolderCPF,
			&a.PixType, &a.PixKey, &a.IsDefault); err != nil {
			continue
		}
		out = append(out, a)
	}
	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ─── POST /cod-wallet-producer/{user_id}/release-pending ─────────────────

// ReleasePending promove transações status='pending' para 'available' quando
// release_at já venceu. Espelha sz_cod_release_due_transactions() do PHP,
// porém limitado a um único produtor (a UI tem ação batch por linha).
// Sempre seta updated_at=NOW() — paridade com o cron PHP, evita órfãos no
// índice updated_at usado por queries do tipo "últimos 30 dias".
func (h *CodWalletProducerHandler) ReleasePending(w http.ResponseWriter, r *http.Request) {
	uidStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || userID == 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	ctx := r.Context()

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.Err(w, 503, "table_missing",
			"tabela sz_cod_wallet_transactions não migrada")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE sz_cod_wallet_transactions
		 SET status='available',
		     updated_at=NOW()
		 WHERE user_id=$1
		   AND status='pending'
		   AND release_at IS NOT NULL
		   AND release_at <= NOW()`, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":             true,
		"user_id":        userID,
		"released_count": tag.RowsAffected(),
	})
}

// ─── POST /cod-wallet-producer/{user_id}/anticipate ──────────────────────

// anticipateReq é o corpo da antecipação administrativa: o admin DIGITA um
// valor exato a antecipar (pendente → disponível), SEM taxa.
type anticipateReq struct {
	Amount float64 `json:"amount"`
}

// reais converte centavos (int64) de volta para reais (float64) sem drift —
// usado nas respostas pro front.
func reais(cents int64) float64 { return float64(cents) / 100.0 }

// codBalancesCents agrega o pendente e o disponível atuais de um produtor em
// CENTAVOS (int64), usando EXATAMENTE as fórmulas de Summary/List
// (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0),gross) END).
// Operar em centavos elimina o drift de ponto-flutuante na validação
// (amount <= pendente) — antecipar o pendente cheio nunca é falso-rejeitado.
func (h *CodWalletProducerHandler) codBalancesCents(
	ctx context.Context, q pgx.Tx, userID int64,
) (pendingCents, availableCents int64, err error) {
	var pending, available float64
	err = q.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status='pending'   THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS pendente,
			COALESCE(SUM(CASE WHEN status='available' THEN (CASE WHEN type='withdrawal' THEN gross ELSE COALESCE(NULLIF(net,0), gross) END) ELSE 0 END), 0) AS disponivel
		 FROM sz_cod_wallet_transactions
		 WHERE user_id=$1`, userID).Scan(&pending, &available)
	if err != nil {
		return 0, 0, err
	}
	return int64(math.Round(pending * 100)), int64(math.Round(available * 100)), nil
}

// AnticipateAmount é a "antecipação administrativa SEM taxa": o admin DIGITA um
// VALOR e antecipa exatamente esse valor (pendente → disponível). Diferente de
// AnticipateAll (antigo, que liberava TODO o pendente), aqui o valor é exato.
//
// MECANISMO (cron-safe): PROMOVE OS RECEBÍVEIS REAIS pendentes
// (type='cod_received', status='pending') para 'available' — NÃO cria um offset
// sintético. O bug do mecanismo antigo (par de adjustments +available / -pending
// com release_at=NULL) era que o cron, ao amadurecer os recebíveis reais que
// continuavam pendentes, DOBRAVA o valor antecipado no disponível. Promovendo as
// próprias linhas, depois da maturação não sobra nada para o cron re-liberar.
//
// Algoritmo (uma transação pgx):
//  1. SELECT ... FOR UPDATE das linhas pending (cod_received), ordenadas por
//     release_at NULLS FIRST, id — trava as linhas contra o cron de release.
//  2. Recalcula o pendente DENTRO da tx (centavos). Valida 0 < amount <= pendente;
//     senão rollback + 422 (nunca confia no front).
//  3. Percorre em ordem, remaining := amount (centavos):
//     - remaining >= val → PROMOVE a linha inteira (status='available').
//     - 0 < remaining < val → SPLIT na fronteira: reversa o pai (sai das somas,
//     preserva gross+order_id p/ auditoria) e cria dois filhos: um DISPONÍVEL
//     (=remaining, release_at=NOW()) e um PENDENTE restante (=val-remaining,
//     herda o release_at do pai). break.
//  4. Recalcula disponível/pendente DENTRO da tx (vê as escritas não-commitadas)
//     e commita. Resposta {ok, anticipated, novo_disponivel, novo_pendente}.
//
// Toda a aritmética é em CENTAVOS (int64) — sem drift de float; fronteira de igualdade
// cai no caminho "promove inteiro", então nunca cria filho de valor zero.
func (h *CodWalletProducerHandler) AnticipateAmount(w http.ResponseWriter, r *http.Request) {
	uidStr := chi.URLParam(r, "user_id")
	userID, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || userID == 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}
	ctx := r.Context()

	var req anticipateReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	if !h.tableExists(ctx, "sz_cod_wallet_transactions") {
		httpx.Err(w, 503, "table_missing",
			"tabela sz_cod_wallet_transactions não migrada")
		return
	}

	amountCents := int64(math.Round(req.Amount * 100))
	if amountCents <= 0 {
		httpx.Err(w, 422, "valor_invalido", "valor deve ser maior que zero")
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx) // no-op após Commit; garante rollback em qualquer erro.

	// (1) Trava os recebíveis pendentes reais. Ordem release_at NULLS FIRST, id —
	// determinística e estável (mesma ordem do walk e do teste discriminante).
	rows, err := tx.Query(ctx,
		`SELECT id, order_id, gross, net, release_at
		   FROM sz_cod_wallet_transactions
		  WHERE user_id=$1 AND status='pending' AND type='cod_received'
		  ORDER BY release_at NULLS FIRST, id
		  FOR UPDATE`, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Materializa as linhas (não dá pra fazer UPDATE/INSERT na mesma conexão
	// enquanto o cursor da Query está aberto).
	type pendRow struct {
		id        int64
		orderID   *int64
		gross     float64
		net       float64
		releaseAt *time.Time
	}
	var pend []pendRow
	var pendingCents int64
	for rows.Next() {
		var p pendRow
		if err := rows.Scan(&p.id, &p.orderID, &p.gross, &p.net, &p.releaseAt); err != nil {
			rows.Close()
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		pend = append(pend, p)
		// Valor efetivo do recebível = COALESCE(NULLIF(net,0), gross) = gross.
		pendingCents += valCents(p.net, p.gross)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// (2) Valida contra o pendente recalculado (centavos). Nunca confia no front.
	if amountCents > pendingCents {
		httpx.Err(w, 422, "valor_maior_que_pendente", "valor maior que o pendente")
		return
	}

	// (3) Percorre em ordem, consumindo o amount.
	remaining := amountCents
	for _, p := range pend {
		if remaining <= 0 {
			break
		}
		valC := valCents(p.net, p.gross)
		if remaining >= valC {
			// PROMOVE a linha inteira: pending → available, sem taxa.
			if _, err := tx.Exec(ctx,
				`UPDATE sz_cod_wallet_transactions
				    SET status='available', updated_at=NOW(),
				        description=COALESCE(NULLIF(description,''),'') || ' [antecipado admin sem taxa]'
				  WHERE id=$1`, p.id); err != nil {
				httpx.Err(w, 500, "db_error", err.Error())
				return
			}
			remaining -= valC
			continue
		}

		// 0 < remaining < valC → SPLIT na fronteira.
		// (a) Reversa o pai: sai de TODAS as somas (status='reversed'), preserva
		// gross+order_id para auditoria.
		if _, err := tx.Exec(ctx,
			`UPDATE sz_cod_wallet_transactions
			    SET status='reversed', updated_at=NOW(),
			        description=COALESCE(NULLIF(description,''),'') || ' [substituído por split de antecipação]'
			  WHERE id=$1`, p.id); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		availChild := reais(remaining)
		pendChild := reais(valC - remaining)
		// (b) filho DISPONÍVEL: release_at=NOW() (já liberado).
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_cod_wallet_transactions
			   (user_id, order_id, type, status, amount, gross, net, fee, release_at, created_at, updated_at, description)
			 VALUES ($1,$2,'cod_received','available',$3,$3,0,0, NOW(), NOW(), NOW(), $4)`,
			userID, p.orderID, availChild,
			fmt.Sprintf("Antecipação admin (sem taxa) — parcial do recebível #%d", p.id),
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		// (c) filho PENDENTE restante: HERDA o release_at do pai (cron-safe).
		if _, err := tx.Exec(ctx,
			`INSERT INTO sz_cod_wallet_transactions
			   (user_id, order_id, type, status, amount, gross, net, fee, release_at, created_at, updated_at, description)
			 VALUES ($1,$2,'cod_received','pending',$3,$3,0,0, $4, NOW(), NOW(), $5)`,
			userID, p.orderID, pendChild, p.releaseAt,
			fmt.Sprintf("Restante do recebível #%d após antecipação", p.id),
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		remaining = 0
		break
	}

	// (4) Recalcula os saldos DENTRO da tx (enxerga as escritas não-commitadas) —
	// auto-valida o split antes de commitar.
	novoPendenteCents, novoDisponivelCents, err := h.codBalancesCents(ctx, tx, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":              true,
		"user_id":         userID,
		"anticipated":     reais(amountCents),
		"novo_disponivel": reais(novoDisponivelCents),
		"novo_pendente":   reais(novoPendenteCents),
	})
}

// valCents devolve o valor efetivo de um recebível em centavos (int64), usando
// a MESMA fórmula dos handlers de soma: COALESCE(NULLIF(net,0), gross). Para
// recebíveis COD net é sempre 0, então cai em gross.
func valCents(net, gross float64) int64 {
	v := gross
	if net != 0 {
		v = net
	}
	return int64(math.Round(v * 100))
}
