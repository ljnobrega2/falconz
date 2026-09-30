// zona_schedule.go — regra de agendamento POR ZONA aplicada ao reagendamento de
// pedidos motoboy (clone de frustrado e reagendar comum) no painel admin.
//
// CONTEXTO (REGRA DO DONO 2026-06-23): a nova data de entrega só pode cair em um
// DIA DE FUNCIONAMENTO da zona do pedido (sz_motoboy_zonas.dias_funcionamento) e
// tem de respeitar o CUTOFF (horário limite) por dia (sz_motoboy_zonas.cutoff_horarios).
// Antes a UI usava regra GLOBAL (CUTOFF_HOUR=21 + dias úteis); agora a ELEGIBILIDADE
// da data passa a ser pela zona.
//
// CONVENÇÃO DOS DIAS (confirmada no código existente — NÃO é ISO):
//   - dias_funcionamento: CSV de DOW na convenção PHP date('w') = Postgres EXTRACT(DOW)
//     = Go time.Weekday(): 0=domingo, 1=segunda, … 6=sábado. (sz_motoboy_sanitize_zone_days
//     aceita '0' e rejeita '7' — router.php:21; logo domingo=0, não 7.) Default seg–sáb
//     ('1,2,3,4,5,6') quando a coluna vem vazia.
//   - cutoff_horarios: JSON por dia. Dois formatos no banco — OBJETO {"0":"21:00",…,"6":"21:00"}
//     (default gravado por wp_json_encode, database.php:107) E array ["21:00",…] (formato
//     legado). Cutoff = para entregar no dia D, o pedido tem de ser feito até
//     cutoffs[DOW(D)] do dia ANTERIOR (D − 1). Default "21:00" por dia.
//
// FUSO: a comparação de cutoff roda em America/Sao_Paulo (espelha o motor do checkout
// em orders-service/schedule.go). O DOW da data escolhida é independente de fuso
// (Weekday() da meia-noite SP da data). Este motor é uma CÓPIA do predicado de
// elegibilidade do schedule.go do orders-service — admin-service e orders-service são
// MÓDULOS Go distintos (sem import compartilhado), então copia-se, não se importa.
//
// O windowing/classificação do schedule.go (frequência→janela→agendamento/pre_agendado)
// NÃO é portado aqui: o split agendado vs pré-agendado do reagendamento continua sendo
// por dias úteis (addBusinessDays(today,5), ver orders.go). Aqui só vive a ELEGIBILIDADE.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/senderzz/admin-service/internal/httpx"
)

// tzSaoPauloAdmin é o fuso usado na comparação de cutoff (espelha o orders-service).
var tzSaoPauloAdmin = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Fallback defensivo: offset fixo -03:00 (sem horário de verão no BR atual).
		return time.FixedZone("America/Sao_Paulo", -3*3600)
	}
	return loc
}()

// zoneSchedule agrega os dados de agendamento da zona de um pedido motoboy.
type zoneSchedule struct {
	HasSchedule bool      // false → zona ausente/sem zona_id (fail-open: permite tudo)
	Dias        []int     // DOW 0=dom..6=sáb permitidos, ordenado, sem repetição
	Cutoffs     [7]string // "HH:MM" por DOW (índice 0..6); default "21:00"
}

// parseDiasFuncionamento normaliza o CSV de dias (DOW 0..6) como o PHP
// sz_motoboy_sanitize_zone_days: aceita só 0–6, único, ordenado; default seg–sáb
// ('1,2,3,4,5,6') quando vazio. Cópia fiel do orders-service/schedule.go.
func parseDiasFuncionamento(csv string) []int {
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
		// Default igual ao PHP: segunda a sábado.
		return []int{1, 2, 3, 4, 5, 6}
	}
	// Ordena ascendente (igual ao sort numérico do PHP).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// parseCutoffs decodifica cutoff_horarios. Tolera os DOIS formatos do banco:
