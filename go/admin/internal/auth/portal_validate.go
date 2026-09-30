// Package auth — validador de TOKEN DE PORTAL no go/admin + middleware DualAuth.
//
// FEAT-RBAC-ORDERS-2026-06-24 (login único, telas de pedidos compartilhadas).
//
// CONTEXTO: o login único faz o go/admin EMITIR tokens de portal (portal_login.go:
// IssuePortalToken / IssuePortalSession, iss=senderzz-portal, sessão em
// senderzz_portal_sessions, assinatura com JWT_SECRET). O dono quer que produtor e
// afiliado usem a MESMA tela de pedidos do admin (batendo em /orders, NÃO em
// /portal/orders), com o BACKEND escopando pelo dono — nunca por input do cliente.
//
// Este arquivo é o lado de VALIDAÇÃO que faltava: o go/admin sabia emitir tokens de
// portal mas não os validava (auth.Middleware exige iss=senderzz-admin e rejeita
// tokens de portal de propósito — SEC-GO-04). Aqui espelhamos FIELMENTE o
// AuthPortalJWT do go/portal:
//   - assinatura HS256 com JWT_SECRET (NÃO ADMIN_JWT_SECRET — tokens de portal são
//     assinados com JWT_SECRET);
//   - iss=senderzz-portal (rejeita iss=senderzz-admin / vazio);
//   - exp obrigatório; rejeita token parcial (pending_2fa=true);
//   - validação da SESSÃO: claim "sid" → senderzz_portal_sessions (token raw|hmac,
//     expires_at>NOW(), status='active'). Sem isso o JWT de portal seria um token
//     MORTO — exatamente o invariante do go/portal.
//
// DualAuth aceita ADMIN token OU PORTAL token e injeta um Actor com a IDENTIDADE
// AUTENTICADA. NUNCA modificar auth.Middleware para aceitar os dois: isso abriria
// TODAS as rotas admin (settings, saques, …) a tokens de portal = privesc. DualAuth
// é montado SÓ nas 4 rotas GET compartilhadas (ver main.go).
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ActorKind identifica a natureza do chamador autenticado.
type ActorKind string

const (
	ActorAdmin    ActorKind = "admin"    // iss=senderzz-admin (super-privilegiado, vê tudo)
	ActorProdutor ActorKind = "produtor" // portal role produtor — escopo o.produtor_id = PortalUserID
	ActorAfiliado ActorKind = "afiliado" // portal role afiliado — escopo o.affiliate_id = WPUserID
)

// Actor é a IDENTIDADE AUTENTICADA resolvida por DualAuth. O escopo por papel nos
// handlers DEVE derivar SEMPRE deste objeto (nunca de query/body do cliente).
type Actor struct {
	Kind ActorKind
	// PortalUserID = senderzz_portal_users.id — chave de atribuição do PRODUTOR
	// (sz_orders.produtor_id). 0 quando Kind=admin.
	PortalUserID int64
	// WPUserID = senderzz_portal_users.wp_user_id — chave de atribuição do AFILIADO
	// (sz_orders.affiliate_id, que guarda o wp_user_id). 0 quando Kind=admin.
	WPUserID int64
	// Role cru do portal (produtor|afiliado|operator|cliente). Vazio quando admin.
	Role string
}

type actorCtxKey struct{}

// ActorFromCtx extrai o Actor autenticado (nil se a rota não passou por DualAuth).
func ActorFromCtx(ctx context.Context) *Actor {
	a, _ := ctx.Value(actorCtxKey{}).(*Actor)
	return a
}

// portalSessionClaims — claims do JWT completo de portal (espelho de PortalClaims
// do go/portal). Só os campos consumidos aqui.
type portalSessionClaims struct {
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	SessionToken string `json:"sid"`
	PendingTwoFA bool   `json:"pending_2fa,omitempty"`
	jwt.RegisteredClaims
}

