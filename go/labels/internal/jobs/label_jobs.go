// Package jobs define os workers Asynq para processamento assíncrono de etiquetas.
//
// Workers disponíveis:
//   - ProcessGeneratePDF   — chama ME GenerateLabel, baixa o PDF, atualiza wc_me_labels
//   - ProcessSyncTracking  — chama ME TrackShipment, sincroniza status em wc_me_labels
//
// Cada worker:
//   - Loga com prefixo [senderzz_labels] para manter convenção do CLAUDE.md
//   - Registra tentativas em wc_me_queue para auditoria
//   - Retorna erro para que o Asynq faça retry automático (com backoff exponencial)
//   - Não silencia erros — fail-closed: em caso de falha definitiva, label fica
//     sem PDF/tracking_code para não bloquear a operação principal
//
// Variável de ambiente obrigatória: ME_TOKEN (verificada no construtor do MEClient).
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall" // AUDIT-2026-06-21 #22 — Control hook do dialer (anti-rebinding)
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/me"
	"github.com/senderzz/labels-service/internal/track17"
	"github.com/senderzz/labels-service/internal/trackinghistory"
)

// Constantes de tipo de task — usadas ao enfileirar e ao registrar workers.
const (
	// TypeGeneratePDF: baixa o PDF da etiqueta após CreateShipment + GenerateLabel.
	TypeGeneratePDF = "labels:generate_pdf"
	// TypeSyncTracking: sincroniza status de rastreamento via ME API.
	TypeSyncTracking = "labels:sync_tracking"
	// TypeBackfillTracking: reconcilia o tracking_code via ME por me_shipment_id.
	// AUDIT-2026-07-28: sem isso, etiqueta cujo tracking a ME demora a atribuir
	// (doc oficial: "pode levar até 1 dia útil") ficava com tracking_code NULL
	// pra sempre — nem o poll de status nem o webhook (antes de corrigido)
	// preenchiam depois do 1º tiro em ProcessGeneratePDF.
	TypeBackfillTracking = "labels:backfill_tracking"
)

// ─── Payloads de task ────────────────────────────────────────────────────────

// GeneratePDFPayload é o payload serializado na task Asynq de geração de PDF.
type GeneratePDFPayload struct {
	LabelID    int64  `json:"label_id"`
	ShipmentID string `json:"shipment_id"`
}

// SyncTrackingPayload é o payload serializado na task Asynq de rastreamento.
type SyncTrackingPayload struct {
	LabelID      int64  `json:"label_id"`
	TrackingCode string `json:"tracking_code"`
}

// ─── Funções de enfileiramento ────────────────────────────────────────────────

// EnqueueGeneratePDF enfileira uma task TypeGeneratePDF no Asynq.
// Deve ser chamada imediatamente após o INSERT bem-sucedido em wc_me_labels.
// O Asynq garantirá o retry automático com backoff se o worker falhar.
func EnqueueGeneratePDF(client *asynq.Client, labelID int64, shipmentID string) error {
	payload, err := json.Marshal(GeneratePDFPayload{
		LabelID:    labelID,
		ShipmentID: shipmentID,
	})
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueGeneratePDF: marshal: %w", err)
	}

	task := asynq.NewTask(TypeGeneratePDF, payload,
		// Até 5 tentativas com backoff exponencial gerenciado pelo Asynq.
		asynq.MaxRetry(5),
		// Timeout por execução — 30s para a chamada ME + download do PDF.
		asynq.Timeout(60*time.Second),
		// Retenção do resultado no Redis por 24h para diagnóstico.
		asynq.Retention(24*time.Hour),
	)

	info, err := client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueGeneratePDF: enqueue: %w", err)
	}

	slog.Info("[senderzz_labels] task GeneratePDF enfileirada",
		"task_id", info.ID,
		"label_id", labelID,
		"shipment_id", shipmentID,
	)
	return nil
}

// EnqueueSyncTracking enfileira uma task TypeSyncTracking no Asynq.
// Chamada após confirmar que a etiqueta foi enviada (status=posted).
func EnqueueSyncTracking(client *asynq.Client, labelID int64, trackingCode string) error {
	payload, err := json.Marshal(SyncTrackingPayload{
		LabelID:      labelID,
		TrackingCode: trackingCode,
	})
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueSyncTracking: marshal: %w", err)
	}

	task := asynq.NewTask(TypeSyncTracking, payload,
		asynq.MaxRetry(10),
		asynq.Timeout(30*time.Second),
		asynq.Retention(12*time.Hour),
	)

	info, err := client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueSyncTracking: enqueue: %w", err)
	}

	slog.Info("[senderzz_labels] task SyncTracking enfileirada",
		"task_id", info.ID,
		"label_id", labelID,
		"tracking_code", trackingCode,
	)
	return nil
}

// BackfillTrackingPayload é o payload da task de busca de tracking_code ausente.
type BackfillTrackingPayload struct {
	LabelID    int64  `json:"label_id"`
	ShipmentID string `json:"shipment_id"`
}

// EnqueueBackfillTracking enfileira uma task TypeBackfillTracking. Chamada
// pelo poll periódico (main.go) para etiquetas 'posted'/'released'.
func EnqueueBackfillTracking(client *asynq.Client, labelID int64, shipmentID string) error {
	payload, err := json.Marshal(BackfillTrackingPayload{LabelID: labelID, ShipmentID: shipmentID})
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueBackfillTracking: marshal: %w", err)
	}
	task := asynq.NewTask(TypeBackfillTracking, payload,
		asynq.MaxRetry(10),
		asynq.Timeout(30*time.Second),
		asynq.Retention(12*time.Hour),
	)
	info, err := client.Enqueue(task)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] EnqueueBackfillTracking: enqueue: %w", err)
	}
	slog.Info("[senderzz_labels] task BackfillTracking enfileirada",
		"task_id", info.ID, "label_id", labelID, "shipment_id", shipmentID)
	return nil
}

