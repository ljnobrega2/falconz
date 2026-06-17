// Package handlers — endpoint admin para gerenciamento de notificações push técnicas.
// Espelha senderzz-notifications.php:1162 (PHP legado) — VAPID keys, log de push, envio de teste.
// Tabela: sz_notif_log (id, user_id, event, recipient_type, order_id, status, http_code, error_msg, created_at)
// Options: sz_notif_vapid_public, sz_notif_vapid_private em senderzz_options.
package handlers

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/admin-service/internal/httpx"
)

// PushTecnicoHandler gerencia VAPID keys, log de notificações e envio de teste.
type PushTecnicoHandler struct{ Pool *pgxpool.Pool }

// tableExistsPush verifica presença de uma tabela no schema public.
func (h *PushTecnicoHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// getOption lê um valor de senderzz_options; retorna "" se ausente.
func (h *PushTecnicoHandler) getOption(ctx context.Context, key string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return ""
	}
	var raw string
	_ = h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw)
	return strings.TrimSpace(raw)
}

// upsertOptionStr grava option string (UPSERT em senderzz_options).
func (h *PushTecnicoHandler) upsertOptionStr(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// countTable retorna count(*) da tabela; 0 se tabela inexistente.
func (h *PushTecnicoHandler) countTable(ctx context.Context, table string) int64 {
	if !h.tableExists(ctx, table) {
		return 0
	}
	var n int64
	_ = h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
	return n
}

// ----- GET /push-tecnico/status ------------------------------------------

// PushStatus representa o estado atual da configuração de push.
type PushStatus struct {
	VapidPublic     string `json:"vapid_public"`    // primeiros 20 chars da chave pública
	VapidConfigured bool   `json:"vapid_configured"` // true se chave pública gravada
	DeviceCount     int64  `json:"device_count"`     // sz_push_subscriptions
	LogCount        int64  `json:"log_count"`        // sz_notif_log
	EnvManaged      bool   `json:"env_managed"`      // true se gerenciado via env vars
}

func (h *PushTecnicoHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pub := h.getOption(ctx, "sz_notif_vapid_public")

	// env_managed: verifica se variáveis de ambiente estão definidas.
	envPub := strings.TrimSpace(os.Getenv("VAPID_PUBLIC"))
	envPriv := strings.TrimSpace(os.Getenv("VAPID_PRIVATE"))
	envManaged := envPub != "" && envPriv != ""

	// Se gerenciado por env, prioriza o valor da variável de ambiente.
	if envManaged && pub == "" {
		pub = envPub
	}

	masked := ""
	if len(pub) > 20 {
		masked = pub[:20] + "…"
	} else {
		masked = pub
	}

	out := PushStatus{
		VapidPublic:     masked,
		VapidConfigured: pub != "",
		DeviceCount:     h.countTable(ctx, "sz_push_subscriptions"),
		LogCount:        h.countTable(ctx, "sz_notif_log"),
		EnvManaged:      envManaged,
	}
	httpx.JSON(w, 200, out)
}

// ----- POST /push-tecnico/regenerate-vapid --------------------------------

// RegenerateVapidRequest exige confirmação explícita do usuário.
type RegenerateVapidRequest struct {
	Confirm string `json:"confirm"` // deve ser exatamente "REGENERAR"
}

func (h *PushTecnicoHandler) RegenerateVapid(w http.ResponseWriter, r *http.Request) {
	var in RegenerateVapidRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// Validação exata — substring não aceita (espelha CRIT-04).
	if in.Confirm != "REGENERAR" {
		httpx.Err(w, 400, "confirmation_required", `campo confirm deve ser exatamente "REGENERAR"`)
		return
	}

	// Gera par de chaves EC prime256v1 (padrão VAPID — RFC 8292).
	curve := elliptic.P256()
	privKey, x, y, err := elliptic.GenerateKey(curve, rand.Reader)
	if err != nil {
		httpx.Err(w, 500, "keygen_error", "falha ao gerar chave VAPID: "+err.Error())
		return
	}

	// Chave pública em formato uncompressed (04 || X || Y) codificada em base64url sem padding.
	pubKeyBytes := elliptic.Marshal(curve, x, y)
	pubB64 := base64.RawURLEncoding.EncodeToString(pubKeyBytes)

	// Chave privada (scalar D) codificada em base64url sem padding.
	privB64 := base64.RawURLEncoding.EncodeToString(privKey)

	ctx := r.Context()

	// UPSERT das duas options em senderzz_options.
	if err := h.upsertOptionStr(ctx, "sz_notif_vapid_public", pubB64); err != nil {
		httpx.Err(w, 500, "db_error", "falha ao salvar chave pública: "+err.Error())
		return
	}
	if err := h.upsertOptionStr(ctx, "sz_notif_vapid_private", privB64); err != nil {
		httpx.Err(w, 500, "db_error", "falha ao salvar chave privada: "+err.Error())
		return
	}

	// Retorna apenas os primeiros 20 chars da pública (nunca expõe privada).
	masked := pubB64
	if len(masked) > 20 {
		masked = masked[:20] + "…"
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":                  true,
		"vapid_public_masked": masked,
	})
}

