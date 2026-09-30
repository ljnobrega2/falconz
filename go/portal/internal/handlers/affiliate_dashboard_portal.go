// Package handlers — Dashboard de Comissões do AFILIADO (Portal V2).  // FEAT-PORTAL-SALES
//
// Atende a UX-AUDIT (2.3 Afiliado, ADD P0): "Dashboard do afiliado dedicado com
// aba PADRÃO 'Minhas comissões'": KPIs A receber, A liberar (pendente), Previsto,
// Recebido no mês, Pedidos válidos, Taxa de frustração. A auditoria notou que o
// dado "já existe na API" — este handler AGREGA esse dado num único payload, em vez
// de o front montar a partir de várias chamadas (wallet/summary + orders).
//
// Rota (namespace /wp-json/senderzz/v1):
//
//	GET /portal/affiliates/dashboard  — KPIs de comissão do afiliado logado
//
// ── ESCOPO / id-space (porte fiel — fail-closed) ──────────────────────────────
//
//	É EXCLUSIVO do afiliado (role afiliado). Produtor/operator → 403 (eles têm o
//	seu próprio dashboard de receita; esta tela fala a língua "comissão").
//
//	Duas fontes, escopadas de forma ESTRITA e canônica (idêntico a wallet.go /
//	fees.go / affiliates_portal.go — NUNCA OR/IN com portal id):
//	  1. Ledger de comissão (senderzz_affiliate_transactions) JOIN
//	     senderzz_affiliates a ON a.id = tx.affiliate_id WHERE a.afiliado_id = wp_user_id.
//	     - A receber  (available) = SUM(amount) WHERE status='approved'
//	     - A liberar  (pending)   = SUM(amount) WHERE status='pending'   (= Previsto)
//	     - Recebido no mês        = SUM(amount) WHERE status IN ('approved','paid')
//	                                AND created_at no mês corrente
//	     - Penalidades (débito)   = SUM(amount) WHERE type='penalty'   (audit 2.3 P0:
//	                                "card de Débito — é dinheiro real")
//	     (statuses espelham wallet.go: 'approved'=disponível, 'pending'=a liberar.)
//
//	     FONTE ÚNICA DE VERDADE (anti "segunda fonte" — audit 3.2): A receber/A liberar
//	     usam EXATAMENTE a query do wallet.go Summary (afiliado) — SEM filtro de type.
//	     Provado divergir (afiliado 28: penalty approved R$39 entrava no available do
//	     wallet mas era zerado aqui com type='commission'). Removido o filtro: os dois
//	     endpoints imprimem o MESMO "a receber". O débito é exposto à parte (penalidades),
//	     não subtraído, para o front mostrar o sinal correto.
//	  2. Pedidos atribuídos ao afiliado (sz_orders) WHERE affiliate_id = wp_user_id:
//	     - Pedidos válidos        = COUNT WHERE status NÃO em {frustrado/cancelado/...}
//	     - Frustrados             = COUNT WHERE status em {frustrado/cancelado/...}
//	     - Taxa de frustração     = frustrados / total (0 quando total=0)
//
//	SEGURANÇA: o afiliado SÓ vê os próprios números (escopo afiliado_id/affiliate_id
//	= wp_user_id). Nenhum dado de produtor ou de outro afiliado é exposto. Degrada a
//	zeros (não 500) se uma tabela/coluna do espelho faltar — a tela renderiza.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// AffiliateDashboardHandler agrupa as dependências do dashboard de comissões.
// Construção idêntica aos demais handlers do portal (somente Pool).
type AffiliateDashboardHandler struct {
	Pool *pgxpool.Pool
}

// affiliateDashboard — payload de KPIs do afiliado (casa com a aba "Minhas comissões").
type affiliateDashboard struct {
	AReceber         float64 `json:"a_receber"`         // disponível (status approved) — = wallet.go available
	APending         float64 `json:"a_liberar"`         // pendente a liberar (status pending) — = wallet.go pending
	Previsto         float64 `json:"previsto"`          // = a_liberar (recebível previsto)
	RecebidoNoMes    float64 `json:"recebido_no_mes"`   // creditado no mês corrente (approved|paid)
	PenalidadesTotal float64 `json:"penalidades_total"` // débito do afiliado (type='penalty') — audit 2.3 P0
	PedidosValidos   int64   `json:"pedidos_validos"`   // pedidos atribuídos não-frustrados
	Frustrados       int64   `json:"frustrados"`        // pedidos atribuídos frustrados/cancelados
	TotalPedidos     int64   `json:"total_pedidos"`     // total atribuído (válidos + frustrados)
	TaxaFrustracao   float64 `json:"taxa_frustracao"`   // frustrados / total (0..1)
}