// ─── Workers ─────────────────────────────────────────────────────────────────

// LabelWorker agrupa o pool Postgres e o cliente ME para uso nos workers.
type LabelWorker struct {
	DB      *pgxpool.Pool
	ME      *me.MEClient
	Track17 *track17.Client
	// PDFDir: diretório onde os PDFs serão armazenados localmente.
	// Configurável via env var PDF_STORAGE_DIR (padrão: /var/senderzz/labels).
	PDFDir string
}

// NewLabelWorker cria um LabelWorker com as dependências injetadas.
func NewLabelWorker(db *pgxpool.Pool, meClient *me.MEClient) *LabelWorker {
	pdfDir := os.Getenv("PDF_STORAGE_DIR")
	if pdfDir == "" {
		pdfDir = "/var/senderzz/labels"
	}
	return &LabelWorker{
		DB:      db,
		ME:      meClient,
		Track17: track17.NewClient(),
		PDFDir:  pdfDir,
	}
}

// syncCarrierTracking consulta a 17TRACK conforme o status do pedido:
//   - a_caminho: no máximo uma vez por hora, somente entre 08:00 e 20:00 (SP);
//   - demais status: no máximo uma vez a cada 12 horas;
//   - entregue/terminais: não consulta mais.
//
// O rastreio da ME continua sendo a fonte do status interno; estes eventos são
// uma camada adicional de detalhe da transportadora para o cliente final.
func (w *LabelWorker) syncCarrierTracking(ctx context.Context, trackingCode string) {
	trackingCode = strings.TrimSpace(trackingCode)
	if trackingCode == "" || w.Track17 == nil {
		return
	}
	var company string
	if err := w.DB.QueryRow(ctx, `
		SELECT COALESCE(company_name, '')
		  FROM wc_me_labels
		 WHERE tracking_code = $1
		 ORDER BY id DESC
		 LIMIT 1`, trackingCode).Scan(&company); err != nil {
		return
	}
	carrier, ok := trackingCarrier(company, trackingCode)
	if !ok {
		return
	}

	// Trava no worker: o scheduler deixa de enfileirar labels delivered, mas uma
	// task que já estava pendente não pode fazer uma consulta após a entrega.
	var orderStatus string
	_ = w.DB.QueryRow(ctx, `
		SELECT COALESCE(o.status, '')
		  FROM wc_me_labels l
		  LEFT JOIN sz_orders o ON COALESCE(o.wp_order_id, o.id) = l.wc_order_id
		 WHERE l.tracking_code = $1
		 ORDER BY l.id DESC
		 LIMIT 1`, trackingCode).Scan(&orderStatus)
	interval, allowed := carrierTrackingPollPolicy(orderStatus, time.Now())
	if !allowed {
		return
	}

	var last *time.Time
	if err := w.DB.QueryRow(ctx, `
		INSERT INTO wc_carrier_tracking_sync (tracking_code, carrier_code)
		VALUES ($1, $2)
		ON CONFLICT (tracking_code) DO UPDATE SET updated_at = NOW()
		RETURNING last_checked_at`, trackingCode, carrier).Scan(&last); err != nil {
		slog.Warn("[senderzz_labels] 17TRACK: não foi possível preparar sincronização", "tracking_code", trackingCode, "err", err)
		return
	}
	if last != nil && time.Since(*last) < interval {
		// Mesmo sem consultar novamente a API, reaplica o último evento salvo.
		// Isso cobre reinícios/deploys após a consulta, quando o status do pedido
		// ainda não tinha sido espelhado.
		rows, err := w.DB.Query(ctx, `
			SELECT event_at, description, location, stage
			  FROM wc_carrier_tracking_events
			 WHERE tracking_code = $1
			 ORDER BY event_at DESC`, trackingCode)
		if err != nil {
			return
		}
		var cached []track17.Event
		for rows.Next() {
			var ev track17.Event
			if err := rows.Scan(&ev.Time, &ev.Description, &ev.Location, &ev.Stage); err != nil {
				rows.Close()
				return
			}
			cached = append(cached, ev)
		}
		rows.Close()
		if len(cached) > 0 {
			w.syncOrderStatusFromCarrier(ctx, trackingCode, cached)
		}
		return
	}

	events, err := w.Track17.SyncCarrier(ctx, trackingCode, carrier)
	if err != nil {
		slog.Warn("[senderzz_labels] 17TRACK: consulta falhou", "tracking_code", trackingCode, "err", err)
		return
	}
	for _, ev := range events {
		h := sha256.Sum256([]byte(trackingCode + "\x00" + ev.Time.UTC().Format(time.RFC3339) + "\x00" + ev.Description + "\x00" + ev.Location))
		_, err := w.DB.Exec(ctx, `
			INSERT INTO wc_carrier_tracking_events
				(tracking_code, event_hash, event_at, description, location, stage)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tracking_code, event_hash) DO NOTHING`,
			trackingCode, hex.EncodeToString(h[:]), ev.Time, ev.Description, ev.Location, ev.Stage)
		if err != nil {
			slog.Warn("[senderzz_labels] 17TRACK: falha ao salvar evento", "tracking_code", trackingCode, "err", err)
		}
	}
	if _, err := w.DB.Exec(ctx, `UPDATE wc_carrier_tracking_sync SET last_checked_at = NOW(), updated_at = NOW() WHERE tracking_code = $1`, trackingCode); err != nil {
		slog.Warn("[senderzz_labels] 17TRACK: falha ao marcar consulta", "tracking_code", trackingCode, "err", err)
	}
	if len(events) > 0 {
		w.syncOrderStatusFromCarrier(ctx, trackingCode, events)
		slog.Info("[senderzz_labels] 17TRACK: eventos sincronizados", "tracking_code", trackingCode, "count", len(events))
	}
}