// ----- VAPID push dispatch -----------------------------------------------

// pushSubscription representa uma linha de sz_push_subscriptions.
type pushSubscription struct {
	ID       int64
	UserID   int64
	Endpoint string
	P256dh   string
	Auth     string
}

// loadVapidKeys carrega as chaves VAPID da DB ou env vars.
// Retorna (public_b64url, private_b64url, erro).
func (h *PushTecnicoHandler) loadVapidKeys(ctx context.Context) (string, string, error) {
	envPub := strings.TrimSpace(os.Getenv("VAPID_PUBLIC"))
	envPriv := strings.TrimSpace(os.Getenv("VAPID_PRIVATE"))
	if envPub != "" && envPriv != "" {
		return envPub, envPriv, nil
	}
	pub := h.getOption(ctx, "sz_notif_vapid_public")
	priv := h.getOption(ctx, "sz_notif_vapid_private")
	if pub == "" || priv == "" {
		return "", "", fmt.Errorf("chaves VAPID ausentes — configure no painel ou via env VAPID_PUBLIC/VAPID_PRIVATE")
	}
	return pub, priv, nil
}

// getSubscriptionsForUser busca todas as assinaturas de um usuário.
func (h *PushTecnicoHandler) getSubscriptionsForUser(ctx context.Context, userID int64) ([]pushSubscription, error) {
	if !h.tableExists(ctx, "sz_push_subscriptions") {
		return nil, nil
	}
	rows, err := h.Pool.Query(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth
		 FROM sz_push_subscriptions WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []pushSubscription
	for rows.Next() {
		var s pushSubscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth); err != nil {
			continue
		}
		subs = append(subs, s)
	}
	return subs, nil
}

