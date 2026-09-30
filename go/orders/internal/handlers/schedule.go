// schedule.go — Motor de agendamento de entrega do checkout nativo (motoboy).
//
// Namespace HTTP: /checkout-api/schedule (RAIZ do router, público, sem JWT).
//
// Rota implementada:
//
//	GET /checkout-api/schedule?token=<t>&cep=<cep>
//	  → resolve a zona do CEP, lista as próximas datas de entrega e classifica
//	    cada uma como 'agendamento' ou 'pre_agendado'.
//
// REGRAS (confirmadas pelo dono — implementadas EXATO):
//   - Zona resolvida pelo CEP: sz_motoboy_cep_zonas (faixa cep_inicio..cep_fim)
//     → sz_motoboy_zonas.dias_funcionamento (CSV de dias da semana que a zona
//     entrega). CONVENÇÃO confirmada empiricamente: NÃO é ISO. É a convenção
//     PHP date('w') = Postgres EXTRACT(DOW): 0=domingo … 6=sábado. (sz_motoboy_
//     sanitize_zone_days aceita '0' e rejeita '7'; logo domingo=0, não 7.)
//     Por isso usamos EXTRACT(DOW), NÃO ISODOW.
//   - frequência = nº de dias em dias_funcionamento.
//   - frequência > 3 dias/semana → janela = próximas 30 DATAS DE ENTREGA.
//   - frequência ≤ 3 dias/semana → janela = próximas 5 DATAS DE ENTREGA.
//   - Classificação de cada data ofertada:
//   - janela 30 (>3/sem): as 3 PRIMEIRAS = 'agendamento'; da 4ª em diante = 'pre_agendado'.
//   - janela 5 (≤3/sem):  a 1ª = 'agendamento'; da 2ª em diante = 'pre_agendado'.
//   - pre_agendado: cliente DEVE confirmar até 3 DATAS DE ENTREGA antes; se não
//     confirmar, o pedido é CANCELADO automaticamente (mostrar popup no checkout).
//   - Cutoff: normalmente uma data d só é ofertável se d > hoje (America/Sao_Paulo)
//     E DOW(d) ∈ dias_funcionamento E agora <= (d − 1 dia) às
//     cutoff_horarios[dow]. Exceção: zonas de segunda a sábado com fechamento
//     às 21:00 também ofertam hoje até 21:00.
//     Mesma regra do PHP sz_motoboy_zone_next_dates / sz_motoboy_zone_date_is_allowed.
//
// IMPORTANTE: a MESMA função (computeOfferableDates) é usada por /schedule e pela
// validação do POST /order — assim a data ofertada nunca é rejeitada na criação,
// e o cliente nunca consegue escravar uma data/tipo que o servidor não ofereceu.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/orders-service/internal/httpx"
)

// tzSaoPaulo é o fuso usado em todo o motor de agendamento (espelha o PHP).
var tzSaoPaulo = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Fallback defensivo: offset fixo -03:00 (sem horário de verão no BR atual).
		return time.FixedZone("America/Sao_Paulo", -3*3600)
	}
	return loc
}()

// ── Estruturas de domínio ───────────────────────────────────────────────────

// zoneInfo agrega os dados da zona necessários ao agendamento.
type zoneInfo struct {
	ZonaID   int64
	CDID     int64
	ZonaNome string
	Dias     []int          // DOW 0=dom..6=sáb, ordenado, sem repetição
	Cutoffs  map[int]string // dow → "HH:MM" (sempre 7 entradas)
}

// frequencia retorna o nº de dias de funcionamento da zona.
func (z zoneInfo) frequencia() int { return len(z.Dias) }

// janelaDias retorna o tamanho da janela conforme a frequência.
//
//	frequência > 3 → 30 datas de entrega
//	frequência ≤ 3 → 5 datas de entrega
func (z zoneInfo) janelaDias() int {
	if z.frequencia() > 3 {
		return 30
	}
	return 5
}