// trackingCarrier identifica somente as transportadoras com integração
// explícita no 17TRACK. Códigos desconhecidos ficam fora para não registrar
// um rastreio com carrier errado e consumir cota da conta.
func trackingCarrier(company, trackingCode string) (int, bool) {
	name := strings.ToLower(strings.TrimSpace(company))
	switch {
	case strings.Contains(name, "jadlog") && isJadlogTrackingCode(trackingCode):
		return track17.JadlogCarrier, true
	case strings.Contains(name, "loggi") && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(trackingCode)), "LGI-"):
		return track17.LoggiCarrier, true
	case strings.Contains(name, "correios") && isCorreiosTrackingCode(trackingCode):
		return track17.CorreiosCarrier, true
	default:
		return 0, false
	}
}

func isCorreiosTrackingCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	return len(code) == 13 && code[0] >= 'A' && code[0] <= 'Z' &&
		code[1] >= 'A' && code[1] <= 'Z' &&
		code[2:11] >= "000000000" && code[2:11] <= "999999999" &&
		code[11:] == "BR"
}

// carrierTrackingPollPolicy define a frequência do agregador 17TRACK.
// O parâmetro now é convertido para America/Sao_Paulo internamente para que a
// janela não dependa do fuso do container.
func carrierTrackingPollPolicy(orderStatus string, now time.Time) (interval time.Duration, allowed bool) {
	status := strings.ToLower(strings.TrimSpace(orderStatus))
	if status == "entregue" || status == "completo" || status == "cancelled" || status == "frustrado" || status == "reembolsado" {
		return 0, false
	}
	if status != "a_caminho" {
		return 12 * time.Hour, true
	}

	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	local := now.In(loc)
	// Janela [08:00, 20:00): consulta às 08:00...19:xx; depois das 20h
	// aguarda o próximo ciclo das 08h para não gastar consulta fora do horário.
	if local.Hour() < 8 || local.Hour() >= 20 {
		return 0, false
	}
	return time.Hour, true
}

func (w *LabelWorker) syncOrderStatusFromCarrier(ctx context.Context, trackingCode string, events []track17.Event) {
	var orderID int64
	if err := w.DB.QueryRow(ctx, `SELECT wc_order_id FROM wc_me_labels WHERE tracking_code = $1 ORDER BY id DESC LIMIT 1`, trackingCode).Scan(&orderID); err != nil || orderID <= 0 {
		return
	}
	var at time.Time
	found := false
	deliveredAt := time.Time{}
	delivered := false
	for _, ev := range events {
		text := strings.ToLower(ev.Description)
		if carrierEventIsDelivered(ev) {
			if !delivered || ev.Time.After(deliveredAt) {
				deliveredAt, delivered = ev.Time, true
			}
		}
		if strings.Contains(text, "a caminho") || strings.Contains(text, "saiu para entrega") {
			if !found || ev.Time.After(at) {
				at, found = ev.Time, true
			}
		}
	}
	var previous string
	if err := w.DB.QueryRow(ctx, `SELECT status FROM sz_orders WHERE id = $1`, orderID).Scan(&previous); err != nil {
		return
	}
	if delivered {
		if previous == "entregue" || previous == "completo" || previous == "cancelled" || previous == "frustrado" || previous == "reembolsado" {
			return
		}
		if _, err := w.DB.Exec(ctx, `UPDATE sz_orders SET status = 'entregue', updated_at = NOW() WHERE id = $1 AND status NOT IN ('entregue','completo','cancelled','frustrado','reembolsado')`, orderID); err != nil {
			slog.Warn("[senderzz_labels] 17TRACK: falha ao atualizar pedido para entregue", "order_id", orderID, "err", err)
			return
		}
		_, _ = w.DB.Exec(ctx, `INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo) VALUES ($1, $2, 'entregue', 'webhook')`, orderID, previous)
		slog.Info("[senderzz_labels] 17TRACK: pedido atualizado para entregue", "order_id", orderID, "tracking_code", trackingCode, "event_at", deliveredAt)
		return
	}
	if !found {
		return
	}
	if previous == "a_caminho" || previous == "entregue" || previous == "completo" || previous == "cancelled" || previous == "frustrado" || previous == "reembolsado" {
		return
	}
	if _, err := w.DB.Exec(ctx, `UPDATE sz_orders SET status = 'a_caminho', updated_at = NOW() WHERE id = $1 AND status IN ('embalado','coletado','enviado')`, orderID); err != nil {
		slog.Warn("[senderzz_labels] 17TRACK: falha ao atualizar pedido para a caminho", "order_id", orderID, "err", err)
		return
	}
	_, _ = w.DB.Exec(ctx, `INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo) VALUES ($1, $2, 'a_caminho', 'webhook')`, orderID, previous)
	slog.Info("[senderzz_labels] 17TRACK: pedido atualizado para a caminho", "order_id", orderID, "tracking_code", trackingCode, "event_at", at)
}

