// Package handlers — endpoint admin TpcClientes.
// Espelha a aba "Clientes" do PHP legado (includes/tpc/admin.php:254) sobre
// Postgres. Lista carteiras com nome/email do usuário (via JOIN portal_users),
// permite emitir/cancelar recargas PIX e oferece reset total das tabelas TPC.
//
// Convenções:
//   - Joins (MIGRAÇÃO 2026-07-28): tpc_carteira.user_id, tpc_transacoes.user_id e
//     tpc_recargas.user_id guardam o id NATIVO do portal (senderzz_portal_users.id)
//     — migrado de wp_user_id porque produtor 100% FALK (sem WordPress) nunca tem
//     wp_user_id e ficava com carteira/PIX permanentemente inacessível. JOIN em
//     senderzz_portal_users.id (NUNCA em u.wp_user_id).
//   - Graceful degradation: se senderzz_portal_users não existir, retorna apenas
//     o user_id (sem nome/email) e o filtro `q` passa a casar contra user_id::text.
//   - Recarga real é emitida pelo wallet-service. Aqui só insere a linha pendente
//     em tpc_recargas + gera placeholders (qr_src/copia_cola/security_token) para
//     a UI exibir imediatamente — a confirmação chega pelo webhook PIX.
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
	"github.com/senderzz/admin-service/internal/melhorenvio"
)

type TpcClientesHandler struct{ Pool *pgxpool.Pool }

// meClient — instância única do cliente ME (lê ME_TOKEN/ME_API_URL do ambiente
// uma vez; HasToken() é checado a cada emissão, então funciona mesmo se a env
// var só for setada depois do processo subir via reload).
var tpcMeClient = melhorenvio.NewClient()

// sqlExcluiCOD — predicado SQL que isola Expedição (frete) de COD em tpc_transacoes.
// (FALK) A Carteira Expedição é EXCLUSIVA de frete pré-pago; transações COD
// (Cash-on-Delivery) vazaram para tpc_transacoes com referencia 'sz_cod_produtor_*'
// e descricao "Venda COD Senderzz ...". O dono pediu COD fora das telas de Expedição.
// referencia é NULLABLE → NULL é tratado como NÃO-COD (linha legítima de frete).
// Usa o alias `t` (igual em todas as subqueries de tpc_transacoes deste handler).
// As linhas COD continuam visíveis nas telas próprias (sz_cod_*, Carteira/Transações COD).
// Referência estruturada tem precedência sobre texto livre. `freight_transfer:*`
// é crédito legítimo da Expedição mesmo que a descrição mencione a carteira COD;
// o fallback pela descrição só vale para lançamentos legados sem referência.
const sqlExcluiCOD = `(t.referencia IS NULL OR t.referencia NOT LIKE 'sz_cod%')
	AND (COALESCE(t.referencia, '') <> '' OR COALESCE(t.descricao, '') NOT ILIKE '%cod%')`

// sqlSaldoExpedicao — saldo de Expedição DERIVADO das transações de frete confirmadas
// (credito − reserva), excluindo COD. NÃO usa o c.saldo armazenado porque ele está
// contaminado pelo vazamento COD (ex.: cliente #15 tinha saldo=1770,08 = soma exata
// dos 27 créditos COD).
// AUDIT-2026-07-28: tipo='debito' NUNCA existiu em tpc_transacoes (só 'credito' e
// 'reserva' — confirmado via SELECT DISTINCT tipo). A fórmula original subtraía um
// tipo inexistente → reservas de frete nunca eram descontadas, só créditos (incluindo
// estornos de etiqueta cancelada) somavam puro. Cliente #51 (Lucas/sac@gestao.io):
// recarga R$50 + 3 estornos (28,54+28,54+20,89) = R$127,97 exibido, quando o saldo
// real (recarga − reservas ainda ativas) era R$8,89. Corrigido pro tipo real.
// AUDIT-2026-07-29: 'debito' É permitido pelo CHECK da tabela mesmo não sendo usado
// na prática — subtrai também por robustez (achado ao vivo: ajuste manual usou
// 'debito' e ficou invisível nesta fórmula até ser corrigido pra 'reserva').
const sqlSaldoExpedicao = `COALESCE((
	SELECT COALESCE(SUM(CASE WHEN t.tipo='credito' THEN t.valor ELSE 0 END),0)
	     - COALESCE(SUM(CASE WHEN t.tipo IN ('reserva','debito') THEN t.valor ELSE 0 END),0)
	FROM tpc_transacoes t
	WHERE t.user_id = c.user_id AND t.status = 'confirmado'
	  AND ` + sqlExcluiCOD + `), 0)`

