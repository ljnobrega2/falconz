// Package handlers — recarga PIX (M-01): emissão e consulta de status.
//
// Porte FIEL de:
//   - tpc_endpoint_recarregar  (rest-api.php)  → PostRecarregar
//   - tpc_endpoint_pix_status  (rest-api.php)  → GetRecargaPix
//   - tpc_criar_recarga + tpc_gerar_pix_me (pix.php) → fluxo de criação/emissão
//
// Fluxo de PostRecarregar (espelha tpc_endpoint_recarregar):
//  1. valida valor >= 10 (mínimo do WP).
//  2. cria recarga 'pendente' em tpc_recargas (espelha tpc_criar_recarga).
//  3. emite PIX via Melhor Envio (POST /me/balance — espelha tpc_gerar_pix_me).
//  4. persiste me_pix_id, pix_qr (qr_src), pix_codigo (copia_cola), expires_at.
//  5. se a emissão falhar → cancela a recarga e retorna erro claro (sem panic).
//
// Mapeamento de colunas (schema-wallet.sql):
//
//	qr_src      → pix_qr
//	copia_cola  → pix_codigo
//	link/security_token NÃO têm coluna no Postgres — devolvidos só na resposta.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/wallet-service/internal/httpx"
	"github.com/senderzz/wallet-service/internal/melhorenvio"
	"github.com/senderzz/wallet-service/internal/middleware"
	"github.com/shopspring/decimal"
)

// valorMinimoRecarga espelha o piso de R$ 10,00 (tpc_criar_recarga: $valor < 10).
const valorMinimoRecarga = 10.0

// RecargaHandler agrupa as dependências do handler de recarga PIX.
type RecargaHandler struct {
	db *pgxpool.Pool
	me *melhorenvio.Client
}

// NewRecargaHandler cria um RecargaHandler com o pool e o cliente ME.
func NewRecargaHandler(db *pgxpool.Pool, me *melhorenvio.Client) *RecargaHandler {
	return &RecargaHandler{db: db, me: me}
}

// recarregarRequest é o corpo de POST /recarregar (espelha args 'valor').
type recarregarRequest struct {
	Valor float64 `json:"valor"`
}

// ── POST /recarregar ──────────────────────────────────────────────────────────

