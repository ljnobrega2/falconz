// Package contract define os tipos de resposta compartilhados entre os serviços Go.
// Mantém compatibilidade com o contrato da API PHP existente.
package contract

// Response é o envelope padrão de resposta da API Senderzz.
// Equivale ao padrão {"ok": bool, "erro": "..."} / {"ok": true, ...} do PHP.
type Response struct {
	OK      bool   `json:"ok"`
	Erro    string `json:"erro,omitempty"`
	Message string `json:"message,omitempty"`
}

// ErrResponse retorna um envelope de erro padronizado.
func ErrResponse(msg string) Response {
	return Response{OK: false, Erro: msg}
}

// OKResponse retorna um envelope de sucesso.
func OKResponse() Response {
	return Response{OK: true}
}

// AuthClaims representa o payload JWT de autenticação interna (tp-carteira/v1).
type AuthClaims struct {
	UserID   int64  `json:"user_id"`
	WPUserID int64  `json:"wp_user_id"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// MotoboyClaims é o contexto do motoboy autenticado via X-MB-Token.
type MotoboyClaims struct {
	ID       int64  `json:"id"`
	Nome     string `json:"nome"`
	Telefone string `json:"telefone"`
}

// PortalClaims é o contexto do usuário portal autenticado via session cookie.
type PortalClaims struct {
	PortalUserID int64  `json:"portal_user_id"`
	WPUserID     int64  `json:"wp_user_id"`
	Role         string `json:"role"`
	Email        string `json:"email"`
}
