// Package handlers — endpoint admin TpcConfig.
// Espelha tab_tpc_configuracoes() em includes/tpc/admin.php:656 (PHP legado)
// sobre Postgres. Lê/grava configurações em `senderzz_options` (key/value).
//
// Convenções importantes:
//   - Booleans no PHP são persistidos como string 'yes' (checked) ou
//     ausente/'' (unchecked) — get_option('chave', 'yes')+checked($v,'yes').
//     Aqui aceitamos 'yes'|'1'|'true' como verdadeiro e ESCREVEMOS sempre
//     'yes'/'no' para manter compat bidirecional com o lado PHP.
//   - Segredos podem ser injetados por variáveis de ambiente
//     (SENDERZZ_ME_TOKEN, SENDERZZ_WEBHOOK_SECRET, SENDERZZ_JWT_SECRET).
//     Quando setadas, o handler retorna *_from_env=true e bloqueia escrita.
//   - Shipping classes não existem no Go admin (sem wp_term_taxonomy).
//     A listagem em /wallet-owners enumera apenas os IDs presentes no
//     mapa JSON `senderzz_shipping_class_wallet_owners`. UI permite
//     adicionar class_id manualmente.
//   - Mascaramento de segredos: "••• " + últimos 4. Vazio retorna "".
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type TpcConfigHandler struct{ Pool *pgxpool.Pool }

// ----- helpers ------------------------------------------------------------

// tableExists verifica presença de uma tabela no schema public.
func (h *TpcConfigHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// getOption lê uma option string (default se não existir / tabela ausente).
func (h *TpcConfigHandler) getOption(ctx context.Context, key, def string) string {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw); err != nil {
		return def
	}
	return raw
}

// getOptionBool lê uma option boolean. Aceita 'yes'|'1'|'true'.
// `def` é usado quando a option não existe (paridade com get_option(,'yes')).
func (h *TpcConfigHandler) getOptionBool(ctx context.Context, key string, def bool) bool {
	if !h.tableExists(ctx, "senderzz_options") {
		return def
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"=$1`, key).Scan(&raw); err != nil {
		return def
	}
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return def
	}
	return v == "yes" || v == "1" || v == "true" || v == "on"
}

// getOptionInt lê uma option inteira com fallback.
func (h *TpcConfigHandler) getOptionInt(ctx context.Context, key string, def int) int {
	raw := h.getOption(ctx, key, "")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}

// getOptionFloatVal lê uma option float com fallback.
func (h *TpcConfigHandler) getOptionFloatVal(ctx context.Context, key string, def float64) float64 {
	raw := h.getOption(ctx, key, "")
	if raw == "" {
		return def
	}
	s := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

// upsertOption grava option (UPSERT em senderzz_options).
func (h *TpcConfigHandler) upsertOption(ctx context.Context, key, value string) error {
	if !h.tableExists(ctx, "senderzz_options") {
		return nil
	}
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ($1, $2)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, key, value)
	return err
}

// maskSecret aplica máscara "••• " + últimos 4 chars. "" → "".
func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		// Para segredos muito curtos não revela nada além de ocultos.
		return "•••"
	}
	return "••• " + s[len(s)-4:]
}

// envHasValue retorna true se a variável de ambiente está setada e não vazia.
func envHasValue(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}

// buildWebhookURL monta a URL pública do webhook PIX.
// Usa APP_BASE_URL se setado; senão fallback fixo de produção (admin.php:703).
func buildWebhookURL() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
	if base == "" {
		base = "https://app.senderzz.com.br"
	}
	return base + "/wp-json/tp-carteira/v1/webhook/pix"
}

