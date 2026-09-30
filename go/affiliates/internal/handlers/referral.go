// Package handlers — código de indicação PERMANENTE por usuário (referral).
//
// # FEAT-RBAC-2026-06-21
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET /affiliates/referral        — (AUTENTICADO) retorna o código de indicação
//	                                  fixo do usuário da sessão; gera-o lazy no 1º acesso.
//	GET /affiliates/referral/{code} — (PÚBLICO, sem JWT) resolve um código → dono
//	                                  da indicação (campos MÍNIMOS p/ atribuição) + a
//	                                  estrutura de recompensa (somente leitura, gated).
//	                                  Sob /affiliates/ para casar o gateway FALK.
//
// ── id-space (crítico) ────────────────────────────────────────────────────────
//
//	referral_code é chaveado pelo PORTAL id (senderzz_portal_users.id), que é o
//	mesmo id carregado no claim "sub" do JWT (auth.PortalUser.ID). Cadeia VERIFICADA
//	na stack FALK (pura-Go, único emissor de tokens deste serviço):
//	  go/portal handlers/auth.go::Login → SELECT id FROM senderzz_portal_users
//	  → auth/jwt.go::EmitJWT põe Subject/sub = id (portal id)
//	  → affiliates auth.go injeta sub em PortalUser.ID.
//	wp_user_id é NULLABLE (contas Go-nativas), por isso NUNCA chavear por ele.
//
//	Este handler faz lookup de IDENTIDADE direto em senderzz_portal_users pela sua
//	PK (id) — NÃO toca senderzz_affiliates. Por isso a questão de id-space das
//	colunas produtor_id/afiliado_id (do vínculo) é ORTOGONAL e fica fora de escopo.
//	O resolver devolve portal_user_id (id) E wp_user_id (nullable) só para o
//	chamador ter ambos à mão; QUAL id-space um futuro "redeem" de indicação usará
//	é decisão daquele fluxo, não pré-julgada aqui.
//
// ── Privacidade / LGPD ────────────────────────────────────────────────────────
//
//	O resolver é PÚBLICO e é uma superfície de enumeração. Por isso:
//	  - código hex de 8-16 chars (md5 upper do trigger 434, ou 16-hex legado do
//	    caminho lazy do Go) → validado por isHexCode antes de tocar o banco;
//	  - retorna SOMENTE nome, role e ids de atribuição. NUNCA e-mail/telefone/PII.
//
// ── Financeiro (recompensa) — GATED ───────────────────────────────────────────
//
//	A recompensa de indicação é LIDA das options sz_invite_reward_* (estrutura e
//	leitura apenas). NÃO há payout automático aqui — qualquer crédito é decisão de
//	negócio do dono e fica gated (ver inviteRewardConfig). Hard-rule do projeto:
//	não inventar semântica financeira — deixar configurável/documentado.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/affiliates-service/internal/auth"
	"github.com/senderzz/affiliates-service/internal/httpx"
)

// maxReferralGenAttempts limita as tentativas de geração de código único na
// colisão (improvável com 8 bytes de entropia). Fail-closed após o limite.
const maxReferralGenAttempts = 5

// referralLinkBase é a base pública do link fixo de indicação. Configurável via
// REFERRAL_PUBLIC_BASE p/ não hardcodar o domínio (default falklog.com.br). O
// caminho pretty /r/{code} é mapeado pelo gateway/front → este serviço resolve
// via GET /wp-json/senderzz/v1/affiliates/referral/{code} (sob /affiliates/ p/
// casar o roteamento do gateway FALK).
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

// ── GET /affiliates/referral (autenticado) ────────────────────────────────────