// limiteAgendamento retorna quantas das primeiras datas são 'agendamento'.
//
// Pedido dono 2026-07-28: fixo em 5 (antes variava 1 ou 3 conforme frequência
// da zona) — "Agendamento" sempre mostra os próximos 5 dias ofertáveis da
// zona (já filtrados pelos dias de funcionamento dela); o resto da janela
// (até 30 ou 5, conforme frequencia/janelaDias) vira 'pre_agendado'.
func (z zoneInfo) limiteAgendamento() int {
	return 5
}

// offeredDate é uma data de entrega ofertada já classificada.
type offeredDate struct {
	Data  string `json:"data"`  // YYYY-MM-DD
	Tipo  string `json:"tipo"`  // 'agendamento' | 'pre_agendado'
	Label string `json:"label"` // rótulo PT-BR amigável ("Amanhã", "Segunda-feira 23/06")
}

// ── Resolução de zona pelo CEP ──────────────────────────────────────────────

// resolveZonaPorCEP resolve a zona ativa que cobre o CEP informado.
//
// Espelha sz_motoboy_resolver_zona: faixa cep_inicio <= cep <= cep_fim, zona ativa.
// Retorna (nil, nil) quando nenhuma faixa cobre o CEP (fora de área de entrega).
func resolveZonaPorCEP(ctx context.Context, db *pgxpool.Pool, cep string) (*zoneInfo, error) {
	cep = onlyDigits(cep)
	if len(cep) != 8 {
		return nil, nil
	}

	var (
		zonaID, cdID int64
		zonaNome     string
		diasCSV      string
		cutoffsJSON  *string
	)
	err := db.QueryRow(ctx,
		`SELECT z.id, z.cd_id, z.nome, z.dias_funcionamento, z.cutoff_horarios
		   FROM sz_motoboy_cep_zonas cz
		   JOIN sz_motoboy_zonas z ON z.id = cz.zona_id AND z.ativo = TRUE
		  WHERE cz.cep_inicio <= $1 AND cz.cep_fim >= $1
		  ORDER BY z.id
		  LIMIT 1`,
		cep,
	).Scan(&zonaID, &cdID, &zonaNome, &diasCSV, &cutoffsJSON)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	z := &zoneInfo{
		ZonaID:   zonaID,
		CDID:     cdID,
		ZonaNome: strings.TrimSpace(zonaNome),
		Dias:     parseDiasFuncionamento(diasCSV),
		Cutoffs:  parseCutoffs(cutoffsJSON),
	}
	return z, nil
}

// parseDiasFuncionamento normaliza o CSV de dias (DOW 0..6) como no PHP
// sz_motoboy_sanitize_zone_days: aceita só 0–6, único, ordenado; default 0–6 vazio? não:
// o default do PHP é '1,2,3,4,5,6' (seg–sáb) quando vazio.
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

// parseCutoffs decodifica cutoff_horarios. Formato do banco: array JSON de 7
// strings indexado por dia (["21:00",...]) OU objeto {"0":"21:00",...}.
// Sempre retorna mapa com as 7 chaves (0..6); ausentes viram "21:00".
func parseCutoffs(raw *string) map[int]string {
	out := map[int]string{0: "21:00", 1: "21:00", 2: "21:00", 3: "21:00", 4: "21:00", 5: "21:00", 6: "21:00"}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return out
	}
	s := strings.TrimSpace(*raw)

	// Tenta array ["HH:MM", ...] (formato observado no banco).
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

	// Tenta objeto {"0":"HH:MM", ...} (formato gravado pelo wp_json_encode do PHP).
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