// boolToYesNo serializa para o formato esperado pelo PHP legacy ('yes'/'no').
func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// genHexSecret gera N bytes aleatórios e retorna em hex (2N chars).
// Para 48 chars hex usamos 24 bytes.
func genHexSecret(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ----- payloads -----------------------------------------------------------

// tpcConfigME — bloco "Melhor Envio" da resposta GET.
type tpcConfigME struct {
	TokenMasked          string  `json:"token_masked"`
	TokenFromEnv         bool    `json:"token_from_env"`
	WebhookSecretMasked  string  `json:"webhook_secret_masked"`
	WebhookSecretFromEnv bool    `json:"webhook_secret_from_env"`
	JWTSecretFromEnv     bool    `json:"jwt_secret_from_env"`
	WebhookURL           string  `json:"webhook_url"`
	SaldoAtualME         float64 `json:"saldo_atual_me"`
	SaldoAt              string  `json:"saldo_at"`
}

// tpcConfigRegras — regras da carteira.
type tpcConfigRegras struct {
	SaldoMinimo float64 `json:"saldo_minimo"`
}

// tpcConfigMotor — toggles + Motor Senderzz.
type tpcConfigMotor struct {
	PixValidMinutes        int  `json:"pix_valid_minutes"`
	PixAutoCancelExpired   bool `json:"pix_auto_cancel_expired"`
	EnforceWalletOnLabel   bool `json:"enforce_wallet_on_label"`
	BlockDuplicateLabel    bool `json:"block_duplicate_label"`
	CheckoutTemplateID     int  `json:"checkout_template_id"`
}

// tpcConfigResp — resposta completa do GET /tpc-config.
type tpcConfigResp struct {
	ME     tpcConfigME     `json:"me"`
	Regras tpcConfigRegras `json:"regras"`
	Motor  tpcConfigMotor  `json:"motor"`
}

// ----- GET /tpc-config ----------------------------------------------------

func (h *TpcConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Token / secret podem vir do banco OU do env. Env "vence" e bloqueia escrita.
	tokenFromEnv := envHasValue("SENDERZZ_ME_TOKEN")
	whFromEnv := envHasValue("SENDERZZ_WEBHOOK_SECRET")
	jwtFromEnv := envHasValue("SENDERZZ_JWT_SECRET")

	rawToken := h.getOption(ctx, "tpc_me_token", "")
	if tokenFromEnv {
		rawToken = os.Getenv("SENDERZZ_ME_TOKEN")
	}
	rawSecret := h.getOption(ctx, "tpc_webhook_secret", "")
	if whFromEnv {
		rawSecret = os.Getenv("SENDERZZ_WEBHOOK_SECRET")
	}

	out := tpcConfigResp{
		ME: tpcConfigME{
			TokenMasked:          maskSecret(rawToken),
			TokenFromEnv:         tokenFromEnv,
			WebhookSecretMasked:  maskSecret(rawSecret),
			WebhookSecretFromEnv: whFromEnv,
			JWTSecretFromEnv:     jwtFromEnv,
			WebhookURL:           buildWebhookURL(),
			SaldoAtualME:         h.getOptionFloatVal(ctx, "tpc_saldo_atual_me", 0),
			SaldoAt:              h.getOption(ctx, "tpc_saldo_atual_me_at", ""),
		},
		Regras: tpcConfigRegras{
			SaldoMinimo: h.getOptionFloatVal(ctx, "tpc_saldo_minimo", 0),
		},
		Motor: tpcConfigMotor{
			PixValidMinutes:      h.getOptionInt(ctx, "tpc_pix_valid_minutes", 30),
			PixAutoCancelExpired: h.getOptionBool(ctx, "tpc_pix_auto_cancel_expired", true),
			EnforceWalletOnLabel: h.getOptionBool(ctx, "senderzz_enforce_wallet_on_label", true),
			BlockDuplicateLabel:  h.getOptionBool(ctx, "senderzz_block_duplicate_label", true),
			CheckoutTemplateID:   h.getOptionInt(ctx, "senderzz_checkout_template_id", 140),
		},
	}
	httpx.JSON(w, 200, out)
}

// ----- POST /tpc-config ---------------------------------------------------

// tpcConfigSave — payload do POST. Ponteiros permitem distinguir
// "não enviado" de "enviado vazio" (string vazia limpa, ausente = mantém).
type tpcConfigSave struct {
	MeToken              *string  `json:"me_token"`
	WebhookSecret        *string  `json:"webhook_secret"`
	SaldoMinimo          *float64 `json:"saldo_minimo"`
	PixValidMinutes      *int     `json:"pix_valid_minutes"`
	PixAutoCancelExpired *bool    `json:"pix_auto_cancel_expired"`
	EnforceWalletOnLabel *bool    `json:"enforce_wallet_on_label"`
	BlockDuplicateLabel  *bool    `json:"block_duplicate_label"`
	CheckoutTemplateID   *int     `json:"checkout_template_id"`
}

func (h *TpcConfigHandler) Save(w http.ResponseWriter, r *http.Request) {
	var in tpcConfigSave
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	ctx := r.Context()

	// Token ME — só grava se NÃO está vindo do env.
	if in.MeToken != nil && !envHasValue("SENDERZZ_ME_TOKEN") {
		if err := h.upsertOption(ctx, "tpc_me_token", strings.TrimSpace(*in.MeToken)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Webhook secret — idem.
	if in.WebhookSecret != nil && !envHasValue("SENDERZZ_WEBHOOK_SECRET") {
		if err := h.upsertOption(ctx, "tpc_webhook_secret", strings.TrimSpace(*in.WebhookSecret)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Saldo mínimo — number, default 0.
	if in.SaldoMinimo != nil {
		val := *in.SaldoMinimo
		if val < 0 {
			val = 0
		}
		if err := h.upsertOption(ctx, "tpc_saldo_minimo", strconv.FormatFloat(val, 'f', 2, 64)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// PIX válido em minutos — range 5-1440.
	if in.PixValidMinutes != nil {
		v := *in.PixValidMinutes
		if v < 5 || v > 1440 {
			httpx.Err(w, 400, "out_of_range", "pix_valid_minutes deve estar entre 5 e 1440")
			return
		}
		if err := h.upsertOption(ctx, "tpc_pix_valid_minutes", strconv.Itoa(v)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Booleans — sempre persistir como 'yes'/'no' (paridade PHP).
	if in.PixAutoCancelExpired != nil {
		if err := h.upsertOption(ctx, "tpc_pix_auto_cancel_expired", boolToYesNo(*in.PixAutoCancelExpired)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.EnforceWalletOnLabel != nil {
		if err := h.upsertOption(ctx, "senderzz_enforce_wallet_on_label", boolToYesNo(*in.EnforceWalletOnLabel)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}
	if in.BlockDuplicateLabel != nil {
		if err := h.upsertOption(ctx, "senderzz_block_duplicate_label", boolToYesNo(*in.BlockDuplicateLabel)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Checkout template ID — inteiro positivo.
	if in.CheckoutTemplateID != nil {
		v := *in.CheckoutTemplateID
		if v < 1 {
			v = 140
		}
		if err := h.upsertOption(ctx, "senderzz_checkout_template_id", strconv.Itoa(v)); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
	}

	// Retorna estado atualizado para o cliente sincronizar.
	h.Get(w, r)
}

// ----- GET /tpc-config/wallet-owners --------------------------------------

// walletOwnerItem — entrada da tabela "Dono financeiro por classe".
type walletOwnerItem struct {
	ClassID   int    `json:"class_id"`
	ClassName string `json:"class_name"`
	UserID    int64  `json:"user_id"`
	UserNome  string `json:"user_nome"`
	UserEmail string `json:"user_email"`
}

func (h *TpcConfigHandler) GetWalletOwners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []walletOwnerItem{}

	// Mapa JSON existente em option (class_id_string → user_id).
	mp := map[string]any{}
	raw := h.getOption(ctx, "senderzz_shipping_class_wallet_owners", "")
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &mp)
	}

	// Resolve nome/email do usuário via senderzz_portal_users (se disponível).
	hasUsers := h.tableExists(ctx, "senderzz_portal_users")

	for kStr, vAny := range mp {
		classID, errC := strconv.Atoi(strings.TrimSpace(kStr))
		if errC != nil {
			continue
		}
		// Aceita number ou string vinda do JSON.
		var userID int64
		switch v := vAny.(type) {
		case float64:
			userID = int64(v)
		case string:
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			userID = n
		case json.Number:
			n, _ := v.Int64()
			userID = n
		}

		item := walletOwnerItem{
			ClassID:   classID,
			ClassName: "Classe #" + strconv.Itoa(classID),
			UserID:    userID,
		}

		if hasUsers && userID > 0 {
			var nome, email string
			err := h.Pool.QueryRow(ctx,
				`SELECT COALESCE(nome,''), COALESCE(email,'')
				 FROM senderzz_portal_users
				 WHERE id=$1 OR wp_user_id=$1
				 LIMIT 1`, userID).Scan(&nome, &email)
			if err == nil {
				item.UserNome = nome
				item.UserEmail = email
			}
		}

		out = append(out, item)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}

// ----- POST /tpc-config/wallet-owners -------------------------------------

type rawWalletOwnerItem struct {
	ClassID int   `json:"class_id"`
	UserID  int64 `json:"user_id"`
}

func (h *TpcConfigHandler) SaveWalletOwners(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []rawWalletOwnerItem `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// Monta mapa class_id → user_id. user_id <= 0 remove a entrada.
	mp := map[string]int64{}
	for _, it := range body.Items {
		if it.ClassID < 0 {
			continue
		}
		if it.UserID > 0 {
			mp[strconv.Itoa(it.ClassID)] = it.UserID
		}
	}

	b, err := json.Marshal(mp)
	if err != nil {
		httpx.Err(w, 500, "encode_error", err.Error())
		return
	}
	if err := h.upsertOption(r.Context(), "senderzz_shipping_class_wallet_owners", string(b)); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true})
}

// ----- POST /tpc-config/regenerate-secret ---------------------------------

type regenerateBody struct {
	Which string `json:"which"`
}

func (h *TpcConfigHandler) RegenerateSecret(w http.ResponseWriter, r *http.Request) {
	var in regenerateBody
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}
	which := strings.ToLower(strings.TrimSpace(in.Which))

	var optionKey, envKey string
	switch which {
	case "webhook":
		optionKey = "tpc_webhook_secret"
		envKey = "SENDERZZ_WEBHOOK_SECRET"
	case "jwt":
		optionKey = "tpc_jwt_secret"
		envKey = "SENDERZZ_JWT_SECRET"
	default:
		httpx.Err(w, 400, "invalid_which", "which deve ser 'webhook' ou 'jwt'")
		return
	}

	// Se o segredo é gerenciado por env, não permite regeneração via UI.
	if envHasValue(envKey) {
		httpx.Err(w, 403, "env_managed", "segredo gerenciado por variável de ambiente — não pode ser regenerado pela UI")
		return
	}

	// 48 chars hex = 24 bytes aleatórios.
	secret, err := genHexSecret(24)
	if err != nil {
		httpx.Err(w, 500, "rand_error", err.Error())
		return
	}
	if err := h.upsertOption(r.Context(), optionKey, secret); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}

	httpx.JSON(w, 200, map[string]any{
		"ok":     true,
		"which":  which,
		"masked": maskSecret(secret),
	})
}

// ----- GET /tpc-config/me-balance -----------------------------------------
// Busca o saldo ao vivo da conta ME via GET /api/v2/me/balance.
// Espelha tpc_consultar_saldo_me() em includes/tpc/pix.php:283.
// PHP chama o ME a cada page load sem persistir — idem aqui (sem escrita no DB).

func (h *TpcConfigHandler) GetMEBalance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Resolve token com mesma precedência env→DB do GET /tpc-config.
	token := os.Getenv("SENDERZZ_ME_TOKEN")
	if token == "" {
		token = h.getOption(ctx, "tpc_me_token", "")
	}
	if strings.TrimSpace(token) == "" {
		httpx.JSON(w, 200, map[string]any{
			"balance":    0.0,
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"erro":       "token ME não configurado",
		})
		return
	}

	// Base URL: usa env ME_API_BASE se setado (suporte sandbox), senão produção.
	apiBase := strings.TrimRight(strings.TrimSpace(os.Getenv("ME_API_BASE")), "/")
	if apiBase == "" {
		apiBase = "https://www.melhorenvio.com.br/api/v2"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/me/balance", nil)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{
			"balance":    0.0,
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"erro":       fmt.Sprintf("erro ao criar requisição: %s", err.Error()),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Senderzz Logistics (suporte@app.senderzz.com.br)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		httpx.JSON(w, 200, map[string]any{
			"balance":    0.0,
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"erro":       fmt.Sprintf("erro na chamada ME: %s", err.Error()),
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		httpx.JSON(w, 200, map[string]any{
			"balance":    0.0,
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"erro":       fmt.Sprintf("ME retornou HTTP %d", resp.StatusCode),
		})
		return
	}

	// Resposta esperada: {"balance": float, ...}
	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		httpx.JSON(w, 200, map[string]any{
			"balance":    0.0,
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
			"erro":       "resposta ME inválida",
		})
		return
	}

	var balance float64
	if v, ok := data["balance"]; ok {
		switch b := v.(type) {
		case float64:
			balance = b
		case json.Number:
			balance, _ = b.Float64()
		case string:
			s := strings.TrimSpace(strings.ReplaceAll(b, ",", "."))
			balance, _ = strconv.ParseFloat(s, 64)
		}
	}

	httpx.JSON(w, 200, map[string]any{
		"balance":    balance,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ----- GET /shipping-classes ----------------------------------------------
// Lista as classes de entrega disponíveis para o dropdown de wallet-owners.
// Espelha WC()->shipping->get_shipping_classes() + linha id=0 "Produtos sem classe".
// Reutiliza a mesma tabela senderzz_shipping_classes que /expedicao/shipping-classes.
// Degradação graciosa se tabela não existir → retorna só a linha id=0.

func (h *TpcConfigHandler) GetShippingClasses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Linha id=0 — "Produtos sem classe" (paridade com PHP: term_id=0).
	items := []map[string]any{
		{"id": 0, "name": "Produtos sem classe"},
	}

	if h.tableExists(ctx, "senderzz_shipping_classes") {
		rows, err := h.Pool.Query(ctx,
			`SELECT id, COALESCE(name,'') FROM senderzz_shipping_classes ORDER BY name ASC, id ASC`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int64
				var name string
				if scanErr := rows.Scan(&id, &name); scanErr != nil {
					continue
				}
				if strings.TrimSpace(name) == "" {
					name = fmt.Sprintf("Classe #%d", id)
				}
				items = append(items, map[string]any{"id": id, "name": name})
			}
		}
	}

	httpx.JSON(w, 200, map[string]any{"items": items})
}

// ----- GET /users/search --------------------------------------------------
// Busca usuários do portal (nome + email) para o dropdown de wallet-owners.
// Espelha get_users(['fields'=>['ID','display_name','user_email'],'number'=>500]) do PHP.
// ?q= filtro ILIKE opcional. Limite máximo 500.

type userSearchItem struct {
	ID       int64  `json:"id"`
	WPUserID *int64 `json:"wp_user_id"`
	Nome     string `json:"nome"`
	Email    string `json:"email"`
}

func (h *TpcConfigHandler) GetUsersSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	out := []userSearchItem{}

	if !h.tableExists(ctx, "senderzz_portal_users") {
		httpx.JSON(w, 200, map[string]any{"items": out})
		return
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT id, wp_user_id, COALESCE(nome,''), COALESCE(email,'')
		 FROM senderzz_portal_users
		 WHERE ($1='' OR nome ILIKE '%'||$1||'%' OR email ILIKE '%'||$1||'%')
		 ORDER BY nome ASC, email ASC
		 LIMIT 500`, q)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var u userSearchItem
		if scanErr := rows.Scan(&u.ID, &u.WPUserID, &u.Nome, &u.Email); scanErr != nil {
			continue
		}
		out = append(out, u)
	}

	httpx.JSON(w, 200, map[string]any{"items": out})
}
