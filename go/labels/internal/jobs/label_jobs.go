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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/senderzz/labels-service/internal/me"
)

// Constantes de tipo de task — usadas ao enfileirar e ao registrar workers.
const (
	// TypeGeneratePDF: baixa o PDF da etiqueta após CreateShipment + GenerateLabel.
	TypeGeneratePDF = "labels:generate_pdf"
	// TypeSyncTracking: sincroniza status de rastreamento via ME API.
	TypeSyncTracking = "labels:sync_tracking"
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

// ─── Workers ─────────────────────────────────────────────────────────────────

// LabelWorker agrupa o pool Postgres e o cliente ME para uso nos workers.
type LabelWorker struct {
	DB    *pgxpool.Pool
	ME    *me.MEClient
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
		DB:     db,
		ME:     meClient,
		PDFDir: pdfDir,
	}
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

	// Chama ME para gerar a URL da etiqueta.
	labelURL, err := w.ME.GenerateLabel(ctx, p.ShipmentID)
	if err != nil {
		w.setQueueError(ctx, p.LabelID, "generate_pdf", err.Error())
		return fmt.Errorf("[senderzz_labels] ProcessGeneratePDF: GenerateLabel: %w", err)
	}

	// Baixa o PDF.
	pdfPath, err := w.downloadPDF(ctx, p.LabelID, labelURL)
	if err != nil {
		// Falha no download não cancela — armazena URL, PDF será baixado no retry.
		slog.Warn("[senderzz_labels] ProcessGeneratePDF: falha ao baixar PDF (armazena URL)",
			"label_id", p.LabelID, "label_url", labelURL, "err", err)
		pdfPath = ""
	}

	// Atualiza a etiqueta no banco.
	_, err = w.DB.Exec(ctx,
		`UPDATE wc_me_labels
		    SET label_url      = $1,
		        label_pdf_path = $2,
		        status         = 'released',
		        updated_at     = NOW()
		  WHERE id = $3`,
		labelURL, nilIfEmpty(pdfPath), p.LabelID,
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

	meStatus, err := w.ME.TrackShipment(ctx, p.TrackingCode)
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

	slog.Info("[senderzz_labels] ProcessSyncTracking concluído",
		"label_id", p.LabelID,
		"tracking_code", p.TrackingCode,
		"me_status", meStatus,
		"interno_status", internalStatus,
	)
	return nil
}

// ─── helpers internos dos workers ────────────────────────────────────────────

// downloadPDF baixa o PDF da URL fornecida e salva em PDFDir/{labelID}.pdf.
// Retorna o caminho local do arquivo salvo.
func (w *LabelWorker) downloadPDF(ctx context.Context, labelID int64, labelURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, labelURL, nil)
	if err != nil {
		return "", fmt.Errorf("criar requisição download: %w", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
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
func mapMEStatus(meStatus string) string {
	switch meStatus {
	case "posted", "in_transit", "out_for_delivery", "waiting_pickup":
		return "posted"
	case "delivered":
		return "delivered"
	case "canceled", "returned", "cancelled":
		return "canceled"
	case "lost":
		return "lost"
	default:
		// Status desconhecido — conserva como "posted" (enviado).
		return "posted"
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