// carrierEventIsDelivered distingue entrega final de entrega em ponto de apoio.
// A Jadlog usa textos como "entregue em um ponto" para registrar a entrada do
// pacote na rede; isso NÃO encerra o pedido do cliente. O stage Delivered é a
// evidência preferencial. As mensagens abaixo são fallback para provedores que
// não preenchem stage, mas descrevem explicitamente a entrega final.
func carrierEventIsDelivered(ev track17.Event) bool {
	if strings.EqualFold(strings.TrimSpace(ev.Stage), "delivered") {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(ev.Description))
	if strings.Contains(text, "entregue em um ponto") || strings.Contains(text, "entregue no ponto") || strings.Contains(text, "ponto jadlog") {
		return false
	}
	return strings.Contains(text, "entregue com sucesso") ||
		strings.Contains(text, "entrega concluída") ||
		strings.Contains(text, "entrega realizada ao destinatário")
}

func isJadlogTrackingCode(code string) bool {
	if len(code) != 14 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveBackfillTracking trata a transição da Jadlog: antes da coleta a ME
// pode expor apenas authorization_code; depois, tracking passa a ser o código
// definitivo. Para Loggi, authorization_code não é rastreio e nunca é usado.
func resolveBackfillTracking(currentCode, company string, ev *me.ShipmentStatus) string {
	if ev == nil {
		return strings.TrimSpace(currentCode)
	}
	if tracking := strings.TrimSpace(ev.Tracking); tracking != "" {
		return tracking
	}
	if strings.Contains(strings.ToLower(company), "jadlog") {
		if authorization := strings.TrimSpace(ev.AuthorizationCode); authorization != "" {
			return authorization
		}
	}
	return strings.TrimSpace(currentCode)
}

// ProcessGeneratePDF é o handler Asynq para TypeGeneratePDF.
//
// Fluxo:
//  1. Deserializa payload (label_id, shipment_id).
//  2. Registra tentativa em wc_me_queue.
//  3. Chama ME.GenerateLabel para obter a URL da etiqueta.
//  4. Baixa o PDF da URL retornada.
//  5. Salva o PDF em PDFDir/{label_id}.pdf.
//  6. Atualiza wc_me_labels: label_url, label_pdf_path, status=released.
//  7. Marca wc_me_queue como processed_at = NOW().
func (w *LabelWorker) ProcessGeneratePDF(ctx context.Context, task *asynq.Task) error {
	var p GeneratePDFPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: payload inválido: %w", err)
	}

	slog.Info("[senderzz_labels] ProcessGeneratePDF iniciado",
		"label_id", p.LabelID,
		"shipment_id", p.ShipmentID,
	)

	// Registra tentativa em wc_me_queue para auditoria.
	if err := w.incrementQueueAttempt(ctx, p.LabelID, "generate_pdf"); err != nil {
		// Falha no registro não cancela o processamento — apenas loga.
		slog.Warn("[senderzz_labels] ProcessGeneratePDF: falha ao registrar tentativa",
			"label_id", p.LabelID, "err", err)
	}

	// Checkout ME: paga o envio antes de gerar. Sem isso ME retorna "Envio não está pago".
	//
	// AUDIT-2026-07-27: job idempotente por design (retry no Asynq), mas o checkout
	// NÃO é — 1ª tentativa pode pagar com sucesso e falhar só no passo seguinte
	// (GenerateLabel respondeu "encaminhado para geração", assíncrono do lado da
	// ME). O retry então re-tentava CheckoutShipment num envio JÁ PAGO → ME
	// recusa com 422 "Existe uma ou mais orders que já foram pagas ou inválidas" →
	// job trava pra sempre, PDF nunca sai (pedido #1633: emitiu, debitou, mas
	// ficava preso aqui). Trata essa mensagem específica como sucesso idempotente
	// (já pago = objetivo do checkout já alcançado) em vez de abortar.
	if err := w.ME.CheckoutShipment(ctx, p.ShipmentID); err != nil {
		if !strings.Contains(err.Error(), "foram pagas") {
			w.setQueueError(ctx, p.LabelID, "generate_pdf", err.Error())
			return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: CheckoutShipment: %w", err)
		}
		slog.Info("[senderzz_labels] ProcessGeneratePDF: shipment já pago (retry), seguindo pra GenerateLabel",
			"label_id", p.LabelID, "shipment_id", p.ShipmentID)
	}

	// Chama ME para gerar a URL da etiqueta. AUDIT-2026-07-27: geração é
	// assíncrona do lado da ME — retry pode achar "O envio ja esta gerado" (a 1ª
	// chamada já disparou e terminou nesse meio-tempo); nesse caso busca a URL
	// via PrintLabel em vez de insistir em GenerateLabel (que não retorna URL
	// pra shipment já gerado, só erro).
	labelURL, err := w.ME.GenerateLabel(ctx, p.ShipmentID)
	if err != nil {
		if strings.Contains(err.Error(), "envio ja") {
			printURL, printErr := w.ME.PrintLabel(ctx, p.ShipmentID)
			if printErr != nil {
				w.setQueueError(ctx, p.LabelID, "generate_pdf", printErr.Error())
				return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: PrintLabel (fallback): %w", printErr)
			}
			labelURL = printURL
		} else {
			w.setQueueError(ctx, p.LabelID, "generate_pdf", err.Error())
			return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: GenerateLabel: %w", err)
		}
	}

	// Baixa o PDF.
	pdfPath, err := w.downloadPDF(ctx, p.LabelID, labelURL)
	if err != nil {
		// Falha no download não cancela — armazena URL, PDF será baixado no retry.
		slog.Warn("[senderzz_labels] ProcessGeneratePDF: falha ao baixar PDF (armazena URL)",
			"label_id", p.LabelID, "label_url", labelURL, "err", err)
		pdfPath = ""
	}

	// Tenta obter o código de rastreio já nesta etapa (GAP-RASTREIO-2026-07-28):
	// a ME às vezes já atribui o tracking assim que a etiqueta é gerada/paga, sem
	// precisar esperar o sync de status. Best-effort — falha aqui NÃO aborta o
	// job (o PDF é o objetivo principal); tracking vazio só significa que a
	// transportadora ainda não postou, tenta de novo depois via sync periódico.
	trackingCode, trackErr := w.ME.GetShipmentTracking(ctx, p.ShipmentID)
	if trackErr != nil {
		slog.Warn("[senderzz_labels] ProcessGeneratePDF: falha ao consultar tracking (não bloqueia)",
			"label_id", p.LabelID, "shipment_id", p.ShipmentID, "err", trackErr)
	}

	// Atualiza a etiqueta no banco.
	// print_url = label_url: o admin usa print_url para o botão "Imprimir/PDF".
	// tracking_code: só sobrescreve se veio um valor novo (COALESCE mantém o que
	// já estava gravado se a ME não retornou nada desta vez).
	_, err = w.DB.Exec(ctx,
		`UPDATE wc_me_labels
		    SET label_url      = $1,
		        print_url      = $1,
		        label_pdf_path = $2,
		        tracking_code  = COALESCE(NULLIF($4, ''), tracking_code),
		        status         = 'released',
		        updated_at     = NOW()
		  WHERE id = $3`,
		labelURL, nilIfEmpty(pdfPath), p.LabelID, trackingCode,
	)
	if err != nil {
		w.setQueueError(ctx, p.LabelID, "generate_pdf", err.Error())
		return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: UPDATE wc_me_labels: %w", err)
	}

	// Marca job como concluído na fila durável.
	w.markQueueProcessed(ctx, p.LabelID, "generate_pdf")

	slog.Info("[senderzz_labels] ProcessGeneratePDF concluído",
		"label_id", p.LabelID,
		"label_url", labelURL,
		"pdf_path", pdfPath,
	)
	return nil
}

