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
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/senderzz/motoboy-service/internal/auth"
	"github.com/senderzz/motoboy-service/internal/httpx"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginHandler implementa os endpoints legados de autenticação por senha.
type LoginHandler struct {
	Pool *pgxpool.Pool
}

// AUDIT-2026-06-21 #6: deriva o hash de armazenamento do token_app.
// HMAC-SHA256(token_raw, WP_SALT_AUTH) em hex — MESMA derivação do PHP
// (sz_mb_hash_token_app) e do middleware AuthMotoboy. Alterar aqui exige
// alterar nos dois outros pontos, senão a auth quebra.
func hashTokenApp(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(os.Getenv("WP_SALT_AUTH")))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

// AUDIT-2026-06-21 HIGH-2: rate limiter in-process para /login/definir-senha,
// espelhando o limite por telefone+IP do PHP (5 / 10 min). Sem dep externa
// (a política do serviço proíbe libs de rate em geocode.go). Janela deslizante
// simples com expurgo preguiçoso.
type defSenhaLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

var defSenhaRL = &defSenhaLimiter{hits: make(map[string][]time.Time)}

// allow registra uma tentativa para a chave e retorna false se excedeu 5 em 10 min.
func (l *defSenhaLimiter) allow(key string) bool {
	const max = 5
	const window = 10 * time.Minute
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// clientIP extrai o IP do request (sem porta), considerando X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Login — POST /login (DESATIVADO)
//
// Port FIEL de sz_mb_api_login(): a rota /login LEGADA está desativada
// (AUDIT Onda 1 — account takeover). O fluxo seguro é /login/verificar →
// /login/definir-senha | /login/autenticar. Mantida apenas para erro claro.
func (h *LoginHandler) Login(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusGone, map[string]any{
		"ok":   false,
		"erro": "Método de login desatualizado. Atualize o aplicativo do motoboy.",
		"use":  "login/autenticar",
	})
}

// buscarPorTelefone espelha sz_mb_buscar_por_telefone: compara o telefone do
// banco (sem +,-,espaço,(,)) contra o normalizado OU '55'+normalizado.
func (h *LoginHandler) buscarPorTelefone(ctx context.Context, telefoneNorm string) (id int64, nome string, zonaID *int64, pinHash *string, err error) {
	err = h.Pool.QueryRow(ctx, `
		SELECT id, nome, zona_id, pin_hash
		  FROM sz_motoboys
		 WHERE REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(telefone,'+',''),'-',''),' ',''),'(',''),')','') IN ($1, $2)
		   AND ativo = true
		 LIMIT 1`,
		telefoneNorm, "55"+telefoneNorm,
	).Scan(&id, &nome, &zonaID, &pinHash)
	return
}

// LoginVerificar — POST /login/verificar (PÚBLICO)
// Body: {telefone}
//
// Port FIEL de sz_mb_api_login_verificar(): rota pública (sem token). Recebe o
// telefone, normaliza (+55 stripped), busca o motoboy ativo e responde
// {ok, nome, tem_senha}. tem_senha = pin_hash não-vazio — informa ao PWA se a
// próxima tela é "definir senha" ou "autenticar".
//
// SEC-GO: o WP aplica rate limit por IP (10/5min); aqui o front é o mesmo PWA e
// a resposta não revela credenciais — apenas se o número existe e se tem senha.
func (h *LoginHandler) LoginVerificar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	telefone := normalizarTelefone(req.Telefone)
	if telefone == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe o telefone.")
		return
	}

	ctx := r.Context()

	_, nome, _, pinHash, err := h.buscarPorTelefone(ctx, telefone)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Número não cadastrado. Fale com o administrador.")
		return
	}

	temSenha := pinHash != nil && *pinHash != ""
	httpx.WriteOK(w, map[string]any{
		"nome":      nome,
		"tem_senha": temSenha,
	})
}

