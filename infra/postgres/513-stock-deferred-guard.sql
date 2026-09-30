-- 513-stock-deferred-guard.sql — corrige risco real (não hipotético) descoberto
-- ao revisar suporte a COD via API: trg_stock_order_item_ins (512) roda AFTER
-- INSERT ON sz_order_items, e checa `EXISTS sz_motoboy_pedidos` pra não
-- double-bookar com o trigger nativo de COD (210-fixes-v471-stock.sql).
--
-- Problema: em TODO caminho de criação de pedido motoboy (checkout.go nativo E
-- a API), a ORDEM de insert é sz_orders → sz_order_items → sz_motoboy_pedidos
-- (motoboy é o ÚLTIMO insert, sempre). Minha trigger IMEDIATA (não-deferred)
-- roda no INSERT de sz_order_items, ANTES de sz_motoboy_pedidos existir — o
-- EXISTS nunca acha nada, trata todo pedido motoboy como Expedição, reserva
-- estoque errado, e quando sz_motoboy_pedidos É inserido depois, o trigger
-- NATIVO reserva DE NOVO → dupla reserva real pro mesmo pedido físico.
--
-- Nenhum pedido motoboy real foi criado desde que 511/512 subiram (confirmado
-- via query em produção antes desta migration) — bug real, mas ainda não
-- disparado. Corrigido antes de qualquer pedido motoboy passar por aqui.
--
-- Fix: CONSTRAINT TRIGGER ... DEFERRABLE INITIALLY DEFERRED — roda no COMMIT
-- da transação, depois que TODOS os inserts (inclusive sz_motoboy_pedidos, não
-- importa a ordem) já aconteceram. O EXISTS sempre vê o estado final correto.

DROP TRIGGER IF EXISTS trg_stock_order_item_ins ON sz_order_items;

CREATE CONSTRAINT TRIGGER trg_stock_order_item_ins
    AFTER INSERT ON sz_order_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION sz_stock_apply_order_item_ins();
