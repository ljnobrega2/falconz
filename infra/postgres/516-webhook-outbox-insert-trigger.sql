-- =============================================================================
-- 516-webhook-outbox-insert-trigger.sql — enfileira webhook no INSERT do pedido.
--   trg_sz_webhook_outbox (410) só dispara em UPDATE OF status → o status
--   inicial do pedido (ex.: 'pending' na criação via checkout/API) NUNCA
--   gerava evento. Pedido garantiu (dono): webhook desde a criação até o fim,
--   em TODOS os status. Trigger de INSERT separado (status_de sempre NULL,
--   já que não há estado anterior) — mesma tabela outbox, mesmo dispatcher.
-- =============================================================================

CREATE OR REPLACE FUNCTION sz_webhook_outbox_enqueue_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO sz_webhook_outbox (order_id, event_type, status_de, status_para)
    VALUES (NEW.id, 'order_status_' || NEW.status, NULL, NEW.status);
    RETURN NEW;
END; $$;
COMMENT ON FUNCTION sz_webhook_outbox_enqueue_insert() IS
  'Trigger: enfileira evento do status INICIAL do pedido recém-criado (mesma tx do INSERT).';

DROP TRIGGER IF EXISTS trg_sz_webhook_outbox_insert ON sz_orders;
CREATE TRIGGER trg_sz_webhook_outbox_insert
    AFTER INSERT ON sz_orders
    FOR EACH ROW
    EXECUTE FUNCTION sz_webhook_outbox_enqueue_insert();
