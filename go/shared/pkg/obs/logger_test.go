package obs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// NewLogger emite JSON estruturado com o atributo "service" e o nível configurado,
// capturado num buffer (função pura, sem efeito global).
func TestNewLogger_JSONShape(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LoggerOptions{
		Level:   slog.LevelInfo,
		Writer:  &buf,
		Service: "labels",
	})

	logger.Info("pedido criado", "order_id", 42)

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("nenhuma linha de log emitida")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("log não é JSON válido: %v\nlinha: %s", err, line)
	}
	if rec["msg"] != "pedido criado" {
		t.Fatalf("msg=%v; esperava 'pedido criado'", rec["msg"])
	}
	if rec["level"] != "INFO" {
		t.Fatalf("level=%v; esperava 'INFO'", rec["level"])
	}
	if rec["service"] != "labels" {
		t.Fatalf("service=%v; esperava 'labels'", rec["service"])
	}
	// order_id vira float64 no JSON genérico.
	if id, _ := rec["order_id"].(float64); id != 42 {
		t.Fatalf("order_id=%v; esperava 42", rec["order_id"])
	}
}

// O nível mínimo é respeitado: Debug não sai quando o nível é Info.
func TestNewLogger_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(LoggerOptions{Level: slog.LevelInfo, Writer: &buf})

	logger.Debug("invisível")
	if buf.Len() != 0 {
		t.Fatalf("Debug emitido sob nível Info: %s", buf.String())
	}

	logger.Warn("visível")
	if !strings.Contains(buf.String(), "visível") {
		t.Fatalf("Warn não emitido sob nível Info: %s", buf.String())
	}
}

// SetupDefault instala o logger como slog.Default e o devolve.
func TestSetupDefault_InstallsDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	logger := SetupDefault(LoggerOptions{Service: "obs-test"})
	if logger == nil {
		t.Fatal("SetupDefault devolveu nil")
	}
	if slog.Default() != logger {
		t.Fatal("slog.Default não foi substituído por SetupDefault")
	}
}