// objeto {"0":"HH:MM",…} (default do wp_json_encode) E array ["HH:MM",…] (legado).
// Sempre retorna [7]string indexado por DOW; ausentes/inválidos viram "21:00".
func parseCutoffs(raw string) [7]string {
	out := [7]string{"21:00", "21:00", "21:00", "21:00", "21:00", "21:00", "21:00"}
	s := strings.TrimSpace(raw)
	if s == "" {
		return out
	}

	// Tenta array ["HH:MM", …] (formato legado).
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil && len(arr) > 0 {
		for i, v := range arr {
			if i > 6 {
				break
			}
			if t := sanitizeHHMM(v); t != "" {
				out[i] = t
			}
		}
		return out
	}

	// Tenta objeto {"0":"HH:MM", …} (default gravado pelo wp_json_encode do PHP).
	var obj map[string]string
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		for k, v := range obj {
			d, err := strconv.Atoi(k)
			if err != nil || d < 0 || d > 6 {
				continue
			}
			if t := sanitizeHHMM(v); t != "" {
				out[d] = t
			}
		}
	}
	return out
}

// sanitizeHHMM valida e normaliza "HH:MM"; retorna "" se inválido.
func sanitizeHHMM(s string) string {
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

// loadZoneSchedule resolve a regra de agendamento da zona de um pedido motoboy.
//
// Lê zona_id de sz_motoboy_pedidos e, se houver, dias_funcionamento/cutoff_horarios de
// sz_motoboy_zonas. FAIL-OPEN (REGRA DO DONO: "zona sem regra = permite tudo"): se o
// pedido não tem zona_id, a zona não existe ou as tabelas estão ausentes →
// HasSchedule=false (a UI não desabilita por zona; o gate não rejeita).
//
// DISTINÇÃO: zona AUSENTE → HasSchedule=false (allow-all). Zona REAL com dias vazios →
// default seg–sáb pelo parser canônico (consistência com schedule.go) — NÃO allow-all.
func (h *OrdersHandler) loadZoneSchedule(ctx context.Context, mbPedidoID int64) zoneSchedule {
	out := zoneSchedule{Dias: []int{}}
	if !h.tableExists(ctx, "sz_motoboy_pedidos") {
		return out
	}

	var zonaID sql.NullInt64
	err := h.Pool.QueryRow(ctx,
		`SELECT zona_id FROM sz_motoboy_pedidos WHERE id = $1`, mbPedidoID,
	).Scan(&zonaID)
	// DONO 2026-06-24: zona_id=0 NÃO é "sem agenda" — é a zona "(Sem zona)" que EXISTE
	// em sz_motoboy_zonas com dias_funcionamento=1..6 (Seg-Sáb, sem domingo). Só NULL/
	// negativo cai no fail-open; 0 segue pra query e respeita a agenda Seg-Sáb.
	if err != nil || !zonaID.Valid || zonaID.Int64 < 0 {
		return out // sem zona (NULL/<0) → fail-open
	}

	if !h.tableExists(ctx, "sz_motoboy_zonas") {
		return out
	}

	var diasCSV sql.NullString
	var cutoffsJSON sql.NullString
	err = h.Pool.QueryRow(ctx,
		`SELECT COALESCE(dias_funcionamento,''), COALESCE(cutoff_horarios,'')
		   FROM sz_motoboy_zonas WHERE id = $1`, zonaID.Int64,
	).Scan(&diasCSV, &cutoffsJSON)
	if err != nil {
		return out // zona inexistente → fail-open
	}

	out.HasSchedule = true
	out.Dias = parseDiasFuncionamento(diasCSV.String)
	out.Cutoffs = parseCutoffs(cutoffsJSON.String)
	return out
}

// dateAllowedByZone aplica o predicado de elegibilidade da zona à data escolhida
// (mesma regra do orders-service/schedule.go e do filtro do FalkDatePicker no front):
//
//	DOW(d) ∈ dias_funcionamento  E  agora ≤ (d − 1 dia) às cutoffs[DOW(d)] em SP.
//
// d deve ser a data em meia-noite no fuso SP. now é o instante atual (qualquer fuso —
// convertido internamente p/ SP). Quando HasSchedule=false retorna SEMPRE true (fail-open).
// Retorna (ok, motivoPT) — motivo preenchido só quando ok=false, para a mensagem 400.
func (z zoneSchedule) dateAllowed(d, now time.Time) (bool, string) {
	if !z.HasSchedule {
		return true, ""
	}
	dow := int(d.Weekday()) // 0=domingo..6=sábado (mesma convenção do banco)

	inDias := false
	for _, dd := range z.Dias {
		if dd == dow {
			inDias = true
			break
		}
	}
	if !inDias {
		return false, "a zona deste pedido não entrega no dia da semana escolhido (" + diasSemanaFullAdmin[dow] + ")"
	}

	// Cutoff: deadline = (d − 1 dia) às cutoffs[dow], no fuso de São Paulo.
	cut := z.Cutoffs[dow]
	if cut == "" {
		cut = "21:00"
	}
	hh, _ := strconv.Atoi(cut[0:2])
	mm, _ := strconv.Atoi(cut[3:5])
	deadline := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, tzSaoPauloAdmin).AddDate(0, 0, -1)
	if now.In(tzSaoPauloAdmin).After(deadline) {
		return false, "o horário limite (" + cut + " do dia anterior) para entregar nesta data já passou"
	}
	return true, ""
}

