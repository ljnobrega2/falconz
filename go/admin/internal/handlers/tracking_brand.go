// Package handlers — endpoint admin para configuração de marca do tracking.
// Espelha senderzz_tracking_brands em includes/senderzz-tracking-brand.php:87.
// Persiste em senderzz_options como JSON map[class_id]→{logo,cor,cor_texto,nome,rodape}.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/admin-service/internal/httpx"
)

// TrackingBrandHandler gerencia marcas por classe de envio.
type TrackingBrandHandler struct{ Pool *pgxpool.Pool }

// reHex valida cor hexadecimal #RRGGBB.
var reHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// trackingBrandEntry espelha a estrutura PHP senderzz_tracking_brands[class_id].
type trackingBrandEntry struct {
	ClassID   int    `json:"class_id"`
	ClassName string `json:"class_name,omitempty"`
	Logo      string `json:"logo"`
	Cor       string `json:"cor"`
	CorTexto  string `json:"cor_texto"`
	Nome      string `json:"nome"`
	Rodape    string `json:"rodape"`
}

// brandMap é o formato interno persistido no option (sem class_name, indexado por class_id).
type brandMap map[string]struct {
	Logo     string `json:"logo"`
	Cor      string `json:"cor"`
	CorTexto string `json:"cor_texto"`
	Nome     string `json:"nome"`
	Rodape   string `json:"rodape"`
}

// --- helpers internos -------------------------------------------------------

// tableExistsTB verifica presença de tabela no schema public.
func (h *TrackingBrandHandler) tableExistsTB(ctx context.Context, name string) bool {
	var ok bool
	_ = h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema='public' AND table_name=$1
		)`, name).Scan(&ok)
	return ok
}

// readBrandMap lê o mapa do option senderzz_tracking_brands.
func (h *TrackingBrandHandler) readBrandMap(ctx context.Context) brandMap {
	bm := brandMap{}
	if !h.tableExistsTB(ctx, "senderzz_options") {
		return bm
	}
	var raw string
	if err := h.Pool.QueryRow(ctx,
		`SELECT value FROM senderzz_options WHERE "key"='senderzz_tracking_brands'`).Scan(&raw); err != nil {
		return bm
	}
	_ = json.Unmarshal([]byte(raw), &bm)
	return bm
}

// saveBrandMap persiste o mapa via UPSERT em senderzz_options.
func (h *TrackingBrandHandler) saveBrandMap(ctx context.Context, bm brandMap) error {
	if !h.tableExistsTB(ctx, "senderzz_options") {
		return nil
	}
	b, err := json.Marshal(bm)
	if err != nil {
		return err
	}
	_, err = h.Pool.Exec(ctx,
		`INSERT INTO senderzz_options ("key", value)
		 VALUES ('senderzz_tracking_brands', $1)
		 ON CONFLICT ("key") DO UPDATE SET value = EXCLUDED.value`, string(b))
	return err
}

// className resolve o nome da classe de envio pelo class_id.
// Graceful: tabela ausente → "Classe #N". class_id=0 → "Padrão (sem classe)".
func (h *TrackingBrandHandler) className(ctx context.Context, classID int) string {
	if classID == 0 {
		return "Padrão (sem classe)"
	}
	fallback := fmt.Sprintf("Classe #%d", classID)
	if !h.tableExistsTB(ctx, "senderzz_shipping_classes") {
		return fallback
	}
	var name string
	if err := h.Pool.QueryRow(ctx,
		`SELECT COALESCE(name,'') FROM senderzz_shipping_classes WHERE id=$1`, classID).Scan(&name); err != nil {
		return fallback
	}
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// buildList converte brandMap em slice ordenado com class_name preenchido.
func (h *TrackingBrandHandler) buildList(ctx context.Context, bm brandMap) []trackingBrandEntry {
	items := make([]trackingBrandEntry, 0, len(bm))
	for k, v := range bm {
		id, _ := strconv.Atoi(k)
		items = append(items, trackingBrandEntry{
			ClassID:   id,
			ClassName: h.className(ctx, id),
			Logo:      v.Logo,
			Cor:       v.Cor,
			CorTexto:  v.CorTexto,
			Nome:      v.Nome,
			Rodape:    v.Rodape,
		})
	}
	return items
}

// --- GET /tracking-brand ----------------------------------------------------

// GetAll retorna a lista de marcas configuradas por classe.
func (h *TrackingBrandHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bm := h.readBrandMap(ctx)
	httpx.JSON(w, 200, map[string]any{"items": h.buildList(ctx, bm)})
}

// --- POST /tracking-brand ---------------------------------------------------

// SaveAll recebe items e salva o mapa completo.
func (h *TrackingBrandHandler) SaveAll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []trackingBrandEntry `json:"items"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Err(w, 400, "bad_request", "json inválido")
		return
	}

	// Valida cores; rejeita qualquer hex inválido.
	for _, it := range body.Items {
		if it.Cor != "" && !reHex.MatchString(it.Cor) {
			httpx.Err(w, 400, "invalid_hex", fmt.Sprintf("cor inválida para class_id %d: %q (esperado #RRGGBB)", it.ClassID, it.Cor))
			return
		}
		if it.CorTexto != "" && !reHex.MatchString(it.CorTexto) {
			httpx.Err(w, 400, "invalid_hex", fmt.Sprintf("cor_texto inválida para class_id %d: %q (esperado #RRGGBB)", it.ClassID, it.CorTexto))
			return
		}
	}

	ctx := r.Context()

	// Lê mapa existente e faz merge (preserva classes não enviadas).
	bm := h.readBrandMap(ctx)
	for _, it := range body.Items {
		if it.ClassID < 0 {
			continue
		}
		key := strconv.Itoa(it.ClassID)
		bm[key] = struct {
			Logo     string `json:"logo"`
			Cor      string `json:"cor"`
			CorTexto string `json:"cor_texto"`
			Nome     string `json:"nome"`
			Rodape   string `json:"rodape"`
		}{
			Logo:     it.Logo,
			Cor:      it.Cor,
			CorTexto: it.CorTexto,
			Nome:     it.Nome,
			Rodape:   it.Rodape,
		}
	}

	if err := h.saveBrandMap(ctx, bm); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": h.buildList(ctx, bm)})
}

