-- =============================================================================
-- Seed DEMO — produtor de teste do portal FALKZ (produtor.demo@falkz.test).
-- Objetivo: popular as telas do Portal V2 com dados realistas para avaliação.
-- NÃO usar em produção. Idempotente (WHERE NOT EXISTS / ON CONFLICT). AUDIT-2026-06-18.
--
-- ATENÇÃO — DOIS ID-SPACES (intencional, confirmado nos handlers Go):
--   * pedidos / produtos / estoque / motoboy → produtor_id = 46  (portal_users.id)
--       orders.go:172  WHERE o.produtor_id = $1 ($1 = portal id)
--       products.go / stock_portal.go → sp.produtor_id = portal id
--   * carteira de frete (tpc_carteira/tpc_transacoes) → user_id = 990001 (wp_user_id)
--       wallet_expedition_portal.go:30 "Escopo SEMPRE por u.WPUserID"
--   A linha tpc_carteira pré-existente em user_id=46 (saldo 0) é STALE e nenhum
--   handler a lê — deixada como está.
--
-- Colunas GERADAS (NÃO inserir): sz_orders.gross; ids GENERATED ALWAYS.
-- =============================================================================
BEGIN;

-- ── 1. Carteira de frete (TPC) — keyed por wp_user_id 990001 ─────────────────
INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado, created_at)
VALUES (990001, 2500.00, 180.00, NOW() - INTERVAL '25 days')
ON CONFLICT (user_id) DO UPDATE
    SET saldo = EXCLUDED.saldo,
        saldo_reservado = EXCLUDED.saldo_reservado;

-- ── 2. Ledger de transações (tpc_transacoes) — wp_user_id 990001 ─────────────
INSERT INTO tpc_transacoes (user_id, tipo, valor, saldo_apos, descricao, referencia, status, created_at)
VALUES
  (990001, 'credito', 2000.00, 2000.00, 'Recarga PIX',                          'demo:recarga:1',   'confirmado', NOW() - INTERVAL '24 days'),
  (990001, 'credito', 1000.00, 3000.00, 'Recarga PIX',                          'demo:recarga:2',   'confirmado', NOW() - INTERVAL '12 days'),
  (990001, 'debito',    94.60, 2905.40, 'Frete Melhor Envio #DM-1002',          'demo:frete:1002',  'confirmado', NOW() - INTERVAL '6 days'),
  (990001, 'debito',   124.80, 2780.60, 'Frete Melhor Envio #DM-1004',          'demo:frete:1004',  'confirmado', NOW() - INTERVAL '4 days'),
  (990001, 'debito',   280.60, 2500.00, 'Frete Melhor Envio #DM-1006',          'demo:frete:1006',  'confirmado', NOW() - INTERVAL '1 day'),
  (990001, 'debito',   180.00, 2500.00, 'Reserva frete (em separação) #DM-1007','demo:reserva:1007','pendente',   NOW() - INTERVAL '6 hours')
ON CONFLICT (user_id, referencia, tipo) DO NOTHING;

-- ── 3. Produtos do produtor 46 (com dimensões) ──────────────────────────────
INSERT INTO sz_products (produtor_id, nome, sku, preco, descricao, categoria, status, altura, largura, comprimento, peso, created_at)
SELECT v.* FROM (VALUES
  (46::bigint, 'Produto Demo A — Suplemento Whey 900g', 'DEMO-A-WHEY900', 159.90::numeric, 'Whey protein concentrado 900g, sabor baunilha. Produto de demonstração.', 'Suplementos', 'active', 18.0::numeric, 14.0::numeric, 14.0::numeric, 1.05::numeric, NOW() - INTERVAL '20 days'),
  (46::bigint, 'Produto Demo B — Creatina 300g',        'DEMO-B-CREA300', 89.90::numeric,  'Creatina monohidratada 300g, pote. Produto de demonstração.',           'Suplementos', 'active', 12.0::numeric, 10.0::numeric, 10.0::numeric, 0.40::numeric, NOW() - INTERVAL '18 days'),
  (46::bigint, 'Produto Demo C — Coqueteleira 700ml',   'DEMO-C-SHAKER',  29.90::numeric,  'Coqueteleira 700ml com mola misturadora. Produto de demonstração.',     'Acessórios',  'active', 22.0::numeric, 10.0::numeric, 10.0::numeric, 0.18::numeric, NOW() - INTERVAL '15 days')
) AS v(produtor_id, nome, sku, preco, descricao, categoria, status, altura, largura, comprimento, peso, created_at)
WHERE NOT EXISTS (SELECT 1 FROM sz_products p WHERE p.produtor_id = 46 AND p.sku = v.sku);

