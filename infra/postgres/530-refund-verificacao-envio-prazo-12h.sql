-- AUDIT-2026-07-31 (dono) — reembolso de pedido era 100% stub: admin pedia
-- /orders/{id}/refund e o sistema marcava payment_status='refunded' na hora,
-- incondicionalmente, sem checar se a remessa (Melhor Envio) foi realmente
-- cancelada/devolvida. Regra pedida: verificar envio por envio; quando
-- confirmado reembolsado de verdade (ME confirma cancelamento da remessa),
-- reembolsa; se não for possível confirmar, prazo fixo de 12h (mesmo padrão
-- já usado no estorno de etiqueta pro produtor — ver estorno_reconcile.go,
-- que usa 24h de folga sobre os 12h documentados pela própria ME; aqui o
-- pedido do dono foi 12h direto).
--
-- wc_me_labels.status já é sincronizado em tempo real pelo webhook nativo da
-- ME (me_webhook.go) — não precisa de nova chamada de API, só consultar a
-- coluna. Pedidos sem etiqueta ME (motoboy/COD) nunca têm como verificar →
-- sempre caem no prazo de 12h.

CREATE TABLE IF NOT EXISTS sz_order_refund_requests (
    order_id         BIGINT        NOT NULL PRIMARY KEY REFERENCES sz_orders(id),
    requested_by     BIGINT        NULL,
    motivo           TEXT          NULL,
    requested_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deadline_at      TIMESTAMPTZ   NOT NULL,
    status           VARCHAR(20)   NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'settled')),
    verification_note VARCHAR(40)  NULL, -- 'me_confirmado' | 'prazo_12h_vencido'
    settled_at       TIMESTAMPTZ   NULL
);

CREATE INDEX IF NOT EXISTS idx_refund_req_pending
    ON sz_order_refund_requests (deadline_at) WHERE status = 'pending';

-- sz_settle_refund_request: liquida UM pedido de reembolso (idempotente —
-- WHERE status='pending' evita liquidar 2x). Mesma mutação que já existia
-- inline em PostOrderRefund (payment_status + sz_order_payments), só que
-- agora GATED por verificação ao invés de incondicional.
CREATE OR REPLACE FUNCTION sz_settle_refund_request(p_order_id BIGINT, p_note VARCHAR(40))
RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE
    v_updated INT;
BEGIN
    UPDATE sz_order_refund_requests
       SET status = 'settled', settled_at = NOW(), verification_note = p_note
     WHERE order_id = p_order_id AND status = 'pending';
    GET DIAGNOSTICS v_updated = ROW_COUNT;
    IF v_updated = 0 THEN
        RETURN FALSE;
    END IF;

    UPDATE sz_orders SET payment_status = 'refunded', updated_at = NOW() WHERE id = p_order_id;
    UPDATE sz_order_payments SET status = 'refunded' WHERE order_id = p_order_id AND status = 'paid';
    RETURN TRUE;
END; $$;

-- sz_process_refund_requests_due: varre pedidos de reembolso pendentes.
-- (a) se a etiqueta ATIVA (mais recente) do pedido já foi confirmada
--     cancelada pela ME (wc_me_labels.status='canceled') → liquida agora.
-- (b) senão, se já passou do prazo de 12h → liquida mesmo assim (fallback).
-- Idempotente, chamável a cada tick do cron.
CREATE OR REPLACE FUNCTION sz_process_refund_requests_due()
RETURNS INT LANGUAGE plpgsql AS $$
DECLARE
    r RECORD;
    v_settled INT := 0;
    v_me_canceled BOOLEAN;
BEGIN
    FOR r IN SELECT order_id, deadline_at FROM sz_order_refund_requests WHERE status = 'pending' LOOP
        v_me_canceled := EXISTS (
            SELECT 1 FROM wc_me_labels l
             WHERE l.wc_order_id = r.order_id
               AND l.status = 'canceled'
               AND l.id = (SELECT l2.id FROM wc_me_labels l2
                            WHERE l2.wc_order_id = r.order_id
                            ORDER BY l2.id DESC LIMIT 1)
        );
        IF v_me_canceled THEN
            IF sz_settle_refund_request(r.order_id, 'me_confirmado') THEN
                v_settled := v_settled + 1;
            END IF;
        ELSIF NOW() > r.deadline_at THEN
            IF sz_settle_refund_request(r.order_id, 'prazo_12h_vencido') THEN
                v_settled := v_settled + 1;
            END IF;
        END IF;
    END LOOP;
    RETURN v_settled;
END; $$;

COMMENT ON FUNCTION sz_process_refund_requests_due IS
    'AUDIT-2026-07-31: liquida reembolso confirmado por remessa (ME cancelou) '
    'ou por prazo vencido (12h, fallback quando não dá pra verificar). Chamado '
    'pelo cron a cada tick (idempotente).';
