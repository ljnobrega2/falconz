// Package handlers — Área de SUPORTE do ADMIN (chamados/tickets).
//
// Espelha o handler do portal (go/portal/internal/handlers/support_portal.go)
// e a UI templates/portal/v2/sections/support.php — MAS do lado do ADMIN, que
// vê TODOS os chamados de TODOS os usuários (sem o escopo portal_user_id que o
// portal aplica). Aqui o objetivo é o atendimento: o admin lista os chamados,
// abre o detalhe e lê a conversa.
//
// Rotas (sob /wp-json/senderzz/v1/admin/, auth=middleware admin iss=senderzz-admin):
//
//	GET  /support/tickets        → lista de chamados (todos os usuários) + filtros
//	GET  /support/tickets/{id}   → chamado + mensagens (thread)
//
// SOMENTE LEITURA por ora: o admin lê o chamado e o histórico. Responder/fechar
// pelo admin envolveria gravar autor_tipo='admin' (a tabela suporta), mas isso é
// escopo futuro — o pedido (#45) é "aparecer numa área de suporte no admin com
// detalhamento". Não inventamos mutação não solicitada.
//
// DIFERENÇA-CHAVE vs. o portal (NÃO copiar o user-scoping de lá): o portal filtra
// WHERE portal_user_id = u.ID; o admin NÃO filtra por dono — lista tudo e faz
// LEFT JOIN em senderzz_portal_users para mostrar QUEM abriu cada chamado.
//
// "Pedido relacionado": a tabela sz_portal_tickets NÃO tem coluna de pedido
// (ver includes/motoboy/database.php DDL — só categoria ENUM com valor 'pedido',
// nenhum FK a um pedido específico). Por isso o detalhe expõe `categoria`, não um
// link de pedido. Não há coluna para JOIN; inventá-la quebraria a query.
//
// Tabelas: sz_portal_tickets, sz_portal_ticket_msgs. ATENÇÃO: ainda podem NÃO
// existir no espelho Postgres (schema-motoboy.sql deferiu para "Fase futura") —
// por isso o guard tableExists → lista vazia (mesmo padrão dos demais handlers).
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// SupportHandler expõe a área de suporte (tickets) para o admin.
type SupportHandler struct{ Pool *pgxpool.Pool }

// tableExists — checagem genérica para qualquer tabela public.<name>
// (degradação graciosa durante a janela de migração, como os demais handlers).
func (h *SupportHandler) tableExists(ctx context.Context, name string) bool {
	return tableExistsCached(ctx, h.Pool, name) // AUDIT-2026-06-18 Onda2 (go-infoschema-cache)
}

// supportTicketListItem — uma linha na listagem de chamados do admin.
type supportTicketListItem struct {
	ID             int64   `json:"id"`
	PortalUserID   int64   `json:"portal_user_id"`
	UsuarioNome    string  `json:"usuario_nome"`  // quem abriu (LEFT JOIN portal_users)
	UsuarioEmail   string  `json:"usuario_email"` // idem
	Assunto        string  `json:"assunto"`
	Categoria      string  `json:"categoria"`
	Status         string  `json:"status"`
	Prioridade     string  `json:"prioridade"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	TotalMsgs      int64   `json:"total_msgs"`
	UltimaMsgAutor *string `json:"ultima_msg_autor"`
}

// supportTicketDetail — cabeçalho do chamado no detalhe.
type supportTicketDetail struct {
	ID           int64   `json:"id"`
	PortalUserID int64   `json:"portal_user_id"`
	UsuarioNome  string  `json:"usuario_nome"`
	UsuarioEmail string  `json:"usuario_email"`
	Assunto      string  `json:"assunto"`
	Categoria    string  `json:"categoria"`
	Status       string  `json:"status"`
	Prioridade   string  `json:"prioridade"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	FechadoAt    *string `json:"fechado_at"`
}

// supportTicketMessage — uma mensagem (cliente ou admin) de um chamado.
type supportTicketMessage struct {
	ID        int64   `json:"id"`
	AutorTipo string  `json:"autor_tipo"`
	AutorNome *string `json:"autor_nome"`
	Mensagem  string  `json:"mensagem"`
	CreatedAt string  `json:"created_at"`
}

