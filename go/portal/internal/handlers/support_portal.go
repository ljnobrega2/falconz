// Package handlers — handler de Suporte (Tickets) do Portal V2.
//
// Espelha src/Portal/Portal_Page.php (ajax_tickets_list / ajax_ticket_create /
// ajax_ticket_msgs / ajax_ticket_send_msg / ajax_ticket_close) e a UI
// templates/portal/v2/sections/support.php.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/support/tickets                 — lista chamados do usuário
//	POST /portal/support/tickets                 — abre um novo chamado
//	GET  /portal/support/tickets/{id}            — chamado + mensagens
//	POST /portal/support/tickets/{id}/messages   — responde ao chamado (cliente)
//	POST /portal/support/tickets/{id}/close      — fecha o chamado
//
// User-scoping (segurança):
//
//	Tickets são de propriedade direta — sz_portal_tickets.portal_user_id = u.ID
//	(o id em senderzz_portal_users, NÃO o wp_user_id). Cada usuário só consulta
//	os próprios chamados; produtor vê o dele, afiliado o dele. Os JOINs canônicos
//	afiliado/produtor NÃO se aplicam aqui (não há dado de terceiro envolvido) —
//	a query escopada por portal_user_id já garante o isolamento de role.
//
// Tabelas: sz_portal_tickets, sz_portal_ticket_msgs (criadas em
// includes/motoboy/database.php). ATENÇÃO: ainda NÃO existem no schema Postgres
// (schema-motoboy.sql:24-26 deferiu para "Fase futura") — ver nota ao integrador.
//
// Paridade com o PHP (não improvisar):
//   - Postgres não tem ON UPDATE CURRENT_TIMESTAMP → updated_at setado à mão.
//   - SendMessage só bumpa o ticket quando status='respondido' → 'em_analise';
//     mensagem comum NÃO mexe no ticket (a lista ordena por updated_at DESC).
//   - Validações de tamanho em bytes (len), idênticas ao strlen do PHP:
//     assunto >= 5, mensagem >= 10 (criar) / >= 3 (responder).
//   - categoria ∈ {financeiro,pedido,tecnico,outro}, default 'outro'.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// SupportHandler agrupa as dependências dos handlers de Suporte.
type SupportHandler struct {
	Pool *pgxpool.Pool
}

// listSupportLimit é o teto de chamados na listagem. AUDIT PERF-list-endpoints-hard-limit:
// busca limit+1 (N+1) para devolver has_more sem COUNT (sem truncamento silencioso).
const listSupportLimit = 50

// listTicketMsgsLimit é o teto de mensagens no Detail de um chamado (espelha o
// LIMIT 100 histórico). Mesmo padrão N+1 → msgs_has_more sem COUNT. // PERF-list-endpoints-hard-limit
const listTicketMsgsLimit = 100

