// SEC-LABELS: testes do contrato de PAYLOAD das tasks Asynq (fila de etiquetas).
//
// O enfileiramento real (EnqueueGeneratePDF/EnqueueSyncTracking) exige Redis e fica
// fora do escopo "sem rede" do projeto. O que É puro/testável — e o que de fato
// importa para a fila não corromper jobs — é o round-trip de serialização do payload:
// o que ProcessGeneratePDF/ProcessSyncTracking desserializam DEVE ser exatamente o
// que foi serializado no enqueue. Se um campo JSON tag divergir, o worker processaria
// label_id/shipment_id/tracking_code zerados — bug silencioso. Estes testes travam
// esse contrato.
package jobs

import (
	"encoding/json"
	"testing"
)

// TestGeneratePDFPayloadRoundTrip: serializar→desserializar preserva os campos.
// Espelha o caminho EnqueueGeneratePDF (marshal) → ProcessGeneratePDF (unmarshal).
func TestGeneratePDFPayloadRoundTrip(t *testing.T) {
	in := GeneratePDFPayload{LabelID: 42, ShipmentID: "ship-abc-123"}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal GeneratePDFPayload: %v", err)
	}

	var out GeneratePDFPayload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal GeneratePDFPayload: %v", err)
	}
	if out.LabelID != in.LabelID {
		t.Errorf("LabelID: esperava %d, obteve %d", in.LabelID, out.LabelID)
	}
	if out.ShipmentID != in.ShipmentID {
		t.Errorf("ShipmentID: esperava %q, obteve %q", in.ShipmentID, out.ShipmentID)
	}

	// As JSON tags são o contrato do payload na fila — se mudarem, o worker quebra.
	if !json.Valid(raw) {
		t.Fatal("payload serializado não é JSON válido")
	}
	var asMap map[string]any
	_ = json.Unmarshal(raw, &asMap)
	if _, ok := asMap["label_id"]; !ok {
		t.Errorf("payload deveria conter a chave 'label_id', obteve %s", raw)
	}
	if _, ok := asMap["shipment_id"]; !ok {
		t.Errorf("payload deveria conter a chave 'shipment_id', obteve %s", raw)
	}
}

// TestSyncTrackingPayloadRoundTrip: idem para o payload de rastreamento.
func TestSyncTrackingPayloadRoundTrip(t *testing.T) {
	in := SyncTrackingPayload{LabelID: 7, TrackingCode: "BR123456789BR"}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal SyncTrackingPayload: %v", err)
	}

	var out SyncTrackingPayload
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal SyncTrackingPayload: %v", err)
	}
	if out.LabelID != in.LabelID {
		t.Errorf("LabelID: esperava %d, obteve %d", in.LabelID, out.LabelID)
	}
	if out.TrackingCode != in.TrackingCode {
		t.Errorf("TrackingCode: esperava %q, obteve %q", in.TrackingCode, out.TrackingCode)
	}

	var asMap map[string]any
	_ = json.Unmarshal(raw, &asMap)
	if _, ok := asMap["tracking_code"]; !ok {
		t.Errorf("payload deveria conter a chave 'tracking_code', obteve %s", raw)
	}
}

// TestPayloadUnmarshalLixoNaoPanica: payload corrompido na fila → erro, nunca panic.
// ProcessGeneratePDF/ProcessSyncTracking retornam erro nesse caso (job vai a retry),
// e o recover do worker (ErrorHandler) registra — mas o unmarshal em si só erra.
func TestPayloadUnmarshalLixoNaoPanica(t *testing.T) {
	var p1 GeneratePDFPayload
	if err := json.Unmarshal([]byte(`{lixo`), &p1); err == nil {
		t.Error("GeneratePDFPayload: payload corrompido deveria gerar erro de unmarshal")
	}
	var p2 SyncTrackingPayload
	if err := json.Unmarshal([]byte(`not-json`), &p2); err == nil {
		t.Error("SyncTrackingPayload: payload corrompido deveria gerar erro de unmarshal")
	}
}
