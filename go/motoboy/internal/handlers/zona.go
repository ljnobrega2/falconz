package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/senderzz/motoboy-service/internal/httpx"
)

// ZonaHandler agrupa dependências para o resolver de zona por CEP.
type ZonaHandler struct {
	Pool  *pgxpool.Pool
	Cache *CEPCache
}

// GetZonaCEP responde GET /zona-cep?cep=XXXXXXXX — resolver zona de entrega pelo CEP.
//
// Replica a lógica de sz_mb_api_zona_cep em PHP:
//  1. Sanitiza o CEP (apenas dígitos, 8 chars).
//  2. Busca o range cep_inicio..cep_fim em sz_motoboy_cep_zonas.
//  3. Retorna zona + dias disponíveis calculados com cutoff_horarios.
//
// Cache em memória com TTL 12h para evitar queries repetidas por CEP.
func (h *ZonaHandler) GetZonaCEP(w http.ResponseWriter, r *http.Request) {
	cepRaw := r.URL.Query().Get("cep")
	cep := sanitizeCEP(cepRaw)
	if len(cep) != 8 {
		httpx.WriteErr(w, http.StatusBadRequest, "CEP inválido — informe 8 dígitos numéricos")
		return
	}

	// Tenta cache antes de bater no banco.
	if h.Cache != nil {
		if cached, ok := h.Cache.Get(cep); ok {
			w.Header().Set("X-Cache", "HIT")
			httpx.WriteOK(w, cached)
			return
		}
	}

	result, err := h.resolveZona(r.Context(), cep)
	if err != nil {
		slog.Warn("[zona] CEP sem cobertura", "cep", cep, "err", err)
		httpx.WriteErr(w, http.StatusNotFound, "CEP fora da área de cobertura")
		return
	}

	// Armazena no cache.
	if h.Cache != nil {
		h.Cache.Set(cep, result)
	}

	w.Header().Set("X-Cache", "MISS")
	httpx.WriteOK(w, result)
}

// resolveZona busca a zona correspondente ao CEP no banco de dados.
func (h *ZonaHandler) resolveZona(ctx context.Context, cep string) (map[string]any, error) {
	type ZonaRow struct {
		ZonaID            int64
		ZonaNome          string
		ZonaDescricao     *string
		DiasFuncionamento string
		CutoffHorarios    *string
		CDID              int64
		CDNome            string
		CDCidade          string
		CDUF              string
	}

	var z ZonaRow
	// ativo é TINYINT(1) no MySQL → smallint no Postgres após pgloader.
	// Comparar contra 1 (não true) até o schema ser migrado para boolean.
	err := h.Pool.QueryRow(ctx,
		`SELECT
			z.id,
			z.nome,
			z.descricao,
			z.dias_funcionamento,
			z.cutoff_horarios,
			c.id  AS cd_id,
			c.nome AS cd_nome,
			c.cidade,
			c.uf
		 FROM sz_motoboy_cep_zonas cz
		 JOIN sz_motoboy_zonas z ON z.id = cz.zona_id AND z.ativo = 1
		 JOIN sz_motoboy_cds   c ON c.id = z.cd_id   AND c.ativo = 1
		WHERE cz.cep_inicio <= $1 AND cz.cep_fim >= $1
		ORDER BY cz.id ASC
		LIMIT 1`,
		cep,
	).Scan(
		&z.ZonaID, &z.ZonaNome, &z.ZonaDescricao,
		&z.DiasFuncionamento, &z.CutoffHorarios,
		&z.CDID, &z.CDNome, &z.CDCidade, &z.CDUF,
	)
	if err != nil {
		return nil, fmt.Errorf("zona não encontrada para CEP %s: %w", cep, err)
	}

	// Calcula próximas datas disponíveis com base nos dias de funcionamento e cutoffs.
	diasDisponiveis := calcularDatasDisponiveis(z.DiasFuncionamento, z.CutoffHorarios)

	return map[string]any{
		"zona_id":             z.ZonaID,
		"zona_nome":           z.ZonaNome,
		"zona_descricao":      z.ZonaDescricao,
		"dias_funcionamento":  z.DiasFuncionamento,
		"cd_id":               z.CDID,
		"cd_nome":             z.CDNome,
		"cd_cidade":           z.CDCidade,
		"cd_uf":               z.CDUF,
		"datas_disponiveis":   diasDisponiveis,
	}, nil
}

