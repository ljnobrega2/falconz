// Handler do canal do titular — LGPD Art. 18/19 (Data Subject Requests).
// Tabela: senderzz_data_subject_requests (criada em 380-lgpd-completo.sql).
//
// AUDIT-LGPD-2026-06-24 A3 + B6: o canal do titular era WRITE-ONLY — o portal
// gravava o pedido (POST /portal/data-request) mas NENHUM handler lia/processava
// a fila, então o SLA de 15 dias do Art. 19 nunca era observado. Este handler dá
// ao DPO/operador a leitura e o tratamento da fila:
//
//	GET  /data-subject-requests?status=        → lista pedidos do titular (prazo ASC)
//	POST /data-subject-requests/{id}            → atualiza status/response (trata o pedido)
//	GET  /data-subject-requests/lookup?phone=&cpf=  → localiza PII do pedido por TEL/CPF
//
// O lookup (B6) existe porque o checkout COD NÃO coleta e-mail — o titular que
// comprou por COD só é localizável por TELEFONE ou CPF. Ele devolve o MÍNIMO
// necessário p/ o atendimento (id, número, nome, tel/cpf MASCARADOS, data), nunca
// endereço/e-mail (minimização — Art. 6º III).
//
// Escopo ADMIN-ONLY: registrado no grupo auth.Middleware (não DualAuth). PT-BR
// nos comentários/mensagens (convenção do projeto).
//
// PENDENTE: tela do DPO no admin-ui (não feita nesta entrega — só a API + rotas).
package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/auth"
	"github.com/senderzz/admin-service/internal/httpx"
)

// DSRHandler expõe a leitura/tratamento do canal do titular (Art. 18).
type DSRHandler struct{ Pool *pgxpool.Pool }

// tableExists — degradação graciosa quando a migração 380 ainda não rodou.
func (h *DSRHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// dsrStatuses — os 4 estados válidos (espelha o CHECK chk_dsr_status do schema).
// 'concluido' e 'negado' são TERMINAIS (carimbam fulfilled_at).
var dsrStatuses = map[string]bool{
	"recebido":     true,
	"em_andamento": true,
	"concluido":    true,
	"negado":       true,
}

// dsrTerminal informa se o status encerra o pedido (atende ao Art. 19 — resposta).
func dsrTerminal(status string) bool {
	return status == "concluido" || status == "negado"
}

// DataSubjectRequest — shape retornado pela API. Campos NULLáveis viram string
// vazia (timestamps via ::text + COALESCE; ids via ponteiro no scan).
type DataSubjectRequest struct {
	ID          int64  `json:"id"`
	UserID      *int64 `json:"user_id"` // NULL = titular não-cadastrado (só e-mail/COD)
	Email       string `json:"email"`
	RequestType string `json:"request_type"`
	Status      string `json:"status"`
	SLADeadline string `json:"sla_deadline"`
	FulfilledAt string `json:"fulfilled_at"` // '' enquanto não encerrado
	HandledBy   *int64 `json:"handled_by"`   // NULL enquanto ninguém tratou
	Response    string `json:"response"`
	RequestedAt string `json:"requested_at"`
}

// ---------------------------------------------------------------------------
// GET /data-subject-requests?status=
// ---------------------------------------------------------------------------
//
// Lista os pedidos do titular ordenados por sla_deadline ASC — prioriza o que
// VENCE primeiro (o índice idx_dsr_sla serve essa ordenação). O filtro ?status=
// quando AUSENTE cai no default 'recebido' (a fila de entrada que o DPO precisa
// atender); valor explícito filtra por aquele status. Tabela ausente → vazio.
func (h *DSRHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_data_subject_requests") {
		httpx.JSON(w, 200, map[string]any{"items": []DataSubjectRequest{}, "total": int64(0)})
		return
	}

	// ?status= ausente = 'recebido' (fila de entrada). Valor inválido idem default.
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if !dsrStatuses[status] {
		status = "recebido"
	}

	rows, err := h.Pool.Query(ctx, `
		SELECT id, user_id, email, request_type, status,
		       sla_deadline::text,
		       COALESCE(fulfilled_at::text, '') AS fulfilled_at,
		       handled_by,
		       COALESCE(response, '')           AS response,
		       requested_at::text
		FROM senderzz_data_subject_requests
		WHERE status = $1
		ORDER BY sla_deadline ASC, id ASC`, status)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []DataSubjectRequest{}
	for rows.Next() {
		var d DataSubjectRequest
		// user_id/handled_by NULLáveis → ponteiros (NULL vira nil no JSON).
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.Email, &d.RequestType, &d.Status,
			&d.SLADeadline, &d.FulfilledAt, &d.HandledBy, &d.Response, &d.RequestedAt,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, d)
	}

	httpx.JSON(w, 200, map[string]any{
		"items": items,
		"total": int64(len(items)),
	})
}