// PostRecarregar cria uma recarga e emite o PIX correspondente.
// Porte FIEL de tpc_endpoint_recarregar (rest-api.php).
func (h *RecargaHandler) PostRecarregar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	var req recarregarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return
	}
	// Espelha tpc_criar_recarga: $valor < 10 → false.
	if req.Valor < valorMinimoRecarga {
		httpx.WriteErr(w, http.StatusBadRequest, "valor mínimo de recarga é R$ 10,00")
		return
	}

	// Fail-fast em dev sem credencial ME (requisito: erro claro, nunca panic).
	if !h.me.HasToken() {
		reqLog(r).Error("[tpc_recarregar] ME_TOKEN ausente — não é possível emitir PIX", "user_id", userID)
		httpx.WriteErr(w, http.StatusServiceUnavailable, "emissão de PIX indisponível: token Melhor Envio não configurado")
		return
	}

	ctx := r.Context()

	// ── 1. Cria a recarga pendente (espelha tpc_criar_recarga) ────────────────
	recargaID, err := h.criarRecargaPendente(ctx, userID, req.Valor)
	if err != nil {
		reqLog(r).Error("[tpc_recarregar] erro ao criar recarga", "user_id", userID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "não foi possível criar a recarga interna")
		return
	}

	// ── 2. Token de segurança + redirect_url (espelha tpc_pix_gerar_token) ────
	securityToken := pixSecurityToken(recargaID)
	redirectURL := buildPixRedirectURL(recargaID, securityToken)

	// ── 3. Emite o PIX via Melhor Envio (espelha tpc_gerar_pix_me) ────────────
	pix, err := h.me.GerarPix(ctx, req.Valor, redirectURL)
	if err != nil {
		// Falha na emissão → cancela a recarga (espelha tpc_cancelar_recarga_pendente).
		h.cancelarRecarga(ctx, recargaID)
		if errors.Is(err, melhorenvio.ErrSemToken) {
			reqLog(r).Error("[tpc_recarregar] token ME ausente na emissão", "recarga_id", recargaID)
			httpx.WriteErr(w, http.StatusServiceUnavailable, "emissão de PIX indisponível: token Melhor Envio não configurado")
			return
		}
		reqLog(r).Error("[tpc_recarregar] erro ao gerar PIX", "recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// ── 4. Persiste dados do PIX na recarga ───────────────────────────────────
	// qr_src → pix_qr, copia_cola → pix_codigo, expires_at do timestamp da ME.
	if err := h.persistirPix(ctx, recargaID, pix); err != nil {
		reqLog(r).Error("[tpc_recarregar] erro ao persistir dados do PIX", "recarga_id", recargaID, "err", err)
		// Não cancela: o PIX foi emitido. Loga e segue — o usuário ainda recebe os dados.
	}

	reqLog(r).Info("[tpc_recarregar] recarga criada e PIX emitido",
		"recarga_id", recargaID,
		"user_id", userID,
		"valor", fmt.Sprintf("%.2f", req.Valor),
		"pix_id", pix.PixID,
		"has_qr", pix.QRSrc != "",
		"has_copia", pix.CopiaCola != "",
	)

	// Resposta espelha tpc_endpoint_recarregar (HTTP 201) + bloco "pix".
	httpx.WriteCreated(w, map[string]any{
		"recarga_id": recargaID,
		"valor":      req.Valor,
		"valor_fmt":  formatBRL(req.Valor),
		"expira_em":  pix.ExpiraEm,
		"pix": map[string]any{
			"recarga_id":     recargaID,
			"pix_id":         pix.PixID,
			"qr_src":         pix.QRSrc,
			"copia_cola":     pix.CopiaCola,
			"link":           pix.Link,
			"expira_em":      pix.ExpiraEm,
			"expires_ts":     pix.ExpiresTS,
			"security_token": securityToken,
		},
	})
}

// ── GET /recarga/{recarga_id}/pix ───────────────────────────────────────────

// GetRecargaPix retorna o status + dados do PIX de uma recarga do usuário.
// Porte FIEL de tpc_endpoint_pix_status (rest-api.php).
func (h *RecargaHandler) GetRecargaPix(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	recargaID, err := strconv.ParseInt(chi.URLParam(r, "recarga_id"), 10, 64)
	if err != nil || recargaID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "recarga_id inválido")
		return
	}

	// Carrega a recarga com os campos exibidos no portal.
	var (
		rUserID   int64
		status    string
		valor     string
		qrSrc     *string
		copiaCola *string
		expiresAt *int64 // epoch (extract(epoch) da coluna timestamptz)
	)
	err = h.db.QueryRow(r.Context(),
		`SELECT user_id, status, valor,
		        pix_qr, pix_codigo,
		        CASE WHEN expires_at IS NULL THEN NULL
		             ELSE EXTRACT(EPOCH FROM expires_at)::bigint END
		 FROM tpc_recargas
		 WHERE id = $1`,
		recargaID,
	).Scan(&rUserID, &status, &valor, &qrSrc, &copiaCola, &expiresAt)

	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rUserID != userID) {
		// Espelha PHP: !recarga || user_id divergente → 404.
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if err != nil {
		reqLog(r).Error("[tpc_pix_status] erro ao consultar recarga", "recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar recarga")
		return
	}

	pago := status == "confirmado"
	// em_analise: DIVERGÊNCIA CONHECIDA do WP. O CHECK de tpc_recargas.status
	// (schema-wallet.sql) não inclui 'analise', então o webhook mantém a recarga
	// como 'pendente' em vez de 'analise'. Consequência: para um PIX em análise,
	// o WP retornaria em_analise=true / "Em análise…", enquanto aqui retorna
	// em_analise=false / "Aguardando pagamento". Fidelidade total exige adicionar
	// 'analise' ao CHECK em infra/postgres/schema-wallet.sql (fora deste pacote).
	// Quando o CHECK passar a aceitar 'analise', este código já reflete o estado
	// corretamente sem alteração (status=='analise' → true).
	emAnalise := status == "analise"

	resp := map[string]any{
		"recarga_id":   recargaID,
		"status":       status,
		"pago":         pago,
		"em_analise":   emAnalise,
		"status_label": statusLabel(status),
		"qr_src":       deref(qrSrc),
		"copia_cola":   deref(copiaCola),
		// DIVERGÊNCIA CONHECIDA: o WP devolve o link real de pagamento (coluna 'link'),
		// mas tpc_recargas no Postgres (schema-wallet.sql) não tem coluna 'link'.
		// Retornamos "" — espelha o null-fallback do WP ($recarga['link'] ?? '') e é
		// mais seguro que injetar o copia-cola (EMV) num campo semanticamente de URL.
		// O link real continua disponível na resposta de POST /recarregar (pix.link).
		"link":       "",
		"expira_em":  expiresEpochToISO(expiresAt),
		"expires_ts": derefInt64(expiresAt),
	}

	// saldo/saldo_fmt só quando pago (espelha o match do PHP).
	if pago {
		saldo, errSaldo := h.consultarSaldo(r.Context(), userID)
		if errSaldo == nil {
			resp["saldo"] = saldo.StringFixed(2)
			resp["saldo_fmt"] = "R$ " + formatBRLNumber(saldo.InexactFloat64())
		} else {
			resp["saldo"] = nil
			resp["saldo_fmt"] = nil
		}
	} else {
		resp["saldo"] = nil
		resp["saldo_fmt"] = nil
	}

	httpx.WriteOK(w, resp)
}

