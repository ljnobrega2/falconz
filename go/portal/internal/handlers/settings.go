// Package handlers — handlers de configurações do Portal V2.
//
// Rotas (namespace /wp-json/senderzz/v1):
//
//	GET  /portal/settings       — retorna configurações do usuário
//	POST /portal/settings       — atualiza configurações
//	POST /portal/settings/2fa   — ativa/desativa 2FA por e-mail
//
// Configurações armazenadas em senderzz_portal_users.settings (JSONB):
//
//	{
//	  "pix_key":          string,   // chave PIX (CPF, CNPJ, e-mail, telefone, aleatória)
//	  "pix_key_tipo":     string,   // cpf | cnpj | email | telefone | aleatoria
//	  "notify_email":     bool,     // receber notificações por e-mail
//	  "notify_whatsapp":  bool      // receber notificações por WhatsApp
//	}
//
// shipping_class_id é armazenado na coluna dedicada (não no JSONB de settings)
// pois é referenciada por outros serviços (motoboy, session.go).
package handlers

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/senderzz/portal-service/internal/auth"
	"github.com/senderzz/portal-service/internal/httpx"
)

// SettingsHandler agrupa dependências dos handlers de configuração.
type SettingsHandler struct {
	Pool *pgxpool.Pool
}

// updateSettingsRequest é o body de POST /portal/settings.
type updateSettingsRequest struct {
	PIXKey          *string `json:"pix_key"`
	PIXKeyTipo      *string `json:"pix_key_tipo"`
	NotifyEmail     *bool   `json:"notify_email"`
	NotifyWhatsApp  *bool   `json:"notify_whatsapp"`
	ShippingClassID *int64  `json:"shipping_class_id"`
	// AutoEmitLabels: produtor habilita emissão automática de etiqueta ao aprovar pedido.
	AutoEmitLabels *bool `json:"auto_emit_labels"`
}

// twoFAToggleRequest é o body de POST /portal/settings/2fa.
type twoFAToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// ── GET /portal/settings ──────────────────────────────────────────────────────

