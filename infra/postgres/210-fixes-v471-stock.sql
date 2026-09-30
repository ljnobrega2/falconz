-- =============================================================================
-- schema-fixes-v471-stock.sql  — SISTEMA DE ESTOQUE (FEAT-STOCK)
--
-- Regra central (pedido do dono): "pedido PRÉ-AGENDADO não contabiliza estoque".
--
-- CONTEXTO: o ciclo de reserva é dirigido pelo STATUS do pedido de entrega
-- (sz_motoboy_pedidos), NÃO pelo status do pedido financeiro (sz_orders) — pois
-- 'pre_agendado' é um status de sz_motoboy_pedidos (ver schema-preagendado-*.sql).
-- Um pedido nasce 'agendado' (dentro da cota da zona → compromete estoque) ou
-- 'pre_agendado' (fora da cota, pode auto-cancelar → NÃO compromete estoque).
--
-- POR QUE TRIGGER (e não hook em Go): as transições de status de
-- sz_motoboy_pedidos estão ESPALHADAS por ~7 pontos do go/motoboy (ol.go faz
-- UPDATE cru fora do bridge). Um gatilho no banco é o único choke point que
-- NENHUM caminho de código pode burlar — e é atômico com a própria mudança.
--
-- CARDINALIDADE: sz_motoboy_pedidos tem UNIQUE(wc_order_id) → 1 order ↔ 1 pedido.
-- Logo reservar TODOS os itens do order por pedido é correto (sem dupla reserva).
-- Join confirmado: sz_orders.wp_order_id = sz_motoboy_pedidos.wc_order_id
-- (precedente: go/motoboy/internal/handlers/status_bridge.go:13,104).
--
-- Idempotente: CREATE ... IF NOT EXISTS / CREATE OR REPLACE / DROP TRIGGER IF EXISTS.
-- =============================================================================

-- ── Tabelas ──────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS sz_stock (
    id                  BIGSERIAL PRIMARY KEY,
    product_id          BIGINT      NOT NULL,
    variation_id        BIGINT      NOT NULL DEFAULT 0,
    cd_id               BIGINT      NOT NULL DEFAULT 0,
    qty_available       INTEGER     NOT NULL DEFAULT 0,
    qty_reserved        INTEGER     NOT NULL DEFAULT 0,
    low_stock_threshold INTEGER     NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (product_id, variation_id, cd_id),
    CONSTRAINT chk_stock_reserved_nonneg  CHECK (qty_reserved  >= 0),
    CONSTRAINT chk_stock_available_nonneg CHECK (qty_available >= 0)
);
CREATE INDEX IF NOT EXISTS idx_stock_product ON sz_stock (product_id);