// ProcessSyncTracking é o handler Asynq para TypeSyncTracking.
//
// Fluxo:
//  1. Deserializa payload (label_id, tracking_code).
//  2. Registra tentativa em wc_me_queue.
//  3. Chama ME.TrackShipment para obter o status atual.
//  4. Mapeia status ME → status interno (ver mapeamento abaixo).
//  5. Atualiza wc_me_labels.status se mudou.
func (w *LabelWorker) ProcessSyncTracking(ctx context.Context, task *asynq.Task) error {
	var p SyncTrackingPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessSyncTracking: payload inválido: %w", err)
	}

	slog.Info("[senderzz_labels] ProcessSyncTracking iniciado",
		"label_id", p.LabelID,
		"tracking_code", p.TrackingCode,
	)

	if err := w.incrementQueueAttempt(ctx, p.LabelID, "sync_tracking"); err != nil {
		slog.Warn("[senderzz_labels] ProcessSyncTracking: falha ao registrar tentativa",
			"label_id", p.LabelID, "err", err)
	}

	// AUDIT-2026-07-30 #9 (dono: "ARRUMA" — job quebrava com "invalid character
	// '<'"): TrackShipment espera o me_shipment_id (UUID interno da ME), não o
	// tracking_code da transportadora que o payload carrega — o path antigo
	// (GET com tracking_code) nunca existiu de verdade, devolvia HTML.
	var shipmentID string
	if err := w.DB.QueryRow(ctx,
		`SELECT me_shipment_id FROM wc_me_labels WHERE id = $1`, p.LabelID,
	).Scan(&shipmentID); err != nil || shipmentID == "" {
		w.setQueueError(ctx, p.LabelID, "sync_tracking", "etiqueta sem me_shipment_id")
		return fmt.Errorf("[senderzz_labels] ProcessSyncTracking: me_shipment_id ausente pra label %d", p.LabelID)
	}

	meStatus, err := w.ME.TrackShipment(ctx, shipmentID)
	if err != nil {
		w.setQueueError(ctx, p.LabelID, "sync_tracking", err.Error())
		return fmt.Errorf("[senderzz_labels] ProcessSyncTracking: TrackShipment: %w", err)
	}

	// Mapeia status ME → status canônico em wc_me_labels.
	// CHECK (status IN ('draft','released','posted','delivered','canceled','lost'))
	internalStatus := mapMEStatus(meStatus)

	_, err = w.DB.Exec(ctx,
		`UPDATE wc_me_labels
		    SET status     = $1,
		        updated_at = NOW()
		  WHERE id = $2
		    AND status != $1`,
		// Só atualiza se mudou — evita writes desnecessários.
		internalStatus, p.LabelID,
	)
	if err != nil {
		w.setQueueError(ctx, p.LabelID, "sync_tracking", err.Error())
		return fmt.Errorf("[senderzz_labels] ProcessSyncTracking: UPDATE wc_me_labels: %w", err)
	}

	w.markQueueProcessed(ctx, p.LabelID, "sync_tracking")

	// GAP-RASTREIO-2026-07-16: até aqui só wc_me_labels.status mudava — o pedido em
	// sz_orders.status ficava parado pra sempre (rastreio "delivered" nunca refletia
	// no pedido nem disparava o outbox de webhook, que é keyed em sz_orders). Ao
	// confirmar entrega pela ME, espelha em sz_orders.status='entregue' (dispara a
	// trigger trg_sz_webhook_outbox). Não sobrescreve status terminais divergentes
	// (cancelled/frustrado/reembolsado) — a etiqueta pode ter sido entregue após o
	// pedido já ter sido cancelado/estornado por outro motivo.
	//
	// AUDIT-2026-07-31 (dono, pedido #1665): JOIN usava só o.wp_order_id — pedido
	// nativo Go (sem WordPress, wp_order_id NULL) NUNCA batia, então o rastreio
	// da ME avançava em wc_me_labels mas sz_orders (e a página pública de rastreio,
	// que lê sz_orders) ficavam parados pra sempre em qualquer pedido nativo com
	// etiqueta ME. COALESCE(o.wp_order_id, o.id) — mesmo padrão usado em todo
	// resto do sistema (checkout.go, sz_financials_refresh, etc).
	if internalStatus == "delivered" {
		if _, err := w.DB.Exec(ctx,
			`WITH previous AS (
				SELECT o.id, o.status
				  FROM sz_orders o
				  JOIN wc_me_labels l ON COALESCE(o.wp_order_id, o.id) = l.wc_order_id
				 WHERE l.id = $1
				   AND o.status NOT IN ('entregue', 'cancelled', 'frustrado', 'reembolsado', 'completo')
			), changed AS (
				UPDATE sz_orders o
				   SET status = 'entregue', updated_at = NOW()
				  FROM previous p
				 WHERE o.id = p.id
				 RETURNING o.id
			)
			INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo)
			SELECT p.id, p.status, 'entregue', 'webhook'
			  FROM previous p JOIN changed c ON c.id = p.id`,
			p.LabelID,
		); err != nil {
			// Não falha o job por isso — wc_me_labels já refletiu o status real;
			// perder o espelho em sz_orders é degradação, não motivo de retry infinito.
			slog.Error("[senderzz_labels] ProcessSyncTracking: falha ao espelhar status em sz_orders",
				"label_id", p.LabelID, "err", err)
		}
	}
	// AUDIT-2026-07-28: pedido dono — "demais etapas configuradas automaticamente
	// pelo rastreio via API da ME". "posted" cobre posted/in_transit/out_for_delivery/
	// waiting_pickup (mapMEStatus) — a transportadora já retirou/está levando. Espelha
	// sz_orders.status='enviado' só a partir de 'embalado' (produtor/admin já separou
	// e imprimiu — MarkPacked). Pedido ainda não separado não pula pra "enviado" só
	// porque a ME mudou de ideia sobre o rótulo; e não regride status mais avançado
	// (entregue/terminal) se a ME reportar um evento intermediário atrasado.
	if internalStatus == "posted" {
		// AUDIT-2026-07-31 (dono): 'enviado' sobrepõe 'coletado' — origem aceita
		// embalado OU coletado (operador logístico pode ter marcado coletado
		// manualmente antes da ME confirmar postagem).
		if _, err := w.DB.Exec(ctx,
			`WITH previous AS (
				SELECT o.id, o.status
				  FROM sz_orders o
				  JOIN wc_me_labels l ON COALESCE(o.wp_order_id, o.id) = l.wc_order_id
				 WHERE l.id = $1
				   AND o.status IN ('embalado', 'coletado')
			), changed AS (
				UPDATE sz_orders o
				   SET status = 'enviado', updated_at = NOW()
				  FROM previous p
				 WHERE o.id = p.id
				 RETURNING o.id
			)
			INSERT INTO sz_order_status_history (order_id, status_de, status_para, actor_tipo)
			SELECT p.id, p.status, 'enviado', 'webhook'
			  FROM previous p JOIN changed c ON c.id = p.id`,
			p.LabelID,
		); err != nil {
			slog.Error("[senderzz_labels] ProcessSyncTracking: falha ao espelhar 'enviado' em sz_orders",
				"label_id", p.LabelID, "err", err)
		}
	}

	// Best-effort: uma falha no agregador não pode impedir a sincronização
	// oficial da etiqueta nem gerar retry infinito do job principal.
	w.syncCarrierTracking(ctx, p.TrackingCode)

	slog.Info("[senderzz_labels] ProcessSyncTracking concluído",
		"label_id", p.LabelID,
		"tracking_code", p.TrackingCode,
		"me_status", meStatus,
		"interno_status", internalStatus,
	)
	return nil
}

