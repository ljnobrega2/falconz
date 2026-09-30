// motoboy_shared.go — REGRAS DE MUTAÇÃO de pedidos motoboy compartilhadas entre o
// PRODUTOR (motoboy_portal.go) e o AFILIADO (orders_mutations.go).
//
// Este arquivo é a CASA ÚNICA de três regras de negócio que valiam apenas para o
// produtor no admin e que a 2ª onda do portal precisa aplicar IGUAL para os dois
// papéis (sem copiar 3x — REGRA DA TASK item 5):
//
//  1. ZONA SCHEDULE  — elegibilidade da nova data de entrega por DIA DE FUNCIONAMENTO
//     + CUTOFF da zona do pedido. ESPELHA go/admin .../zona_schedule.go (admin-service
//     e portal-service são MÓDULOS Go distintos — copia-se, não se importa). Os
//     símbolos levam sufixo …MB para não colidir com nada no pacote handlers do portal.
//  2. GATE DE DATA   — past-check + zona + split 5 dias úteis → newStatus. Usado pelos
//     QUATRO caminhos (reagendar produtor, reagendar-clone produtor, reagendar afiliado,
//     reagendar-clone afiliado).
//  3. CLONE          — advisory lock + MAX(wc_order_id) sintético ≥ 900M + re-check
//     FRUSTRADO dentro da tx (TOCTOU) + INSERT…SELECT que clona e reseta o ciclo. NÃO
//     cria sz_orders (re-entrega não re-contabiliza receita) → o clone nasce ÓRFÃO.
//
// As funções aqui são FREE FUNCTIONS sobre *pgxpool.Pool / pgx.Tx (não métodos de
// handler) porque MotoboyHandler e OrdersHandler só carregam Pool — assim os dois
// papéis chamam exatamente o mesmo código.
//
// FUSO/CONVENÇÃO DE DIAS: idênticos ao admin (ver zona_schedule.go). DOW PHP date('w')
// = Postgres EXTRACT(DOW) = Go time.Weekday(): 0=domingo … 6=sábado (NÃO ISO).
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/portal-service/internal/auth"
)

// ── ZONA SCHEDULE (espelha go/admin zona_schedule.go) ──────────────────────────

// tzSaoPauloMB — fuso da comparação de cutoff (espelha o orders-service/admin).
var tzSaoPauloMB = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("America/Sao_Paulo", -3*3600)
	}
	return loc
}()