// ── POST /pix/{recarga_id}/ja-paguei ────────────────────────────────────────

// PostPixJaPaguei registra que o cliente AFIRMA ter pago — fail-closed.
// Porte FIEL de tpc_endpoint_pix_ja_paguei (pix.php:469).
//
// IMPORTANTE (fail-closed por design): este endpoint NÃO confirma o pagamento
// nem credita saldo nem escreve status. Ele só:
//  1. valida a posse (JWT user == recarga.user_id; senão 404, igual ao PHP);
//  2. registra a intenção no log para rastreabilidade ([tpc_pix] ja_paguei);
//  3. devolve a mensagem de prazo (confirmado=true se já estiver confirmado pelos
//     mecanismos automáticos; senão confirmado=false + "...em até 30 minutos...").
//
// O crédito real ocorre SOMENTE via confirmação verdadeira:
//   - webhook PIX (PostPixWebhook → confirmarRecarga, credita idempotente), ou
//   - reconciliação por cron (jobs.ReconcileTask: expira pendentes vencidas; a
//     confirmação por consulta à ME é responsabilidade do webhook/redirect do WP).
//
// "Dispara reconciliação daquela recarga" no mundo Go = sinalizar, não confirmar:
// não existe método de consultar status de um PIX específico na ME aqui (o client
// só EMITE PIX), e o reconcile job é global (não por-recarga). Forçar um confirm
// por-recarga aqui violaria o fail-closed (V-NEW-01/02 do WP) — então apenas
// sinalizamos via log, espelhando 1:1 o que o PHP faz.
//
// Por que NÃO marcamos status='analise'/'aguardando-conferencia': o CHECK de
// tpc_recargas.status (infra/postgres/schema-wallet.sql — fora deste pacote) não
// inclui esses valores; um UPDATE compilaria mas lançaria check_violation em
// runtime. O PHP também não muda status aqui — apenas loga.
func (h *RecargaHandler) PostPixJaPaguei(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == 0 {
		httpx.WriteErr(w, http.StatusUnauthorized, "não autenticado")
		return
	}

	recargaID, err := strconv.ParseInt(chi.URLParam(r, "recarga_id"), 10, 64)
	if err != nil || recargaID <= 0 {
		httpx.WriteErr(w, http.StatusBadRequest, "recarga_id inválido")
		return
	}

	// Carrega user_id + status da recarga (espelha tpc_get_recarga + checagem de posse).
	var (
		rUserID int64
		status  string
	)
	err = h.db.QueryRow(r.Context(),
		`SELECT user_id, status FROM tpc_recargas WHERE id = $1`,
		recargaID,
	).Scan(&rUserID, &status)

	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rUserID != userID) {
		// Espelha PHP: !recarga || user_id divergente → 404.
		httpx.WriteErr(w, http.StatusNotFound, "recarga não encontrada")
		return
	}
	if err != nil {
		reqLog(r).Error("[tpc_pix] erro ao consultar recarga em ja-paguei", "recarga_id", recargaID, "err", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao consultar recarga")
		return
	}

	// Registra a intenção no log (espelha tpc_pix_log('ja_paguei', ...)).
	reqLog(r).Info("[tpc_pix] ja_paguei",
		"recarga_id", recargaID,
		"user_id", userID,
		"status", status,
		"ip", clientIP(r),
	)

	// Se já confirmado pelos mecanismos automáticos, avisa o cliente.
	if status == "confirmado" {
		httpx.WriteOK(w, map[string]any{
			"confirmado": true,
			"mensagem":   "Seu pagamento já foi confirmado! Verifique seu saldo.",
		})
		return
	}

	// Mensagem de prazo — sem creditar nem alterar status (fail-closed).
	httpx.WriteOK(w, map[string]any{
		"confirmado": false,
		"mensagem":   "Recebemos sua confirmação. O crédito será processado em até 30 minutos. Em casos de análise pelo banco, pode levar até 24 horas.",
	})
}

