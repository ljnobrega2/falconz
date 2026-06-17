// Package handlers — endpoint admin para etiquetas de pedidos motoboy (QR + print).
//
// Espelha sz_mb_tab_etiquetas() em includes/motoboy/admin.php:1383 (§2.5) sobre
// Postgres. O front renderiza o grid de etiquetas em CSS print-friendly; aqui
// só montamos a lista de pedidos + dados de endereço + package_code com QR.
//
// Surface:
//
//	GET /motoboy-etiquetas?date=YYYY-MM-DD&status=
//	    → items com tudo necessário para a etiqueta (cliente, endereço, valor,
//	      forma de pgto, QR code).
//
// O package_code reproduz EXATAMENTE o algoritmo de
// sz_mbc_package_code() em includes/senderzz-motoboy-custody.php:90, para
// que o leitor de QR do PWA (que valida códigos PHP) também valide os Go
// gerados aqui:
//
//	seed = pedido_id . "|" . wc_order_id
//	sig  = strtoupper( substr( hmac_sha256( seed, WP_SALT_AUTH ), 0, 14 ) )
//	code = "SZ-" + wc_order_id + "-" + pedido_id + "-" + sig
//
// Importante: a chave HMAC vem de env (WP_SALT_AUTH / MOTOBOY_INTERNAL_SECRET /
// TPC_WEBHOOK_SECRET — getenv com fallback). Não há mirror de wp_options no
// Postgres atual, portanto o "option fallback" do enunciado degrada para chave
// vazia quando nenhuma env está definida — o código continua sendo gerado
// (operação não falha), apenas a assinatura fica determinística sobre seed
// vazia. Em produção, ao menos uma das envs DEVE estar setada.
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

type MotoboyEtiquetasHandler struct{ Pool *pgxpool.Pool }

// ─── Tipos de resposta ───────────────────────────────────────────────────