// hashPortalToken reproduz o HMAC-SHA256(token_raw, WP_SALT_AUTH) do go/portal
// (session.go::hashToken) — usado para casar o "sid" raw contra token_hmac no banco.
func hashPortalToken(token string) string {
	salt := os.Getenv("WP_SALT_AUTH")
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// errPortalConfig sinaliza configuração ausente (fail-closed → 503, não 401).
var errPortalConfig = errors.New("portal auth não configurado")

// validatePortalToken valida um Bearer JWT de PORTAL e resolve o PortalUser do banco.
//
// Espelha AuthPortalJWT do go/portal: HS256 + JWT_SECRET, iss=senderzz-portal, exp
// obrigatório, rejeita token parcial, e valida a SESSÃO (sid → senderzz_portal_sessions).
//
// Retornos:
//   - (*Actor, nil)           → token de portal válido e sessão viva.
//   - (nil, errPortalConfig)  → JWT_SECRET/WP_SALT_AUTH ausentes (chamador → 503).
//   - (nil, err)              → token inválido / sessão morta (chamador → 401).
func validatePortalToken(ctx context.Context, pool *pgxpool.Pool, tokStr string) (*Actor, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, errPortalConfig
	}
	// WP_SALT_AUTH é necessário para casar sessões PHP legadas (token_hmac).
	if os.Getenv("WP_SALT_AUTH") == "" {
		return nil, errPortalConfig
	}

	tok, err := jwt.ParseWithClaims(tokStr, &portalSessionClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("alg inesperado")
		}
		return []byte(secret), nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		// iss=senderzz-portal: rejeita tokens admin (iss=senderzz-admin) e qualquer
		// outro issuer — espelha a separação de espaços de id do portal_login.go.
		jwt.WithIssuer("senderzz-portal"),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid {
		return nil, errors.New("token de portal inválido")
	}
	cl, ok := tok.Claims.(*portalSessionClaims)
	if !ok {
		return nil, errors.New("claims de portal inválidas")
	}
	// Token parcial (2FA pendente) NÃO autentica rotas protegidas (espelha ParseJWT).
	if cl.PendingTwoFA {
		return nil, errors.New("token parcial (2FA pendente)")
	}

	// Validação da sessão: claim "sid" (raw) → senderzz_portal_sessions. Aceita raw
	// OU hmac (compat com sessões PHP legadas, idêntico ao go/portal). status='active'
	// e expires_at>NOW(). Resolve id (PK portal) + wp_user_id + role.
	sessionRaw := cl.SessionToken
	sessionHMAC := hashPortalToken(sessionRaw)

	var portalUserID, wpUserID int64
	var role string
	err = pool.QueryRow(ctx, `
		SELECT u.id, COALESCE(u.wp_user_id, 0), u.role
		  FROM senderzz_portal_sessions s
		  JOIN senderzz_portal_users u ON u.id = s.user_id
		 WHERE s.token IN ($1, $2)
		   AND s.expires_at > NOW()
		   AND u.status = 'active'
		 LIMIT 1`,
		sessionRaw, sessionHMAC,
	).Scan(&portalUserID, &wpUserID, &role)
	if err != nil {
		return nil, errors.New("sessão de portal inválida ou expirada")
	}

	kind := ActorProdutor
	switch {
	case role == "produtor" || role == "producer":
		kind = ActorProdutor
	case role == "afiliado" || role == "affiliate" || role == "afiliada" || role == "cliente":
		kind = ActorAfiliado
	default:
		// operator/operador e demais roles: sem escopo definido nestas telas →
		// fail-closed (handler devolve lista vazia / 403). Marcamos como produtor
		// NÃO seria seguro; usamos um Kind que os handlers tratam como "sem escopo".
		kind = ActorKind(role)
	}

	return &Actor{
		Kind:         kind,
		PortalUserID: portalUserID,
		WPUserID:     wpUserID,
		Role:         role,
	}, nil
}

// DualAuth é o middleware das rotas GET de pedidos COMPARTILHADAS (admin-ui usada por
// admin/produtor/afiliado). Aceita ADMIN token OU PORTAL token:
//
//   - iss=senderzz-admin → caminho IDÊNTICO ao auth.Middleware (injeta *Admin no ctx
//     para preservar o comportamento atual: PII logging, FromCtx, etc.) + Actor{admin}.
//   - iss=senderzz-portal → valida assinatura/sessão e injeta Actor{produtor|afiliado}.
//
// A decisão admin-vs-portal é pelo ISSUER do token (não por input). O handler escopa
// pela identidade do Actor, NUNCA por query/body do cliente.
//
// NUNCA reusar isto nas rotas admin genéricas — só nas 4 GET compartilhadas.
func DualAuth(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeErr(w, http.StatusUnauthorized, "token ausente")
				return
			}
			tokStr := strings.TrimPrefix(h, "Bearer ")

			// 1) Tenta ADMIN (iss=senderzz-admin, assinado com secret() = ADMIN_JWT_SECRET
			//    com fallback JWT_SECRET). Caminho IDÊNTICO ao auth.Middleware.
			adminTok, adminErr := jwt.ParseWithClaims(tokStr, &Claims{}, func(t *jwt.Token) (any, error) {
				if t.Method.Alg() != "HS256" {
					return nil, errors.New("alg inválido")
				}
				return secret(), nil
			},
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithIssuer("senderzz-admin"),
				jwt.WithExpirationRequired(),
			)
			if adminErr == nil && adminTok.Valid {
				cl := adminTok.Claims.(*Claims)
				var a Admin
				qerr := pool.QueryRow(r.Context(),
					`SELECT id, email, nome FROM senderzz_admin_users WHERE id=$1 AND ativo=TRUE`, cl.AdminID).
					Scan(&a.ID, &a.Email, &a.Nome)
				if qerr != nil {
					writeErr(w, http.StatusUnauthorized, "admin inválido")
					return
				}
				ctx := context.WithValue(r.Context(), adminKey, &a)
				ctx = context.WithValue(ctx, actorCtxKey{}, &Actor{Kind: ActorAdmin})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 2) Tenta PORTAL (iss=senderzz-portal, assinado com JWT_SECRET + sessão viva).
			actor, perr := validatePortalToken(r.Context(), pool, tokStr)
			if perr != nil {
				if errors.Is(perr, errPortalConfig) {
					// Configuração ausente é erro de infra → 503 (fail-closed, não 401).
					writeErr(w, http.StatusServiceUnavailable, "serviço temporariamente indisponível")
					return
				}
				writeErr(w, http.StatusUnauthorized, "token inválido")
				return
			}
			ctx := context.WithValue(r.Context(), actorCtxKey{}, actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