// diasSemanaFullAdmin — nomes PT-BR por DOW (0=dom..6=sáb) p/ as mensagens 400.
var diasSemanaFullAdmin = [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"}

// ── GET /orders/motoboy/{id}/zona-schedule ────────────────────────────────────

// zonaScheduleResponse é o contrato consumido pelo FalkDatePicker no front.
// has_schedule=false → a UI não desabilita por zona (cai no min-only).
type zonaScheduleResponse struct {
	HasSchedule bool      `json:"has_schedule"`
	Dias        []int     `json:"dias"`    // DOW 0=dom..6=sáb permitidos
	Cutoffs     [7]string `json:"cutoffs"` // "HH:MM" por DOW (índice 0..6)
}

// ZonaSchedule devolve a regra de agendamento da zona do pedido motoboy ({id} =
// sz_motoboy_pedidos.id — MESMO id do POST /reagendar-clone, funciona p/ originais
// E clones órfãos). O front usa dias+cutoffs para desabilitar datas no calendário.
// NOTA: só ReagendarClone valida a data escolhida contra a zona no backend; o
// Reagendar comum NÃO tem gate de zona server-side — a regra de zona do reagendar
// comum é aplicada apenas no picker (UI). Abrir esta rota p/ o portal é o que permite
// ao picker da cópia portal carregar a agenda e desabilitar dias fora de funcionamento.
//
// EXPOSTA NO GRUPO DualAuth (admin OU portal): READ-ONLY com OWNERSHIP-GATE igual ao
// das mutações — admin vê tudo; produtor/afiliado só a agenda do pedido DELE; cross-user
// → 404 unificado (não revela existência de pedido alheio). A cópia da tela no portal
// usa esta agenda p/ desabilitar dias fora de funcionamento no picker de reagendar.
func (h *OrdersHandler) ZonaSchedule(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	mbPedidoID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || mbPedidoID <= 0 {
		httpx.Err(w, 400, "bad_request", "id inválido")
		return
	}
	ctx := r.Context()

	// OWNERSHIP-GATE (DualAuth): admin lê tudo; produtor/afiliado só o pedido DELE.
	// 404 unificado — não revela existência de pedido alheio. Read-only (sem escrita).
	if !h.actorOwnsMotoboyPedido(ctx, mbPedidoID) {
		httpx.Err(w, 404, "not_found", "pedido motoboy não encontrado")
		return
	}

	z := h.loadZoneSchedule(ctx, mbPedidoID)
	httpx.JSON(w, 200, zonaScheduleResponse{
		HasSchedule: z.HasSchedule,
		Dias:        z.Dias,
		Cutoffs:     z.Cutoffs,
	})
}