// Get retorna as configurações do usuário autenticado.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var settingsRaw []byte
	var shippingClassID *int64
	var twofaEnabled bool

	err := h.Pool.QueryRow(r.Context(),
		`SELECT settings, shipping_class_id, twofa_enabled
		   FROM senderzz_portal_users
		  WHERE id = $1`,
		u.ID,
	).Scan(&settingsRaw, &shippingClassID, &twofaEnabled)
	if err != nil {
		slog.Error("[portal_settings] erro ao buscar configurações", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Parse auto_emit_labels from JSONB settings for convenient top-level access.
	var parsedSettings struct {
		AutoEmitLabels bool `json:"auto_emit_labels"`
	}
	_ = json.Unmarshal(settingsRaw, &parsedSettings)

	httpx.WriteOK(w, map[string]any{
		"settings":          json.RawMessage(settingsRaw),
		"shipping_class_id": shippingClassID,
		"twofa_enabled":     twofaEnabled,
		"auto_emit_labels":  parsedSettings.AutoEmitLabels,
	})
}

// ── POST /portal/settings ─────────────────────────────────────────────────────

// Update atualiza as configurações do usuário autenticado.
// Campos no body são todos opcionais — apenas os enviados são atualizados.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	// Atualiza shipping_class_id na coluna dedicada (referenciada por outros serviços).
	if req.ShippingClassID != nil {
		_, err := h.Pool.Exec(r.Context(),
			`UPDATE senderzz_portal_users SET shipping_class_id = $1 WHERE id = $2`,
			req.ShippingClassID, u.ID,
		)
		if err != nil {
			slog.Error("[portal_settings] erro ao atualizar shipping_class_id", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	// Atualiza campos do JSONB settings individualmente via jsonb_set.
	// Isso preserva campos existentes não enviados no request.
	type settingsField struct {
		key   string
		value any
	}

	fields := make([]settingsField, 0, 5)
	if req.PIXKey != nil {
		fields = append(fields, settingsField{key: "pix_key", value: *req.PIXKey})
	}
	if req.PIXKeyTipo != nil {
		fields = append(fields, settingsField{key: "pix_key_tipo", value: *req.PIXKeyTipo})
	}
	if req.NotifyEmail != nil {
		fields = append(fields, settingsField{key: "notify_email", value: *req.NotifyEmail})
	}
	if req.NotifyWhatsApp != nil {
		fields = append(fields, settingsField{key: "notify_whatsapp", value: *req.NotifyWhatsApp})
	}
	if req.AutoEmitLabels != nil {
		fields = append(fields, settingsField{key: "auto_emit_labels", value: *req.AutoEmitLabels})
	}

	for _, f := range fields {
		valueJSON, err := json.Marshal(f.value)
		if err != nil {
			continue
		}
		_, err = h.Pool.Exec(r.Context(),
			`UPDATE senderzz_portal_users
			    SET settings = jsonb_set(
			            COALESCE(settings, '{}'),
			            $1::text[],
			            $2::jsonb,
			            true
			        )
			  WHERE id = $3`,
			[]string{f.key}, valueJSON, u.ID,
		)
		if err != nil {
			slog.Error("[portal_settings] erro ao atualizar campo de settings",
				"user_id", u.ID, "field", f.key, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	slog.Info("[portal_settings] configurações atualizadas", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{"mensagem": "configurações atualizadas com sucesso"})
}

// ── POST /portal/settings/2fa ─────────────────────────────────────────────────

// Toggle2FA ativa ou desativa o 2FA por e-mail para o usuário autenticado.
// Quando desativado, remove qualquer código 2FA pendente.
func (h *SettingsHandler) Toggle2FA(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req twoFAToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	_, err := h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users SET twofa_enabled = $1 WHERE id = $2`,
		req.Enabled, u.ID,
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao atualizar twofa_enabled", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Se desativando, remove código 2FA pendente para não deixar lixo no banco.
	if !req.Enabled {
		_, _ = h.Pool.Exec(r.Context(),
			`DELETE FROM senderzz_portal_2fa WHERE user_id = $1`,
			u.ID,
		)
	}

	status := "desativado"
	if req.Enabled {
		status = "ativado"
	}

	slog.Info("[portal_settings] 2FA atualizado", "user_id", u.ID, "enabled", req.Enabled)
	httpx.WriteOK(w, map[string]any{
		"twofa_enabled": req.Enabled,
		"mensagem":      "2FA " + status + " com sucesso",
	})
}

// ── Marca do checkout (white-label v1) — senderzz_portal_user_meta ─────────────
//
// CHECKOUT-BRANDING: o PRODUTOR define o LOGO e a COR PRIMÁRIA do checkout dos
// SEUS links. Armazenado em senderzz_portal_user_meta (UNIQUE user_id,meta_key),
// keyed por PORTAL id (u.ID) — o MESMO id-space de senderzz_checkout_links.producer_id
// (links_portal.go grava producer_id = u.ID), que o go/orders GetOffer usa para LER
// a marca (resolveProducerBrand). Por isso o round-trip casa: o produtor grava com
// u.ID e o checkout lê com producer_id. Duas chaves:
//
//	sz_brand_logo_url       — URL https do logo (vazio = logo padrão FALK)
//	sz_brand_primary_color  — hex #RRGGBB / #RGB (vazio = azul FALK)
const (
	metaBrandLogoURL   = "sz_brand_logo_url"
	metaBrandColor     = "sz_brand_primary_color"
	metaBrandName      = "sz_brand_name"
	metaBrandBanner    = "sz_checkout_banner_url"
	metaBrandWhatsApp  = "sz_brand_whatsapp"
	metaBrandTextColor = "sz_brand_text_color"
)

// reHexColor valida cor hex #RGB ou #RRGGBB (espelha sanitizeHexColor do front).
var reHexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// brandSettingsRequest é o body de POST /portal/settings/brand. Campos ponteiro:
// apenas os ENVIADOS são gravados (nil = não mexe; "" = limpa = volta ao padrão FALK).
type brandSettingsRequest struct {
	Name         *string `json:"name"`
	LogoURL      *string `json:"logo_url"`
	PrimaryColor *string `json:"primary_color"`
	BannerURL    *string `json:"banner_url"`
	WhatsApp     *string `json:"whatsapp"`
	TextColor    *string `json:"text_color"`
}

func onlyDigitsSettings(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, value)
}

// GetBrand retorna a marca do checkout (logo + cor) do usuário autenticado.
// Sem role gate na leitura — devolve só a própria meta (vazia p/ quem nunca gravou).
func (h *SettingsHandler) GetBrand(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT meta_key, meta_value
		   FROM senderzz_portal_user_meta
		   WHERE user_id = $1 AND meta_key IN ($2, $3, $4, $5, $6, $7)`,
		u.ID, metaBrandLogoURL, metaBrandColor, metaBrandName, metaBrandBanner, metaBrandWhatsApp, metaBrandTextColor,
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao buscar marca do checkout", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer rows.Close()
	logoURL, primaryColor, name, bannerURL, whatsapp, textColor := "", "", "", "", "", ""
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			slog.Error("[portal_settings] erro ao ler marca do checkout", "user_id", u.ID, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		switch k {
		case metaBrandLogoURL:
			logoURL = v
		case metaBrandColor:
			primaryColor = v
		case metaBrandName:
			name = v
		case metaBrandBanner:
			bannerURL = v
		case metaBrandWhatsApp:
			whatsapp = v
		case metaBrandTextColor:
			textColor = v
		}
	}
	httpx.WriteOK(w, map[string]any{
		"logo_url":      logoURL,
		"primary_color": primaryColor,
		"name":          name,
		"banner_url":    bannerURL,
		"whatsapp":      whatsapp,
		"text_color":    textColor,
	})
}

// UpdateBrand grava a marca do checkout do produtor autenticado.
//
// Gate: EXCLUSIVO do produtor (isProdutorRole) — afiliado/operator recebem 403,
// consistente com a governança de produtor (afiliados_portal.go). O logo é
// validado como URL https:// (a CSP do checkout é img-src 'self' data: https: —
// um http:// passaria aqui mas não renderizaria no cliente). A cor é hex #RRGGBB.
func (h *SettingsHandler) UpdateBrand(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !isProdutorRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas produtores podem definir a marca do checkout")
		return
	}

	var req brandSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}

	type metaPair struct{ key, value string }
	pairs := make([]metaPair, 0, 6)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if len(name) > 80 {
			httpx.WriteErr(w, http.StatusBadRequest, "nome da marca muito longo")
			return
		}
		pairs = append(pairs, metaPair{metaBrandName, name})
	}

	if req.LogoURL != nil {
		logo := strings.TrimSpace(*req.LogoURL)
		if logo != "" {
			if !strings.HasPrefix(strings.ToLower(logo), "https://") && !strings.HasPrefix(logo, "/uploads/products/") {
				httpx.WriteErr(w, http.StatusBadRequest, "o logo deve ser uma URL https://")
				return
			}
			if len(logo) > 1000 {
				httpx.WriteErr(w, http.StatusBadRequest, "URL do logo muito longa")
				return
			}
		}
		pairs = append(pairs, metaPair{metaBrandLogoURL, logo})
	}
	if req.PrimaryColor != nil {
		color := strings.ToLower(strings.TrimSpace(*req.PrimaryColor))
		if color != "" && !reHexColor.MatchString(color) {
			httpx.WriteErr(w, http.StatusBadRequest, "cor inválida (use hex, ex.: #2563eb)")
			return
		}
		pairs = append(pairs, metaPair{metaBrandColor, color})
	}
	if req.BannerURL != nil {
		banner := strings.TrimSpace(*req.BannerURL)
		if banner != "" && !strings.HasPrefix(strings.ToLower(banner), "https://") && !strings.HasPrefix(banner, "/uploads/products/") {
			httpx.WriteErr(w, http.StatusBadRequest, "o banner deve ser uma URL https://")
			return
		}
		if len(banner) > 1000 {
			httpx.WriteErr(w, http.StatusBadRequest, "URL do banner muito longa")
			return
		}
		pairs = append(pairs, metaPair{metaBrandBanner, banner})
	}
	if req.WhatsApp != nil {
		whatsapp := strings.TrimSpace(*req.WhatsApp)
		if whatsapp != "" {
			whatsapp = onlyDigitsSettings(whatsapp)
			if len(whatsapp) < 10 || len(whatsapp) > 13 {
				httpx.WriteErr(w, http.StatusBadRequest, "WhatsApp inválido. Informe DDD e número.")
				return
			}
		}
		pairs = append(pairs, metaPair{metaBrandWhatsApp, whatsapp})
	}
	if req.TextColor != nil {
		color := strings.ToLower(strings.TrimSpace(*req.TextColor))
		if color != "" && !reHexColor.MatchString(color) {
			httpx.WriteErr(w, http.StatusBadRequest, "cor de texto inválida (use hex, ex.: #182235)")
			return
		}
		pairs = append(pairs, metaPair{metaBrandTextColor, color})
	}

	for _, p := range pairs {
		if _, err := h.Pool.Exec(r.Context(),
			`INSERT INTO senderzz_portal_user_meta (user_id, meta_key, meta_value)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (user_id, meta_key) DO UPDATE SET meta_value = EXCLUDED.meta_value`,
			u.ID, p.key, p.value,
		); err != nil {
			slog.Error("[portal_settings] erro ao gravar marca do checkout",
				"user_id", u.ID, "key", p.key, "err", err)
			httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
	}

	slog.Info("[portal_settings] marca do checkout atualizada", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{"mensagem": "marca do checkout atualizada com sucesso"})
}

// UploadPublicImage recebe logo/banner/foto de produto. Os arquivos são públicos
// por natureza e usam o mesmo volume/servidor estático das imagens de produto.
func (h *SettingsHandler) UploadPublicImage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}
	if !isProdutorRole(u.Role) {
		httpx.WriteErr(w, http.StatusForbidden, "apenas produtores podem enviar imagens")
		return
	}
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		httpx.WriteErr(w, 400, "multipart inválido")
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		httpx.WriteErr(w, 400, "campo image obrigatório")
		return
	}
	defer file.Close()
	if header.Size > 8<<20 {
		httpx.WriteErr(w, 413, "imagem excede 8MB")
		return
	}
	sniff := make([]byte, 512)
	n, readErr := io.ReadFull(file, sniff)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		httpx.WriteErr(w, 400, "imagem inválida")
		return
	}
	mime := http.DetectContentType(sniff[:n])
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		httpx.WriteErr(w, 500, "não foi possível ler a imagem")
		return
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[mime]
	if ext == "" {
		httpx.WriteErr(w, 400, "imagem precisa ser JPEG, PNG ou WEBP")
		return
	}
	dir := os.Getenv("PRODUCT_IMG_UPLOAD_PATH")
	if dir == "" {
		dir = "./uploads/products"
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		httpx.WriteErr(w, 500, "falha ao criar diretório")
		return
	}
	raw := make([]byte, 16)
	if _, err = crand.Read(raw); err != nil {
		httpx.WriteErr(w, 500, "falha ao gerar nome seguro")
		return
	}
	name := fmt.Sprintf("producer-%s%s", hex.EncodeToString(raw), ext)
	path := filepath.Join(dir, name)
	dst, err := os.Create(path)
	if err != nil {
		httpx.WriteErr(w, 500, "falha ao criar arquivo")
		return
	}
	if _, err = io.Copy(dst, file); err != nil {
		dst.Close()
		_ = os.Remove(path)
		httpx.WriteErr(w, 500, "falha ao gravar arquivo")
		return
	}
	if err = dst.Close(); err != nil {
		httpx.WriteErr(w, 500, "falha ao finalizar arquivo")
		return
	}
	httpx.WriteOK(w, map[string]any{"url": "/uploads/products/" + name})
}