// ── GET /portal/affiliates/dashboard ──────────────────────────────────────────

// Dashboard agrega os KPIs de comissão do afiliado logado num único payload.
func (h *AffiliateDashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	// Exclusivo do afiliado — produtor/operator têm dashboard próprio (receita).
	// Usa isAffiliate (só 'afiliado'), o MESMO predicado de wallet.go — sem isso, um
	// role 'affiliate' pegaria a carteira COD no wallet mas o dashboard aqui (divergência).
	if !isAffiliate(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "Dashboard de comissões disponível apenas para afiliados.")
		return
	}
	ctx := r.Context()

	out := affiliateDashboard{}

	// ── Fonte 1: ledger de comissão (escopo por vínculo: a.afiliado_id = wp_user_id).
	h.aggregateLedger(ctx, u.WPUserID, &out)

	// ── Fonte 2: pedidos atribuídos (escopo estrito: o.affiliate_id = wp_user_id).
	h.aggregateOrders(ctx, u.WPUserID, &out)

	// Previsto = a liberar (recebível pendente). Mantido como campo próprio p/ o
	// front exibir o rótulo "Previsto" sem recomputar.
	out.Previsto = out.APending

	httpx.WriteOK(w, map[string]any{"dashboard": out})
}

// aggregateLedger soma a comissão por status do ledger do afiliado (via vínculo).
// Degrada a zeros (não derruba a resposta) se a tabela/coluna faltar.
func (h *AffiliateDashboardHandler) aggregateLedger(ctx context.Context, wpUserID int64, out *affiliateDashboard) {
	if wpUserID == 0 {
		return
	}
	// 'approved' = disponível, 'pending' = a liberar (mesma convenção de wallet.go).
	// CRIT-A (AUDIT-CRIT-AB): a_receber subtrai penalty (despesa gravada positiva no PHP)
	// como -ABS(amount) + piso GREATEST(0,...) — idêntico ao wallet.go Summary (fonte única
	// de verdade), senão o penalty approved inflava o disponível (afiliado wp28: R$51→R$0).
	// recebido_no_mes e penalidades SÃO discriminados por type (KPIs informativos à parte;
	// PenalidadesTotal segue exibido POSITIVO como display informativo).
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	_ = h.Pool.QueryRow(ctx,
		`SELECT
		    GREATEST(0, COALESCE(SUM(CASE
		        WHEN tx.status = 'approved' AND tx.type = 'penalty' THEN -ABS(tx.amount)
		        WHEN tx.status = 'approved' THEN tx.amount
		        ELSE 0 END), 0))::float8,
		    COALESCE(SUM(CASE WHEN tx.status = 'pending'  THEN tx.amount ELSE 0 END), 0)::float8,
		    COALESCE(SUM(CASE WHEN tx.status IN ('approved','paid')
		                       AND tx.type = 'commission'
		                       AND tx.created_at >= $2 THEN tx.amount ELSE 0 END), 0)::float8,
		    COALESCE(SUM(CASE WHEN tx.type = 'penalty' THEN tx.amount ELSE 0 END), 0)::float8
		   FROM senderzz_affiliate_transactions tx
		   JOIN senderzz_affiliates a ON a.id = tx.affiliate_id
		  WHERE a.afiliado_id = $1`,
		wpUserID, monthStart,
	).Scan(&out.AReceber, &out.APending, &out.RecebidoNoMes, &out.PenalidadesTotal)
}

// aggregateOrders conta pedidos atribuídos válidos vs frustrados e a taxa.
// Escopo estrito o.affiliate_id = wp_user_id. Degrada a zeros se a coluna faltar.
func (h *AffiliateDashboardHandler) aggregateOrders(ctx context.Context, wpUserID int64, out *affiliateDashboard) {
	if wpUserID == 0 {
		return
	}
	var frustrados, validos int64
	err := h.Pool.QueryRow(ctx,
		`SELECT
		    COALESCE(SUM(CASE WHEN o.status IN ('frustrado','cancelled','cancelado','reembolsado','refunded')
		                      THEN 1 ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN o.status IN ('frustrado','cancelled','cancelado','reembolsado','refunded')
		                      THEN 0 ELSE 1 END), 0)
		   FROM sz_orders o
		  WHERE o.affiliate_id = $1`,
		wpUserID,
	).Scan(&frustrados, &validos)
	if err != nil {
		return
	}
	out.Frustrados = frustrados
	out.PedidosValidos = validos
	out.TotalPedidos = frustrados + validos
	if out.TotalPedidos > 0 {
		out.TaxaFrustracao = round2(float64(frustrados) / float64(out.TotalPedidos))
	}
}