// ── helpers de banco ──────────────────────────────────────────────────────────

// criarRecargaPendente insere uma recarga 'pendente' e retorna o id.
// Espelha tpc_criar_recarga (sem o me_pix_id ainda — só após a emissão).
func (h *RecargaHandler) criarRecargaPendente(ctx context.Context, userID int64, valor float64) (int64, error) {
	var id int64
	err := h.db.QueryRow(ctx,
		`INSERT INTO tpc_recargas (user_id, valor, status, expires_at)
		 VALUES ($1, $2, 'pendente', NOW() + INTERVAL '15 minutes')
		 RETURNING id`,
		userID, fmt.Sprintf("%.2f", valor),
	).Scan(&id)
	return id, err
}

// persistirPix grava me_pix_id, pix_qr, pix_codigo e expires_at após a emissão.
// Espelha o $wpdb->update da recarga em tpc_gerar_pix_me.
func (h *RecargaHandler) persistirPix(ctx context.Context, recargaID int64, pix *melhorenvio.PixResult) error {
	var mePixID *string
	if pix.PixID != "" {
		mePixID = &pix.PixID
	}
	_, err := h.db.Exec(ctx,
		`UPDATE tpc_recargas
		 SET me_pix_id  = $1,
		     pix_qr     = $2,
		     pix_codigo = $3,
		     expires_at = CASE WHEN $4::bigint > 0
		                       THEN to_timestamp($4::bigint)
		                       ELSE expires_at END
		 WHERE id = $5`,
		mePixID, pix.QRSrc, pix.CopiaCola, pix.ExpiresTS, recargaID,
	)
	return err
}

// cancelarRecarga marca a recarga como 'cancelado' (espelha tpc_cancelar_recarga).
func (h *RecargaHandler) cancelarRecarga(ctx context.Context, recargaID int64) {
	_, err := h.db.Exec(ctx,
		`UPDATE tpc_recargas
		 SET status = 'cancelado'
		 WHERE id = $1 AND status = 'pendente'`,
		recargaID,
	)
	if err != nil {
		slog.Error("[tpc_recarregar] erro ao cancelar recarga após falha", "recarga_id", recargaID, "err", err)
	}
}

// ── helpers de token / URL ────────────────────────────────────────────────────