// tableExists — guarda de migração graceful (idêntico aos demais handlers portal).
// As tabelas sz_portal_tickets / sz_portal_ticket_msgs ainda NÃO existem no espelho
// Postgres (schema-motoboy.sql deferiu para "Fase futura"). Sem este guard, a
// listagem dispara 42P01 (relation does not exist) → 500. Conta nova / ambiente sem
// as tabelas devolve lista vazia em vez de erro (mesmo empty-state do .php).
func (h *SupportHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// allowedTicketCategorias — whitelist de categorias aceitas (exact match).
// Espelha o in_array do PHP; qualquer outro valor cai em 'outro'.
var allowedTicketCategorias = map[string]bool{
	"financeiro": true,
	"pedido":     true,
	"tecnico":    true,
	"outro":      true,
}

// ── Responses ───────────────────────────────────────────────────────────────

// ticketListItem representa uma linha na listagem de chamados.
type ticketListItem struct {
	ID             int64   `json:"id"`
	Assunto        string  `json:"assunto"`
	Categoria      string  `json:"categoria"`
	Status         string  `json:"status"`
	Prioridade     string  `json:"prioridade"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	TotalMsgs      int64   `json:"total_msgs"`
	UltimaMsgAutor *string `json:"ultima_msg_autor"`
}

// ticketDetail representa o cabeçalho do chamado no detalhe.
type ticketDetail struct {
	ID         int64   `json:"id"`
	Assunto    string  `json:"assunto"`
	Categoria  string  `json:"categoria"`
	Status     string  `json:"status"`
	Prioridade string  `json:"prioridade"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	FechadoAt  *string `json:"fechado_at"`
}

// ticketMessage representa uma mensagem de um chamado.
type ticketMessage struct {
	ID        int64   `json:"id"`
	AutorTipo string  `json:"autor_tipo"`
	AutorNome *string `json:"autor_nome"`
	Mensagem  string  `json:"mensagem"`
	CreatedAt string  `json:"created_at"`
}

// ── Requests ────────────────────────────────────────────────────────────────

// createTicketRequest é o body de POST /portal/support/tickets.
type createTicketRequest struct {
	Assunto   string `json:"assunto"`
	Categoria string `json:"categoria"`
	Mensagem  string `json:"mensagem"`
}

// sendTicketMessageRequest é o body de POST /portal/support/tickets/{id}/messages.
type sendTicketMessageRequest struct {
	Mensagem string `json:"mensagem"`
}

// ── GET /portal/support/tickets ──────────────────────────────────────────────

// List retorna os chamados do usuário autenticado (escopo portal_user_id = u.ID).
// Espelha ajax_tickets_list.
func (h *SupportHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// Tabela não migrada ainda — devolve lista vazia em vez de 500 (mirror do
	// empty-state do .php; mantém o contrato de resposta {"tickets":[],"total":0}).
	if !h.tableExists(r.Context(), "sz_portal_tickets") {
		httpx.WriteOK(w, map[string]any{"tickets": []ticketListItem{}, "total": 0, "has_more": false, "limit": listSupportLimit})
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT t.id, t.assunto, t.categoria, t.status, t.prioridade,
		        to_char(t.created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at,
		        to_char(t.updated_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS updated_at,
		        (SELECT COUNT(*) FROM sz_portal_ticket_msgs m WHERE m.ticket_id = t.id) AS total_msgs,
		        (SELECT m2.autor_tipo FROM sz_portal_ticket_msgs m2
		           WHERE m2.ticket_id = t.id ORDER BY m2.id DESC LIMIT 1) AS ultima_msg_autor
		   FROM sz_portal_tickets t
		  WHERE t.portal_user_id = $1
		  ORDER BY t.updated_at DESC
		  LIMIT $2`,
		u.ID, listSupportLimit+1, // N+1: detecta truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	)
	if err != nil {
		slog.Error("[portal_support] erro ao listar chamados", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	tickets := []ticketListItem{}
	for rows.Next() {
		var t ticketListItem
		if err := rows.Scan(
			&t.ID, &t.Assunto, &t.Categoria, &t.Status, &t.Prioridade,
			&t.CreatedAt, &t.UpdatedAt, &t.TotalMsgs, &t.UltimaMsgAutor,
		); err != nil {
			slog.Error("[portal_support] erro ao ler chamado", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler chamados")
			return
		}
		tickets = append(tickets, t)
	}
	if rows.Err() != nil {
		slog.Error("[portal_support] erro após iteração", "user_id", u.ID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar chamados")
		return
	}

	hasMore := len(tickets) > listSupportLimit
	if hasMore {
		tickets = tickets[:listSupportLimit]
	}

	httpx.WriteOK(w, map[string]any{"tickets": tickets, "total": len(tickets), "has_more": hasMore, "limit": listSupportLimit})
}

// ── POST /portal/support/tickets ─────────────────────────────────────────────

// Create abre um novo chamado (ticket + primeira mensagem do cliente).
// Espelha ajax_ticket_create: dois inserts numa transação.
func (h *SupportHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req createTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// Validações idênticas ao PHP (strlen → len, em bytes).
	if len(req.Assunto) < 5 {
		httpx.WriteErr(w, http.StatusBadRequest, "Assunto muito curto.")
		return
	}
	if len(req.Mensagem) < 10 {
		httpx.WriteErr(w, http.StatusBadRequest, "Mensagem muito curta.")
		return
	}
	categoria := req.Categoria
	if !allowedTicketCategorias[categoria] {
		categoria = "outro"
	}

	autorNome := u.Name
	if autorNome == "" {
		autorNome = "Cliente"
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		slog.Error("[portal_support] erro ao iniciar transação", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	var ticketID int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO sz_portal_tickets
		    (portal_user_id, assunto, categoria, status, prioridade, created_at, updated_at)
		 VALUES ($1, $2, $3, 'aberto', 'normal', NOW(), NOW())
		 RETURNING id`,
		u.ID, req.Assunto, categoria,
	).Scan(&ticketID)
	if err != nil {
		slog.Error("[portal_support] erro ao criar chamado", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao criar ticket.")
		return
	}

	_, err = tx.Exec(r.Context(),
		`INSERT INTO sz_portal_ticket_msgs
		    (ticket_id, autor_tipo, autor_id, autor_nome, mensagem, created_at)
		 VALUES ($1, 'cliente', $2, $3, $4, NOW())`,
		ticketID, u.ID, autorNome, req.Mensagem,
	)
	if err != nil {
		slog.Error("[portal_support] erro ao gravar primeira mensagem", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao criar ticket.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_support] erro ao commit", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao criar ticket.")
		return
	}

	slog.Info("[portal_support] chamado criado", "user_id", u.ID, "ticket_id", ticketID, "categoria", categoria)
	httpx.WriteOK(w, map[string]any{"ticket_id": ticketID})
}

// ── GET /portal/support/tickets/{id} ─────────────────────────────────────────

// Detail retorna o chamado + suas mensagens.
// Ownership embutido na query (id + portal_user_id). Espelha ajax_ticket_msgs.
func (h *SupportHandler) Detail(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	ticketID, ok := parseTicketID(w, r)
	if !ok {
		return
	}

	var t ticketDetail
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id, assunto, categoria, status, prioridade,
		        to_char(created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at,
		        to_char(updated_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS updated_at,
		        to_char(fechado_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS fechado_at
		   FROM sz_portal_tickets
		  WHERE id = $1 AND portal_user_id = $2
		  LIMIT 1`,
		ticketID, u.ID,
	).Scan(&t.ID, &t.Assunto, &t.Categoria, &t.Status, &t.Prioridade,
		&t.CreatedAt, &t.UpdatedAt, &t.FechadoAt)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "Ticket não encontrado.")
		return
	}
	if err != nil {
		slog.Error("[portal_support] erro ao buscar chamado", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, autor_tipo, autor_nome, mensagem,
		        to_char(created_at AT TIME ZONE 'America/Sao_Paulo', 'DD/MM/YYYY HH24:MI') AS created_at
		   FROM sz_portal_ticket_msgs
		  WHERE ticket_id = $1
		  ORDER BY id ASC
		  LIMIT $2`,
		ticketID, listTicketMsgsLimit+1,
	)
	if err != nil {
		slog.Error("[portal_support] erro ao buscar mensagens", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	msgs := []ticketMessage{}
	for rows.Next() {
		var m ticketMessage
		if err := rows.Scan(&m.ID, &m.AutorTipo, &m.AutorNome, &m.Mensagem, &m.CreatedAt); err != nil {
			slog.Error("[portal_support] erro ao ler mensagem", "user_id", u.ID, "ticket_id", ticketID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler mensagens")
			return
		}
		msgs = append(msgs, m)
	}
	if rows.Err() != nil {
		slog.Error("[portal_support] erro após iteração de mensagens", "user_id", u.ID, "ticket_id", ticketID, "err", rows.Err())
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar mensagens")
		return
	}

	// msgs_has_more=true quando veio a mensagem extra → o front sabe que a thread
	// foi truncada (antes a truncagem em 100 era silenciosa). // PERF-list-endpoints-hard-limit
	msgsHasMore := len(msgs) > listTicketMsgsLimit
	if msgsHasMore {
		msgs = msgs[:listTicketMsgsLimit]
	}

	httpx.WriteOK(w, map[string]any{"ticket": t, "msgs": msgs, "msgs_has_more": msgsHasMore, "msgs_limit": listTicketMsgsLimit})
}

// ── POST /portal/support/tickets/{id}/messages ───────────────────────────────

// SendMessage adiciona uma resposta do cliente a um chamado não fechado.
// Espelha ajax_ticket_send_msg: só bumpa o ticket de 'respondido' → 'em_analise'.
func (h *SupportHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	ticketID, ok := parseTicketID(w, r)
	if !ok {
		return
	}

	var req sendTicketMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if len(req.Mensagem) < 3 {
		httpx.WriteErr(w, http.StatusBadRequest, "Mensagem vazia.")
		return
	}

	// Ownership + estado embutidos na query (id + portal_user_id + status != 'fechado').
	var status string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT status FROM sz_portal_tickets
		  WHERE id = $1 AND portal_user_id = $2 AND status != 'fechado'
		  LIMIT 1`,
		ticketID, u.ID,
	).Scan(&status)

	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "Ticket não encontrado ou fechado.")
		return
	}
	if err != nil {
		slog.Error("[portal_support] erro ao buscar chamado p/ resposta", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	autorNome := u.Name
	if autorNome == "" {
		autorNome = "Cliente"
	}

	_, err = h.Pool.Exec(r.Context(),
		`INSERT INTO sz_portal_ticket_msgs
		    (ticket_id, autor_tipo, autor_id, autor_nome, mensagem, created_at)
		 VALUES ($1, 'cliente', $2, $3, $4, NOW())`,
		ticketID, u.ID, autorNome, req.Mensagem,
	)
	if err != nil {
		slog.Error("[portal_support] erro ao gravar resposta", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Só reabre análise se estava 'respondido'. Mensagem comum NÃO bumpa updated_at
	// (paridade: a lista ordenada por updated_at DESC não deve reordenar).
	if status == "respondido" {
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE sz_portal_tickets SET status = 'em_analise', updated_at = NOW() WHERE id = $1`,
			ticketID,
		)
		if err != nil {
			slog.Error("[portal_support] erro ao atualizar status do chamado", "user_id", u.ID, "ticket_id", ticketID, "err", err)
			// Mensagem já gravada — não falha a requisição por causa do status.
		}
	}

	slog.Info("[portal_support] resposta enviada", "user_id", u.ID, "ticket_id", ticketID)
	httpx.WriteOK(w, map[string]any{})
}

// ── POST /portal/support/tickets/{id}/close ──────────────────────────────────

// Close marca o chamado como fechado. Espelha ajax_ticket_close.
func (h *SupportHandler) Close(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	ticketID, ok := parseTicketID(w, r)
	if !ok {
		return
	}

	// Ownership + estado embutidos: só fecha se for do usuário e ainda não fechado.
	// updated_at setado à mão (Postgres não tem ON UPDATE CURRENT_TIMESTAMP).
	result, err := h.Pool.Exec(r.Context(),
		`UPDATE sz_portal_tickets
		    SET status = 'fechado', fechado_at = NOW(), updated_at = NOW()
		  WHERE id = $1 AND portal_user_id = $2 AND status != 'fechado'`,
		ticketID, u.ID,
	)
	if err != nil {
		slog.Error("[portal_support] erro ao fechar chamado", "user_id", u.ID, "ticket_id", ticketID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if result.RowsAffected() == 0 {
		httpx.WriteErr(w, http.StatusNotFound, "Ticket não encontrado.")
		return
	}

	slog.Info("[portal_support] chamado fechado", "user_id", u.ID, "ticket_id", ticketID)
	httpx.WriteOK(w, map[string]any{})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// parseTicketID extrai e valida o {id} da rota. Escreve 400 e retorna ok=false
// se inválido.
func parseTicketID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := chi.URLParam(r, "id")
	ticketID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || ticketID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return ticketID, true
}