// LoginDefinirSenha — POST /login/definir-senha
// Body: {telefone, senha, token_app}
//
// Port FIEL de sz_mb_api_login_definir_senha(): define a 1ª senha. Bloqueia se
// a conta já tem pin_hash (deve usar "Alterar senha"). Grava pin_hash (bcrypt) +
// token_app. Retorna {ok, motoboy:{id,nome,zona_id}}.
func (h *LoginHandler) LoginDefinirSenha(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
		Senha    string `json:"senha"`
		TokenApp string `json:"token_app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	telefone := normalizarTelefone(req.Telefone)
	// WP exige telefone, senha >= 4 chars e token_app.
	if telefone == "" || len(req.Senha) < 4 || req.TokenApp == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Parâmetros inválidos.")
		return
	}

	// AUDIT-2026-06-21 HIGH-2: rate limit por telefone+IP (5/10min), espelhando o PHP.
	// Antes, o bootstrap de credencial (conta sem pin_hash) não tinha NENHUM limite —
	// permitia varredura de telefones + account-takeover pré-auth. A prova-de-posse forte
	// via OTP confirmado é aplicada no caminho de escrita PHP (sz_mb_api_login_definir_senha).
	rlKey := telefone + "|" + clientIP(r)
	if !defSenhaRL.allow(rlKey) {
		httpx.WriteErr(w, http.StatusTooManyRequests, "Muitas tentativas. Aguarde alguns minutos.")
		return
	}

	ctx := r.Context()

	motoboyID, nome, zonaID, pinHash, err := h.buscarPorTelefone(ctx, telefone)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "Telefone não cadastrado.")
		return
	}
	// Não permite redefinir se já tem senha (usar trocar-senha).
	if pinHash != nil && *pinHash != "" {
		httpx.WriteErr(w, http.StatusConflict, `Conta já possui senha. Use "Alterar senha" no perfil.`)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Senha), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("[login] falha ao gerar hash de senha", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// AUDIT-2026-06-21 #6: grava o HASH do token_app; o app guarda o RAW.
	if _, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET pin_hash = $1, token_app = $2 WHERE id = $3`,
		string(hash), hashTokenApp(req.TokenApp), motoboyID,
	); err != nil {
		slog.Error("[login] falha ao atualizar senha", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao salvar. Tente novamente.")
		return
	}

	slog.Info("[login] senha definida", "motoboy_id", motoboyID)
	httpx.WriteOK(w, map[string]any{
		"motoboy": map[string]any{"id": motoboyID, "nome": nome, "zona_id": zonaID},
	})
}

// LoginAutenticar — POST /login/autenticar
// Body: {telefone, senha, token_app}
//
// Port FIEL de sz_mb_api_login_autenticar(): valida senha contra pin_hash
// (password_verify/bcrypt), atualiza token_app e retorna {ok, motoboy}. Mensagem
// genérica em qualquer falha (não revela se o telefone existe).
func (h *LoginHandler) LoginAutenticar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
		Senha    string `json:"senha"`
		TokenApp string `json:"token_app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	telefone := normalizarTelefone(req.Telefone)
	if telefone == "" || req.Senha == "" || req.TokenApp == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Parâmetros inválidos.")
		return
	}

	const msgInvalido = "Telefone ou senha incorretos." // Genérico — não revela existência.

	ctx := r.Context()

	motoboyID, nome, zonaID, pinHash, err := h.buscarPorTelefone(ctx, telefone)
	if err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, msgInvalido)
		return
	}
	if pinHash == nil || *pinHash == "" {
		httpx.WriteErr(w, http.StatusUnauthorized, msgInvalido)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*pinHash), []byte(req.Senha)); err != nil {
		slog.Warn("[login] senha incorreta", "motoboy_id", motoboyID)
		httpx.WriteErr(w, http.StatusUnauthorized, msgInvalido)
		return
	}

	// Login OK — atualiza token_app do dispositivo.
	// AUDIT-2026-06-21 #6: grava o HASH do token_app; o app guarda o RAW.
	if _, err := h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET token_app = $1 WHERE id = $2`, hashTokenApp(req.TokenApp), motoboyID,
	); err != nil {
		slog.Error("[login] falha ao atualizar token_app", "motoboy_id", motoboyID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao autenticar")
		return
	}

	slog.Info("[login] motoboy autenticado via senha", "motoboy_id", motoboyID)
	httpx.WriteOK(w, map[string]any{
		"motoboy": map[string]any{"id": motoboyID, "nome": nome, "zona_id": zonaID},
	})
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
	// WP exige nova senha >= 4 chars (sz_mb_api_trocar_senha).
	if len(req.SenhaNova) < 4 {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "Nova senha deve ter ao menos 4 caracteres.")
		return
	}

	ctx := r.Context()

	var pinHash *string
	if err := h.Pool.QueryRow(ctx,
		`SELECT pin_hash FROM sz_motoboys WHERE id = $1`, mb.ID,
	).Scan(&pinHash); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar motoboy")
		return
	}

	// Só valida a senha atual se já existe pin_hash (espelha o ! empty($row->pin_hash)).
	if pinHash != nil && *pinHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(*pinHash), []byte(req.SenhaAtual)); err != nil {
			httpx.WriteErr(w, http.StatusUnauthorized, "Senha atual incorreta.")
			return
		}
	}

	novoHash, err := bcrypt.GenerateFromPassword([]byte(req.SenhaNova), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("[login] falha ao gerar hash de nova senha", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	if _, err = h.Pool.Exec(ctx,
		`UPDATE sz_motoboys SET pin_hash = $1 WHERE id = $2`,
		string(novoHash), mb.ID,
	); err != nil {
		slog.Error("[login] falha ao trocar senha", "motoboy_id", mb.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao trocar senha")
		return
	}

	slog.Info("[login] senha trocada", "motoboy_id", mb.ID)
	httpx.WriteOK(w, map[string]any{"msg": "Senha atualizada com sucesso."})
}