// Referral retorna o código de indicação PERMANENTE do usuário da sessão.
// Gera-o lazy (crypto/rand, 16 hex) no 1º acesso e persiste em
// senderzz_portal_users.referral_code (chaveado pelo portal id = user.ID).
//
// Idempotente: chamadas seguintes devolvem o mesmo código.
func (h *AffiliatesHandler) Referral(w http.ResponseWriter, r *http.Request) {
	user := auth.PortalUserFromCtx(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	code, err := h.ensureReferralCode(r.Context(), user.ID)
	if err != nil {
		if isUndefinedColumnOrTable(err) {
			// Migração 433 ainda não aplicada — degrada graciosamente (sem 500).
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso de indicação indisponível no momento")
			return
		}
		slog.Error("[referral] falha ao garantir código de indicação", "user_id", user.ID, "err", err)
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
// id-space: portalID = auth.PortalUser.ID = JWT sub = senderzz_portal_users.id
// (cadeia Login→EmitJWT verificada — ver doc do pacote). Por isso o WHERE usa a
// PK `id`. NÃO "alinhar" para wp_user_id como em Request/List (aquilo opera em
// senderzz_affiliates, outro id-space) — wp_user_id é NULLABLE e quebraria contas
// Go-nativas.
//
// Geração: tenta gravar um código de 16 hex; em colisão de UNIQUE (improvável),
// re-tenta até maxReferralGenAttempts. A escrita usa um UPDATE condicional
// (referral_code IS NULL) para não sobrescrever um código já atribuído por uma
// chamada concorrente — nesse caso relê o valor vencedor.
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
		code, err := randomHex(8) // 8 bytes = 16 chars hex (cabe em VARCHAR(16))
		if err != nil {
			return "", err
		}

		var saved string
		// UPDATE condicional: só grava se ainda estiver NULL. RETURNING devolve o
		// código gravado por ESTA query; se outra goroutine venceu a corrida, o
		// WHERE não casa (0 linhas) e relemos o vencedor abaixo.
		err = h.Pool.QueryRow(ctx,
			`UPDATE senderzz_portal_users
			    SET referral_code = $1
			  WHERE id = $2 AND referral_code IS NULL
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
			// Usuário inexistente: nada a fazer (não deveria ocorrer com JWT válido).
			return "", pgx.ErrNoRows
		}
		// Colisão de UNIQUE no código aleatório → tenta de novo com novo código.
		if isUniqueViolation(err) {
			continue
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

// ── GET /affiliates/referral/{code} (público — SEM JWT) ───────────────────────

// ResolveReferral resolve um código de indicação para o seu dono.
//
// PÚBLICO: clicado por um prospect deslogado. NÃO carrega jwtAuth.
//
// Retorno (campos MÍNIMOS — privacidade/LGPD):
//
//	owner: { portal_user_id, wp_user_id (nullable), nome, role }
//	reward: estrutura de recompensa lida das options (somente leitura, gated)
//
// NUNCA retorna e-mail/telefone/qualquer PII além do nome público.
func (h *AffiliatesHandler) ResolveReferral(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	// Validação barata de formato (8-16 hex) antes de tocar o banco — também evita
	// que entradas absurdas virem queries.
	//
	// COMPRIMENTO VARIÁVEL (correção): os códigos REAIS são de 8 chars — o trigger
	// 434 (sz_gen_referral_code, BEFORE INSERT) gera upper(md5(...),8) sempre que
	// referral_code vem NULL, e o backfill da 434 cobre os usuários existentes. Como
	// o trigger dispara em TODO INSERT, o caminho lazy do Go (randomHex(8)=16 hex)
	// nunca casa o WHERE referral_code IS NULL → 100% dos códigos no banco têm 8
	// chars. A regra anterior (exatamente 16) rejeitava TODO código real. Aceitamos
	// 8-16 hex para cobrir os 8-char reais e o eventual 16-char legado.
	if !isHexCode(code) {
		httpx.WriteErr(w, http.StatusNotFound, "indicação não encontrada")
		return
	}

	ctx := r.Context()

	var (
		portalID int64
		wpUserID *int64
		nome     string
		role     string
	)
	err := h.Pool.QueryRow(ctx,
		`SELECT id, wp_user_id, COALESCE(nome,''), COALESCE(role,'')
		   FROM senderzz_portal_users
		  WHERE referral_code = $1
		    AND ativo = TRUE`,
		code,
	).Scan(&portalID, &wpUserID, &nome, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		// Código inexistente OU dono inativo → mesma resposta (não vaza estado).
		httpx.WriteErr(w, http.StatusNotFound, "indicação não encontrada")
		return
	}
	if err != nil {
		if isUndefinedColumnOrTable(err) {
			httpx.WriteErr(w, http.StatusServiceUnavailable, "recurso de indicação indisponível no momento")
			return
		}
		slog.Error("[referral] falha ao resolver código", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao resolver indicação")
		return
	}

	reward := h.inviteRewardConfig(ctx)

	httpx.WriteOK(w, map[string]any{
		"referral_code": code,
		"owner": map[string]any{
			"portal_user_id": portalID, // PK senderzz_portal_users.id (chave do referral_code)
			"wp_user_id":     wpUserID, // nullable: NULL p/ contas Go-nativas
			"nome":           nome,
			"role":           role,
		},
		"reward": reward,
	})
}

// ── Recompensa de indicação (somente leitura, GATED) ──────────────────────────

// inviteRewardConfig lê a estrutura de recompensa de indicação das options
// senderzz_options. FEAT-RBAC-2026-06-21.
//
// HARD-RULE financeira: NÃO inventamos semântica. Esta função apenas LÊ a config
// e a expõe — nenhum crédito/payout é executado aqui (campo "enabled"=false
// sinaliza que o pagamento é gated/decisão do dono).
//
// Reconciliação de nomes (documentado, não chutado): o admin-ui já commitou
// sz_invite_reward_type / sz_invite_reward_value (vocab none|fixed|percent),
// enquanto a spec da tarefa cita sz_invite_reward_tipo / sz_invite_reward_valor.
// Nenhuma chave está persistida ainda (backend gap). Lemos AMBAS as grafias e
// preferimos _type/_value (a do front, que é o único escritor commitado).
func (h *AffiliatesHandler) inviteRewardConfig(ctx context.Context) map[string]any {
	tipo := firstNonEmptyOption(ctx, h.Pool, "sz_invite_reward_type", "sz_invite_reward_tipo")
	valor := firstNonEmptyOption(ctx, h.Pool, "sz_invite_reward_value", "sz_invite_reward_valor")

	tipo = strings.ToLower(strings.TrimSpace(tipo))
	switch tipo {
	case "fixed", "percent":
		// ok — tipos de recompensa válidos
	default:
		tipo = "none"
	}

	return map[string]any{
		"type":  tipo,
		"value": valor, // string crua da option; consumidor formata/valida
		// payout automático NÃO implementado — recompensa gated (decisão do dono).
		"enabled": false,
	}
}

// firstNonEmptyOption lê várias chaves de senderzz_options e retorna o 1º valor
// não-vazio (na ordem informada). Resiliente a tabela ausente (retorna "").
func firstNonEmptyOption(ctx context.Context, pool *pgxpool.Pool, keys ...string) string {
	for _, k := range keys {
		var v *string
		err := pool.QueryRow(ctx,
			`SELECT value FROM senderzz_options WHERE name = $1`, k,
		).Scan(&v)
		if err != nil {
			// Sem linha / tabela ausente → tenta a próxima chave.
			continue
		}
		if v != nil && strings.TrimSpace(*v) != "" {
			return *v
		}
	}
	return ""
}

// ── Helpers de classificação de erro / validação ──────────────────────────────

// isUniqueViolation detecta violação de UNIQUE (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// isUndefinedColumnOrTable detecta coluna (42703) ou tabela (42P01) inexistente
// — usado para degradar graciosamente quando a migração 433 ainda não rodou.
func isUndefinedColumnOrTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42703" || pgErr.Code == "42P01"
	}
	return false
}

// reReferralCode valida o formato do referral_code: hex de comprimento variável,
// 8 a 16 chars. Os códigos reais têm 8 (md5 upper, trigger 434); o teto de 16
// cobre o eventual código legado de 16 hex (randomHex(8) do caminho lazy do Go).
var reReferralCode = regexp.MustCompile(`^[0-9A-Fa-f]{8,16}$`)

// isHexCode valida que s é hex de 8-16 chars (formato do referral_code). Substitui
// o antigo isHex16 (exatamente 16), que rejeitava TODO código real de 8 chars.
func isHexCode(s string) bool {
	return reReferralCode.MatchString(s)
}