// diasSemanaFullMB — nomes PT-BR por DOW (0=dom..6=sáb) p/ as mensagens de erro.
var diasSemanaFullMB = [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"}

// zoneScheduleMB agrega os dados de agendamento da zona de um pedido motoboy.
// HasSchedule=false → fail-open (zona ausente/sem zona_id → permite qualquer data).
type zoneScheduleMB struct {
	HasSchedule bool
	Dias        []int     // DOW 0=dom..6=sáb permitidos
	Cutoffs     [7]string // "HH:MM" por DOW; default "21:00"
}

// parseDiasFuncionamentoMB normaliza o CSV de dias (DOW 0..6). Default seg–sáb quando
// vazio (espelha sz_motoboy_sanitize_zone_days do PHP). Cópia fiel do admin.
func parseDiasFuncionamentoMB(csv string) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 6 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return []int{1, 2, 3, 4, 5, 6}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// parseCutoffsMB decodifica cutoff_horarios. Tolera objeto {"0":"HH:MM",…} E array
// ["HH:MM",…] (formato legado, que é o presente no banco — ver zonas 3/4/5). Sempre
// retorna [7]string por DOW; ausentes/inválidos viram "21:00". Cópia fiel do admin.
func parseCutoffsMB(raw string) [7]string {
	out := [7]string{"21:00", "21:00", "21:00", "21:00", "21:00", "21:00", "21:00"}
	s := strings.TrimSpace(raw)
	if s == "" {
		return out
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil && len(arr) > 0 {
		for i, v := range arr {
			if i > 6 {
				break
			}
			if t := sanitizeHHMMMB(v); t != "" {
				out[i] = t
			}
		}
		return out
	}
	var obj map[string]string
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		for k, v := range obj {
			d, err := strconv.Atoi(k)
			if err != nil || d < 0 || d > 6 {
				continue
			}
			if t := sanitizeHHMMMB(v); t != "" {
				out[d] = t
			}
		}
	}
	return out
}

// sanitizeHHMMMB valida e normaliza "HH:MM"; "" se inválido. Cópia fiel do admin.
func sanitizeHHMMMB(s string) string {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

// tableExistsMB — guarda de migração graceful (free function p/ os helpers compartilhados).
func tableExistsMB(ctx context.Context, pool *pgxpool.Pool, name string) bool {
	var ok bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// loadZoneScheduleMB resolve a regra de agendamento da zona de um pedido motoboy
// (mbPedidoID = sz_motoboy_pedidos.id). FAIL-OPEN: sem zona_id (0/NULL), zona
// inexistente ou tabelas ausentes → HasSchedule=false. ESPELHA admin loadZoneSchedule.
func loadZoneScheduleMB(ctx context.Context, pool *pgxpool.Pool, mbPedidoID int64) zoneScheduleMB {
	out := zoneScheduleMB{Dias: []int{}}
	if !tableExistsMB(ctx, pool, "sz_motoboy_pedidos") {
		return out
	}

	var zonaID sql.NullInt64
	err := pool.QueryRow(ctx,
		`SELECT zona_id FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&zonaID)
	if err != nil || !zonaID.Valid || zonaID.Int64 <= 0 {
		return out // sem zona → fail-open (zona_id=0 é o caso comum nos dados)
	}

	if !tableExistsMB(ctx, pool, "sz_motoboy_zonas") {
		return out
	}

	var diasCSV sql.NullString
	var cutoffsJSON sql.NullString
	err = pool.QueryRow(ctx,
		`SELECT COALESCE(dias_funcionamento,''), COALESCE(cutoff_horarios,'')
		   FROM sz_motoboy_zonas WHERE id = $1`, zonaID.Int64,
	).Scan(&diasCSV, &cutoffsJSON)
	if err != nil {
		return out // zona inexistente → fail-open
	}

	out.HasSchedule = true
	out.Dias = parseDiasFuncionamentoMB(diasCSV.String)
	out.Cutoffs = parseCutoffsMB(cutoffsJSON.String)
	return out
}

// dateAllowed aplica o predicado de elegibilidade da zona à data escolhida:
//
//	DOW(d) ∈ dias_funcionamento  E  agora ≤ (d − 1 dia) às cutoffs[DOW(d)] em SP.
//
// d deve ser meia-noite no fuso SP. now é o instante atual. HasSchedule=false → true
// (fail-open). Retorna (ok, motivoPT). ESPELHA admin zoneSchedule.dateAllowed.
func (z zoneScheduleMB) dateAllowed(d, now time.Time) (bool, string) {
	if !z.HasSchedule {
		return true, ""
	}
	dow := int(d.Weekday())

	inDias := false
	for _, dd := range z.Dias {
		if dd == dow {
			inDias = true
			break
		}
	}
	if !inDias {
		return false, "a zona deste pedido não entrega no dia da semana escolhido (" + diasSemanaFullMB[dow] + ")"
	}

	cut := z.Cutoffs[dow]
	if cut == "" {
		cut = "21:00"
	}
	hh, _ := strconv.Atoi(cut[0:2])
	mm, _ := strconv.Atoi(cut[3:5])
	deadline := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, tzSaoPauloMB).AddDate(0, 0, -1)
	if now.In(tzSaoPauloMB).After(deadline) {
		return false, "o horário limite (" + cut + " do dia anterior) para entregar nesta data já passou"
	}
	return true, ""
}

// ── GATE DE DATA (compartilhado pelos 4 caminhos de reagendar/clone) ───────────

// reagendarDateGate valida a data escolhida (YYYY-MM-DD já parseada) para um pedido
// motoboy e devolve o newStatus a gravar. Regras (idênticas ao admin):
//   - data no passado → erro
//   - zona schedule (loadZoneScheduleMB(mbPedidoID)) — fail-open se sem zona
//   - > 5 dias úteis a partir de hoje → 'pre_agendado'; senão 'agendado'
//
// Retorna (newStatus, httpStatus, msg). httpStatus==0 → ok. Quando httpStatus≠0,
// newStatus="" e msg traz a mensagem PT-BR p/ o erro. Centraliza as 3 validações
// para que produtor e afiliado (reagendar E clone) apliquem EXATAMENTE a mesma regra.
func reagendarDateGate(ctx context.Context, pool *pgxpool.Pool, mbPedidoID int64, chosen time.Time) (newStatus string, httpStatus int, msg string) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if chosen.Before(today) {
		return "", 400, "data no passado não permitida"
	}

	// GATE POR ZONA (REGRA DO DONO 2026-06-23): a nova data tem de cair num DIA DE
	// FUNCIONAMENTO da zona E respeitar o CUTOFF. Fail-open se o pedido não tem zona.
	zsched := loadZoneScheduleMB(ctx, pool, mbPedidoID)
	chosenSP := time.Date(chosen.Year(), chosen.Month(), chosen.Day(), 0, 0, 0, 0, tzSaoPauloMB)
	if ok, motivo := zsched.dateAllowed(chosenSP, time.Now()); !ok {
		return "", 400, "Não é possível reagendar para esta data: " + motivo + "."
	}

	// Split agendado vs pré-agendado por dias úteis (addBusinessDaysMB vive em motoboy_portal.go).
	newStatus = "agendado"
	if chosen.After(addBusinessDaysMB(today, 5)) {
		newStatus = "pre_agendado"
	}
	return newStatus, 0, ""
}

// ── AUDIT genérico (actor_tipo parametrizável) ─────────────────────────────────

// writeMotoboyAuditPortal registra uma ação em sz_motoboy_audit com actor_tipo
// parametrizável ('produtor' | 'afiliado'). actor_id = u.WPUserID. Best-effort:
// nunca derruba a request. Generaliza o writeMotoboyAudit (produtor-only) do
// motoboy_portal.go para que o afiliado também audite suas mutações.
func writeMotoboyAuditPortal(ctx context.Context, pool *pgxpool.Pool, u *auth.PortalUser, actorTipo string, pedidoID int64, acao, deStatus, paraStatus string) {
	if !tableExistsMB(ctx, pool, "sz_motoboy_audit") {
		return
	}
	var actorID *int64
	if u != nil {
		tmp := u.WPUserID
		actorID = &tmp
	}
	var de any
	if deStatus != "" {
		de = deStatus
	}
	source := "portal_" + actorTipo
	meta := `{"source":"` + source + `"}`
	if u != nil {
		meta = `{"source":"` + source + `","portal_user_id":` + strconv.FormatInt(u.ID, 10) +
			`,"email":"` + strings.ReplaceAll(u.Email, `"`, `\"`) + `"}`
	}
	_, _ = pool.Exec(ctx,
		`INSERT INTO sz_motoboy_audit
		   (pedido_id, motoboy_id, actor_tipo, actor_id,
		    acao, de_status, para_status, meta_json, created_at)
		 VALUES ($1, NULL, $2, $3, $4, $5, $6, $7, NOW())`,
		pedidoID, actorTipo, actorID, acao, de, paraStatus, meta,
	)
}

// ── CLONE (venda + itens + endereços + meta + pedido motoboy, tudo novo) ────────

// cloneFrustradoPedido executa o CLONE de um pedido motoboy FRUSTRADO/CANCELADO numa
// tx própria. mbPedidoID = sz_motoboy_pedidos.id do ORIGINAL (já resolvido como
// pertencente ao usuário pelo CALLER — este helper NÃO faz ownership, só a mecânica
// do clone). ESPELHA admin ReagendarClone.
//
// REGRA DO DONO 2026-07-21: o clone é IDÊNTICO ao original — produto, afiliado,
// financeiro, comissão — só muda a data de entrega. Isso significa clonar a VENDA
// (sz_orders + items + addresses + meta) também, não só o pedido motoboy — senão o
// clone nasce órfão e a lista mostra tudo em branco (afiliado/produto/comissão).
//
// Passos:
//  1. re-check FRUSTRADO/CANCELADO dentro da tx (TOCTOU) → 409 se não for.
//  2. resolve a venda-fonte (sz_orders do original) → 409 se não existir.
//  3. clona sz_orders (status/payment_status voltam a 'pending', novo id/order_number).
//  4. clona sz_order_items, sz_order_addresses, sz_order_meta (exceto _sz_delivery_date,
//     que recebe a NOVA data).
//  5. clona sz_motoboy_pedidos vinculado à venda nova (wc_order_id = id da venda nova).
//
// Retorna (cloneID, newOrderID, httpStatus, msg). httpStatus==0 → ok.
func cloneFrustradoPedido(ctx context.Context, pool *pgxpool.Pool, mbPedidoID int64, dataYMD, newStatus string) (cloneID, newOrderID int64, httpStatus int, msg string) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, 500, "erro interno"
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1) Re-check FRUSTRADO/CANCELADO dentro da tx (TOCTOU guard contra clone-de-clone/loop).
	var srcStatus string
	var origWCOrderID int64
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(status,''), COALESCE(wc_order_id,0) FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&srcStatus, &origWCOrderID)
	if err == pgx.ErrNoRows {
		return 0, 0, 404, "pedido motoboy não encontrado"
	}
	if err != nil {
		return 0, 0, 500, "erro interno"
	}
	if srcStatus != "frustrado" && srcStatus != "cancelado" {
		return 0, 0, 409,
			"apenas pedidos frustrados ou cancelados podem ser reagendados por clone (status atual: " + srcStatus + ")"
	}

	// 2) Venda-fonte: sem ela não dá pra clonar "idêntico".
	var origOrderID int64
	err = tx.QueryRow(ctx,
		`SELECT id FROM sz_orders WHERE COALESCE(wp_order_id, id) = $1`, origWCOrderID,
	).Scan(&origOrderID)
	if err != nil {
		return 0, 0, 409, "pedido original sem venda associada (sz_orders) — não é possível reagendar por cópia"
	}

	// 3) Clona a venda.
	err = tx.QueryRow(ctx, `
		INSERT INTO sz_orders (
		    order_number, wp_order_id, user_id, produtor_id, affiliate_id, status,
		    subtotal, shipping, total, payment_method, payment_status, currency,
		    customer_note, ip_address, user_agent, customer_name, billing_email,
		    affiliate_amount, senderzz_fee, producer_net, shipping_class, shipping_class_id,
		    delivery_fee, transaction_fee, created_at, updated_at
		)
		SELECT
		    '', wp_order_id, user_id, produtor_id, affiliate_id, 'pending',
		    subtotal, shipping, total, payment_method, 'pending', currency,
		    customer_note, ip_address, user_agent, customer_name, billing_email,
		    affiliate_amount, senderzz_fee, producer_net, shipping_class, shipping_class_id,
		    delivery_fee, transaction_fee, NOW(), NOW()
		FROM sz_orders WHERE id = $1
		RETURNING id`,
		origOrderID,
	).Scan(&newOrderID)
	if err != nil {
		return 0, 0, 500, "erro interno"
	}
	if _, err := tx.Exec(ctx,
		`UPDATE sz_orders SET order_number = 'SZ-' || lpad(id::text, 7, '0') WHERE id = $1`,
		newOrderID,
	); err != nil {
		return 0, 0, 500, "erro interno"
	}

	// 4) Clona itens, endereços e meta (exceto _sz_delivery_date → NOVA data).
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_items (order_id, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta)
		SELECT $2, produto_id, nome, sku, quantidade, preco_unit, subtotal, meta
		FROM sz_order_items WHERE order_id = $1`,
		origOrderID, newOrderID,
	); err != nil {
		return 0, 0, 500, "erro interno"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_addresses (order_id, tipo, nome, email, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf, pais)
		SELECT $2, tipo, nome, email, telefone, cep, logradouro, numero, complemento, bairro, cidade, uf, pais
		FROM sz_order_addresses WHERE order_id = $1`,
		origOrderID, newOrderID,
	); err != nil {
		return 0, 0, 500, "erro interno"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_meta (order_id, meta_key, meta_value)
		SELECT $2, meta_key, meta_value
		FROM sz_order_meta WHERE order_id = $1 AND meta_key <> '_sz_delivery_date'`,
		origOrderID, newOrderID,
	); err != nil {
		return 0, 0, 500, "erro interno"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO sz_order_meta (order_id, meta_key, meta_value) VALUES ($1, '_sz_delivery_date', $2)`,
		newOrderID, dataYMD,
	); err != nil {
		return 0, 0, 500, "erro interno"
	}

	// 5) Clona o pedido motoboy, vinculado à venda nova.
	err = tx.QueryRow(ctx,
		`INSERT INTO sz_motoboy_pedidos (
		     wc_order_id, cd_id, zona_id, motoboy_id, status,
		     dest_nome, dest_telefone, dest_cep, dest_endereco, dest_numero,
		     dest_complemento, dest_produto, quantidade,
		     dest_bairro, dest_cidade, dest_uf, dest_lat, dest_lng,
		     valor_pedido, valor_taxa,
		     data_entrega, reagendado_para,
		     ts_aprovado, created_at, updated_at
		 )
		 SELECT
		     $2, cd_id, zona_id, NULL, $3,
		     dest_nome, dest_telefone, dest_cep, dest_endereco, dest_numero,
		     dest_complemento, dest_produto, quantidade,
		     dest_bairro, dest_cidade, dest_uf, dest_lat, dest_lng,
		     valor_pedido, valor_taxa,
		     $4::date, $4::date,
		     NOW(), NOW(), NOW()
		 FROM sz_motoboy_pedidos
		 WHERE id = $1
		 RETURNING id`,
		mbPedidoID, newOrderID, newStatus, dataYMD,
	).Scan(&cloneID)
	if err != nil {
		return 0, 0, 500, "erro interno"
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 500, "erro interno"
	}
	return cloneID, newOrderID, 0, ""
}

// ── CANCELAR + BRIDGE (compartilhado pelos cancelamentos single produtor/afiliado) ──

// cancelMotoboyWithBridge cancela UM pedido motoboy (mbPedidoID já resolvido como
// pertencente ao usuário pelo CALLER — este helper NÃO faz ownership) e ESPELHA o
// estorno em sz_orders na MESMA tx (motoboy 'cancelado' → sz_orders 'cancelled').
//
// Sem o bridge, sz_motoboy_pedidos vira 'cancelado' mas sz_orders.status fica intocado
// → o pedido continua contando como receita ativa (não estorna) e o rastreio diverge.
// Mapeamento 'cancelado' → 'cancelled' (em inglês — 'cancelado' NÃO é CHECK-válido em
// sz_orders). Idempotente: AND o.status <> 'cancelled' (0 linhas = já cancelado, NÃO é erro).
//
// Retorna (szBridged, httpStatus, msg). szBridged=true → o estorno em sz_orders mexeu
// pelo menos 1 linha (p/ o caller logar o sync). httpStatus==0 → ok. NÃO audita nem
// loga — o caller faz isso (actor_tipo difere entre produtor e afiliado).
func cancelMotoboyWithBridge(ctx context.Context, pool *pgxpool.Pool, mbPedidoID, wcOrderID int64) (szBridged bool, httpStatus int, msg string) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, 500, "erro interno"
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	tag, err := tx.Exec(ctx,
		`UPDATE sz_motoboy_pedidos SET status = 'cancelado', updated_at = NOW() WHERE id = $1`,
		mbPedidoID,
	)
	if err != nil {
		return false, 500, "erro interno"
	}
	if tag.RowsAffected() == 0 {
		return false, 404, "pedido motoboy não encontrado"
	}

	szTag, err := tx.Exec(ctx,
		// id-space FIX: pedidos COD Go-native têm wp_order_id NULL e wc_order_id = id.
		`UPDATE sz_orders SET status = 'cancelled', updated_at = NOW()
		  WHERE COALESCE(wp_order_id, id) = $1 AND status <> 'cancelled'`,
		wcOrderID,
	)
	if err != nil {
		return false, 500, "erro interno"
	}

	if err = tx.Commit(ctx); err != nil {
		return false, 500, "erro interno"
	}
	return szTag.RowsAffected() > 0, 0, ""
}