// pixSecurityToken espelha tpc_pix_gerar_token:
//
//	substr(hash_hmac('sha256', 'pix_retorno_'.$recarga_id, $secret), 0, 32)
//
// O secret é o mesmo tpc_jwt_secret do WP → env JWT_SECRET.
func pixSecurityToken(recargaID int64) string {
	secret := os.Getenv("JWT_SECRET")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("pix_retorno_%d", recargaID)))
	full := hex.EncodeToString(mac.Sum(nil))
	if len(full) > 32 {
		return full[:32]
	}
	return full
}

// buildPixRedirectURL monta a redirect_url do retorno PIX.
// Espelha add_query_arg([recarga_id, sz_token], rest_url('tp-carteira/v1/pix/retorno')).
//
// A base vem de WP_REST_BASE_URL e DEVE ser ABSOLUTA (ex: https://app.senderzz.com.br/wp-json):
// o rest_url() do WP é sempre absoluto e a API ME exige redirect_url absoluta —
// uma base relativa faz a emissão retornar 4xx. Se WP_REST_BASE_URL não estiver
// configurada, caímos no caminho relativo apenas para não quebrar dev local;
// configure-a em produção/staging junto com ME_TOKEN.
func buildPixRedirectURL(recargaID int64, token string) string {
	base := strings.TrimRight(os.Getenv("WP_REST_BASE_URL"), "/")
	if base == "" {
		// Sem base configurada: caminho relativo (dev). Em produção, defina WP_REST_BASE_URL.
		base = "/wp-json"
	}
	return fmt.Sprintf("%s/tp-carteira/v1/pix/retorno?recarga_id=%d&sz_token=%s", base, recargaID, token)
}

// ── helpers de apresentação (espelham number_format / match do PHP) ───────────

// statusLabel espelha o match($recarga['status']) de tpc_endpoint_pix_status.
func statusLabel(status string) string {
	switch status {
	case "confirmado":
		return "Confirmado"
	case "analise":
		return "Em análise — aguardando confirmação do banco"
	case "cancelado":
		return "Cancelado"
	case "expirado":
		return "Expirado"
	default:
		return "Aguardando pagamento"
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// expiresEpochToISO converte o epoch da coluna expires_at para string ISO-8601 UTC.
// Espelha o $recarga['expires_at'] devolvido pelo PHP (string de data).
// Retorna "" quando não há expires_at.
func expiresEpochToISO(epoch *int64) string {
	if epoch == nil || *epoch <= 0 {
		return ""
	}
	return time.Unix(*epoch, 0).UTC().Format("2006-01-02 15:04:05")
}

// consultarSaldo lê o saldo da carteira do usuário (leitura simples, sem lock).
// Espelha tpc_get_saldo. Retorna 0 quando o usuário não tem carteira.
func (h *RecargaHandler) consultarSaldo(ctx context.Context, userID int64) (decimal.Decimal, error) {
	var sStr string
	err := h.db.QueryRow(ctx,
		`SELECT saldo FROM tpc_carteira WHERE user_id = $1`,
		userID,
	).Scan(&sStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(sStr)
}

// formatBRL espelha 'R$ ' . number_format($valor, 2, ',', '.') do PHP.
func formatBRL(v float64) string {
	return "R$ " + formatBRLNumber(v)
}

// formatBRLNumber formata um float no padrão brasileiro: milhar com '.', decimal com ','.
func formatBRLNumber(v float64) string {
	// Formata com 2 casas e ponto decimal padrão, depois troca separadores.
	s := fmt.Sprintf("%.2f", v) // ex: "1234.50"
	intPart, decPart := s, "00"
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
		decPart = s[dot+1:]
	}
	neg := false
	if len(intPart) > 0 && intPart[0] == '-' {
		neg = true
		intPart = intPart[1:]
	}
	// Insere '.' a cada 3 dígitos da direita para a esquerda.
	var grouped []byte
	n := len(intPart)
	for i := 0; i < n; i++ {
		if i > 0 && (n-i)%3 == 0 {
			grouped = append(grouped, '.')
		}
		grouped = append(grouped, intPart[i])
	}
	out := string(grouped) + "," + decPart
	if neg {
		out = "-" + out
	}
	return out
}
