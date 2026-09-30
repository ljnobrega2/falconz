// Package handlers — regras de negócio compartilhadas portadas FIEL do WP.
//
// Este arquivo concentra os helpers que espelham, byte-a-byte quando possível,
// as funções de validação/custódia do plugin WordPress:
//
//   - packageCode / parsePackageCode  ← sz_mbc_package_code / sz_mbc_parse_package_code
//     (includes/senderzz-motoboy-custody.php)
//   - validarCPF                       ← sz_mb_validar_cpf      (rest-api.php)
//   - validarGPSOperacional            ← sz_mb_validar_gps_operacional
//   - validarFotoBase64                ← sz_mb_validar_foto_base64
//   - parseMoney                       ← sz_mb_parse_money
//   - normalizarTelefone               ← sz_mb_normalizar_telefone
//   - taxaFrustrado / calcularTaxaFrustrado ← sz_mbw_get_taxa_frustrado /
//     sz_motoboy_calcular_taxa_frustrado
//
// Mantém os marcadores de patch onde se aplicam (SEC-LIVE-02, etc.).
package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── QR / código de pacote ───────────────────────────────────────────────────

// pkgCodePattern espelha o regex de sz_mbc_parse_package_code():
//
//	/SZ-(\d+)-(\d+)-([A-F0-9]{8,})/  (após strtoupper)
//
// Grupo 1 = wc_order_id, grupo 2 = pedido_id, grupo 3 = assinatura HMAC.
var pkgCodePattern = regexp.MustCompile(`SZ-(\d+)-(\d+)-([A-F0-9]{8,})`)

// packageCode reproduz EXATAMENTE sz_mbc_package_code() do PHP:
//
//	$seed = $pedido_id . '|' . $wc_order_id;                       // pedido PRIMEIRO, separador PIPE
//	$sig  = substr( hash_hmac('sha256', $seed, wp_salt('auth')), 0, 14 );
//	return 'SZ-' . $wc_order_id . '-' . $pedido_id . '-' . strtoupper($sig);
//
// Atenção à assimetria: no seed o pedido_id vem primeiro (com '|'); na string
// final o wc_order_id vem primeiro (com '-'). E a assinatura é MAIÚSCULA.
func packageCode(pedidoID, wcOrderID int64, salt string) string {
	seed := fmt.Sprintf("%d|%d", pedidoID, wcOrderID)
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(seed))
	sig := strings.ToUpper(hex.EncodeToString(mac.Sum(nil))[:14])
	return fmt.Sprintf("SZ-%d-%d-%s", wcOrderID, pedidoID, sig)
}

// parsedPackage espelha o retorno de sz_mbc_parse_package_code().
type parsedPackage struct {
	WCOrderID int64
	PedidoID  int64
	Code      string // código normalizado (uppercase, trim)
}

// parsePackageCode normaliza (uppercase+trim) e extrai os ids do QR.
// Retorna ok=false quando não casa com o padrão SZ-…-…-….
func parsePackageCode(code string) (parsedPackage, bool) {
	norm := strings.ToUpper(strings.TrimSpace(code))
	m := pkgCodePattern.FindStringSubmatch(norm)
	if m == nil {
		return parsedPackage{Code: norm}, false
	}
	wc, _ := strconv.ParseInt(m[1], 10, 64)
	ped, _ := strconv.ParseInt(m[2], 10, 64)
	return parsedPackage{WCOrderID: wc, PedidoID: ped, Code: norm}, true
}

// wpSaltAuth lê WP_SALT_AUTH (mesmo segredo usado por auth.go no HMAC do portal
// e por sz_mbc_package_code() em PHP via wp_salt('auth')).
func wpSaltAuth() string { return os.Getenv("WP_SALT_AUTH") }

// ── FEAT-SKU — validação de código de barras do produto vs SKU do pedido ─────
//
// Regra do dono: "validação de sku do produto com o que ta no pedido se for
// diferente nao deixa seguir". O motoboy bipa o código de barras do produto ao
// colocar EM ROTA e ao DEVOLVER após frustrado; o SKU lido tem que estar entre
// os SKUs do pedido. Se não estiver → 422 e a ação NÃO avança.
//
// Esta função NÃO grava em sz_pack_scans (o caller decide o context e registra
// sempre que houve leitura). Ela só compara o SKU bipado contra o conjunto de
// SKUs do pedido, usando o JOIN canônico (sz_order_items + sz_orders) e casando
// o wc_order_id do pedido motoboy por COALESCE(o.wp_order_id, o.id).