-- ── 4. Estoque (sz_stock) — DEVE preceder os motoboy pedidos ─────────────────
INSERT INTO sz_stock (product_id, variation_id, cd_id, qty_available, qty_reserved, low_stock_threshold, updated_at)
SELECT p.id, 0, 1, s.qty, 0, 5, NOW()
FROM sz_products p
JOIN (VALUES ('DEMO-A-WHEY900', 80), ('DEMO-B-CREA300', 65), ('DEMO-C-SHAKER', 120)) AS s(sku, qty) ON s.sku = p.sku
WHERE p.produtor_id = 46
ON CONFLICT (product_id, variation_id, cd_id) DO NOTHING;

-- ── 5. Pedidos (sz_orders) — produtor_id = 46, status variados ───────────────
INSERT INTO sz_orders
 (order_number, user_id, produtor_id, customer_name, billing_email, total, subtotal, shipping,
  senderzz_fee, delivery_fee, transaction_fee, affiliate_amount, producer_net,
  shipping_class_id, status, payment_status, payment_method, currency, created_at)
SELECT * FROM (VALUES
  ('DM-1001'::varchar, 46::bigint, 46::bigint, 'Mariana Alves',  'mariana@demo.test',  189.80::numeric, 159.90::numeric, 29.90::numeric,  9.49::numeric, 12.00::numeric, 5.69::numeric, 0.00::numeric, 162.62::numeric, NULL::bigint, 'completo',    'paid',    'pix', 'BRL', NOW() - INTERVAL '8 days'),
  ('DM-1002', 46, 46, 'Rafael Souza',    'rafael@demo.test',   189.80, 159.90, 29.90,  9.49, 12.00, 5.69, 0.00, 162.62, NULL, 'entregue',    'paid',    'pix', 'BRL', NOW() - INTERVAL '6 days'),
  ('DM-1003', 46, 46, 'Carla Mendes',    'carla@demo.test',    119.80,  89.90, 29.90,  5.39, 12.00, 3.59, 0.00,  68.92, NULL, 'enviado',     'paid',    'pix', 'BRL', NOW() - INTERVAL '5 days'),
  ('DM-1004', 46, 46, 'Bruno Lima',      'bruno@demo.test',    279.70, 249.80, 29.90, 14.99, 12.00, 8.39, 0.00, 213.42, NULL, 'entregue',    'paid',    'cod', 'BRL', NOW() - INTERVAL '4 days'),
  ('DM-1005', 46, 46, 'Patrícia Rocha',  'patricia@demo.test',  99.80,  89.90,  9.90,  4.50, 12.00, 2.99, 0.00,  70.41, NULL, 'aguardando',  'pending', 'pix', 'BRL', NOW() - INTERVAL '2 days'),
  ('DM-1006', 46, 46, 'Diego Ferreira',  'diego@demo.test',    449.70, 419.80, 29.90, 22.49, 12.00, 13.49,0.00, 371.82, NULL, 'em_separacao','paid',    'pix', 'BRL', NOW() - INTERVAL '1 day'),
  ('DM-1007', 46, 46, 'Juliana Castro',  'juliana@demo.test',  219.80, 189.80, 30.00, 11.39, 12.00, 6.59, 0.00, 159.82, NULL, 'processing',  'paid',    'pix', 'BRL', NOW() - INTERVAL '6 hours'),
  ('DM-1008', 46, 46, 'Felipe Nunes',    'felipe@demo.test',   119.80,  89.90, 29.90,  5.39, 12.00, 3.59, 0.00,  68.92, NULL, 'frustrado',   'paid',    'cod', 'BRL', NOW() - INTERVAL '3 days')
) AS v(order_number, user_id, produtor_id, customer_name, billing_email, total, subtotal, shipping,
       senderzz_fee, delivery_fee, transaction_fee, affiliate_amount, producer_net,
       shipping_class_id, status, payment_status, payment_method, currency, created_at)