// --- POST /tracking-brand/add-class -----------------------------------------

// AddClass adiciona slot vazio para uma classe (se ainda não existir).
func (h *TrackingBrandHandler) AddClass(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClassID int `json:"class_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil || body.ClassID < 0 {
		httpx.Err(w, 400, "bad_request", "class_id inválido")
		return
	}

	ctx := r.Context()
	bm := h.readBrandMap(ctx)
	key := strconv.Itoa(body.ClassID)

	// Não sobrescreve slot já existente.
	if _, exists := bm[key]; !exists {
		bm[key] = struct {
			Logo     string `json:"logo"`
			Cor      string `json:"cor"`
			CorTexto string `json:"cor_texto"`
			Nome     string `json:"nome"`
			Rodape   string `json:"rodape"`
		}{}
	}

	if err := h.saveBrandMap(ctx, bm); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": h.buildList(ctx, bm)})
}

// --- DELETE /tracking-brand/{class_id} --------------------------------------

// DeleteClass remove a classe do mapa e retorna a lista atualizada.
func (h *TrackingBrandHandler) DeleteClass(w http.ResponseWriter, r *http.Request) {
	classID, err := strconv.Atoi(chi.URLParam(r, "class_id"))
	if err != nil || classID <= 0 {
		httpx.Err(w, 400, "bad_request", "class_id inválido")
		return
	}

	ctx := r.Context()
	bm := h.readBrandMap(ctx)
	delete(bm, strconv.Itoa(classID))

	if err := h.saveBrandMap(ctx, bm); err != nil {
		httpx.Err(w, 500, "db_error", err.Error())
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": h.buildList(ctx, bm)})
}