// clienteRow — linha exibida na listagem.
type clienteRow struct {
	UserID            int64   `json:"user_id"`
	Nome              string  `json:"nome"`
	Email             string  `json:"email"`
	Saldo             float64 `json:"saldo"`
	SaldoReservado    float64 `json:"saldo_reservado"`
	SaldoDisponivel   float64 `json:"saldo_disponivel"`
	TransacoesCount   int64   `json:"transacoes_count"`
	UltimaAtualizacao string  `json:"ultima_atualizacao"`
}

type clienteTransacao struct {
	ID        int64   `json:"id"`
	Tipo      string  `json:"tipo"`
	Valor     float64 `json:"valor"`
	SaldoApos float64 `json:"saldo_apos"`
	Descricao string  `json:"descricao"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

type clienteRecarga struct {
	ID        int64   `json:"id"`
	Valor     float64 `json:"valor"`
	Status    string  `json:"status"`
	MePixID   *string `json:"me_pix_id"`
	QRSrc     *string `json:"qr_src"`
	CopiaCola *string `json:"copia_cola"`
	ExpiresAt *string `json:"expires_at"`
	CreatedAt string  `json:"created_at"`
}

func (h *TpcClientesHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// List — GET /tpc-clientes?q=&limit=100&page=1
//
// Lista carteiras com nome/email vindos de senderzz_portal_users via JOIN em
// id nativo. Filtro `q` casa email/nome (LIKE case-insensitive). Quando
// portal_users não existe, devolve apenas user_id e o filtro casa user_id::text.
func (h *TpcClientesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	if !h.tableExists(ctx, "tpc_carteira") {
		httpx.JSON(w, 200, map[string]any{
			"items": []clienteRow{}, "total": 0, "page": page, "per_page": limit,
		})
		return
	}

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")
	hasTx := h.tableExists(ctx, "tpc_transacoes")

	// Monta query dinâmica conforme a presença de cada tabela.
	// `tx_count` vem de subquery para não estourar GROUP BY.
	var (
		sqlList  string
		sqlTotal string
	)

	if hasUsers {
		// LISTAR TODAS AS CARTEIRAS: a tabela dirigente é tpc_carteira; o
		// senderzz_portal_users entra por LEFT JOIN APENAS para enriquecer
		// nome/email. Antes havia INNER JOIN ... AND u.role='produtor', que
		// escondia toda carteira cujo dono não fosse 'produtor' OU não tivesse
		// linha em portal_users — exibindo só 2 de 53 carteiras. O LEFT JOIN sem
		// filtro de role traz TODAS, inclusive user_id com saldo real cujo papel
		// é afiliado/operator/cliente ou que ainda não está no portal.
		//
		// SEGURANÇA DO SUM (verificado contra a DDL, não inflama saldo):
		//   - tpc_carteira tem UNIQUE (user_id)  → 1 linha por user_id.
		//   - senderzz_portal_users tem wp_user_id UNIQUE → cada c.user_id casa
		//     no máximo 1 usuário. Logo o LEFT JOIN não faz fan-out e
		//     SUM(c.saldo) sobre o grupo c.user_id é o próprio saldo da carteira.
		//
		// user_id DEVOLVIDO = c.user_id (chave canônica das tabelas tpc_*).
		// Get/CreateRecarga/CancelRecarga consultam tpc_* DIRETO por $1 = c.user_id,
		// então é c.user_id (não u.wp_user_id) que precisa voltar — e sob LEFT JOIN
		// u.wp_user_id seria NULL justamente para as carteiras recém-expostas.
		// GROUP BY c.user_id agrega por carteira; tx_count conta pela MESMA chave
		// (t.user_id = c.user_id). ultima = MIN(created_at) do grupo.
		//
		// Filtro `q`: email/nome via ILIKE (quando há match em portal_users);
		// user_id::text casa o próprio c.user_id.
		// saldo EXIBIDO = só Expedição. Quando tpc_transacoes existe, derivamos das
		// transações de frete confirmadas (sqlSaldoExpedicao) para não exibir COD.
		// Sem a tabela de transações, caímos no c.saldo armazenado (não há como filtrar).
		saldoExpr := `COALESCE(SUM(c.saldo), 0)`
		if hasTx {
			saldoExpr = sqlSaldoExpedicao
		}
		sqlList = `
			SELECT c.user_id                         AS user_id,
			       COALESCE(MAX(u.nome),  '')        AS nome,
			       COALESCE(MAX(u.email), '')        AS email,
			       ` + saldoExpr + `                 AS saldo,
			       COALESCE(SUM(c.saldo_reservado),0) AS saldo_reservado,
			       MIN(c.created_at)::text           AS ultima
		`
		if hasTx {
			// Conta SÓ transações de Expedição (exclui COD) pela chave canônica.
			sqlList += `, COALESCE((SELECT COUNT(*) FROM tpc_transacoes t
			                         WHERE t.user_id = c.user_id AND ` + sqlExcluiCOD + `), 0) AS tx_count`
		} else {
			sqlList += `, 0::bigint AS tx_count`
		}
		sqlList += `
			FROM tpc_carteira c
			-- MIGRAÇÃO 2026-07-28: tpc_carteira.user_id foi migrado por completo pro
			-- id NATIVO do portal (senderzz_portal_users.id) — o mixed-state que
			-- exigia o LATERAL com fallback wp_user_id/id (AUDIT-2026-07-27) acabou.
			-- Manter o fallback por wp_user_id agora seria PERIGOSO: c.user_id já é
			-- sempre id nativo, então um wp_user_id de OUTRA pessoa que coincida
			-- numericamente venceria o match certo (era exatamente essa colisão que
			-- o LATERAL com prioridade tentava esquivar antes da migração).
			LEFT JOIN senderzz_portal_users u ON u.id = c.user_id
			WHERE ($1 = ''
			       OR u.email ILIKE '%'||$1||'%'
			       OR u.nome  ILIKE '%'||$1||'%'
			       OR c.user_id::text = $1)
			  AND u.role = 'produtor'
			GROUP BY c.user_id
			ORDER BY saldo DESC NULLS LAST, user_id DESC
			LIMIT $2 OFFSET $3`

		// Total = nº de carteiras distintas que casam o filtro. DISTINCT torna a
		// contagem robusta independente de qualquer fan-out do join.
		sqlTotal = `
			SELECT COUNT(DISTINCT c.user_id)
			FROM tpc_carteira c
			-- MIGRAÇÃO 2026-07-28: mesmo motivo do sqlList acima (join direto por
			-- id nativo, sem fallback wp_user_id).
			LEFT JOIN senderzz_portal_users u ON u.id = c.user_id
			WHERE ($1 = ''
			       OR u.email ILIKE '%'||$1||'%'
			       OR u.nome  ILIKE '%'||$1||'%'
			       OR c.user_id::text = $1)
			  AND u.role = 'produtor'`
	} else {
		// Sem portal_users: lista só por user_id; `q` casa user_id::text.
		// saldo EXIBIDO = só Expedição (derivado das tx de frete quando disponível).
		saldoExpr := `COALESCE(c.saldo, 0)`
		if hasTx {
			saldoExpr = sqlSaldoExpedicao
		}
		sqlList = `
			SELECT c.user_id,
			       ''::text                        AS nome,
			       ''::text                        AS email,
			       ` + saldoExpr + `              AS saldo,
			       COALESCE(c.saldo_reservado, 0)  AS saldo_reservado,
			       c.created_at::text                AS ultima
		`
		if hasTx {
			sqlList += `, COALESCE((SELECT COUNT(*) FROM tpc_transacoes t WHERE t.user_id = c.user_id AND ` + sqlExcluiCOD + `), 0) AS tx_count`
		} else {
			sqlList += `, 0::bigint AS tx_count`
		}
		sqlList += `
			FROM tpc_carteira c
			WHERE ($1 = '' OR c.user_id::text = $1)
			ORDER BY saldo DESC NULLS LAST, c.user_id DESC
			LIMIT $2 OFFSET $3`

		sqlTotal = `
			SELECT COUNT(*) FROM tpc_carteira c
			WHERE ($1 = '' OR c.user_id::text = $1)`
	}

	rows, err := h.Pool.Query(ctx, sqlList, q, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []clienteRow{}
	for rows.Next() {
		var c clienteRow
		if err := rows.Scan(
			&c.UserID, &c.Nome, &c.Email,
			&c.Saldo, &c.SaldoReservado, &c.UltimaAtualizacao,
			&c.TransacoesCount,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		c.SaldoDisponivel = c.Saldo - c.SaldoReservado
		items = append(items, c)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx, sqlTotal, q).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"per_page": limit,
	})
}

// Get — GET /tpc-clientes/{user_id}
//
// Detalhe de um cliente: carteira (saldo + reservado + disponivel), últimas 50
// transações e últimas 10 recargas. Joga 404 se não houver carteira.
func (h *TpcClientesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if userID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}

	if !h.tableExists(ctx, "tpc_carteira") {
		httpx.Err(w, 503, "tables_missing", "tpc_carteira ainda não migrada")
		return
	}

	var (
		nome, email      string
		saldo, reservado float64
		ultima           string
	)

	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	// saldo EXIBIDO no detalhe = só Expedição. Deriva das transações de frete
	// confirmadas (sqlSaldoExpedicao) quando tpc_transacoes existe, senão usa o
	// c.saldo armazenado. Mesma lógica do List — mantém a tela de detalhe isolada
	// de COD (o cliente #15 mostrava 1770,08 = soma exata dos 27 créditos COD).
	hasTx := h.tableExists(ctx, "tpc_transacoes")
	saldoSel := `COALESCE(c.saldo, 0)`
	if hasTx {
		saldoSel = sqlSaldoExpedicao
	}

	var queryDetail string
	if hasUsers {
		// JOIN CANÔNICO (MIGRAÇÃO 2026-07-28): user_id da URL = id nativo do portal
		// (List devolve u.id). Casamos a carteira por c.user_id = $1 e o produtor
		// por u.id = c.user_id. LEFT JOIN para ainda exibir saldo mesmo se o
		// produtor não estiver em portal_users.
		queryDetail = `
			SELECT COALESCE(u.nome,  ''),
			       COALESCE(u.email, ''),
			       ` + saldoSel + `,
			       COALESCE(c.saldo_reservado, 0),
			       c.created_at::text
			FROM tpc_carteira c
			LEFT JOIN senderzz_portal_users u ON u.id = c.user_id AND u.role = 'produtor'
			WHERE c.user_id = $1`
	} else {
		queryDetail = `
			SELECT ''::text, ''::text,
			       ` + saldoSel + `,
			       COALESCE(c.saldo_reservado, 0),
			       c.created_at::text
			FROM tpc_carteira c
			WHERE c.user_id = $1`
	}

	err := h.Pool.QueryRow(ctx, queryDetail, userID).
		Scan(&nome, &email, &saldo, &reservado, &ultima)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Err(w, 404, "not_found", "carteira não encontrada para esse user_id")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	cliente := clienteRow{
		UserID:            userID,
		Nome:              nome,
		Email:             email,
		Saldo:             saldo,
		SaldoReservado:    reservado,
		SaldoDisponivel:   saldo - reservado,
		UltimaAtualizacao: ultima,
	}

	// Últimas 50 transações — SÓ Expedição (exclui COD via sqlExcluiCOD). Sem esse
	// filtro, abrir o detalhe de #15 numa tela de Expedição re-exibiria os 27 COD.
	txs := []clienteTransacao{}
	if hasTx {
		rows, err := h.Pool.Query(ctx,
			`SELECT t.id, t.tipo, COALESCE(t.valor,0), COALESCE(t.saldo_apos,0),
			        COALESCE(t.descricao,''), COALESCE(t.status,''),
			        t.created_at::text
			 FROM tpc_transacoes t
			 WHERE t.user_id = $1 AND `+sqlExcluiCOD+`
			 ORDER BY t.id DESC LIMIT 50`, userID)
		if err == nil {
			for rows.Next() {
				var t clienteTransacao
				_ = rows.Scan(&t.ID, &t.Tipo, &t.Valor, &t.SaldoApos,
					&t.Descricao, &t.Status, &t.CreatedAt)
				txs = append(txs, t)
			}
			rows.Close()
			cliente.TransacoesCount = int64(len(txs))
		}
	}

	// Últimas 10 recargas.
	recargas := []clienteRecarga{}
	if h.tableExists(ctx, "tpc_recargas") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(valor,0), COALESCE(status,''),
			        me_pix_id, pix_qr, pix_codigo,
			        expires_at::text, created_at::text
			 FROM tpc_recargas
			 WHERE user_id = $1
			 ORDER BY id DESC LIMIT 10`, userID)
		if err == nil {
			for rows.Next() {
				var rec clienteRecarga
				_ = rows.Scan(&rec.ID, &rec.Valor, &rec.Status,
					&rec.MePixID, &rec.QRSrc, &rec.CopiaCola,
					&rec.ExpiresAt, &rec.CreatedAt)
				recargas = append(recargas, rec)
			}
			rows.Close()
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"cliente":    cliente,
		"transacoes": txs,
		"recargas":   recargas,
	})
}