// logDelivery insere uma entrada em sz_notif_log.
func (h *PushTecnicoHandler) logDelivery(ctx context.Context,
	userID int64, event string, orderID *int64, subsID *int64,
	status string, httpCode int, responseText, errorMsg, recipientType string,
) {
	if !h.tableExists(ctx, "sz_notif_log") {
		return
	}
	_, _ = h.Pool.Exec(ctx,
		`INSERT INTO sz_notif_log
		 (user_id, event, recipient_type, order_id, status, http_code, error_msg, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
		userID, event, recipientType, orderID, status, httpCode, truncate(errorMsg+responseText, 900),
	)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// deleteSubscription remove uma assinatura expirada/inválida.
func (h *PushTecnicoHandler) deleteSubscription(ctx context.Context, id int64) {
	if !h.tableExists(ctx, "sz_push_subscriptions") {
		return
	}
	_, _ = h.Pool.Exec(ctx, `DELETE FROM sz_push_subscriptions WHERE id=$1`, id)
}

// sendPushResult é o resultado de um envio VAPID.
type sendPushResult struct {
	Status       string
	HTTPCode     int
	ResponseText string
	ErrorMsg     string
}

// sendPush envia uma notificação Web Push (RFC 8292 + RFC 8188) a uma assinatura.
// Implementação sem dependência externa — usa apenas stdlib + golang.org/x/crypto.
func sendPush(sub pushSubscription, payload, vapidPublic, vapidPrivate, sub_email string) sendPushResult {
	if sub.Endpoint == "" {
		return sendPushResult{Status: "failed", ErrorMsg: "Endpoint vazio."}
	}
	if sub.P256dh == "" || sub.Auth == "" {
		return sendPushResult{Status: "failed", ErrorMsg: "Subscription sem p256dh/auth. Remova o dispositivo e ative novamente no PWA."}
	}

	// --- 1. VAPID JWT (ES256) -----------------------------------------------
	privRaw, err := base64.RawURLEncoding.DecodeString(vapidPrivate)
	if err != nil || len(privRaw) != 32 {
		return sendPushResult{Status: "failed", ErrorMsg: "Chave VAPID privada inválida."}
	}
	pubRaw, err := base64.RawURLEncoding.DecodeString(vapidPublic)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Chave VAPID pública inválida."}
	}
	// Garante formato uncompressed (0x04 || X || Y = 65 bytes)
	if len(pubRaw) == 64 {
		pubRaw = append([]byte{0x04}, pubRaw...)
	}
	if len(pubRaw) != 65 || pubRaw[0] != 0x04 {
		return sendPushResult{Status: "failed", ErrorMsg: "Chave VAPID pública em formato inválido (esperado 65 bytes uncompressed)."}
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(privRaw)
	x := new(big.Int).SetBytes(pubRaw[1:33])
	y := new(big.Int).SetBytes(pubRaw[33:65])
	ecPrivKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}

	// Extrai origin (scheme://host) do endpoint para o campo aud do JWT.
	parsed, err := url.Parse(sub.Endpoint)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Endpoint inválido: " + err.Error()}
	}
	audience := parsed.Scheme + "://" + parsed.Host

	subEmail := sub_email
	if subEmail == "" {
		subEmail = "mailto:admin@senderzz.com"
	}

	claims := jwtlib.MapClaims{
		"aud": audience,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": subEmail,
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodES256, claims)
	tokenStr, err := tok.SignedString(ecPrivKey)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao assinar JWT VAPID: " + err.Error()}
	}

	// --- 2. Encrypt payload (RFC 8188 aes128gcm) ----------------------------
	receiverKey, err := base64.RawURLEncoding.DecodeString(sub.P256dh)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "p256dh inválido: " + err.Error()}
	}
	authSecret, err := base64.RawURLEncoding.DecodeString(sub.Auth)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "auth inválido: " + err.Error()}
	}
	if len(receiverKey) != 65 || receiverKey[0] != 0x04 {
		return sendPushResult{Status: "failed", ErrorMsg: "p256dh deve ser 65 bytes uncompressed."}
	}
	if len(authSecret) < 16 {
		return sendPushResult{Status: "failed", ErrorMsg: "auth muito curto (< 16 bytes)."}
	}

	// Gera par efêmero P-256 para ECDH
	ephCurve := ecdh.P256()
	ephPriv, err := ephCurve.GenerateKey(rand.Reader)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao gerar chave efêmera: " + err.Error()}
	}
	ephPubRaw := ephPriv.PublicKey().Bytes() // 65 bytes uncompressed

	// Importa a chave pública do receptor
	recvPub, err := ephCurve.NewPublicKey(receiverKey)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao importar p256dh: " + err.Error()}
	}

	// ECDH: shared secret
	sharedSecret, err := ephPriv.ECDH(recvPub)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha no ECDH: " + err.Error()}
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao gerar salt: " + err.Error()}
	}

	// RFC 8291: PRK_key = HMAC-SHA256(auth_secret, shared_secret)
	// key_info = "WebPush: info\0" || receiver_pub || ephemeral_pub
	prkKey := hmacSHA256(authSecret, sharedSecret)
	keyInfo := append([]byte("WebPush: info\x00"), receiverKey...)
	keyInfo = append(keyInfo, ephPubRaw...)
	ikm := hkdfExpand(prkKey, keyInfo, 32)

	// RFC 8188: PRK = HMAC-SHA256(salt, ikm)
	prk := hmacSHA256(salt, ikm)
	cek := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)

	// AES-128-GCM encrypt (payload || 0x02 padding delimiter)
	plain := append([]byte(payload), 0x02)
	block, err := aes.NewCipher(cek)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao criar cipher: " + err.Error()}
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao criar GCM: " + err.Error()}
	}
	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	// RFC 8188 body: salt(16) || rs(4, big-endian) || idlen(1) || ephemeral_pub(65) || ciphertext+tag
	recordSize := uint32(4096)
	body := make([]byte, 0, 16+4+1+65+len(ciphertext))
	body = append(body, salt...)
	body = append(body, byte(recordSize>>24), byte(recordSize>>16), byte(recordSize>>8), byte(recordSize))
	body = append(body, byte(65)) // idlen = len(ephemeral_pub)
	body = append(body, ephPubRaw...)
	body = append(body, ciphertext...)

	// --- 3. HTTP POST para o endpoint push ----------------------------------
	req, err := http.NewRequest("POST", sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: "Falha ao criar request: " + err.Error()}
	}
	req.Header.Set("Authorization", "vapid t="+tokenStr+", k="+vapidPublic)
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return sendPushResult{Status: "failed", ErrorMsg: err.Error()}
	}
	defer resp.Body.Close()

	respBodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	respText := strings.TrimSpace(string(respBodyBytes))
	code := resp.StatusCode
	ok := code >= 200 && code < 300

	errMsg := ""
	if !ok {
		errMsg = fmt.Sprintf("Push service retornou HTTP %d", code)
		if respText != "" {
			if len(respText) > 280 {
				respText = respText[:280]
			}
			errMsg += " — " + respText
		}
		if code == 400 || code == 401 || code == 403 {
			errMsg += " | Ação: no celular, desative e ative as notificações novamente."
		}
	}
	status := "sent"
	if !ok {
		status = "failed"
	}
	return sendPushResult{Status: status, HTTPCode: code, ResponseText: respText, ErrorMsg: errMsg}
}

// hmacSHA256 computa HMAC-SHA256(key, data).
func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// hkdfExpand executa HKDF-Expand(prk, info, length) — RFC 5869 Section 2.3.
func hkdfExpand(prk, info []byte, length int) []byte {
	n := (length + sha256.Size - 1) / sha256.Size
	okm := make([]byte, 0, n*sha256.Size)
	prev := []byte{}
	for i := 1; i <= n; i++ {
		mac := hmac.New(sha256.New, prk)
		mac.Write(prev)
		mac.Write(info)
		mac.Write([]byte{byte(i)})
		prev = mac.Sum(nil)
		okm = append(okm, prev...)
	}
	return okm[:length]
}

// ----- POST /push-tecnico/test-send ---------------------------------------

// TestSendRequest parâmetros para disparo manual de teste.
type TestSendRequest struct {
	UserID int64  `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func (h *PushTecnicoHandler) TestSend(w http.ResponseWriter, r *http.Request) {
	var in TestSendRequest
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	if in.UserID <= 0 {
		httpx.Err(w, 400, "validation_error", "user_id obrigatório")
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		httpx.Err(w, 400, "validation_error", "title obrigatório")
		return
	}

	ctx := r.Context()

	// Carrega chaves VAPID.
	vapidPub, vapidPriv, err := h.loadVapidKeys(ctx)
	if err != nil {
		httpx.Err(w, 400, "vapid_not_configured", err.Error())
		return
	}

	// Busca assinaturas do usuário.
	subs, err := h.getSubscriptionsForUser(ctx, in.UserID)
	if err != nil {
		httpx.Err(w, 500, "db_error", "falha ao buscar assinaturas: "+err.Error())
		return
	}
	if len(subs) == 0 {
		// Nenhum dispositivo — registra no log e retorna aviso.
		h.logDelivery(ctx, in.UserID, "admin_test", nil, nil,
			"failed", 0, "", "Nenhum dispositivo inscrito.", "admin_test")
		httpx.Err(w, 422, "no_subscriptions", "usuário não possui dispositivo inscrito para push")
		return
	}

	// Constrói payload JSON (espelha formato WP admin_test).
	payloadMap := map[string]any{
		"title": in.Title,
		"body":  in.Body,
		"icon":  "",
		"badge": "",
		"data":  map[string]any{"url": "/meus-pedidos/"},
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	payloadStr := string(payloadBytes)

	// Envia para cada assinatura e loga resultado.
	sentCount := 0
	for _, sub := range subs {
		result := sendPush(sub, payloadStr, vapidPub, vapidPriv, "")
		subID := sub.ID
		h.logDelivery(ctx, in.UserID, "admin_test", nil, &subID,
			result.Status, result.HTTPCode, result.ResponseText, result.ErrorMsg, "admin_test")
		if result.Status == "sent" {
			sentCount++
		}
		// Remove assinatura expirada/inválida (410 Gone ou 404 Not Found).
		if result.HTTPCode == 404 || result.HTTPCode == 410 {
			h.deleteSubscription(ctx, sub.ID)
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"sent":       sentCount,
		"total_subs": len(subs),
	})
}

// ----- GET /push-tecnico/logs --------------------------------------------

// NotifLogItem representa uma entrada do log de notificações.
type NotifLogItem struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	DisplayName   string    `json:"display_name"` // nome do portal_user ou "user #ID"
	Event         string    `json:"event"`
	RecipientType string    `json:"recipient_type"`
	OrderID       *int64    `json:"order_id"`
	Status        string    `json:"status"`
	HTTPCode      int       `json:"http_code"`
	ErrorMsg      string    `json:"error_msg"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *PushTecnicoHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []NotifLogItem{}

	if !h.tableExists(ctx, "sz_notif_log") {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	// Lê parâmetro limit; padrão 25.
	limitStr := r.URL.Query().Get("limit")
	limit := 25
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
		limit = n
	}

	// LEFT JOIN com senderzz_portal_users para exibir nome em vez de ID numérico.
	// senderzz_portal_users.wp_user_id é o WP user ID armazenado em sz_notif_log.user_id.
	// Quando não há correspondência (user_id=0 p.ex.) usa fallback "user #ID".
	rows, err := h.Pool.Query(ctx,
		`SELECT l.id,
		        COALESCE(l.user_id,0),
		        COALESCE(pu.nome, ''),
		        COALESCE(l.event,''),
		        COALESCE(l.recipient_type,''),
		        l.order_id,
		        COALESCE(l.status,''),
		        COALESCE(l.http_code,0),
		        COALESCE(l.error_msg,''),
		        l.created_at
		 FROM sz_notif_log l
		 LEFT JOIN senderzz_portal_users pu ON pu.wp_user_id = l.user_id
		 ORDER BY l.id DESC
		 LIMIT $1`, limit)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var it NotifLogItem
		var rawNome string
		if err := rows.Scan(
			&it.ID, &it.UserID, &rawNome, &it.Event, &it.RecipientType,
			&it.OrderID, &it.Status, &it.HTTPCode, &it.ErrorMsg, &it.CreatedAt,
		); err != nil {
			continue
		}
		if rawNome != "" {
			it.DisplayName = rawNome
		} else {
			it.DisplayName = fmt.Sprintf("user #%d", it.UserID)
		}
		out = append(out, it)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /push-tecnico/logs/{id}/reprocess ----------------------------

func (h *PushTecnicoHandler) ReprocessLog(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Err(w, 400, "bad_id", "id inválido")
		return
	}

	ctx := r.Context()

	if !h.tableExists(ctx, "sz_notif_log") {
		httpx.Err(w, 404, "table_missing", "sz_notif_log não existe")
		return
	}

	// Lê order_id, event e user_id da linha do log.
	var (
		logUserID int64
		logEvent  string
		logOrder  *int64
	)
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(user_id,0), COALESCE(event,''), order_id
		 FROM sz_notif_log WHERE id=$1`, id).
		Scan(&logUserID, &logEvent, &logOrder)
	if err != nil {
		httpx.Err(w, 404, "not_found", "entrada de log não encontrada")
		return
	}

	// Para reprocessar precisamos de order_id + event (espelha lógica WP sz_notif_reprocess).
	if logOrder == nil || *logOrder <= 0 || logEvent == "" {
		httpx.Err(w, 422, "insufficient_data",
			"entrada de log sem order_id ou event — não é possível reprocessar")
		return
	}

	// Carrega chaves VAPID.
	vapidPub, vapidPriv, err := h.loadVapidKeys(ctx)
	if err != nil {
		httpx.Err(w, 400, "vapid_not_configured", err.Error())
		return
	}

	// Resolve o wp_user_id do produtor a partir do order_id.
	// Espelha sz_notif_producer_for_order + sz_notif_get_wp_user_id_for_portal_producer.
	producerWPUserID, resolveErr := h.resolveProducerWPUserID(ctx, *logOrder, logUserID)
	if resolveErr != nil || producerWPUserID <= 0 {
		msg := "Não foi possível identificar o produtor do pedido"
		if resolveErr != nil {
			msg += ": " + resolveErr.Error()
		}
		httpx.Err(w, 422, "producer_not_found", msg)
		return
	}

	// Busca assinaturas do produtor.
	subs, err := h.getSubscriptionsForUser(ctx, producerWPUserID)
	if err != nil || len(subs) == 0 {
		httpx.Err(w, 422, "no_subscriptions", "produtor não possui dispositivos inscritos")
		return
	}

	// Constrói payload de reprocessamento.
	payloadMap := map[string]any{
		"title": "Reprocessamento — " + logEvent,
		"body":  fmt.Sprintf("Pedido #%d reprocessado pelo admin.", *logOrder),
		"icon":  "",
		"badge": "",
		"data": map[string]any{
			"order_id":         *logOrder,
			"event":            logEvent,
			"manual_reprocess": 1,
			"url":              "/meus-pedidos/",
		},
	}
	payloadBytes, _ := json.Marshal(payloadMap)
	payloadStr := string(payloadBytes)

	// Envia para cada assinatura.
	sentCount := 0
	for _, sub := range subs {
		result := sendPush(sub, payloadStr, vapidPub, vapidPriv, "")
		subID := sub.ID
		h.logDelivery(ctx, producerWPUserID, logEvent, logOrder, &subID,
			result.Status, result.HTTPCode, result.ResponseText, result.ErrorMsg, "manual_reprocess")
		if result.Status == "sent" {
			sentCount++
		}
		if result.HTTPCode == 404 || result.HTTPCode == 410 {
			h.deleteSubscription(ctx, sub.ID)
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":         true,
		"sent":       sentCount,
		"total_subs": len(subs),
	})
}

// resolveProducerWPUserID resolve o WP user ID do produtor de um pedido.
// sz_notif_fire() sempre armazena o wp_user_id do produtor em sz_notif_log.user_id;
// portanto o logUserID é a fonte canônica. Só tenta resolver via schema quando logUserID=0
// (linhas de sistema geradas por sz_notif_log_system).
func (h *PushTecnicoHandler) resolveProducerWPUserID(_ context.Context, orderID, logUserID int64) (int64, error) {
	if logUserID > 0 {
		// user_id no log já é o wp_user_id do produtor — fonte canônica.
		return logUserID, nil
	}
	return 0, fmt.Errorf("entrada de log #ordem %d não tem user_id do produtor (linha de sistema)", orderID)
}

// ----- serialização auxiliar ----------------------------------------------

// Mantém imports ativos.
var _ = json.Marshal
