// Package doublewrite implementa o padrão de escrita dupla durante migração.
// Durante a Fase 1 (strangler fig), escritas vão para MySQL (WP) E Postgres (Go).
// Após cutover confirmado, remover as escritas MySQL.
package doublewrite

import (
	"context"
	"log/slog"
)

// Writer representa um destino de escrita (MySQL legacy ou Postgres novo).
type Writer interface {
	Write(ctx context.Context, table string, data map[string]any) error
}

// ReadWriter é um Writer que também suporta leitura para reconciliação.
type ReadWriter interface {
	Writer
	Count(ctx context.Context, table string) (int64, error)
	LastID(ctx context.Context, table string) (int64, error)
}

// DualWriter escreve em dois destinos e loga divergências.
// primary = novo (Postgres), secondary = legado (MySQL via WP)
// Se secondary falhar, loga mas não retorna erro (não bloqueia migração).
type DualWriter struct {
	primary   Writer
	secondary Writer
	logger    *slog.Logger
}

// NewDualWriter cria um DualWriter.
func NewDualWriter(primary, secondary Writer, logger *slog.Logger) *DualWriter {
	return &DualWriter{primary: primary, secondary: secondary, logger: logger}
}

// Write escreve em primary primeiro. Se secondary falhar, loga e continua.
func (d *DualWriter) Write(ctx context.Context, table string, data map[string]any) error {
	if err := d.primary.Write(ctx, table, data); err != nil {
		return err // falha no primary é fatal
	}
	if err := d.secondary.Write(ctx, table, data); err != nil {
		d.logger.Error("double-write secondary failed",
			"table", table,
			"error", err,
		)
		// Não retorna erro — secondary (WP/MySQL) é legado durante transição
	}
	return nil
}

// ReconcileResult representa o resultado da reconciliação de uma tabela.
type ReconcileResult struct {
	Table          string
	PrimaryCount   int64
	SecondaryCount int64
	Divergence     int64
	OK             bool
}

// Reconciler verifica divergências entre primary e secondary.
type Reconciler struct {
	primary   ReadWriter
	secondary ReadWriter
	logger    *slog.Logger
}

// NewReconciler cria um Reconciler.
func NewReconciler(primary, secondary ReadWriter, logger *slog.Logger) *Reconciler {
	return &Reconciler{primary: primary, secondary: secondary, logger: logger}
}

// Reconcile compara contagens primary vs secondary para cada tabela.
func (r *Reconciler) Reconcile(ctx context.Context, tables []string) []ReconcileResult {
	results := make([]ReconcileResult, 0, len(tables))
	for _, table := range tables {
		pc, err1 := r.primary.Count(ctx, table)
		sc, err2 := r.secondary.Count(ctx, table)
		if err1 != nil || err2 != nil {
			r.logger.Error("reconcile count error", "table", table, "err1", err1, "err2", err2)
			continue
		}
		div := pc - sc
		if div < 0 {
			div = -div
		}
		ok := div == 0
		if !ok {
			r.logger.Warn("reconcile divergence",
				"table", table,
				"primary", pc,
				"secondary", sc,
				"divergence", div,
			)
		}
		results = append(results, ReconcileResult{
			Table:          table,
			PrimaryCount:   pc,
			SecondaryCount: sc,
			Divergence:     div,
			OK:             ok,
		})
	}
	return results
}

// MotoboyCutoverTables são as tabelas do módulo motoboy a reconciliar.
var MotoboyCutoverTables = []string{
	"sz_motoboy_pedidos",
	"sz_motoboys",
	"sz_motoboy_cds",
	"sz_motoboy_zonas",
	"sz_motoboy_cep_zonas",
	"sz_motoboy_zona_pivot",
	"sz_motoboy_comprovantes",
	"sz_motoboy_fechamento",
	"sz_motoboy_audit",
}