// normalizarSKU prepara o SKU para comparação: trim + UPPER (scanners costumam
// emitir espaço/quebra-de-linha à direita; banco pode ter case misto).
func normalizarSKU(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// skuMatchResult resume o veredito da validação de SKU de um pedido.
type skuMatchResult struct {
	OK          bool   // SKU bipado bate com algum item do pedido
	ExpectedCSV string // SKUs esperados do pedido (csv, p/ mensagem/auditoria)
	ProductID   *int64 // produto_id do item que casou (nil quando não casou)
	HasExpected bool   // o pedido tem ao menos 1 SKU cadastrado
}

// validarSKUPedido compara o SKU bipado contra os SKUs do pedido (wcOrderID =
// wc_order_id do sz_motoboy_pedidos). Retorna ok=true quando o SKU bipado está
// no conjunto de SKUs do pedido. ExpectedCSV lista os SKUs esperados (auditoria
// + mensagem). Quando o pedido NÃO tem nenhum SKU cadastrado (HasExpected=false)
// não há como conferir: o caller bloqueia (fail-closed) — não dá pra garantir
// que é o produto certo, então não deixa seguir.
//
// Usa *pgxpool.Pool (não a interface queryer) porque precisa de Query multi-row.
func validarSKUPedido(ctx context.Context, pool *pgxpool.Pool, wcOrderID int64, skuBipado string) (skuMatchResult, error) {
	res := skuMatchResult{}
	alvo := normalizarSKU(skuBipado)

	// JOIN canônico (contrato do dono): casa o pedido motoboy por
	// COALESCE(o.wp_order_id, o.id). Ignora itens sem SKU (NULL/'').
	rows, err := pool.Query(ctx, `
		SELECT oi.produto_id, oi.sku
		  FROM sz_order_items oi
		  JOIN sz_orders o ON o.id = oi.order_id
		 WHERE COALESCE(o.wp_order_id, o.id) = $1
		   AND oi.sku IS NOT NULL AND oi.sku <> ''`,
		wcOrderID,
	)
	if err != nil {
		return res, err
	}
	defer rows.Close()

	var esperados []string
	for rows.Next() {
		var pid int64
		var sku string
		if err := rows.Scan(&pid, &sku); err != nil {
			return res, err
		}
		esperados = append(esperados, sku)
		if normalizarSKU(sku) == alvo {
			res.OK = true
			p := pid
			res.ProductID = &p
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	res.HasExpected = len(esperados) > 0
	res.ExpectedCSV = strings.Join(esperados, ",")
	return res, nil
}

// registrarPackScan grava a leitura (bipada ou digitada) em sz_pack_scans —
// trilha de auditoria (accountability), independente do veredito. Só é chamada
// quando houve leitura (skuBipado não-vazio). Best-effort: falha de log não
// derruba a operação. context = 'em_rota' | 'devolucao'.
func registrarPackScan(ctx context.Context, pool *pgxpool.Pool, scanCtx string, wcOrderID int64, productID *int64, skuScanned, skuExpected string, matched, manualTyped bool, actor string) {
	var exp any
	if strings.TrimSpace(skuExpected) != "" {
		exp = skuExpected
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO sz_pack_scans
			(context, wc_order_id, product_id, sku_scanned, sku_expected, matched, quantity, manual_typed, actor, scanned_at)
		VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, NOW())`,
		scanCtx, wcOrderID, productID, strings.TrimSpace(skuScanned), exp, matched, manualTyped, actor,
	)
	if err != nil {
		// Não interrompe a operação; só registra no log do servidor.
		slog.Warn("[FEAT-SKU] falha ao gravar sz_pack_scans", "context", scanCtx, "wc_order_id", wcOrderID, "err", err)
	}
}

// ── CPF — port FIEL de sz_mb_validar_cpf() ──────────────────────────────────

var soNumeros = regexp.MustCompile(`\D+`)

// validarCPF reproduz o algoritmo de dígitos verificadores de sz_mb_validar_cpf().
//   - remove não-dígitos
//   - exige 11 dígitos
//   - rejeita sequências repetidas (00000000000, 11111111111, …)
//   - valida os 2 dígitos verificadores
func validarCPF(cpf string) bool {
	cpf = soNumeros.ReplaceAllString(cpf, "")
	if len(cpf) != 11 {
		return false
	}
	// Rejeita todos os dígitos iguais (equivalente a /^(\d)\1{10}$/).
	todosIguais := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			todosIguais = false
			break
		}
	}
	if todosIguais {
		return false
	}
	for t := 9; t < 11; t++ {
		soma := 0
		for i := 0; i < t; i++ {
			soma += int(cpf[i]-'0') * ((t + 1) - i)
		}
		digito := (soma * 10) % 11
		if digito == 10 {
			digito = 0
		}
		if int(cpf[t]-'0') != digito {
			return false
		}
	}
	return true
}

// ── GPS — port FIEL de sz_mb_validar_gps_operacional() ──────────────────────

// validarGPSOperacional retorna (ok, mensagem). accuracy nil = sem aferição.
// Espelha exatamente os limites do PHP.
func validarGPSOperacional(lat, lng float64, accuracy *float64) (bool, string) {
	// is_numeric já garantido pelos tipos float64 do JSON; lat/lng==0 cai no abs<1e-6.
	abs := func(f float64) float64 {
		if f < 0 {
			return -f
		}
		return f
	}
	if abs(lat) < 0.000001 || abs(lng) < 0.000001 || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return false, "Localização GPS inválida."
	}
	if accuracy != nil && *accuracy > 150 {
		return false, "Precisão do GPS insuficiente. Aguarde melhorar o sinal e tente novamente."
	}
	return true, ""
}

// ── Foto — port FIEL de sz_mb_validar_foto_base64() ─────────────────────────

var fotoDataURIPattern = regexp.MustCompile(`(?i)^data:image/(jpeg|jpg|png|webp);base64,`)
var fotoStripPattern = regexp.MustCompile(`(?i)^data:image/[a-zA-Z0-9.+-]+;base64,`)

// validarFotoBase64 reproduz as validações de sz_mb_validar_foto_base64(),
// exceto a aferição de dimensões via getimagesizefromstring (não há decode de
// imagem aqui — o PWA só envia foto vinda da câmera). Mantém tamanho min/max
// e o data-URI exigido, que é o que protege contra payload não-imagem.
func validarFotoBase64(b64 string) (bool, string) {
	if strings.TrimSpace(b64) == "" {
		return false, "Foto obrigatória."
	}
	if !fotoDataURIPattern.MatchString(b64) {
		return false, "Formato da foto inválido. Tire uma foto pelo aplicativo."
	}
	payload := fotoStripPattern.ReplaceAllString(b64, "")
	bin, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(bin) < 3000 {
		return false, "Foto inválida ou muito pequena. Tire uma nova foto."
	}
	if len(bin) > 8*1024*1024 {
		return false, "Foto muito grande. Tire uma foto menor."
	}
	return true, ""
}

// ── Dinheiro — port FIEL de sz_mb_parse_money() ─────────────────────────────

// parseMoney aceita número (float) já desserializado do JSON e o normaliza
// (max(0, valor) arredondado a 2 casas). Para strings "R$ 1.234,56" também
// normaliza como o PHP (remove R$/espaço/ponto-milhar, vírgula→ponto).
func parseMoney(v any) float64 {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case string:
		s := strings.TrimSpace(x)
		s = strings.NewReplacer("R$", "", " ", "", ".", "").Replace(s)
		s = strings.ReplaceAll(s, ",", ".")
		f, _ = strconv.ParseFloat(s, 64)
	case nil:
		f = 0
	}
	if f < 0 {
		f = 0
	}
	return round2(f)
}

// round2 arredonda a 2 casas (half-away-from-zero, como o round() do PHP).
func round2(f float64) float64 {
	if f >= 0 {
		return float64(int64(f*100+0.5)) / 100
	}
	return float64(int64(f*100-0.5)) / 100
}

// nomeMuitoCurto espelha mb_strlen(trim($nome)) < 3 do WP entregar.
func nomeMuitoCurto(nome string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(nome)) < 3
}

// ── Telefone — port FIEL de sz_mb_normalizar_telefone() ─────────────────────

// normalizarTelefone remove não-dígitos e o prefixo 55 quando o número tem
// 12 ou 13 dígitos começando com 55 (mesma regra do PHP).
func normalizarTelefone(raw string) string {
	digits := soNumeros.ReplaceAllString(raw, "")
	if (len(digits) == 13 || len(digits) == 12) && strings.HasPrefix(digits, "55") {
		digits = digits[2:]
	}
	return digits
}

// ── Taxa de frustração — port FIEL ──────────────────────────────────────────

// taxaFrustrado espelha sz_mbw_get_taxa_frustrado($motoboy_id):
//   - option sz_mbw_taxa_frustrado_mb_{id} > 0 → usa ela (por motoboy)
//   - senão option sz_mbw_taxa_frustrado (default 5.00)
func taxaFrustrado(ctx context.Context, q queryer, motoboyID int64) float64 {
	if motoboyID > 0 {
		perMB := optionFloat(ctx, q, "sz_mbw_taxa_frustrado_mb_"+strconv.FormatInt(motoboyID, 10))
		if perMB > 0 {
			return perMB
		}
	}
	v := optionFloat(ctx, q, "sz_mbw_taxa_frustrado")
	if v == 0 {
		return 5.00
	}
	return v
}

// frustradoIsento decide a ISENÇÃO da penalty do PRODUTOR: é isento (1ª
// frustração) quando NÃO existe nenhum pedido frustrado prévio para o MESMO
// TELEFONE do cliente (dest_telefone normalizado, strip +55), excluindo o
// próprio wc_order_id. Regra do dono: "1ª frustração GRÁTIS por TELEFONE do
// cliente; 2ª+ do mesmo telefone cobra".
//
// O telefone é normalizado em Go (normalizarTelefone) e comparado contra a
// MESMA normalização aplicada em SQL sobre dest_telefone (remove não-dígitos +
// strip do prefixo 55 quando 12/13 dígitos). Telefone vazio → trata como 1ª
// (isento): sem chave para agrupar, não há "repeat" comprovável.
func frustradoIsento(ctx context.Context, q queryer, wcOrderID int64, destTelefone string) bool {
	tel := normalizarTelefone(destTelefone)
	if tel == "" {
		return true
	}
	var count int
	_ = q.QueryRow(ctx, `
		SELECT COUNT(*)
		  FROM sz_motoboy_pedidos
		 WHERE status = 'frustrado'
		   AND wc_order_id <> $2
		   AND (
		     CASE
		       WHEN length(regexp_replace(dest_telefone, '\D', '', 'g')) IN (12, 13)
		            AND left(regexp_replace(dest_telefone, '\D', '', 'g'), 2) = '55'
		       THEN substring(regexp_replace(dest_telefone, '\D', '', 'g') FROM 3)
		       ELSE regexp_replace(dest_telefone, '\D', '', 'g')
		     END
		   ) = $1`,
		tel, wcOrderID,
	).Scan(&count)
	return count == 0
}

// optionFloat lê uma option numérica de wp_options (ParseFloat de string vazia = 0).
// USADO só pela taxa do MOTOBOY (sz_mbw_*). As penalties de frustração de
// produtor/afiliado vivem em senderzz_options — ver szOption()/penaltyOptions().
func optionFloat(ctx context.Context, q queryer, name string) float64 {
	var s string
	_ = q.QueryRow(ctx, `SELECT option_value FROM wp_options WHERE option_name=$1 LIMIT 1`, name).Scan(&s)
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// ── Penalty de frustração — produtor + afiliado (senderzz_options) ───────────
//
// Modelo do dono (fonte da verdade):
//   - Produtor: isento (1ª por telefone) → prod_first (default 0);
//     senão → prod_repeat (default 8). Penalty CONGELA em valor_taxa_frustrado.
//   - Afiliado (só se o pedido tem afiliado): paga em TODAS (sem isenção).
//     aff_first (default 5) na 1ª por telefone; aff_repeat (default 5) nas demais.
//     Hoje first==repeat==5 → numericamente igual; a distinção fica pronta caso
//     o dono diferencie no futuro. Penalty CONGELA em valor_taxa_frustrado_afiliado.
//
// Tudo lido de senderzz_options (NÃO wp_options): colunas name/value.

// szOption lê uma option de senderzz_options. Retorna (value, present) —
// distingue AUSENTE (present=false → caller usa o default) de presente-zero
// (present=true, value="0"/"0.0000"). Colunas: name, value.
func szOption(ctx context.Context, q queryer, name string) (string, bool) {
	var v *string
	if err := q.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE name=$1 LIMIT 1`, name,
	).Scan(&v); err != nil || v == nil {
		return "", false
	}
	return *v, true
}

// szOptionFloat lê uma option numérica de senderzz_options com default quando
// AUSENTE. Presente mas não-numérico/ vazio → default (fail-safe).
func szOptionFloat(ctx context.Context, q queryer, name string, def float64) float64 {
	s, present := szOption(ctx, q, name)
	if !present {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return f
}

// penaltyConfig agrega os 4 valores de penalty (já resolvidos dos options +
// overrides). É um VALOR puro: selecionarPenalties() decide quanto cobrar a
// partir dele sem tocar no banco — o que permite teste unitário branco.
type penaltyConfig struct {
	ProdFirst  float64 // produtor, 1ª por telefone (default 0)
	ProdRepeat float64 // produtor, 2ª+ por telefone (default 8)
	AffFirst   float64 // afiliado, 1ª por telefone  (default 5)
	AffRepeat  float64 // afiliado, 2ª+ por telefone (default 5)
}

// carregarPenaltyConfig lê os defaults GLOBAIS de senderzz_options e aplica os
// overrides por entidade quando existirem (COALESCE p/ global). Os overrides são
// jsonb opcionais (sz_frustration_prod_overrides / sz_frustration_aff_overrides);
// HOJE não existem → cai 100% no global. O override do AFILIADO é aplicado quando
// affiliateID > 0 (temos o id aqui); o do PRODUTOR fica global-only por enquanto —
// precisaria do producer_id que esta camada não busca ainda (TODO honesto).
func carregarPenaltyConfig(ctx context.Context, q queryer, affiliateID int64) penaltyConfig {
	cfg := penaltyConfig{
		// prod_first: sz_prod_first_frustration_penalty (ausente → 0).
		ProdFirst: szOptionFloat(ctx, q, "sz_prod_first_frustration_penalty", 0),
		// prod_repeat: sz_aff_producer_frustration_penalty (default 8) — chave do dono.
		ProdRepeat: szOptionFloat(ctx, q, "sz_aff_producer_frustration_penalty", 8),
		// aff_first: sz_aff_first_frustration_penalty (default 5).
		AffFirst: szOptionFloat(ctx, q, "sz_aff_first_frustration_penalty", 5),
		// aff_repeat: sz_aff_default_penalty_value (default 5).
		AffRepeat: szOptionFloat(ctx, q, "sz_aff_default_penalty_value", 5),
	}
	// Override por afiliado (jsonb sz_frustration_aff_overrides → { "<id>": {first,repeat} }).
	// Ausente/sem chave → mantém global (COALESCE). Não gold-plate: só afiliado.
	if affiliateID > 0 {
		applyAffOverride(ctx, q, affiliateID, &cfg)
	}
	return cfg
}

// applyAffOverride sobrescreve AffFirst/AffRepeat com o override do afiliado, se
// existir no jsonb. Ausente → no-op (mantém global). Best-effort: erro/JSON
// inválido não derruba a baixa, só mantém o global.
func applyAffOverride(ctx context.Context, q queryer, affiliateID int64, cfg *penaltyConfig) {
	raw, present := szOption(ctx, q, "sz_frustration_aff_overrides")
	if !present || strings.TrimSpace(raw) == "" {
		return
	}
	var overrides map[string]struct {
		First  *float64 `json:"first"`
		Repeat *float64 `json:"repeat"`
	}
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		slog.Warn("[frustracao] sz_frustration_aff_overrides inválido (jsonb)", "err", err)
		return
	}
	if ov, ok := overrides[strconv.FormatInt(affiliateID, 10)]; ok {
		if ov.First != nil {
			cfg.AffFirst = *ov.First
		}
		if ov.Repeat != nil {
			cfg.AffRepeat = *ov.Repeat
		}
	}
}

// selecionarPenalties decide os valores CONGELADOS a partir do config + flags.
// Função PURA (sem I/O) — testável em branco. isento = 1ª frustração por
// telefone (produtor); temAfiliado = o pedido tem afiliado (senão penalty=0).
//
//	produtor = isento ? ProdFirst : ProdRepeat
//	afiliado = temAfiliado ? (isento ? AffFirst : AffRepeat) : 0   (sem isenção: paga sempre)
func selecionarPenalties(cfg penaltyConfig, isento, temAfiliado bool) (penaltyProdutor, penaltyAfiliado float64) {
	if isento {
		penaltyProdutor = cfg.ProdFirst
	} else {
		penaltyProdutor = cfg.ProdRepeat
	}
	if temAfiliado {
		if isento {
			penaltyAfiliado = cfg.AffFirst
		} else {
			penaltyAfiliado = cfg.AffRepeat
		}
	}
	return round2(penaltyProdutor), round2(penaltyAfiliado)
}

// affiliateDoPedido descobre o affiliate_id do pedido via sz_orders, casando o
// wc_order_id por COALESCE(o.wp_order_id, o.id) (JOIN canônico do projeto).
// Retorna 0 quando não há afiliado (ou pedido sem linha em sz_orders).
func affiliateDoPedido(ctx context.Context, q queryer, wcOrderID int64) int64 {
	var aff *int64
	_ = q.QueryRow(ctx, `
		SELECT affiliate_id FROM sz_orders
		 WHERE COALESCE(wp_order_id, id) = $1
		 LIMIT 1`, wcOrderID,
	).Scan(&aff)
	if aff == nil {
		return 0
	}
	return *aff
}

// computarPenaltiesFrustrado é o ponto único usado na BAIXA de frustrado
// (rota.go e ol.go): resolve isento (por telefone), descobre o afiliado e
// devolve as duas penalties CONGELADAS + o flag isento p/ gravar. Lê de
// senderzz_options. Deve ser chamada DENTRO da mesma tx da baixa (passa a tx
// como queryer) p/ snapshot consistente.
func computarPenaltiesFrustrado(ctx context.Context, q queryer, wcOrderID int64, destTelefone string) (penaltyProdutor, penaltyAfiliado float64, isento bool, affiliateID int64) {
	isento = frustradoIsento(ctx, q, wcOrderID, destTelefone)
	affiliateID = affiliateDoPedido(ctx, q, wcOrderID)
	cfg := carregarPenaltyConfig(ctx, q, affiliateID)
	penaltyProdutor, penaltyAfiliado = selecionarPenalties(cfg, isento, affiliateID > 0)
	return penaltyProdutor, penaltyAfiliado, isento, affiliateID
}

// ── Custódia (guarda de janela de migração) ─────────────────────────────────

// custodyTablesExist verifica se sz_motoboy_stock_custody existe (to_regclass,
// mesmo idioma de bridgeUpdatePedidoStatus). Durante a migração as tabelas de
// custódia podem genuinamente não existir; nesse caso as regras de custódia
// são puladas (no-op fiel: o WP também só as aplica quando a função existe).
func custodyTablesExist(ctx context.Context, q queryer) bool {
	var reg *string
	if err := q.QueryRow(ctx, `SELECT to_regclass('public.sz_motoboy_stock_custody')::text`).Scan(&reg); err != nil {
		return false
	}
	return reg != nil
}

// custodyPendingReturnExists espelha o bloqueio de sz_mbc_start_route_by_qr():
// "este motoboy possui pacote frustrado aguardando devolução/confirmação do OL"
// → COUNT custódia frustrated/return_declared de OUTRO pedido do mesmo motoboy.
func custodyPendingReturnExists(ctx context.Context, q queryer, motoboyID, pedidoID int64) bool {
	var n int
	_ = q.QueryRow(ctx, `
		SELECT COUNT(*) FROM sz_motoboy_stock_custody
		 WHERE motoboy_id=$1 AND pedido_id<>$2
		   AND physical_status IN ('frustrated','return_declared')`,
		motoboyID, pedidoID,
	).Scan(&n)
	return n > 0
}

// queryer é a interface mínima satisfeita por *pgxpool.Pool e pgx.Tx.
type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ queryer = (*pgxpool.Pool)(nil)
	_ queryer = (pgx.Tx)(nil)
)
