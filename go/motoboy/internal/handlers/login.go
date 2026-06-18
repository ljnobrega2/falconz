// Package handlers — autenticação legada (login/senha) e troca de senha.
//
// Este arquivo cobre os endpoints legacy de login por telefone+senha bcrypt,
// que existiam antes da migração para OTP. Mantidos para compatibilidade
// com versões antigas do PWA do motoboy.
//
// NOTA: requer golang.org/x/crypto/bcrypt — adicionar com:
//   go get golang.org/x/crypto/bcrypt
//   go mod tidy
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginHandler implementa os endpoints legados de autenticação por senha.
type LoginHandler struct {
	Pool *pgxpool.Pool
}

// Login — POST /login
// Body: {telefone, senha}
// Lookup motoboy por telefone, verifica bcrypt, retorna token_app + JWT.
func (h *LoginHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
		Senha    string `json:"senha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	req.Telefone = strings.TrimSpace(req.Telefone)
	if req.Telefone == "" || req.Senha == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "telefone e senha obrigatórios")
		return
	}

	ctx := r.Context()

	var motoboyID int64
	var nome, senhaHash, tokenApp string
	err := h.Pool.QueryRow(ctx, `
		SELECT id, nome, senha_hash, token_app
		FROM sz_motoboys
		WHERE telefone = $1 AND ativo = 1
		LIMIT 1`,
		req.Telefone,
	).Scan(&motoboyID, &nome, &senhaHash, &tokenApp)
	if err != nil {
		// Resposta genérica — não revela se número existe.
		telLen := len(req.Telefone)
		if telLen > 4 {
			telLen = 4
		}
		slog.Warn("[login] tentativa de login para número desconhecido", "tel_prefix", req.Telefone[:telLen]+"****")
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	// Verifica bcrypt.
	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte(req.Senha)); err != nil {
		slog.Warn("[login] senha incorreta", "motoboy_id", motoboyID)
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	jwtToken, err := emitirJWT(motoboyID, nome)
	if err != nil {
		slog.Error("[login] falha ao emitir JWT", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao emitir token")
		return
	}

	slog.Info("[login] motoboy autenticado via senha", "motoboy_id", motoboyID)
	httpx.WriteOK(w, map[string]any{
		"token":      jwtToken,
		"token_app":  tokenApp,
		"motoboy_id": motoboyID,
		"nome":       nome,
	})
}

// LoginVerificar — POST /login/verificar
// Valida X-MB-Token (via middleware authMotoboy já aplicado) e retorna info do motoboy.
func (h *LoginHandler) LoginVerificar(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}
	httpx.WriteOK(w, map[string]any{
		"motoboy_id": mb.ID,
		"nome":       mb.Nome,
		"cd_id":      mb.CdID,
		"zona_id":    mb.ZonaID,
	})
}

// LoginDefinirSenha — POST /login/definir-senha
// Body: {token, senha_nova}
// Usa token_app como token de reset, faz bcrypt hash e atualiza sz_motoboys.
func (h *LoginHandler) LoginDefinirSenha(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token     string `json:"token"`
		SenhaNova string `json:"senha_nova"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Token == "" || req.SenhaNova == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token e senha_nova obrigatórios")
		return
	}
	if len(req.SenhaNova) < 6 {
		httpx.WriteErr(w, http.StatusBadRequest, "senha deve ter ao menos 6 caracteres")
		return
	}

	ctx := r.Context()

	// Valida token_app.
	var motoboyID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_motoboys WHERE token_app = $1 AND ativo = 1 LIMIT 1`,
		req.Token,
	).Scan(&motoboyID)
	if err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "token inválido")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.SenhaNova), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("[login] falha ao gerar hash de senha", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET senha_hash = $1 WHERE id = $2`,
		string(hash), motoboyID,
	)
	if err != nil {
		slog.Error("[login] falha ao atualizar senha", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao atualizar senha")
		return
	}

	slog.Info("[login] senha definida via token", "motoboy_id", motoboyID)
	httpx.WriteOK(w, map[string]any{"msg": "Senha definida com sucesso."})
}

// TrocarSenha — POST /motoboy/trocar-senha (requer X-MB-Token)
// Body: {senha_atual, senha_nova}
func (h *LoginHandler) TrocarSenha(w http.ResponseWriter, r *http.Request) {
	mb := auth.MotoboyfromCtx(r.Context())
	if mb == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autorizado")
		return
	}

	var req struct {
		SenhaAtual string `json:"senha_atual"`
		SenhaNova  string `json:"senha_nova"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.SenhaAtual == "" || req.SenhaNova == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "senha_atual e senha_nova obrigatórios")
		return
	}
	if len(req.SenhaNova) < 6 {
		httpx.WriteErr(w, http.StatusBadRequest, "nova senha deve ter ao menos 6 caracteres")
		return
	}

	ctx := r.Context()

	var senhaHash string
	err := h.Pool.QueryRow(ctx,
		`SELECT senha_hash FROM sz_motoboys WHERE id = $1`, mb.ID,
	).Scan(&senhaHash)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar motoboy")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(senhaHash), []byte(req.SenhaAtual)); err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "senha atual incorreta")
		return
	}

	novoHash, err := bcrypt.GenerateFromPassword([]byte(req.SenhaNova), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("[login] falha ao gerar hash de nova senha", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	_, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET senha_hash = $1 WHERE id = $2`,
		string(novoHash), mb.ID,
	)
	if err != nil {
		slog.Error("[login] falha ao trocar senha", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao trocar senha")
		return
	}

	slog.Info("[login] senha trocada", "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"msg": "Senha atualizada com sucesso."})
}

