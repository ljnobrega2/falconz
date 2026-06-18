// Package handlers — auth do app motoboy (OTP + JWT).
// OTP: 6 dígitos, TTL 5min, armazenado em Postgres (simples, sem Redis externo).
// JWT: claims MotoboyID + nome, mesmo JWT_SECRET do WP.
package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// MobAuthHandler implementa auth do app motoboy via OTP.
type MobAuthHandler struct {
	Pool *pgxpool.Pool
}

type motoboyJWTClaims struct {
	MotoboyID int64  `json:"motoboy_id"`
	Nome      string `json:"nome"`
	jwt.RegisteredClaims
}

// gerarOTP retorna um código OTP de 6 dígitos criptograficamente seguro.
func gerarOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// emitirJWT gera um JWT para o motoboy com validade de 30 dias.
func emitirJWT(motoboyID int64, nome string) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET não configurado")
	}

	claims := motoboyJWTClaims{
		MotoboyID: motoboyID,
		Nome:      nome,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", motoboyID),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * 24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// OTPSolicitar — POST /otp/solicitar (legacy) ou POST /motoboy/otp/solicitar
// Body: {telefone}
// Gera e armazena OTP; em prod envia via SMS (aqui apenas loga).
func (h *MobAuthHandler) OTPSolicitar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Telefone == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "telefone obrigatório")
		return
	}

	ctx := r.Context()

	// Verifica se motoboy existe e está ativo.
	var motoboyID int64
	err := h.Pool.QueryRow(ctx,
		`SELECT id FROM sz_motoboys WHERE telefone=$1 AND ativo=true LIMIT 1`,
		req.Telefone,
	).Scan(&motoboyID)
	if err != nil {
		// Resposta genérica — não revela se número existe.
		slog.Info("[auth] OTP solicitado para número desconhecido", "tel", req.Telefone[:4]+"****")
		httpx.WriteOK(w, map[string]any{"ok": true, "msg": "Se o número estiver cadastrado, você receberá um OTP."})
		return
	}

	otp, err := gerarOTP()
	if err != nil {
		slog.Error("[auth] falha ao gerar OTP", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	expiry := time.Now().Add(5 * time.Minute)

	// Upsert OTP na tabela sz_motoboy_otps (TTL via expires_at).
	_, err = h.Pool.Exec(ctx, `
		INSERT INTO sz_motoboy_otps (motoboy_id, otp, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (motoboy_id) DO UPDATE SET otp=$2, expires_at=$3, tentativas=0`,
		motoboyID, otp, expiry,
	)
	if err != nil {
		slog.Error("[auth] falha ao salvar OTP", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// TODO: integrar SMS (Twilio/ZenviaAPI). Por ora loga (dev).
	slog.Info("[auth] OTP gerado (dev — não enviado por SMS)", "motoboy_id", motoboyID, "otp", otp, "expires", expiry)

	httpx.WriteOK(w, map[string]any{"ok": true, "msg": "OTP enviado."})
}

// OTPValidar — POST /otp/confirmar (legacy) ou POST /motoboy/otp/validar
// Body: {telefone, otp}
// Valida OTP e retorna JWT se correto.
func (h *MobAuthHandler) OTPValidar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Telefone string `json:"telefone"`
		OTP      string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if req.Telefone == "" || req.OTP == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "telefone e otp obrigatórios")
		return
	}

	ctx := r.Context()

	var motoboyID int64
	var nome string
	var token string
	err := h.Pool.QueryRow(ctx, `
		SELECT m.id, m.nome, m.token_app
		FROM sz_motoboys m
		WHERE m.telefone=$1 AND m.ativo=true LIMIT 1`,
		req.Telefone,
	).Scan(&motoboyID, &nome, &token)
	if err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "credenciais inválidas")
		return
	}

	// Valida OTP e controle de tentativas.
	var storedOTP string
	var expiry time.Time
	var tentativas int
	err = h.Pool.QueryRow(ctx, `
		SELECT otp, expires_at, tentativas
		FROM sz_motoboy_otps
		WHERE motoboy_id=$1`, motoboyID,
	).Scan(&storedOTP, &expiry, &tentativas)
	if err != nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "OTP não solicitado ou expirado")
		return
	}

	if tentativas >= 5 {
		httpx.WriteErr(w, http.StatusTooManyRequests, "muitas tentativas — solicite novo OTP")
		return
	}

	_, _ = h.Pool.Exec(ctx, `UPDATE sz_motoboy_otps SET tentativas=tentativas+1 WHERE motoboy_id=$1`, motoboyID)

	if time.Now().After(expiry) {
		httpx.WriteErr(w, http.StatusUnauthorized, "OTP expirado")
		return
	}
	if req.OTP != storedOTP {
		httpx.WriteErr(w, http.StatusUnauthorized, "OTP incorreto")
		return
	}

	// OTP válido: limpa da tabela.
	_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_motoboy_otps WHERE motoboy_id=$1`, motoboyID)

	jwtToken, err := emitirJWT(motoboyID, nome)
	if err != nil {
		slog.Error("[auth] falha ao emitir JWT", "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao emitir token")
		return
	}

	slog.Info("[auth] motoboy autenticado", "motoboy_id", motoboyID)
	httpx.WriteOK(w, map[string]any{
		"ok":         true,
		"token":      jwtToken,        // JWT para auth futura (Go)
		"token_app":  token,           // token_app legado (compatibilidade PHP)
		"motoboy_id": motoboyID,
		"nome":       nome,
	})
}

// TokenValidar — GET /motoboy/token/validar
// Valida X-MB-Token e retorna info do motoboy (já passado pelo middleware authMotoboy).
func (h *MobAuthHandler) TokenValidar(w http.ResponseWriter, r *http.Request) {
	motoboyID, _ := r.Context().Value("motoboy_id").(int64)
	nome, _ := r.Context().Value("motoboy_nome").(string)

	httpx.WriteOK(w, map[string]any{
		"ok":         true,
		"motoboy_id": motoboyID,
		"nome":       nome,
	})
}