// ListTickets — GET /support/tickets
//
// Lista TODOS os chamados (admin), mais recente primeiro (updated_at DESC).
// Filtros opcionais: ?status=, ?categoria=, ?q= (assunto / nome / e-mail).
// Degrada para lista vazia se a tabela ainda não foi migrada.
func (h *SupportHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_portal_tickets") {
		httpx.JSON(w, 200, map[string]any{"items": []supportTicketListItem{}, "total": int64(0)})
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	search := strings.TrimSpace(q.Get("q"))
	status := strings.TrimSpace(q.Get("status"))
	categoria := strings.TrimSpace(q.Get("categoria"))

	rows, err := h.Pool.Query(ctx,
		`SELECT
		    t.id,
		    t.portal_user_id,
		    COALESCE(pu.nome, '')  AS usuario_nome,
		    COALESCE(pu.email, '') AS usuario_email,
		    t.assunto,
		    t.categoria,
		    t.status,
		    t.prioridade,
		    t.created_at::text,
		    t.updated_at::text,
		    (SELECT COUNT(*) FROM sz_portal_ticket_msgs m WHERE m.ticket_id = t.id) AS total_msgs,
		    (SELECT m2.autor_tipo FROM sz_portal_ticket_msgs m2
		        WHERE m2.ticket_id = t.id ORDER BY m2.id DESC LIMIT 1) AS ultima_msg_autor
		 FROM sz_portal_tickets t
		 LEFT JOIN senderzz_portal_users pu ON pu.id = t.portal_user_id
		 WHERE ($1 = '' OR t.status = $1)
		   AND ($2 = '' OR t.categoria = $2)
		   AND ($3 = '' OR t.assunto ILIKE '%' || $3 || '%'
		                OR pu.nome   ILIKE '%' || $3 || '%'
		                OR pu.email  ILIKE '%' || $3 || '%')
		 ORDER BY t.updated_at DESC, t.id DESC
		 LIMIT $4 OFFSET $5`,
		status, categoria, search, limit, offset)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	items := []supportTicketListItem{}
	for rows.Next() {
		var t supportTicketListItem
		if err := rows.Scan(
			&t.ID, &t.PortalUserID, &t.UsuarioNome, &t.UsuarioEmail,
			&t.Assunto, &t.Categoria, &t.Status, &t.Prioridade,
			&t.CreatedAt, &t.UpdatedAt, &t.TotalMsgs, &t.UltimaMsgAutor,
		); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		items = append(items, t)
	}

	var total int64
	_ = h.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sz_portal_tickets t
		 LEFT JOIN senderzz_portal_users pu ON pu.id = t.portal_user_id
		 WHERE ($1 = '' OR t.status = $1)
		   AND ($2 = '' OR t.categoria = $2)
		   AND ($3 = '' OR t.assunto ILIKE '%' || $3 || '%'
		                OR pu.nome   ILIKE '%' || $3 || '%'
		                OR pu.email  ILIKE '%' || $3 || '%')`,
		status, categoria, search).Scan(&total)

	httpx.JSON(w, 200, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// TicketDetail — GET /support/tickets/{id}
//
// Retorna o chamado (sem escopo de dono — admin) + a thread de mensagens.
func (h *SupportHandler) TicketDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_portal_tickets") {
		httpx.Err(w, 503, "table_not_found", "tabela sz_portal_tickets não existe — migração deferida (schema-motoboy.sql)")
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}

	var t supportTicketDetail
	err = h.Pool.QueryRow(ctx,
		`SELECT
		    t.id,
		    t.portal_user_id,
		    COALESCE(pu.nome, '')  AS usuario_nome,
		    COALESCE(pu.email, '') AS usuario_email,
		    t.assunto,
		    t.categoria,
		    t.status,
		    t.prioridade,
		    t.created_at::text,
		    t.updated_at::text,
		    t.fechado_at::text
		 FROM sz_portal_tickets t
		 LEFT JOIN senderzz_portal_users pu ON pu.id = t.portal_user_id
		 WHERE t.id = $1
		 LIMIT 1`,
		id).Scan(
		&t.ID, &t.PortalUserID, &t.UsuarioNome, &t.UsuarioEmail,
		&t.Assunto, &t.Categoria, &t.Status, &t.Prioridade,
		&t.CreatedAt, &t.UpdatedAt, &t.FechadoAt)
	if err == pgx.ErrNoRows {
		httpx.Err(w, 404, "not_found", "chamado não encontrado")
		return
	}
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, autor_tipo, autor_nome, mensagem, created_at::text
		   FROM sz_portal_ticket_msgs
		  WHERE ticket_id = $1
		  ORDER BY id ASC
		  LIMIT 500`,
		id)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	msgs := []supportTicketMessage{}
	for rows.Next() {
		var m supportTicketMessage
		if err := rows.Scan(&m.ID, &m.AutorTipo, &m.AutorNome, &m.Mensagem, &m.CreatedAt); err != nil {
			httpx.Err(w, 500, "scan_error", err.Error())
			return
		}
		msgs = append(msgs, m)
	}

	httpx.JSON(w, 200, map[string]any{
		"ticket": t,
		"msgs":   msgs,
	})
}
