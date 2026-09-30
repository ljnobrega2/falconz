// Package trackinghistory persiste snapshots idempotentes do rastreio ME.
package trackinghistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Snapshot é o estado retornado pela Melhor Envio em uma consulta/evento.
type Snapshot struct {
	ShipmentID        string
	OrderID           int64
	Status            string
	Tracking          string
	AuthorizationCode string
	CreatedAt         string
	PaidAt            string
	GeneratedAt       string
	PostedAt          string
	ReceivedAt        string
	DeliveredAt       string
	CanceledAt        string
}

// Save grava cada estado novo uma única vez. Reconsultas idênticas são no-op.
func Save(ctx context.Context, db querier, s Snapshot) error {
	if s.ShipmentID == "" {
		return nil
	}
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		s.Status, s.Tracking, s.AuthorizationCode, s.CreatedAt, s.PaidAt,
		s.GeneratedAt, s.PostedAt, s.ReceivedAt, s.DeliveredAt, s.CanceledAt,
		fmt.Sprint(s.OrderID))
	h := sha256.Sum256([]byte(key))
	_, err := db.Exec(ctx, `
		INSERT INTO wc_me_tracking_history
			(me_shipment_id, wc_order_id, me_status, tracking_code, authorization_code,
			 created_at_me, paid_at_me, generated_at_me, posted_at_me, received_at_me,
			 delivered_at_me, canceled_at_me, snapshot_hash)
		VALUES ($1, NULLIF($2, 0), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (me_shipment_id, snapshot_hash) DO NOTHING`,
		s.ShipmentID, s.OrderID, s.Status, s.Tracking, s.AuthorizationCode,
		s.CreatedAt, s.PaidAt, s.GeneratedAt, s.PostedAt, s.ReceivedAt,
		s.DeliveredAt, s.CanceledAt, hex.EncodeToString(h[:]),
	)
	return err
}