// ---------------------------------------------------------------------------
// POST /data-subject-requests/{id}
// ---------------------------------------------------------------------------
//
// Trata o pedido: move o status, grava handled_by (admin do contexto), response
// (texto ao titular) e, em status TERMINAL (concluido|negado), carimba
// fulfilled_at=NOW() — a prova de cumprimento do Art. 19 (resposta em 15 dias).
//
// Validação: status obrigatório e dentro do conjunto válido (não confiamos só no
// CHECK do banco). handled_by sai do admin autenticado (auth.FromCtx), nunca do
// body. fulfilled_at só é (re)carimbado em transição terminal; status não-terminal
// volta a deixar fulfilled_at NULL (reabertura), preservando a verdade do registro.
func (h *DSRHandler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "senderzz_data_subject_requests") {
		httpx.Err(w, 503, "table_missing", "tabela senderzz_data_subject_requests não migrada")
		return
	}

	id, ok := parseIDParam(r) // package-level helper (cod_saques.go)
	if !ok {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	var body struct {
		Status   string `json:"status"`
		Response string `json:"response"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	body.Status = strings.TrimSpace(body.Status)
	if !dsrStatuses[body.Status] {
		httpx.Err(w, 400, "bad_request",
			"status inválido — use recebido|em_andamento|concluido|negado")
		return
	}

	// handled_by = admin autenticado. Sem admin = 401 (middleware barra antes).
	admin := auth.FromCtx(ctx)
	var adminID int64
	if admin != nil {
		adminID = admin.ID
	}

	// fulfilled_at: NOW() em status terminal; NULL caso contrário (reabertura).
	// Passamos o flag terminal como $4 e decidimos no SQL para um único UPDATE.
	terminal := dsrTerminal(body.Status)

	// response: só sobrescreve se veio texto novo; body vazio PRESERVA a resposta
	// anterior (accountability — Art. 37: não apagar a resposta já dada ao titular
	// numa simples mudança de status, ex.: reabertura).
	ct, err := h.Pool.Exec(ctx, `
		UPDATE senderzz_data_subject_requests
		   SET status       = $2,
		       response     = CASE WHEN $3 <> '' THEN $3 ELSE response END,
		       handled_by   = $4,
		       fulfilled_at = CASE WHEN $5 THEN NOW() ELSE NULL END
		 WHERE id = $1`,
		id, body.Status, body.Response, adminID, terminal)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	if ct.RowsAffected() == 0 {
		httpx.Err(w, 404, "not_found", "pedido do titular não encontrado")
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "id": id, "status": body.Status})
}

// ---------------------------------------------------------------------------
// GET /data-subject-requests/lookup?phone=&cpf=
// ---------------------------------------------------------------------------
//
// AUDIT-LGPD-2026-06-24 B6: localiza os pedidos do titular por TELEFONE ou CPF —
// o checkout COD não guarda e-mail, então a busca por e-mail (do portal) não
// alcança esse comprador. Procura o telefone em sz_order_addresses.telefone e o
// CPF em sz_order_meta._billing_cpf, devolvendo o pedido casado (sz_orders).
//
// MINIMIZAÇÃO (Art. 6º III): exige AO MENOS um critério (sem ambos → 400, nunca
// um dump da tabela). Normaliza dígitos nos DOIS lados (a coluna pode ter
// formatação; o operador pode colar '123.456.789-00'). Retorna SÓ o necessário p/
// o atendimento — id, número, nome, telefone/CPF MASCARADOS (sz_mask_cpf + máscara
// de telefone), data — NUNCA endereço/e-mail/CPF integral.
type DSRLookupMatch struct {
	OrderID     int64  `json:"order_id"`
	OrderNumber string `json:"order_number"`
	Nome        string `json:"nome"`
	TelefoneMsk string `json:"telefone_masked"` // só os últimos dígitos
	CPFMsk      string `json:"cpf_masked"`      // '***.XXX.XXX-**' (sz_mask_cpf)
	CreatedAt   string `json:"created_at"`
}

func (h *DSRHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	q := r.URL.Query()
	phone := onlyDigits(q.Get("phone"))
	cpf := onlyDigits(q.Get("cpf"))

	// Minimização: sem critério não há busca (não despeja a tabela de pedidos).
	if phone == "" && cpf == "" {
		httpx.Err(w, 400, "bad_request", "informe phone ou cpf para localizar o titular")
		return
	}
	if !h.tableExists(ctx, "sz_orders") {
		httpx.JSON(w, 200, map[string]any{"items": []DSRLookupMatch{}, "total": int64(0)})
		return
	}

	// Casamento por TELEFONE (sz_order_addresses) OU CPF (_billing_cpf), ambos
	// normalizados a dígitos puros nos dois lados. Critério vazio ('') é neutro
	// (o ramo OR correspondente fica falso). DISTINCT ON evita duplicar o pedido
	// quando ele tem endereço shipping+billing com o mesmo telefone.
	//
	// CPF mascarado via sz_mask_cpf() (primitiva do schema 380, antes sem chamador
	// — wired aqui). Telefone mascarado revelando só os 4 últimos dígitos.
	rows, err := h.Pool.Query(ctx, `
		SELECT DISTINCT ON (o.id)
		       o.id,
		       o.order_number,
		       COALESCE(a.nome, ''),
		       a.telefone,
		       m.meta_value AS cpf,
		       o.created_at::text
		FROM sz_orders o
		LEFT JOIN sz_order_addresses a ON a.order_id = o.id
		LEFT JOIN sz_order_meta m      ON m.order_id = o.id
		                              AND m.meta_key = '_billing_cpf'
		WHERE ($1 <> '' AND regexp_replace(COALESCE(a.telefone,''), '[^0-9]', '', 'g') = $1)
		   OR ($2 <> '' AND regexp_replace(COALESCE(m.meta_value,''), '[^0-9]', '', 'g') = $2)
		ORDER BY o.id DESC
		LIMIT 50`, phone, cpf)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []DSRLookupMatch{}
	for rows.Next() {
		var (
			m       DSRLookupMatch
			tel     *string
			cpfFull *string
		)
		if err := rows.Scan(&m.OrderID, &m.OrderNumber, &m.Nome, &tel, &cpfFull, &m.CreatedAt); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		m.TelefoneMsk = maskPhone(tel)
		m.CPFMsk = maskCPF(cpfFull)
		items = append(items, m)
	}

	httpx.JSON(w, 200, map[string]any{
		"items": items,
		"total": int64(len(items)),
	})
}

// onlyDigits (normaliza CPF/telefone p/ comparação) é o helper package-level
// definido em onboarding.go — reusado aqui, não redeclarado.

// maskPhone revela só os 4 últimos dígitos: '••••••1234'. Entrada vazia → ''.
// Minimização — o operador confirma o titular sem ver o telefone integral.
func maskPhone(p *string) string {
	if p == nil {
		return ""
	}
	d := onlyDigits(*p)
	if d == "" {
		return ""
	}
	if len(d) <= 4 {
		return d
	}
	return strings.Repeat("•", len(d)-4) + d[len(d)-4:]
}

// maskCPF aplica a máscara '***.XXX.XXX-**' (mesma lógica de sz_mask_cpf do schema
// 380). Faz no Go p/ não exigir round-trip ao banco por linha; placeholder seguro
// quando não há 11 dígitos (não vaza a entrada original). Entrada vazia → ''.
func maskCPF(c *string) string {
	if c == nil {
		return ""
	}
	d := onlyDigits(*c)
	if d == "" {
		return ""
	}
	if len(d) != 11 {
		return "***.***.***-**"
	}
	return "***." + d[3:6] + "." + d[6:9] + "-**"
}