WHERE NOT EXISTS (SELECT 1 FROM sz_orders o WHERE o.order_number = v.order_number);

-- ── 6. Itens dos pedidos (sz_order_items) — join por order_number + sku ───────
INSERT INTO sz_order_items (order_id, produto_id, nome, sku, quantidade, preco_unit, subtotal)
SELECT o.id, p.id, p.nome, p.sku, it.qtd, p.preco, (p.preco * it.qtd)
FROM (VALUES
  ('DM-1001','DEMO-A-WHEY900',1),
  ('DM-1002','DEMO-A-WHEY900',1),
  ('DM-1003','DEMO-B-CREA300',1),
  ('DM-1004','DEMO-A-WHEY900',1), ('DM-1004','DEMO-B-CREA300',1),
  ('DM-1005','DEMO-B-CREA300',1),
  ('DM-1006','DEMO-A-WHEY900',2), ('DM-1006','DEMO-C-SHAKER',1),
  ('DM-1007','DEMO-A-WHEY900',1), ('DM-1007','DEMO-C-SHAKER',1),
  ('DM-1008','DEMO-B-CREA300',1)
) AS it(order_number, sku, qtd)
JOIN sz_orders   o ON o.order_number = it.order_number
JOIN sz_products p ON p.sku = it.sku AND p.produtor_id = 46
WHERE NOT EXISTS (SELECT 1 FROM sz_order_items oi WHERE oi.order_id = o.id AND oi.produto_id = p.id);

-- ── 7. Pedidos de entrega motoboy (sz_motoboy_pedidos) ───────────────────────
INSERT INTO sz_motoboy_pedidos
 (wc_order_id, cd_id, zona_id, motoboy_id, status, dest_nome, dest_telefone,
  dest_cep, dest_endereco, dest_numero, dest_bairro, dest_cidade, dest_uf,
  dest_produto, quantidade, valor_pedido, valor_taxa, created_at, updated_at)
SELECT o.id, m.cd_id, m.zona_id, m.motoboy_id, m.status, m.dest_nome, m.dest_telefone,
       m.dest_cep, m.dest_endereco, m.dest_numero, m.dest_bairro, m.dest_cidade, 'SP',
       m.dest_produto, m.quantidade, m.valor_pedido, m.valor_taxa,
       o.created_at, NOW()
FROM (VALUES
  ('DM-1002', 1::bigint, 17::bigint, 1::bigint, 'entregue', 'Rafael Souza',   '11991110001', '07010000', 'Av. Monteiro Lobato',  '1500', 'Centro',     'Guarulhos',             'Produto Demo A — Suplemento Whey 900g', 1, 189.80::numeric, 18.00::numeric),
  ('DM-1003', 1, 4,  3, 'em_rota',  'Carla Mendes',   '11991110002', '06401000', 'Al. Rio Negro',        '200',  'Alphaville', 'Barueri',               'Produto Demo B — Creatina 300g',        1, 119.80, 18.00),
  ('DM-1004', 1, 35, 4, 'entregue', 'Bruno Lima',     '11991110003', '09010000', 'Rua das Figueiras',    '450',  'Centro',     'Santo André',           'Produto Demo A — Suplemento Whey 900g', 2, 279.70, 22.00),
  ('DM-1006', 1, 28, 6, 'agendado', 'Diego Ferreira', '11991110004', '06010000', 'Av. dos Autonomistas', '3500', 'Vila Yara',  'Osasco',                'Produto Demo A — Suplemento Whey 900g', 3, 449.70, 22.00),
  ('DM-1007', 1, 39, 7, 'agendado', 'Juliana Castro', '11991110005', '09710000', 'Av. Kennedy',          '700',  'Jardim',     'São Bernardo do Campo', 'Produto Demo A — Suplemento Whey 900g', 2, 219.80, 22.00)
) AS m(order_number, cd_id, zona_id, motoboy_id, status, dest_nome, dest_telefone,
       dest_cep, dest_endereco, dest_numero, dest_bairro, dest_cidade, dest_produto,
       quantidade, valor_pedido, valor_taxa)
JOIN sz_orders o ON o.order_number = m.order_number
WHERE NOT EXISTS (SELECT 1 FROM sz_motoboy_pedidos mp WHERE mp.wc_order_id = o.id);

COMMIT;