// calcularDatasDisponiveis retorna as próximas datas de entrega disponíveis
// respeitando dias_funcionamento (CSV de 0-6, domingo=0) e cutoff_horarios (JSON).
//
// Replica a lógica de sz_motoboy_available_dates em PHP.
// Retorna até 5 datas nos próximos 14 dias.
func calcularDatasDisponiveis(diasFuncionamento string, cutoffHorariosJSON *string) []string {
	// Parseia os dias ativos.
	diasAtivos := map[int]bool{}
	for _, d := range strings.Split(diasFuncionamento, ",") {
		d = strings.TrimSpace(d)
		if len(d) == 1 && d[0] >= '0' && d[0] <= '6' {
			diasAtivos[int(d[0]-'0')] = true
		}
	}
	if len(diasAtivos) == 0 {
		// Padrão: seg-sáb.
		for i := 1; i <= 6; i++ {
			diasAtivos[i] = true
		}
	}

	// Parseia cutoff_horarios — JSON {"0":"21:00",...,"6":"21:00"}.
	cutoffs := map[string]string{}
	if cutoffHorariosJSON != nil && *cutoffHorariosJSON != "" {
		_ = json.Unmarshal([]byte(*cutoffHorariosJSON), &cutoffs)
	}
	// Fallback para todos os dias: 21:00 do dia anterior.
	defaultCutoff := "21:00"

	agora := time.Now().In(brLocation)
	var datas []string
	for offset := 1; offset <= 14 && len(datas) < 5; offset++ {
		candidato := agora.AddDate(0, 0, offset)
		diaSemana := int(candidato.Weekday()) // 0=domingo

		if !diasAtivos[diaSemana] {
			continue
		}

		// Verifica se o cutoff do dia anterior já passou.
		cutoffStr, ok := cutoffs[fmt.Sprintf("%d", diaSemana)]
		if !ok || cutoffStr == "" {
			cutoffStr = defaultCutoff
		}
		var cutoffH, cutoffM int
		fmt.Sscanf(cutoffStr, "%d:%d", &cutoffH, &cutoffM)

		diaCutoff := agora // cutoff é avaliado em relação a "agora" (dia anterior à entrega)
		cutoffTime := time.Date(diaCutoff.Year(), diaCutoff.Month(), diaCutoff.Day(),
			cutoffH, cutoffM, 0, 0, brLocation)

		// Se a entrega é amanhã (offset=1), o cutoff é hoje.
		// Se a entrega é depois de amanhã (offset>1), o cutoff do dia anterior já passou.
		if offset == 1 && agora.After(cutoffTime) {
			continue // passou do horário limite para entrega amanhã
		}

		datas = append(datas, candidato.Format("2006-01-02"))
	}

	return datas
}

// sanitizeCEP remove todos os não-dígitos do CEP.
func sanitizeCEP(cep string) string {
	var b strings.Builder
	for _, r := range cep {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ── Cache simples em memória com TTL ─────────────────────────────────────────

// CEPCache é um cache in-process (sem dependência externa) com TTL por entrada.
// Para produção com múltiplas instâncias, trocar por Redis via Asynq client ou go-redis.
type CEPCache struct {
	entries map[string]cacheEntry
}

type cacheEntry struct {
	value     map[string]any
	expiresAt time.Time
}

// NewCEPCache cria um cache vazio.
func NewCEPCache() *CEPCache {
	return &CEPCache{entries: make(map[string]cacheEntry)}
}

// Get retorna o valor cacheado e true se ainda válido, ou nil, false se expirado/ausente.
func (c *CEPCache) Get(key string) (map[string]any, bool) {
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

// Set armazena o valor com TTL de 12h (conforme spec).
func (c *CEPCache) Set(key string, value map[string]any) {
	c.entries[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(12 * time.Hour),
	}
}
