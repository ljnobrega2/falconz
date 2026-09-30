// Package handlers — handler de Localidades (Áreas de operação) do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /portal/localidades            — lista regiões (CDs) + zonas/cidades atendidas
//	GET /portal/localidades/{id}/zonas — zonas (cidades) de um CD específico
//
// Espelha templates/portal/v2/sections/localidades.php (UI idêntica):
//   - Lista lateral de regiões (CDs ativos) ordenada por nome ASC.
//   - Para cada CD: cobertura de entrega = zonas (cidades) ordenadas por nome ASC.
//   - Header da região mostra apenas nome + uf (endereço NÃO é exposto ao usuário —
//     ver comentário "Endereço da unidade removido" no .php).
//   - Sem ícone circular, sem badge "Armazém Próprio" (já removidos no WP).
//   - Cada cidade indica se há horário de corte configurado (cutoff_horarios != ”)
//     via flag booleana cutoff — o front mostra o ícone de relógio (mirror do .php
//     `! empty( $sz9lo_zona['cutoff_horarios'] )`).
//
// Escopo do usuário:
//
//	As regiões (CDs) e zonas são configuradas GLOBALMENTE pela Senderzz — a tabela
//	sz_motoboy_cds NÃO possui coluna de produtor/usuário (ver schema em
//	includes/motoboy/database.php). O .php exibe TODOS os CDs ativos para qualquer
//	role do portal. Portanto o escopo aqui é: exige sessão de portal autenticada
//	(produtor | afiliado | operator), e o conjunto de dados é o global de CDs ativos.
//	Mesmo comportamento de ProductsHandler.CDs (já user-gated, retorna todos ativos).
//
// Degradação graciosa: se as tabelas sz_motoboy_cds / sz_motoboy_zonas ainda não
// existirem no espelho Postgres (42P01), retorna lista vazia em vez de 500 — o front
// cai no empty-state "Nenhuma área configurada" (mirror do .php).
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// LocalidadesHandler agrupa as dependências dos handlers de localidades.
// Construído exatamente como WebhookHandler/IntegrationsHandler para que o
// integrador faça a fiação de forma idêntica (Pool injetado).
type LocalidadesHandler struct {
	Pool *pgxpool.Pool
}

// listLocalidadesLimit é o teto de CDs na listagem (espelha o LIMIT 100 histórico).
// AUDIT PERF-list-endpoints-hard-limit: a List busca limit+1 para detectar
// truncamento (has_more) sem COUNT extra e devolve só o teto.
const listLocalidadesLimit = 100

// localidadeZona representa uma zona (cidade atendida) de um CD.
// cutoff é a flag booleana derivada de cutoff_horarios != ” — o .php só checa
// `! empty( $sz9lo_zona['cutoff_horarios'] )` para decidir mostrar o ícone de relógio.
type localidadeZona struct {
	ID     int64  `json:"id"`
	Nome   string `json:"nome"`
	Ativo  bool   `json:"ativo"`
	Cutoff bool   `json:"cutoff"` // true = horário de corte configurado (mostra ícone)
	// Dados CRUS p/ o drawer de Localidades mostrar dias/horários reais (não só o
	// ícone). DiasFuncionamento (ex.: "seg-sex") e CutoffHorarios (texto livre,
	// ex.: "12:00") vêm das colunas homônimas de sz_motoboy_zonas. Vazio → "".
	DiasFuncionamento string `json:"dias_funcionamento"`
	CutoffHorarios    string `json:"cutoff_horarios"`
}

// localidadeCD representa uma região (CD) com suas zonas/cidades atendidas.
// Campos espelham o que o .php exibe: nome + uf no header (endereço não exposto),
// cidade é mantida para o data-lo-search do front (busca por nome/cidade/uf).
type localidadeCD struct {
	ID       int64            `json:"id"`
	Nome     string           `json:"nome"`
	Cidade   string           `json:"cidade"`
	UF       string           `json:"uf"`
	Ativo    bool             `json:"ativo"`
	NumZonas int              `json:"num_zonas"`
	Zonas    []localidadeZona `json:"zonas"`
}

// ── GET /portal/localidades ───────────────────────────────────────────────────

