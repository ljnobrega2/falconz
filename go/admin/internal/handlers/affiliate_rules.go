// Package handlers — endpoints admin para regras padrão do programa de afiliados.
// Espelha defaults globais documentados em AUDIT-ADMIN-WP.md §4.7 e §26.4.
// Persistência em senderzz_options (key/value) — mesma tabela do cod_taxas.go.
//
// Options gerenciadas:
//   sz_aff_default_commission_pct        — comissão % padrão (0-100)
//   sz_aff_default_retention_days        — dias de retenção do saldo
//   sz_aff_default_withdraw_fee          — taxa de saque em R$
//   sz_aff_default_penalty_value         — penalidade reincidente (R$)
//   sz_aff_first_frustration_penalty     — penalidade na 1ª frustração (R$)
//   sz_aff_producer_frustration_penalty  — penalidade aplicada ao produtor (R$)
//   sz_aff_auto_approve                  — aprovação automática (bool: "1"/"0")
//   sz_aff_min_withdraw_amount           — valor mínimo de saque (R$)
//   sz_aff_max_withdraw_per_month        — teto mensal de saque (R$, 0 = ilimitado)
package handlers

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// AffiliateRulesHandler agrupa endpoints da tela "Regras de Afiliados".
type AffiliateRulesHandler struct{ Pool *pgxpool.Pool }

// ---------------------------------------------------------------------------
// Helpers — replicam padrão do cod_taxas.go mas isolados para evitar coupling.
// (Helpers privados deste handler têm prefixo arr_ ou métodos no receiver.)
// ---------------------------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
// Graceful degradation: tabela faltando → endpoints respondem com defaults/zeros.
func (h *AffiliateRulesHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// arrParseFloat aceita number ou string vindos do JS; troca vírgula por ponto
// e força >= 0. Usado em todos os campos numéricos do payload.
func arrParseFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return math.Max(0, x)
	case string:
		s := strings.TrimSpace(strings.ReplaceAll(x, ",", "."))
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return math.Max(0, f)
	case int:
		return math.Max(0, float64(x))
	case int64:
		return math.Max(0, float64(x))
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return 0
}

// arrParseBool aceita bool nativo, número (1 = true) ou string ("1"/"true"/"on").
func arrParseBool(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "1" || s == "true" || s == "on" || s == "yes" || s == "sim"
	}
	return false
}

// getOptionFloat lê option float com fallback. Tabela ausente → fallback.
func (h *AffiliateRulesHandler) getOptionFloat(ctx context.Context, key string, def float64) float64 {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	return arrParseFloat(s)
}