// EtiquetaItem — uma etiqueta pronta para impressão. Espelha os campos
// utilizados pelo template PHP (admin.php:1463-1500).
type EtiquetaItem struct {
	PedidoID        int64   `json:"pedido_id"`
	WcOrderID       int64   `json:"wc_order_id"`
	MotoboyNome     string  `json:"motoboy_nome"`
	ZonaNome        string  `json:"zona_nome"`
	DestNome        string  `json:"dest_nome"`
	DestEndereco    string  `json:"dest_endereco"`
	DestNumero      string  `json:"dest_numero"`
	DestComplemento string  `json:"dest_complemento"`
	DestBairro      string  `json:"dest_bairro"`
	DestCidade      string  `json:"dest_cidade"`
	DestUF          string  `json:"dest_uf"`
	DestCEP         string  `json:"dest_cep"`
	DestTelefone    string  `json:"dest_telefone"`
	ValorPedido     float64 `json:"valor_pedido"`
	PgtoDinheiro    float64 `json:"pgto_dinheiro"`
	PgtoPix         float64 `json:"pgto_pix"`
	PgtoCartao      float64 `json:"pgto_cartao"`
	PackageCode     string  `json:"package_code"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────

// tableExists segue o mesmo padrão dos outros handlers do package.
func (h *MotoboyEtiquetasHandler) tableExists(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// motoboyPackageCode reproduz sz_mbc_package_code() do PHP.
//
// Atenção: ordem do seed é `pedido_id|wc_order_id` — pedido_id PRIMEIRO,
// separador "|". A assinatura é HMAC-SHA256 hex, 14 chars iniciais em
// MAIÚSCULA (o parser PHP exige [A-F0-9]{8,}).
func motoboyPackageCode(pedidoID, wcOrderID int64) string {
	salt := getenv("WP_SALT_AUTH", "MOTOBOY_INTERNAL_SECRET", "TPC_WEBHOOK_SECRET")
	seed := strconv.FormatInt(pedidoID, 10) + "|" + strconv.FormatInt(wcOrderID, 10)
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(seed))
	full := hex.EncodeToString(mac.Sum(nil))
	if len(full) < 14 {
		// Em prática SHA-256 hex tem 64 chars; guard defensivo apenas.
		full = full + strings.Repeat("0", 14-len(full))
	}
	sig := strings.ToUpper(full[:14])
	return "SZ-" + strconv.FormatInt(wcOrderID, 10) + "-" + strconv.FormatInt(pedidoID, 10) + "-" + sig
}

// ─── List ────────────────────────────────────────────────────────────────

// List retorna o conjunto de etiquetas para impressão.
// GET /motoboy-etiquetas?date=YYYY-MM-DD&status=
//
// Defaults:
//   - date  = hoje (UTC server; o front envia YYYY-MM-DD explícito)
//   - status = "agendado" (diverge do PHP que usava "embalado"; spec do
//     painel Go pede "agendado" como default para o fluxo atual)
//
// Filtros são casados via paridade com PHP (admin.php:1389-1402):
//
//	DATE(mp.created_at) = $date AND mp.status = $status
//	ORDER BY motoboy_id, id
//	LIMIT 500
func (h *MotoboyEtiquetasHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		httpx.JSON(w, 200, map[string]any{"items": []EtiquetaItem{}, "count": 0})
		return
	}

	q := r.URL.Query()
	date := strings.TrimSpace(q.Get("date"))
	if date == "" || !isISODate(date) {
		date = time.Now().Format("2006-01-02")
	}
	status := strings.TrimSpace(q.Get("status"))
	if status == "" {
		status = "agendado"
	}

	rows, err := h.Pool.Query(ctx,
		`SELECT
			mp.id,
			COALESCE(mp.wc_order_id, 0),
			COALESCE(m.nome, '')                AS motoboy_nome,
			COALESCE(z.nome, '')                AS zona_nome,
			COALESCE(mp.dest_nome, ''),
			COALESCE(mp.dest_endereco, ''),
			COALESCE(mp.dest_numero, ''),
			COALESCE(mp.dest_complemento, ''),
			COALESCE(mp.dest_bairro, ''),
			COALESCE(mp.dest_cidade, ''),
			COALESCE(mp.dest_uf, ''),
			COALESCE(mp.dest_cep, ''),
			COALESCE(mp.dest_telefone, ''),
			COALESCE(mp.valor_pedido, 0),
			COALESCE(mp.pgto_dinheiro, 0),
			COALESCE(mp.pgto_pix, 0),
			COALESCE(mp.pgto_cartao, 0)
		   FROM sz_motoboy_pedidos mp
		   LEFT JOIN sz_motoboys      m ON m.id = mp.motoboy_id
		   LEFT JOIN sz_motoboy_zonas z ON z.id = mp.zona_id
		  WHERE mp.created_at::date = $1::date
		    AND mp.status = $2
		  ORDER BY mp.motoboy_id NULLS LAST, mp.id ASC
		  LIMIT 500`,
		date, status)
	if err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	defer rows.Close()

	out := []EtiquetaItem{}
	for rows.Next() {
		var it EtiquetaItem
		if err := rows.Scan(
			&it.PedidoID, &it.WcOrderID,
			&it.MotoboyNome, &it.ZonaNome,
			&it.DestNome, &it.DestEndereco, &it.DestNumero, &it.DestComplemento,
			&it.DestBairro, &it.DestCidade, &it.DestUF,
			&it.DestCEP, &it.DestTelefone,
			&it.ValorPedido, &it.PgtoDinheiro, &it.PgtoPix, &it.PgtoCartao,
		); err != nil {
			httpx.Err(w, 500, "db_error", err.Error())
			return
		}
		// Gerado server-side para o front não precisar de chave HMAC.
		it.PackageCode = motoboyPackageCode(it.PedidoID, it.WcOrderID)
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{
		"items":  out,
		"count":  len(out),
		"date":   date,
		"status": status,
	})
}