// List retorna as regiões (CDs ativos) e, para cada uma, as zonas/cidades atendidas.
// Espelha localidades.php: CDs WHERE ativo = 1 ORDER BY nome ASC LIMIT 100, e para
// cada CD as zonas WHERE cd_id = ? ORDER BY nome ASC.
func (h *LocalidadesHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	// 1) CDs ativos (mirror do .php: WHERE ativo = 1 ORDER BY nome ASC LIMIT 100).
	// N+1: busca 1 a mais que o teto p/ detectar truncamento sem COUNT. // PERF-list-endpoints-hard-limit
	cdRows, err := h.Pool.Query(r.Context(),
		`SELECT id, nome, cidade, uf, ativo
		   FROM sz_motoboy_cds
		  WHERE ativo = TRUE
		  ORDER BY nome ASC
		  LIMIT $1`,
		listLocalidadesLimit+1,
	)
	if err != nil {
		// Tabela ainda não existe no espelho → empty-state (mirror $sz9lo_has_cds = false).
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []localidadeCD{}, "total": 0, "has_more": false, "limit": listLocalidadesLimit})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	cds := []localidadeCD{}
	cdIndex := map[int64]int{} // cd_id → índice em cds (para anexar zonas)
	for cdRows.Next() {
		var c localidadeCD
		if err := cdRows.Scan(&c.ID, &c.Nome, &c.Cidade, &c.UF, &c.Ativo); err != nil {
			cdRows.Close()
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler regiões")
			return
		}
		c.Zonas = []localidadeZona{}
		cdIndex[c.ID] = len(cds)
		cds = append(cds, c)
	}
	if cdRows.Err() != nil {
		cdRows.Close()
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar regiões")
		return
	}
	cdRows.Close()

	// has_more=true quando veio a linha extra → truncagem deixa de ser silenciosa.
	// Corta ANTES de anexar zonas: a query de zonas usa cdIDsOf(cds) (só os CDs do
	// teto), então o CD-sentinela jamais recebe cobertura. // PERF-list-endpoints-hard-limit
	hasMore := len(cds) > listLocalidadesLimit
	if hasMore {
		cds = cds[:listLocalidadesLimit]
	}

	// Sem CDs → empty-state, sem precisar consultar zonas.
	if len(cds) == 0 {
		httpx.WriteOK(w, map[string]any{"data": cds, "total": 0, "has_more": hasMore, "limit": listLocalidadesLimit})
		return
	}

	// 2) Zonas de todos os CDs em uma só query (mirror do loop por CD no .php,
	//    mas sem N+1 — agrupa por cd_id e anexa). ORDER BY cd_id, nome ASC.
	zonaRows, err := h.Pool.Query(r.Context(),
		`SELECT id, cd_id, nome, ativo,
		        (cutoff_horarios IS NOT NULL AND cutoff_horarios <> '') AS cutoff,
		        COALESCE(dias_funcionamento, ''), COALESCE(cutoff_horarios, '')
		   FROM sz_motoboy_zonas
		  WHERE cd_id = ANY($1)
		  ORDER BY cd_id ASC, nome ASC`,
		cdIDsOf(cds),
	)
	if err != nil {
		// Sem tabela de zonas → retorna CDs sem cobertura (mirror $sz9lo_has_zona = false).
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": cds, "total": len(cds), "has_more": hasMore, "limit": listLocalidadesLimit})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	for zonaRows.Next() {
		var z localidadeZona
		var cdID int64
		if err := zonaRows.Scan(&z.ID, &cdID, &z.Nome, &z.Ativo, &z.Cutoff, &z.DiasFuncionamento, &z.CutoffHorarios); err != nil {
			zonaRows.Close()
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler cobertura")
			return
		}
		if idx, ok := cdIndex[cdID]; ok {
			cds[idx].Zonas = append(cds[idx].Zonas, z)
		}
	}
	if zonaRows.Err() != nil {
		zonaRows.Close()
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar cobertura")
		return
	}
	zonaRows.Close()

	for i := range cds {
		cds[i].NumZonas = len(cds[i].Zonas)
	}

	httpx.WriteOK(w, map[string]any{"data": cds, "total": len(cds), "has_more": hasMore, "limit": listLocalidadesLimit})
}

// ── GET /portal/localidades/{id}/zonas ────────────────────────────────────────

// Zonas retorna as zonas (cidades atendidas) de um CD específico.
// Mirror do segundo bloco do .php (zonas WHERE cd_id = %d ORDER BY nome ASC).
// Endpoint auxiliar para o front recarregar a cobertura de um CD isoladamente.
func (h *LocalidadesHandler) Zonas(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	cdID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || cdID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "id inválido")
		return
	}

	// Garante que o CD existe e está ativo — mirror do .php que só lista CDs ativos.
	var exists bool
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM sz_motoboy_cds WHERE id = $1 AND ativo = TRUE)`,
		cdID,
	).Scan(&exists); err != nil {
		if isUndefinedTable(err) {
			httpx.WriteErr(w, http.StatusNotFound, "região não encontrada")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if !exists {
		httpx.WriteErr(w, http.StatusNotFound, "região não encontrada")
		return
	}

	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, nome, ativo,
		        (cutoff_horarios IS NOT NULL AND cutoff_horarios <> '') AS cutoff,
		        COALESCE(dias_funcionamento, ''), COALESCE(cutoff_horarios, '')
		   FROM sz_motoboy_zonas
		  WHERE cd_id = $1
		  ORDER BY nome ASC`,
		cdID,
	)
	if err != nil {
		if isUndefinedTable(err) {
			httpx.WriteOK(w, map[string]any{"data": []localidadeZona{}, "total": 0})
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()

	out := []localidadeZona{}
	for rows.Next() {
		var z localidadeZona
		if err := rows.Scan(&z.ID, &z.Nome, &z.Ativo, &z.Cutoff, &z.DiasFuncionamento, &z.CutoffHorarios); err != nil {
			httpx.WriteErr(w, http.StatusInternalServerError, "erro ao ler cobertura")
			return
		}
		out = append(out, z)
	}
	if rows.Err() != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao processar cobertura")
		return
	}

	httpx.WriteOK(w, map[string]any{"data": out, "total": len(out)})
}

// cdIDsOf extrai os ids dos CDs para o filtro cd_id = ANY($1) da query de zonas.
func cdIDsOf(cds []localidadeCD) []int64 {
	ids := make([]int64, 0, len(cds))
	for _, c := range cds {
		ids = append(ids, c.ID)
	}
	return ids
}