// getOptionBool lê option booleana ("1"/"0"/"true"/"false") com fallback.
func (h *AffiliateRulesHandler) getOptionBool(ctx context.Context, key string, def bool) bool {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	if err != nil {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	return arrParseBool(s)
}

// upsertOption grava option (UPSERT em senderzz_options).
// Tabela ausente é silenciada — primeiro POST após migração cria as linhas.
func (h *AffiliateRulesHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// ---------------------------------------------------------------------------
// Payloads
// ---------------------------------------------------------------------------

// AffiliateRules — formato canônico (GET response / POST após normalização).
type AffiliateRules struct {
	DefaultCommissionPct       float64 `json:"default_commission_pct"`
	DefaultRetentionDays       int     `json:"default_retention_days"`
	DefaultWithdrawFee         float64 `json:"default_withdraw_fee"`
	DefaultPenaltyValue        float64 `json:"default_penalty_value"`
	FirstFrustrationPenalty    float64 `json:"first_frustration_penalty"`
	ProducerFrustrationPenalty float64 `json:"producer_frustration_penalty"`
	AutoApprove                bool    `json:"auto_approve"`
	MinWithdrawAmount          float64 `json:"min_withdraw_amount"`
	MaxWithdrawPerMonth        float64 `json:"max_withdraw_per_month"`
}

// rawAffiliateRules aceita string/number/bool do JS (parse normaliza).
type rawAffiliateRules struct {
	DefaultCommissionPct       any `json:"default_commission_pct"`
	DefaultRetentionDays       any `json:"default_retention_days"`
	DefaultWithdrawFee         any `json:"default_withdraw_fee"`
	DefaultPenaltyValue        any `json:"default_penalty_value"`
	FirstFrustrationPenalty    any `json:"first_frustration_penalty"`
	ProducerFrustrationPenalty any `json:"producer_frustration_penalty"`
	AutoApprove                any `json:"auto_approve"`
	MinWithdrawAmount          any `json:"min_withdraw_amount"`
	MaxWithdrawPerMonth        any `json:"max_withdraw_per_month"`
}

// AffiliateRulesStats — KPIs globais do programa de afiliados.
// Join entre senderzz_affiliates (cadastro) e senderzz_affiliate_wallet (saldos).
type AffiliateRulesStats struct {
	TotalAffiliates       int64   `json:"total_affiliates"`
	TotalApproved         int64   `json:"total_approved"`
	TotalPendingApproval  int64   `json:"total_pending_approval"`
	AvgCommissionPct      float64 `json:"avg_commission_pct"`
	TotalBalanceAvailable float64 `json:"total_balance_available"`
	TotalBalancePending   float64 `json:"total_balance_pending"`
}

// ---------------------------------------------------------------------------
// GET /affiliate-rules
// Retorna todos os defaults globais. Valores ausentes caem para os fallbacks
// documentados no cabeçalho do arquivo.
// ---------------------------------------------------------------------------
func (h *AffiliateRulesHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	out := AffiliateRules{
		DefaultCommissionPct:       h.getOptionFloat(ctx, "sz_aff_default_commission_pct", 10.0),
		DefaultRetentionDays:       int(h.getOptionFloat(ctx, "sz_aff_default_retention_days", 7)),
		DefaultWithdrawFee:         h.getOptionFloat(ctx, "sz_aff_default_withdraw_fee", 2.0),
		DefaultPenaltyValue:        h.getOptionFloat(ctx, "sz_aff_default_penalty_value", 5.0),
		FirstFrustrationPenalty:    h.getOptionFloat(ctx, "sz_aff_first_frustration_penalty", 5.0),
		ProducerFrustrationPenalty: h.getOptionFloat(ctx, "sz_aff_producer_frustration_penalty", 8.0),
		AutoApprove:                h.getOptionBool(ctx, "sz_aff_auto_approve", false),
		MinWithdrawAmount:          h.getOptionFloat(ctx, "sz_aff_min_withdraw_amount", 50.0),
		MaxWithdrawPerMonth:        h.getOptionFloat(ctx, "sz_aff_max_withdraw_per_month", 0),
	}
	httpx.JSON(w, 200, out)
}

// ---------------------------------------------------------------------------
// POST /affiliate-rules
// Validação:
//   - Percentual de comissão clampado a 0-100.
//   - Dias >= 0 (inteiro).
//   - Valores monetários >= 0.
//   - auto_approve é bool (qualquer forma textual aceitável).
// UPSERT em senderzz_options para cada chave.
// ---------------------------------------------------------------------------
func (h *AffiliateRulesHandler) Save(w http.ResponseWriter, r *http.Request) {
	var in rawAffiliateRules
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	// Normaliza os campos numéricos via arrParseFloat (já força >= 0).
	pct := arrParseFloat(in.DefaultCommissionPct)
	if pct > 100 {
		pct = 100 // Clamp do percentual de comissão.
	}

	retention := arrParseFloat(in.DefaultRetentionDays)
	if retention < 0 {
		retention = 0
	}
	// Retention é "dias" — armazena como inteiro para evitar fracionário.
	retentionDays := int(math.Round(retention))

	withdrawFee := arrParseFloat(in.DefaultWithdrawFee)
	penaltyDefault := arrParseFloat(in.DefaultPenaltyValue)
	firstFrust := arrParseFloat(in.FirstFrustrationPenalty)
	prodFrust := arrParseFloat(in.ProducerFrustrationPenalty)
	minWithdraw := arrParseFloat(in.MinWithdrawAmount)
	maxWithdraw := arrParseFloat(in.MaxWithdrawPerMonth)

	autoApprove := arrParseBool(in.AutoApprove)

	// Mapa chave/valor (todas as opções gravadas como string no senderzz_options).
	floatPairs := map[string]float64{
		"sz_aff_default_commission_pct":       pct,
		"sz_aff_default_withdraw_fee":         withdrawFee,
		"sz_aff_default_penalty_value":        penaltyDefault,
		"sz_aff_first_frustration_penalty":    firstFrust,
		"sz_aff_producer_frustration_penalty": prodFrust,
		"sz_aff_min_withdraw_amount":          minWithdraw,
		"sz_aff_max_withdraw_per_month":       maxWithdraw,
	}
	for k, v := range floatPairs {
		if err := h.upsertOption(ctx, k, strconv.FormatFloat(v, 'f', 4, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Retention é inteiro — formato sem casas decimais.
	if err := h.upsertOption(ctx, "sz_aff_default_retention_days", strconv.Itoa(retentionDays)); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	// Boolean: convenção "1"/"0" — espelha o que o WP option armazena.
	boolStr := "0"
	if autoApprove {
		boolStr = "1"
	}
	if err := h.upsertOption(ctx, "sz_aff_auto_approve", boolStr); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// GET /affiliate-rules/stats
// KPIs globais do programa. Soma senderzz_affiliate_wallet + COUNT por status
// em senderzz_affiliates. Joins permanecem graceful (tableExists antes da query).
//
// Convenções de status em senderzz_affiliates (vide senderzz-affiliates.php):
//   - 'aprovado'       → ativo (linkado em produto / autorizado a operar)
//   - 'pendente'       → aguardando aprovação manual do produtor
//   - 'recusado'       → rejeitado (não conta como aprovado nem pendente)
// ---------------------------------------------------------------------------
func (h *AffiliateRulesHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AffiliateRulesStats{}

	hasAffiliates := h.tableExists(ctx, "senderzz_affiliates")
	hasWallet := h.tableExists(ctx, "senderzz_affiliate_wallet")

	// --- Contagens de status + média de comissão (vem do cadastro) -----------
	if hasAffiliates {
		// Total geral.
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM senderzz_affiliates`).Scan(&out.TotalAffiliates)

		// Aprovados (status='aprovado') — case-insensitive para tolerar legado.
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM senderzz_affiliates
			 WHERE LOWER(COALESCE(status,'')) = 'aprovado'`).Scan(&out.TotalApproved)

		// Pendentes (status='pendente').
		_ = h.Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM senderzz_affiliates
			 WHERE LOWER(COALESCE(status,'')) = 'pendente'`).Scan(&out.TotalPendingApproval)

		// Média de comissão considerando apenas vínculos com comissao_pct > 0
		// para evitar diluir a média com linhas que herdam o default global.
		// sql.NullFloat64 trata a tabela vazia / AVG nulo sem erro de scan.
		var avg sql.NullFloat64
		_ = h.Pool.QueryRow(ctx,
			`SELECT AVG(comissao_pct)
			 FROM senderzz_affiliates
			 WHERE comissao_pct IS NOT NULL AND comissao_pct > 0`).Scan(&avg)
		if avg.Valid {
			out.AvgCommissionPct = math.Round(avg.Float64*100) / 100 // 2 casas decimais.
		}
	}

	// --- Saldos consolidados (vem da carteira agregada) ----------------------
	if hasWallet {
		_ = h.Pool.QueryRow(ctx,
			`SELECT
				COALESCE(SUM(balance), 0),
				COALESCE(SUM(pending_balance), 0)
			 FROM senderzz_affiliate_wallet`).
			Scan(&out.TotalBalanceAvailable, &out.TotalBalancePending)
	}

	httpx.JSON(w, 200, out)
}