// permiteEntregaHoje identifica a regra especial das zonas de segunda a sábado
// com fechamento às 21:00. O mesmo fechamento que normalmente vale para o dia
// anterior passa a valer para hoje, permitindo o agendamento no próprio dia.
func permiteEntregaHoje(z zoneInfo, now, today time.Time) bool {
	if len(z.Dias) != 6 {
		return false
	}
	for i, dow := range []int{1, 2, 3, 4, 5, 6} {
		if z.Dias[i] != dow || z.Cutoffs[dow] != "21:00" {
			return false
		}
	}
	if int(today.Weekday()) == 0 {
		return false
	}
	fechamento := time.Date(today.Year(), today.Month(), today.Day(), 21, 0, 0, 0, tzSaoPaulo)
	return !now.After(fechamento)
}

// ── Motor de datas ofertáveis (FONTE ÚNICA p/ /schedule e /order) ───────────

// computeOfferableDates lista as datas de entrega ofertáveis da zona, já
// classificadas, na ordem cronológica. Limita pela janela (5 ou 30 datas).
//
// Regra de ofertabilidade (idêntica ao PHP sz_motoboy_zone_next_dates):
//   - itera d = hoje ou amanhã, conforme a regra especial de entrega no mesmo
//     dia, … (a partir de agora em America/Sao_Paulo);
//   - aceita d se DOW(d) ∈ dias_funcionamento;
//   - aplica cutoff: agora <= (d − 1 dia) às cutoff_horarios[DOW(d)];
//   - para quando atingir janelaDias() datas (5 ou 30).
//
// O enumerador de datas usa generate_series no Postgres (datas + EXTRACT(DOW));
// o cutoff é aplicado em Go a partir do JSON já parseado (mais simples e fiel).
func computeOfferableDates(ctx context.Context, db *pgxpool.Pool, z *zoneInfo) ([]offeredDate, error) {
	limite := z.janelaDias()
	if limite <= 0 || len(z.Dias) == 0 {
		return []offeredDate{}, nil
	}

	// Conjunto de DOWs permitidos para filtro em SQL.
	diasIn := make([]string, 0, len(z.Dias))
	for _, d := range z.Dias {
		diasIn = append(diasIn, strconv.Itoa(d))
	}

	now := time.Now().In(tzSaoPaulo)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tzSaoPaulo)
	sameDay := permiteEntregaHoje(*z, now, today)
	startDate := today.AddDate(0, 0, 1)
	if sameDay {
		startDate = today
	}

	// generate_series amplo o suficiente: uma zona de 4 dias/semana precisa de
	// ~53 dias corridos para render 30 datas de entrega. 120 dias dá folga total.
	rows, err := db.Query(ctx,
		`SELECT d::date, EXTRACT(DOW FROM d)::int
		   FROM generate_series($1::date, $1::date + INTERVAL '120 days', INTERVAL '1 day') AS g(d)
		  WHERE EXTRACT(DOW FROM d)::int = ANY($2::int[])
		  ORDER BY d`,
		startDate.Format("2006-01-02"),
		"{"+strings.Join(diasIn, ",")+"}",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]offeredDate, 0, limite)
	idx := 0
	for rows.Next() {
		if len(out) >= limite {
			break
		}
		var dt time.Time
		var dow int
		if err := rows.Scan(&dt, &dow); err != nil {
			return nil, err
		}
		dt = time.Date(dt.Year(), dt.Month(), dt.Day(), 0, 0, 0, 0, tzSaoPaulo)

		// Cutoff: a exceção de mesmo dia usa o fechamento de hoje; as demais
		// datas continuam usando o fechamento do dia anterior.
		cut := z.Cutoffs[dow]
		if cut == "" {
			cut = "21:00"
		}
		hh, _ := strconv.Atoi(cut[0:2])
		mm, _ := strconv.Atoi(cut[3:5])
		deadline := time.Date(dt.Year(), dt.Month(), dt.Day(), hh, mm, 0, 0, tzSaoPaulo)
		if !(sameDay && dt.Equal(today)) {
			deadline = deadline.AddDate(0, 0, -1)
		}
		if now.After(deadline) {
			continue // passou do cutoff — não oferta esta data
		}

		tipo := "pre_agendado"
		if idx < z.limiteAgendamento() {
			tipo = "agendamento"
		}
		out = append(out, offeredDate{
			Data:  dt.Format("2006-01-02"),
			Tipo:  tipo,
			Label: rotuloData(dt, today),
		})
		idx++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// diasSemanaFull são os nomes PT-BR por DOW (0=dom..6=sáb).
var diasSemanaFull = [7]string{"Domingo", "Segunda-feira", "Terça-feira", "Quarta-feira", "Quinta-feira", "Sexta-feira", "Sábado"}

// rotuloData gera um rótulo amigável PT-BR: "Amanhã" para d=hoje+1, senão
// "Segunda-feira 23/06".
func rotuloData(d, today time.Time) string {
	if d.Equal(today) {
		return "Hoje"
	}
	if d.Equal(today.AddDate(0, 0, 1)) {
		return "Amanhã"
	}
	dow := int(d.Weekday()) // time.Weekday: 0=domingo..6=sábado (mesma convenção)
	return fmt.Sprintf("%s %02d/%02d", diasSemanaFull[dow], d.Day(), int(d.Month()))
}

// ── GET /checkout-api/schedule ──────────────────────────────────────────────

// scheduleResponse é o contrato de /schedule.
type scheduleResponse struct {
	Zona       string        `json:"zona"`
	Frequencia int           `json:"frequencia"`
	JanelaDias int           `json:"janela_dias"`
	Datas      []offeredDate `json:"datas"`
}

// GetSchedule resolve a zona do CEP e retorna as datas de entrega ofertáveis.
//
// Query params:
//   - token (obrigatório) — link de checkout; usado para confirmar tipo='motoboy'.
//   - cep   (obrigatório) — CEP do destinatário (8 dígitos após limpeza).
//
// Erros:
//   - 400 token/cep ausentes
//   - 404 oferta não encontrada
//   - 422 oferta não é do tipo motoboy (correio não tem agendamento)
//   - 422 "fora de área de entrega" quando o CEP não cai em nenhuma zona
func (h *CheckoutHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	cep := onlyDigits(r.URL.Query().Get("cep"))
	if token == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "token é obrigatório")
		return
	}
	if len(cep) != 8 {
		httpx.WriteErr(w, http.StatusBadRequest, "CEP inválido (8 dígitos)")
		return
	}

	ctx := r.Context()

	// Confirma que a oferta existe e é do tipo motoboy (correio não agenda).
	var tipo string
	err := h.db.QueryRow(ctx,
		`SELECT tipo FROM senderzz_checkout_links WHERE token = $1 LIMIT 1`, token,
	).Scan(&tipo)
	if err == pgx.ErrNoRows {
		httpx.WriteErr(w, http.StatusNotFound, "oferta não encontrada")
		return
	}
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao buscar oferta")
		return
	}
	// FEAT-LINK-MISTO (2026-07-27): tipo='misto' também agenda quando o CEP
	// resolve pra motoboy (o front só chama /schedule pra misto DEPOIS de
	// confirmar isso via /resolve) — mesma regra de zona/datas do motoboy puro.
	tipoNorm := strings.ToLower(strings.TrimSpace(tipo))
	if tipoNorm != "motoboy" && tipoNorm != "misto" {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "esta oferta não tem agendamento de entrega")
		return
	}

	z, err := resolveZonaPorCEP(ctx, h.db, cep)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao resolver zona")
		return
	}
	if z == nil {
		httpx.WriteErr(w, http.StatusUnprocessableEntity, "fora de área de entrega")
		return
	}

	datas, err := computeOfferableDates(ctx, h.db, z)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "erro ao calcular datas")
		return
	}

	resp := scheduleResponse{
		Zona:       z.ZonaNome,
		Frequencia: z.frequencia(),
		JanelaDias: z.janelaDias(),
		Datas:      datas,
	}
	httpx.WriteOK(w, map[string]any{"agenda": resp})
}