// ── POST /portal/account/email ────────────────────────────────────────────────

// changeEmailRequest é o body de POST /portal/account/email.
// O front (Settings.tsx) envia { email } com o NOVO e-mail.
// AUDIT-2026-06-21 #9: também exige current_password (re-auth) antes de trocar o e-mail.
type changeEmailRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

// emailRegex — validação simples de formato (espelha is_email do WP).
var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ChangeEmail altera o e-mail de login do usuário do portal.
//
// Porte fiel de Portal_Page.php::ajax_change_email:
//   - valida formato do e-mail;
//   - rejeita se o e-mail já está em uso por OUTRO usuário (id <> self);
//   - atualiza senderzz_portal_users.email;
//   - NÃO altera wp_user_id — ele permanece como dono financeiro/carteira
//     (ver senderzz_portal_wallet_user_id em portal-helpers.php: a carteira usa
//     sempre o WP user_id, justamente para não perder saldo ao trocar o e-mail);
//   - revoga TODAS as sessões do portal (força novo login com o novo e-mail).
//
// NÃO toca a tabela de usuários do WP a partir do Go — a sincronização do
// e-mail no WP/carteira é feita pelo lado WP (não migrado), e a coluna dona
// financeira (wp_user_id) é deliberadamente preservada aqui.
func (h *SettingsHandler) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req changeEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	newEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRegex.MatchString(newEmail) {
		httpx.WriteErr(w, http.StatusBadRequest, "E-mail inválido.")
		return
	}

	// AUDIT-2026-06-21 #9: re-autenticação obrigatória antes de trocar o e-mail.
	// Sem isso, uma sessão comprometida troca o e-mail (sem senha) → reset de senha
	// e códigos 2FA passam a ir para o e-mail do atacante = takeover permanente.
	// Espelha ChangePassword (que já exige senha atual). VerifyPassword aceita
	// hash WP 6.8+ ("$wp$") além de bcrypt puro. // CRIT-WP-HASH
	if req.CurrentPassword == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe a senha atual.")
		return
	}
	var storedHash string
	if err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(password_hash, '') FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&storedHash); err != nil {
		slog.Error("[portal_settings] erro ao carregar hash de senha (change_email)", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if !auth.VerifyPassword(storedHash, req.CurrentPassword) {
		httpx.WriteErr(w, http.StatusForbidden, "Senha atual incorreta.")
		return
	}

	// Unicidade: rejeita se o e-mail já pertence a OUTRO usuário do portal.
	// Espelha "SELECT id ... WHERE email=%s AND id<>%d" do WP.
	var existsID int64
	err := h.Pool.QueryRow(r.Context(),
		`SELECT id FROM senderzz_portal_users WHERE LOWER(email) = $1 AND id <> $2 LIMIT 1`,
		newEmail, u.ID,
	).Scan(&existsID)
	if err == nil {
		httpx.WriteErr(w, http.StatusConflict, "Este e-mail já está em uso.")
		return
	}
	if err != pgx.ErrNoRows {
		slog.Error("[portal_settings] erro ao verificar unicidade de e-mail", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	// Atualiza o e-mail de login. wp_user_id é preservado (dono financeiro).
	_, err = h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users SET email = $1 WHERE id = $2`,
		newEmail, u.ID,
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao atualizar e-mail", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao atualizar e-mail do portal.")
		return
	}

	// Revoga todas as sessões — força novo login com o novo e-mail (espelha o WP,
	// que faz DELETE em senderzz_portal_sessions WHERE user_id).
	_, _ = h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_portal_sessions WHERE user_id = $1`,
		u.ID,
	)

	slog.Info("[portal_settings] e-mail alterado", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{
		"success":  true,
		"mensagem": "E-mail alterado. Faça login novamente com o novo e-mail.",
	})
}

// ── POST /portal/account/password ─────────────────────────────────────────────

// changePasswordRequest é o body de POST /portal/account/password.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// reHasLetter / reHasDigit — regras da nova senha (espelha o WP: ≥10 chars,
// pelo menos uma letra e um número).
var (
	reHasLetter = regexp.MustCompile(`[A-Za-z]`)
	reHasDigit  = regexp.MustCompile(`[0-9]`)
)

// ChangePassword altera a senha do usuário do portal.
//
// Porte fiel de Portal_Page.php::ajax_change_password:
//   - exige a senha ATUAL correta (bcrypt CompareHashAndPassword — mesmo esquema
//     do login em auth.go);
//   - nova senha: mínimo 10 caracteres, com letras E números (regra do WP, que é
//     mais estrita que os 8 chars validados no front — o servidor é a autoridade);
//   - grava o novo hash bcrypt (DefaultCost — igual ao cadastro em users_portal.go);
//   - revoga as DEMAIS sessões (mantém a atual viva; o front instrui novo login).
func (h *SettingsHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.CurrentPassword == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe a senha atual.")
		return
	}
	// Regra de força da nova senha — espelha o WP (≥10, letras + números).
	if len(req.NewPassword) < 10 || !reHasLetter.MatchString(req.NewPassword) || !reHasDigit.MatchString(req.NewPassword) {
		httpx.WriteErr(w, http.StatusBadRequest,
			"Nova senha deve ter no mínimo 10 caracteres com letras e números.")
		return
	}

	// Carrega o hash atual e verifica a senha atual via bcrypt (mesmo esquema do login).
	var storedHash string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(password_hash, '') FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&storedHash)
	if err != nil {
		slog.Error("[portal_settings] erro ao carregar hash de senha", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	// Verifica a senha atual aceitando o formato WP 6.8+ ("$wp$") além de bcrypt
	// puro — senão usuários reais (hash "$wp$") não conseguem trocar a senha. // CRIT-WP-HASH
	if !auth.VerifyPassword(storedHash, req.CurrentPassword) {
		httpx.WriteErr(w, http.StatusForbidden, "Senha atual incorreta.")
		return
	}

	// Gera o novo hash bcrypt (DefaultCost — compatível com o login e o cadastro).
	newHash, errHash := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if errHash != nil {
		slog.Error("[portal_settings] erro ao gerar hash da nova senha", "user_id", u.ID, "err", errHash)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}

	_, err = h.Pool.Exec(r.Context(),
		`UPDATE senderzz_portal_users SET password_hash = $1 WHERE id = $2`,
		string(newHash), u.ID,
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao atualizar senha", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao alterar a senha.")
		return
	}

	// Revoga as DEMAIS sessões (mantém a atual; o front pede novo login mesmo assim).
	_, _ = h.Pool.Exec(r.Context(),
		`DELETE FROM senderzz_portal_sessions WHERE user_id = $1 AND token <> $2`,
		u.ID, u.SessionToken,
	)

	slog.Info("[portal_settings] senha alterada", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{
		"success":  true,
		"mensagem": "Senha alterada. Faça login novamente.",
	})
}

// ── POST /portal/account/delete ───────────────────────────────────────────────

// lgpdAnonEmail gera o e-mail neutro usado ao anonimizar uma conta (LGPD).
// A coluna email é UNIQUE NOT NULL, então o valor precisa ser único por id
// (sufixo +{id}) e determinístico (idempotência: re-excluir não colide consigo
// mesmo). Domínio .local nunca é roteável — sem risco de e-mail real.
func lgpdAnonEmail(id int64) string {
	return "removido+" + strconv.FormatInt(id, 10) + "@lgpd.local"
}

// deleteAccountRequest é o body de POST /portal/account/delete.
// Exige a senha atual para confirmar a exclusão (anti-hijack: sessão roubada
// não basta para apagar/anonimizar a conta).
type deleteAccountRequest struct {
	Password string `json:"password"`
	Confirm  string `json:"confirm"` // o front envia "EXCLUIR" como dupla confirmação
}

// DeleteAccount — direito de exclusão LGPD (Art. 18, V/VI) com retenção fiscal.
// Marcador de auditoria: AUDIT LGPD-no-data-deletion-api
//
// NÃO é hard-delete da linha: o vínculo financeiro (wp_user_id → carteira
// tpc_carteira / tpc_transacoes / comissões) precisa sobreviver para a guarda
// contábil/fiscal obrigatória. O que se faz é ANONIMIZAR o dado pessoal:
//   - email   → removido+{id}@lgpd.local (lgpdAnonEmail; UNIQUE sem vazar real);
//   - nome    → "[Conta excluída]";
//   - settings (pix_key, telefone etc.) e integrations (tokens/secrets) → '{}';
//   - password_hash → ” (login fica impossível — bcrypt nunca casa com ”);
//   - twofa_enabled → false; ativo → false (status GENERATED vira 'inactive').
//
// wp_user_id, role, plano, created_at são preservados (ledger/auditoria).
// Revoga TODAS as sessões e remove códigos 2FA pendentes.
//
// Idempotente: reexecutar numa conta já anonimizada é no-op seguro.
func (h *SettingsHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	if req.Password == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "Informe a senha para confirmar a exclusão.")
		return
	}
	// Dupla confirmação: o front exige digitar "EXCLUIR" (anti-clique acidental numa
	// ação irreversível). Server é a autoridade — não confia só no JS. // LGPD-no-data-deletion-api
	if strings.ToUpper(strings.TrimSpace(req.Confirm)) != "EXCLUIR" {
		httpx.WriteErr(w, http.StatusBadRequest, `Digite "EXCLUIR" para confirmar a exclusão da conta.`)
		return
	}

	// Confirma a senha atual via bcrypt (mesmo esquema do login/ChangePassword).
	var storedHash string
	err := h.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(password_hash, '') FROM senderzz_portal_users WHERE id = $1`,
		u.ID,
	).Scan(&storedHash)
	if err != nil {
		slog.Error("[portal_settings] erro ao carregar hash p/ exclusão", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	// Aceita formato WP 6.8+ ("$wp$") além de bcrypt puro (mesmo motivo do login). // CRIT-WP-HASH
	if !auth.VerifyPassword(storedHash, req.Password) {
		httpx.WriteErr(w, http.StatusForbidden, "Senha incorreta.")
		return
	}

	// Anonimiza o dado pessoal numa transação (atomicidade: ou tudo, ou nada).
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		slog.Error("[portal_settings] erro ao abrir tx de exclusão", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro interno")
		return
	}
	defer tx.Rollback(r.Context())

	_, err = tx.Exec(r.Context(),
		`UPDATE senderzz_portal_users
		    SET email         = $2,
		        nome          = '[Conta excluída]',
		        settings      = '{}'::jsonb,
		        integrations  = '{}'::jsonb,
		        password_hash = '',
		        twofa_enabled = false,
		        ativo         = false
		  WHERE id = $1`,
		u.ID, lgpdAnonEmail(u.ID),
	)
	if err != nil {
		slog.Error("[portal_settings] erro ao anonimizar conta", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao excluir a conta.")
		return
	}

	// Remove sessões e códigos 2FA pendentes (não há mais dono ativo).
	_, _ = tx.Exec(r.Context(), `DELETE FROM senderzz_portal_sessions WHERE user_id = $1`, u.ID)
	_, _ = tx.Exec(r.Context(), `DELETE FROM senderzz_portal_2fa WHERE user_id = $1`, u.ID)

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("[portal_settings] erro ao commitar exclusão", "user_id", u.ID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "Erro ao excluir a conta.")
		return
	}

	slog.Info("[portal_settings] conta anonimizada (LGPD)", "user_id", u.ID)
	httpx.WriteOK(w, map[string]any{
		"success":  true,
		"mensagem": "Conta excluída. Seus dados pessoais foram anonimizados conforme a LGPD.",
	})
}