// ProcessBackfillTracking busca o tracking_code de uma etiqueta que ainda não
// tem (por me_shipment_id — não precisa do código pra achar o código).
// AUDIT-2026-07-28: cobre o caso em que a ME demora a atribuir o código (doc
// oficial: "pode levar até 1 dia útil") — sem isso, o único tiro era em
// ProcessGeneratePDF (logo após emitir), e se desse vazio ficava NULL pra
// sempre. Idempotente: só grava se vier código e a etiqueta ainda estiver
// sem um.
//
// AUDIT-2026-07-31 (dono, bravo, com razão — já tínhamos achado isso antes em
// me_status_sync.go e eu não apliquei aqui): "tracking" e "authorization_code"
// NÃO são intercambiáveis por transportadora (AUDIT-2026-07-30 #12, dado cru
// confirmado). Loggi: tracking=código real, authorization_code=outra coisa.
// Jadlog: tracking="" até a coleta física, authorization_code=código real
// (confirmado pelo dono, pedido #1665). GetShipmentTracking só devolve
// "tracking" puro — pra Jadlog ficava vazio pra sempre e o backfill nunca
// preenchia (pedidos #1633/#1635). Troca pra GetShipmentStatus (mesmo dado
// que InternalMEStatusEvents já usa) com o mesmo fallback: tracking primeiro,
// authorization_code se vier vazio.
func (w *LabelWorker) ProcessBackfillTracking(ctx context.Context, task *asynq.Task) error {
	var p BackfillTrackingPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessBackfillTracking: payload inválido: %w", err)
	}

	ev, err := w.ME.GetShipmentStatus(ctx, p.ShipmentID)
	if err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessBackfillTracking: GetShipmentStatus: %w", err)
	}
	var currentCode, company string
	var orderID int64
	if err := w.DB.QueryRow(ctx,
		`SELECT wc_order_id, COALESCE(tracking_code, ''), COALESCE(company_name, '')
		   FROM wc_me_labels WHERE id = $1`, p.LabelID,
	).Scan(&orderID, &currentCode, &company); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessBackfillTracking: ler etiqueta: %w", err)
	}
	trackingCode := resolveBackfillTracking(currentCode, company, ev)
	if trackingCode == "" {
		// ME ainda não atribuiu (nem tracking nem authorization_code) — não é
		// erro, só tenta de novo no próximo poll.
		slog.Info("[senderzz_labels] ProcessBackfillTracking: ME ainda sem tracking", "label_id", p.LabelID)
		return nil
	}
	if err := trackinghistory.Save(ctx, w.DB, trackinghistory.Snapshot{
		ShipmentID: p.ShipmentID, OrderID: orderID, Status: ev.Status,
		Tracking: ev.Tracking, AuthorizationCode: ev.AuthorizationCode,
		CreatedAt: ev.CreatedAt, PaidAt: ev.PaidAt, GeneratedAt: ev.GeneratedAt,
		PostedAt: ev.PostedAt, ReceivedAt: ev.ReceivedAt,
		DeliveredAt: ev.DeliveredAt, CanceledAt: ev.CanceledAt,
	}); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessBackfillTracking: gravar histórico: %w", err)
	}

	if _, err := w.DB.Exec(ctx,
		`UPDATE wc_me_labels
		    SET tracking_code = $1, updated_at = NOW()
		  WHERE id = $2 AND tracking_code IS DISTINCT FROM $1`,
		trackingCode, p.LabelID,
	); err != nil {
		return fmt.Errorf("[senderzz_labels] ProcessBackfillTracking: UPDATE wc_me_labels: %w", err)
	}
	slog.Info("[senderzz_labels] ProcessBackfillTracking: tracking_code preenchido",
		"label_id", p.LabelID, "tracking_code", trackingCode)
	return nil
}