// CreateRecarga — POST /tpc-clientes/{user_id}/recarga
//
// Body: {valor: float, motivo: string}
//
// Insere recarga 'pendente' em tpc_recargas e devolve placeholders de PIX (QR
// + copia-cola + security_token) para a UI exibir imediatamente. A emissão
// real do PIX é responsabilidade do wallet-service (POST /internal/recarga/
// create com HMAC) — esse handler apenas registra a linha e retorna stub.
func (h *TpcClientesHandler) CreateRecarga(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if userID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id inválido")
		return
	}

	var body struct {
		Valor  float64 `json:"valor"`
		Motivo string  `json:"motivo"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if body.Valor < 10 || body.Valor > 100000 {
		httpx.Err(w, 400, "bad_request", "valor mínimo é R$ 10,00 e máximo R$ 100.000,00")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	if !tpcMeClient.HasToken() {
		httpx.Err(w, 503, "me_sem_token", "ME_TOKEN não configurado — emissão de PIX indisponível")
		return
	}

	// Token de segurança (usado no redirect_url — identifica a recarga sem
	// expor o id cru na URL de retorno).
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	securityToken := hex.EncodeToString(tokenBytes)
	redirectURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/") + "/tpc-recarga-retorno?t=" + securityToken

	pix, err := tpcMeClient.GerarPix(ctx, body.Valor, redirectURL)
	if err != nil {
		httpx.Err(w, 502, "me_erro", "Erro ao gerar PIX no Melhor Envio: "+err.Error())
		return
	}

	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	if pix.ExpiresTS > 0 {
		expiresAt = time.Unix(pix.ExpiresTS, 0).UTC()
	}
	mePixID := pix.PixID
	if mePixID == "" {
		mePixID = securityToken // fallback: garante NOT NULL/idempotência mesmo se ME não devolver id
	}

	var recargaID int64
	err = h.Pool.QueryRow(ctx,
		`INSERT INTO tpc_recargas
		   (user_id, valor, status, me_pix_id, pix_qr, pix_codigo, expires_at, created_at)
		 VALUES ($1, $2, 'pendente', $3, $4, $5, $6, NOW())
		 RETURNING id`,
		userID, body.Valor, mePixID, pix.QRSrc, pix.CopiaCola, expiresAt).Scan(&recargaID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	// Nota: tpc_transacoes.tipo é restrito a (credito|debito|reserva) por CHECK
	// constraint. O motivo só é persistido implicitamente como contexto da recarga
	// — a confirmação do PIX (webhook ME → wallet-service) cria a transação real
	// de crédito. O motivo informado pelo admin pode ser registrado em auditoria
	// externa (TODO: senderzz_audit_log quando o handler for migrado).
	_ = body.Motivo

	httpx.JSON(w, 200, map[string]any{
		"ok":             true,
		"recarga_id":     recargaID,
		"user_id":        userID,
		"valor":          body.Valor,
		"qr_src":         pix.QRSrc,
		"copia_cola":     pix.CopiaCola,
		"link":           pix.Link,
		"expires_at":     expiresAt.Format(time.RFC3339),
		"security_token": securityToken,
	})
}

// CancelRecarga — POST /tpc-clientes/{user_id}/cancelar-recarga/{recarga_id}
//
// Marca a recarga como 'cancelado' somente se ainda estiver 'pendente' e
// pertencer ao user_id informado. Idempotente — não retorna erro se nada mudou.
func (h *TpcClientesHandler) CancelRecarga(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _ := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	recargaID, _ := strconv.ParseInt(chi.URLParam(r, "recarga_id"), 10, 64)
	if userID <= 0 || recargaID <= 0 {
		httpx.Err(w, 400, "bad_request", "user_id/recarga_id inválidos")
		return
	}

	if !h.tableExists(ctx, "tpc_recargas") {
		httpx.Err(w, 503, "tables_missing", "tpc_recargas ainda não migrada")
		return
	}

	tag, err := h.Pool.Exec(ctx,
		`UPDATE tpc_recargas
		   SET status = 'cancelado'
		 WHERE id = $1 AND user_id = $2 AND status = 'pendente'`,
		recargaID, userID)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":            true,
		"recarga_id":    recargaID,
		"rows_affected": tag.RowsAffected(),
	})
}

// ResetWalletAll — POST /tpc-clientes/reset-wallet-all
//
// DANGER, mas PRESERVA O LEDGER. Zera os saldos sem apagar o histórico:
//   - tpc_carteira ....... saldo = 0, saldo_reservado = 0 (UPDATE, não DELETE)
//   - tpc_transacoes ..... PRESERVADA — o ledger/histórico NÃO é apagado.
//   - tpc_recargas ....... apaga recargas pendentes/registros (DELETE)
//
// Antes esse handler fazia DELETE FROM tpc_transacoes, destruindo o ledger.
// Agora apenas zeramos tpc_carteira.saldo/saldo_reservado, mantendo a trilha
// contábil intacta para auditoria.
//
// Exige body {"confirm":"RESETAR"} EXATO (case-sensitive). Tudo dentro de uma
// transação — se uma operação falhar, rollback total.
func (h *TpcClientesHandler) ResetWalletAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Confirm string `json:"confirm"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	// Comparação EXATA — sem trim, sem lowercase. Erro se digitou diferente.
	if body.Confirm != "RESETAR" {
		httpx.Err(w, 400, "confirmation_required",
			`envie {"confirm":"RESETAR"} (string exata, case-sensitive) para confirmar`)
		return
	}

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer tx.Rollback(ctx)

	res := map[string]int64{
		"carteira_zeroed":      0,
		"transacoes_preserved": 0,
		"recargas_deleted":     0,
		"order_meta_deleted":   0,
	}

	// Zera saldos SEM apagar a carteira — só UPDATE saldo/saldo_reservado.
	if h.tableExists(ctx, "tpc_carteira") {
		tag, err := tx.Exec(ctx,
			`UPDATE tpc_carteira SET saldo = 0, saldo_reservado = 0`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		res["carteira_zeroed"] = tag.RowsAffected()
	}

	// LEDGER PRESERVADO: tpc_transacoes NÃO é apagada. Apenas contamos as
	// linhas existentes para sinalizar na resposta que o histórico ficou intacto.
	if h.tableExists(ctx, "tpc_transacoes") {
		var n int64
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM tpc_transacoes`).Scan(&n)
		res["transacoes_preserved"] = n
	}

	if h.tableExists(ctx, "tpc_recargas") {
		tag, err := tx.Exec(ctx, `DELETE FROM tpc_recargas`)
		if err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		res["recargas_deleted"] = tag.RowsAffected()
	}

	// AUDIT-2026-07-31: removido fallback pra wp_postmeta — tabela MySQL nativa
	// do WordPress, nunca existiu (nem existiria) no Postgres deste projeto;
	// tableExists() sempre retornava false, branch 100% inalcançável.
	if h.tableExists(ctx, "sz_order_meta") {
		tag, _ := tx.Exec(ctx,
			`DELETE FROM sz_order_meta WHERE meta_key LIKE '_senderzz_wallet_%'`)
		res["order_meta_deleted"] += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":     true,
		"result": res,
	})
}
