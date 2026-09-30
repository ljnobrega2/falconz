// Package handlers — código de indicação PERMANENTE do usuário no Portal V2 (#71).
//
// Espelha go/affiliates/internal/handlers/referral.go (FEAT-RBAC-2026-06-21), mas
// para o serviço go/portal — que é quem o portal-ui (Affiliates.tsx) consome. O
// go/affiliates é um serviço IRMÃO; o portal nunca falava com ele, então o link
// fixo de indicação não estava exposto na superfície que o front bate.
//
// Rota (namespace /wp-json/senderzz/v1):
//
//	GET /portal/affiliates/referral — (AUTENTICADO) devolve o código de indicação
//	    FIXO/PERMANENTE do usuário da sessão + o link público /r/{code}. Gera-o
//	    lazy no 1º acesso (cobre contas anteriores ao backfill da migração 434).
//
// ── id-space (crítico) ────────────────────────────────────────────────────────
//
//	referral_code é chaveado pelo PORTAL id (senderzz_portal_users.id = u.ID do
//	PortalUser = JWT sub). wp_user_id é NULLABLE (contas Go-nativas) → NUNCA
//	chavear por ele. O WHERE usa a PK `id`. Idêntico ao ensureReferralCode do
//	go/affiliates (migração 433/434).
//
// ── Link fixo /r/{code} ───────────────────────────────────────────────────────
//
//	O link é PERMANENTE: o MESMO usuário sempre tem o MESMO código → o link nunca
//	muda (NÃO exige "gerar"). Base configurável via REFERRAL_PUBLIC_BASE (default
//	falklog.com.br), igual ao referralLinkBase do go/affiliates — o gateway/front
//	mapeia /r/{code} → resolve a indicação. Quem se cadastra por ele vira ASSOCIADO
//	do indicador (referred_by, migração 462); o indicador ganha 2,5% COD / 1% frete
//	(sz_referral_payout, REF-PAYOUT 462). A copy do card de convite no front explica
//	essa regra; o payout em si é do cron, não deste handler.
//
// ── Degradação graciosa ───────────────────────────────────────────────────────
//
//	Se a coluna referral_code ainda não existe no espelho (migração 433 não rodou),
//	devolve 503 PT-BR claro (mesmo padrão dos demais handlers do portal) em vez de
//	500 — via isUndefinedReferralColumn (42703 além do 42P01 de isUndefinedTable).
package handlers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// maxReferralGenAttempts limita as tentativas de geração de código único na
// colisão (improvável com 8 bytes de entropia). Fail-closed após o limite.
// Espelha o mesmo nome/constante do go/affiliates.
const maxReferralGenAttempts = 5

// referralLinkBase é a base pública do link fixo de indicação (#71). Configurável
// via REFERRAL_PUBLIC_BASE p/ não hardcodar o domínio (default falklog.com.br). O
// caminho pretty /r/{code} é mapeado pelo gateway/front. Idêntico ao helper homônimo
// do go/affiliates — mantido local p/ não acoplar os dois serviços.
func referralLinkBase() string {
	base := strings.TrimRight(os.Getenv("REFERRAL_PUBLIC_BASE"), "/")
	if base == "" {
		base = "https://falklog.com.br"
	}
	return base
}

// referralLinkFor monta o link fixo público de indicação a partir do código.
func referralLinkFor(code string) string {
	if code == "" {
		return ""
	}
	return referralLinkBase() + "/r/" + code
}

// ── GET /portal/affiliates/referral (autenticado) ─────────────────────────────

// Referral devolve o código de indicação PERMANENTE do usuário da sessão (#71).
// Gera-o lazy no 1º acesso (cobre contas anteriores ao backfill 434) e persiste em
// senderzz_portal_users.referral_code (chaveado pelo portal id = u.ID).
//
// Idempotente: chamadas seguintes devolvem o mesmo código → o link é FIXO.
func (h *AffiliatesHandler) Referral(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	code, err := h.ensureReferralCode(r.Context(), u.ID)
	if err != nil {
		if isUndefinedReferralColumn(err) {
			// Migração 433 ainda não aplicada — degrada graciosamente (sem 500).
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso de indicação indisponível no momento")
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao gerar código de indicação")
		return
	}

	httpx.WriteOK(w, map[string]any{
		"referral_code": code,
		"referral_link": referralLinkFor(code),
	})
}

// ensureReferralCode lê o código existente do usuário ou gera um novo (lazy),
// persistindo-o de forma atômica. Retorna o código vigente.
//
// Geração: 16 hex (randomHex(8), cabe em VARCHAR(16)). A migração 434 backfilla os
// usuários existentes com um código de 8 chars; este fallback só dispara em base
// SEM trigger/backfill (usuário com referral_code NULL). O UPDATE é condicional
// (referral_code IS NULL) p/ não sobrescrever um código já atribuído por chamada
// concorrente — nesse caso relê o vencedor. Espelha o ensureReferralCode do
// go/affiliates 1:1.
func (h *AffiliatesHandler) ensureReferralCode(ctx context.Context, portalID int64) (string, error) {
	// 1) Já existe?
	existing, err := h.readReferralCode(ctx, portalID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	// 2) Gera lazy com retry em colisão de UNIQUE.
	for attempt := 0; attempt < maxReferralGenAttempts; attempt++ {
		code, gerr := randomHex(8) // 8 bytes = 16 chars hex (cabe em VARCHAR(16))
		if gerr != nil {
			return "", gerr
		}

		var saved string
		err = h.Pool.QueryRow(ctx,
			`UPDATE senderzz_portal_users
			    SET referral_code = $1
			  WHERE id = $2 AND (referral_code IS NULL OR referral_code = '')
			  RETURNING referral_code`,
			code, portalID,
		).Scan(&saved)
		if err == nil {
			return saved, nil
		}
		if errors.Is(err, pgx.ErrNoRows) {
			// Ou o usuário não existe, ou já tem código (corrida) — relê.
			current, rerr := h.readReferralCode(ctx, portalID)
			if rerr != nil {
				return "", rerr
			}
			if current != "" {
				return current, nil
			}
			return "", pgx.ErrNoRows
		}
		if isReferralUniqueViolation(err) {
			continue // colisão do código aleatório → novo código
		}
		return "", err
	}
	return "", errors.New("[referral] esgotadas as tentativas de gerar código único")
}

// readReferralCode retorna o referral_code atual do usuário ("" se NULL).
func (h *AffiliatesHandler) readReferralCode(ctx context.Context, portalID int64) (string, error) {
	var code *string
	err := h.Pool.QueryRow(ctx,
		`SELECT referral_code FROM senderzz_portal_users WHERE id = $1`,
		portalID,
	).Scan(&code)
	if err != nil {
		return "", err
	}
	if code == nil {
		return "", nil
	}
	return *code, nil
}

// ── Helpers de classificação de erro ──────────────────────────────────────────

// isReferralUniqueViolation detecta violação de UNIQUE (SQLSTATE 23505) — colisão
// do referral_code aleatório.
func isReferralUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// isUndefinedReferralColumn detecta coluna (42703) OU tabela (42P01) inexistente —
// usado p/ degradar graciosamente quando a migração 433 ainda não rodou (a coluna
// referral_code não existe). isUndefinedTable (products.go) cobre só 42P01.
func isUndefinedReferralColumn(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42703" || pgErr.Code == "42P01"
	}
	return false
}