// ─── helpers internos dos workers ────────────────────────────────────────────

// downloadPDF baixa o PDF da URL fornecida e salva em PDFDir/{labelID}.pdf.
// Retorna o caminho local do arquivo salvo.
func (w *LabelWorker) downloadPDF(ctx context.Context, labelID int64, labelURL string) (string, error) {
	// AUDIT-2026-06-21 #22: SSRF guard. labelURL vem da ME (GenerateLabel) — uma ME
	// comprometida ou um 30x da CDN p/ host interno poderia fazer este sink baixar de
	// http://169.254.169.254 ou de um IP privado e gravar/servir o resultado. Antes da
	// conexão, exigimos https e destino público (validatePublicHTTPSURL); o client
	// endurecido (newSSRFSafeClient) revalida cada redirect e rejeita IP interno no
	// momento do Dial (anti-DNS-rebinding). Não importamos cross-módulo — helpers
	// espelhados de go/cron/internal/dispatch/webhook.go (convenção da task).
	if err := validatePublicHTTPSURL(labelURL); err != nil {
		return "", fmt.Errorf("url da etiqueta rejeitada (SSRF): %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, labelURL, nil)
	if err != nil {
		return "", fmt.Errorf("criar requisição download: %w", err)
	}

	httpClient := newSSRFSafeClient(30 * time.Second)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("baixar PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download PDF retornou status %d", resp.StatusCode)
	}

	pdfPath := filepath.Join(w.PDFDir, fmt.Sprintf("%d.pdf", labelID))

	// Garante que o diretório existe.
	if err := os.MkdirAll(w.PDFDir, 0o755); err != nil {
		return "", fmt.Errorf("criar diretório PDF: %w", err)
	}

	f, err := os.Create(pdfPath)
	if err != nil {
		return "", fmt.Errorf("criar arquivo PDF: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("escrever PDF: %w", err)
	}

	return pdfPath, nil
}

// incrementQueueAttempt incrementa a contagem de tentativas na linha mais recente
// pendente de wc_me_queue para o label_id e action fornecidos.
//
// O worker não insere uma nova linha — o handler POST /labels já inseriu a linha
// em wc_me_queue antes de enfileirar no Asynq. O worker apenas incrementa attempts.
//
// Atenção: Postgres não suporta ORDER BY / LIMIT diretamente em UPDATE.
// Usamos subquery com pk para selecionar a linha mais recente pendente.
func (w *LabelWorker) incrementQueueAttempt(ctx context.Context, labelID int64, action string) error {
	_, err := w.DB.Exec(ctx,
		`UPDATE wc_me_queue
		    SET attempts = attempts + 1
		  WHERE id = (
		      SELECT id
		        FROM wc_me_queue
		       WHERE label_id = $1
		         AND action   = $2
		         AND processed_at IS NULL
		       ORDER BY id DESC
		       LIMIT 1
		  )`,
		labelID, action,
	)
	return err
}

// setQueueError armazena o último erro na linha mais recente pendente em wc_me_queue.
// Postgres não suporta ORDER BY / LIMIT em UPDATE — usa subquery por pk.
func (w *LabelWorker) setQueueError(ctx context.Context, labelID int64, action, errMsg string) {
	_, err := w.DB.Exec(ctx,
		`UPDATE wc_me_queue
		    SET last_error = $1,
		        attempts   = attempts + 1
		  WHERE id = (
		      SELECT id
		        FROM wc_me_queue
		       WHERE label_id = $2
		         AND action   = $3
		         AND processed_at IS NULL
		       ORDER BY id DESC
		       LIMIT 1
		  )`,
		errMsg, labelID, action,
	)
	if err != nil {
		slog.Warn("[senderzz_labels] setQueueError: falha ao registrar erro",
			"label_id", labelID, "action", action, "err", err)
	}
}

// markQueueProcessed marca o job como finalizado com sucesso (processed_at = NOW()).
// Postgres não suporta ORDER BY / LIMIT em UPDATE — usa subquery por pk.
func (w *LabelWorker) markQueueProcessed(ctx context.Context, labelID int64, action string) {
	_, err := w.DB.Exec(ctx,
		`UPDATE wc_me_queue
		    SET processed_at = NOW()
		  WHERE id = (
		      SELECT id
		        FROM wc_me_queue
		       WHERE label_id = $1
		         AND action   = $2
		         AND processed_at IS NULL
		       ORDER BY id DESC
		       LIMIT 1
		  )`,
		labelID, action,
	)
	if err != nil {
		slog.Warn("[senderzz_labels] markQueueProcessed: falha ao marcar job",
			"label_id", labelID, "action", action, "err", err)
	}
}

// mapMEStatus mapeia o status retornado pela ME API para o status canônico interno.
// A ME usa strings descritivas; mapeamos para o CHECK da tabela wc_me_labels.
// Status não reconhecidos são mantidos como "posted" (enviado, aguardando atualização).
// AUDIT-2026-07-30 #9 (dono: "ARRUMA" — mesmo bug já corrigido em
// handlers.mapTrackingStatus, esquecido aqui por ser função duplicada):
// fallback antigo "default: posted" gravava wc_me_labels.status='posted' com
// a ME real ainda em 'released' (etiqueta paga, SEM despacho confirmado).
// Lista completa oficial de status ME (docs.melhorenvio.com.br/reference/
// webhooks): created, pending, released, generated, received, posted,
// delivered, cancelled, undelivered, paused, suspended.
func mapMEStatus(meStatus string) string {
	switch meStatus {
	case "received", "posted", "in_transit", "out_for_delivery":
		return "posted"
	case "delivered":
		return "delivered"
	case "canceled", "cancelled", "returned", "undelivered":
		return "canceled"
	case "lost":
		return "lost"
	// created/pending/released/generated/waiting_pickup/paused/suspended —
	// etiqueta existe mas SEM despacho confirmado.
	default:
		return "released"
	}
}

// nilIfEmpty retorna nil se s é string vazia, ou &s caso contrário.
// Útil para campos opcionais em queries pgx que aceitam *string.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ─── SSRF guard (AUDIT-2026-06-21 #22) ────────────────────────────────────────
//
// Helpers espelhados de go/cron/internal/dispatch/webhook.go::{isBlockedIP,
// validatePublicURL,ssrfSafeControl,newSSRFSafeClient} — copiados de propósito (não
// importamos cross-módulo). DIFERENÇA INTENCIONAL: validatePublicHTTPSURL exige
// scheme HTTPS (a task pede "exigir https"); a versão do dispatch aceitava http+https.

var errBlockedInternalDest = errors.New("destino interno/privado bloqueado (SSRF)")

// isBlockedIP — true quando o IP pertence a faixa que NUNCA deve ser alvo de
// download externo (loopback, privado, link-local, metadata, CGNAT RFC 6598,
// não-especificado, multicast). Cobre IPv4 e IPv6. nil → fail-closed.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true // não resolveu → fail-closed
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	// 100.64.0.0/10 — CGNAT (RFC 6598), não coberto por IsPrivate().
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// validatePublicHTTPSURL exige scheme HTTPS e RESOLVE o host, rejeitando destino
// interno/privado ANTES de qualquer request. Host que resolve p/ múltiplos IPs é
// rejeitado se QUALQUER IP for bloqueado (evita rebinding parcial).
func validatePublicHTTPSURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("url vazia")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url inválida: %v", err)
	}
	// AUDIT-2026-06-21 #22: HTTPS-only (mais restrito que o helper do dispatch).
	if u.Scheme != "https" {
		return fmt.Errorf("url deve usar https://")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url sem host")
	}

	// Host já é IP literal? Valida direto, sem DNS.
	if lit := net.ParseIP(host); lit != nil {
		if isBlockedIP(lit) {
			return fmt.Errorf("destino interno/privado bloqueado: %s", host)
		}
		return nil
	}

	resCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(resCtx, host)
	if err != nil {
		return fmt.Errorf("não foi possível resolver o host %q: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q não resolveu para nenhum IP", host)
	}
	for _, ipa := range ips {
		if isBlockedIP(ipa.IP) {
			return fmt.Errorf("destino interno/privado bloqueado: %s (%s)", host, ipa.IP)
		}
	}
	return nil
}

// ssrfSafeControl é o hook Control de net.Dialer: roda APÓS o resolve real do
// dialer (endereço já em ip:port). Rejeita a conexão se o IP for interno/privado —
// fecha a janela de DNS-rebinding que a validação de URL sozinha não cobre.
func ssrfSafeControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlockedInternalDest // formato inesperado → fail-closed
	}
	if isBlockedIP(net.ParseIP(host)) {
		return errBlockedInternalDest
	}
	return nil
}

// newSSRFSafeClient devolve um *http.Client endurecido contra SSRF:
//   - DialContext.Control bloqueia IP interno NO MOMENTO da conexão (anti-rebinding).
//   - CheckRedirect revalida cada salto com validatePublicHTTPSURL (anti-redirect-SSRF,
//     também rebaixa http→nega: a ME serve PDFs por https).
func newSSRFSafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   ssrfSafeControl,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if err := validatePublicHTTPSURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect bloqueado: %w", err)
			}
			return nil
		},
	}
}