-- Ledger imutável: toda alteração de estoque deixa rastro (auditoria/depuração).
CREATE TABLE IF NOT EXISTS sz_stock_movements (
    id              BIGSERIAL PRIMARY KEY,
    product_id      BIGINT       NOT NULL,
    variation_id    BIGINT       NOT NULL DEFAULT 0,
    cd_id           BIGINT       NOT NULL DEFAULT 0,
    order_id        BIGINT       NULL,   -- wc_order_id de origem (NULL p/ ajuste manual)
    delta_available INTEGER      NOT NULL DEFAULT 0,
    delta_reserved  INTEGER      NOT NULL DEFAULT 0,
    tipo            VARCHAR(20)  NOT NULL
        CHECK (tipo IN ('entrada','reserva','liberacao','commit','ajuste')),
    motivo          TEXT         NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_stock_mov_product ON sz_stock_movements (product_id);
CREATE INDEX IF NOT EXISTS idx_stock_mov_order   ON sz_stock_movements (order_id);

-- ── Função pura de delta (testável isoladamente) ──────────────────────────────
--
-- sz_stock_delta(old, new, qty) → (Δdisponível, Δreservado)
--
-- Conjuntos de status (regra explícita, confirmada):
--   RESERVAM    : agendado, aprovado, embalado, em_rota, a_caminho, reagendado
--   NÃO RESERVAM: pre_agendado, frustrado, cancelado
--   COMMIT      : entregue  (baixa definitiva: consome reserva + reduz disponível)
--
-- Lógica simétrica (wasReserving → willReserve):
--   • commit (new=entregue)         : -qty disponível, -qty reservado (se reservava)
--   • !reservava & reservará        : +qty reservado            (ex.: pre_agendado→agendado, nasce agendado)
--   • reservava & !reservará        : -qty reservado            (ex.: agendado→cancelado/frustrado)
--   • caso contrário                : (0,0)                     (ex.: pre_agendado→cancelado — NÃO conta)
--
-- frustrado: por padrão LIBERA de volta ao estoque (mercadoria retorna). Caso o
-- dono prefira tratar como perda, basta mover 'frustrado' para o conjunto commit.
CREATE OR REPLACE FUNCTION sz_stock_delta(p_old text, p_new text, p_qty integer)
RETURNS TABLE(d_available integer, d_reserved integer)
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    reserving text[] := ARRAY['agendado','aprovado','embalado','em_rota','a_caminho','reagendado'];
    was_res  boolean := p_old = ANY(reserving);
    will_res boolean := p_new = ANY(reserving);
BEGIN
    IF p_new = 'entregue' THEN
        -- Baixa definitiva. Se reservava, consome a reserva; senão só o disponível.
        d_available := -p_qty;
        d_reserved  := CASE WHEN was_res THEN -p_qty ELSE 0 END;
    ELSIF (NOT was_res) AND will_res THEN
        d_available := 0;  d_reserved := p_qty;       -- reserva
    ELSIF was_res AND (NOT will_res) THEN
        d_available := 0;  d_reserved := -p_qty;      -- libera
    ELSE
        d_available := 0;  d_reserved := 0;           -- sem efeito (pre_agendado→cancelado etc.)
    END IF;
    RETURN NEXT;
END;
$$;

-- ── Trigger: aplica o delta a cada mudança de status do pedido de entrega ──────
CREATE OR REPLACE FUNCTION sz_stock_apply_pedido()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_old       text;
    v_item      record;
    v_delta     record;
    v_new_avail integer;
    v_new_res   integer;
BEGIN
    v_old := CASE WHEN TG_OP = 'INSERT' THEN '' ELSE OLD.status END;

    -- Guarda extra (o WHEN do trigger UPDATE já filtra, mas blinda chamadas diretas).
    IF TG_OP = 'UPDATE' AND v_old IS NOT DISTINCT FROM NEW.status THEN
        RETURN NEW;
    END IF;

    -- 1 order ↔ 1 pedido (UNIQUE wc_order_id) → reservar todos os itens do order.
    -- CORREÇÃO CRIT (auditoria 2026-06-18 / DATA-stock-reservation-order): pedidos
    -- NATIVOS do Go têm wp_order_id NULL e o motoboy pedido carrega wc_order_id =
    -- sz_orders.id. O join antigo (o.wp_order_id = wc_order_id) NÃO casava nesses
    -- casos → 0 itens → reserva era no-op SILENCIOSO. COALESCE(wp_order_id, id)
    -- casa tanto pedido nativo (id) quanto importado do WP (wp_order_id).
    FOR v_item IN
        SELECT i.produto_id, i.quantidade
          FROM sz_order_items i
          JOIN sz_orders o ON i.order_id = o.id
         WHERE COALESCE(o.wp_order_id, o.id) = NEW.wc_order_id
    LOOP
        SELECT * INTO v_delta FROM sz_stock_delta(v_old, NEW.status, v_item.quantidade);
        CONTINUE WHEN v_delta.d_available = 0 AND v_delta.d_reserved = 0;

        -- Garante a linha de estoque (produto + CD).
        INSERT INTO sz_stock (product_id, cd_id, qty_available, qty_reserved)
        VALUES (v_item.produto_id, NEW.cd_id, 0, 0)
        ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

        SELECT qty_available + v_delta.d_available,
               qty_reserved  + v_delta.d_reserved
          INTO v_new_avail, v_new_res
          FROM sz_stock
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = NEW.cd_id
         FOR UPDATE;

        -- Clamp ≥0 COM aviso visível (clamp silencioso esconde dupla-liberação).
        IF v_new_res < 0 THEN
            RAISE WARNING '[sz_stock] reserva negativa evitada produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, NEW.cd_id, NEW.id, v_old, NEW.status, v_new_res;
            v_new_res := 0;
        END IF;
        IF v_new_avail < 0 THEN
            RAISE WARNING '[sz_stock] disponivel negativo evitado produto=% cd=% pedido=% (% -> %, calc=%)',
                v_item.produto_id, NEW.cd_id, NEW.id, v_old, NEW.status, v_new_avail;
            v_new_avail := 0;
        END IF;

        UPDATE sz_stock
           SET qty_available = v_new_avail,
               qty_reserved  = v_new_res,
               updated_at    = now()
         WHERE product_id = v_item.produto_id AND variation_id = 0 AND cd_id = NEW.cd_id;

        INSERT INTO sz_stock_movements
            (product_id, variation_id, cd_id, order_id,
             delta_available, delta_reserved, tipo, motivo)
        VALUES
            (v_item.produto_id, 0, NEW.cd_id, NEW.wc_order_id,
             v_delta.d_available, v_delta.d_reserved,
             CASE
                 WHEN NEW.status = 'entregue'    THEN 'commit'
                 WHEN v_delta.d_reserved > 0     THEN 'reserva'
                 ELSE 'liberacao'
             END,
             format('pedido %s: %s -> %s', NEW.id, v_old, NEW.status));
    END LOOP;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_stock_pedido_ins ON sz_motoboy_pedidos;
CREATE TRIGGER trg_stock_pedido_ins
    AFTER INSERT ON sz_motoboy_pedidos
    FOR EACH ROW EXECUTE FUNCTION sz_stock_apply_pedido();

DROP TRIGGER IF EXISTS trg_stock_pedido_upd ON sz_motoboy_pedidos;
CREATE TRIGGER trg_stock_pedido_upd
    AFTER UPDATE OF status ON sz_motoboy_pedidos
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION sz_stock_apply_pedido();
