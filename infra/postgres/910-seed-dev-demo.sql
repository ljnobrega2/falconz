-- Seed DEV/demo — dados mínimos pra avaliar o painel admin (não usar em produção).
-- Idempotente por email/order_number. AUDIT-2026-06-18.
-- Obs: name/status (portal_users) e gross (sz_orders) são colunas GERADAS — não inserir.
BEGIN;

-- Produtores / afiliado (portal users)
INSERT INTO senderzz_portal_users (email, nome, role, plano, ativo, shipping_class_id, created_at)
VALUES
 ('produtor.demo@senderzz.com','Loja Demo Norte','produtor','pro',  TRUE, 101, NOW() - INTERVAL '20 days'),
 ('produtor.sul@senderzz.com', 'Loja Demo Sul',  'produtor','free', TRUE, 102, NOW() - INTERVAL '12 days'),
 ('afiliado.demo@senderzz.com','Afiliado Demo',  'afiliado','free', TRUE, NULL, NOW() - INTERVAL '8 days')
ON CONFLICT (email) DO NOTHING;

-- Carteira TPC + transações para o produtor norte
INSERT INTO tpc_carteira (user_id, saldo, saldo_reservado, created_at)
SELECT id, 1875.40, 120.00, NOW() - INTERVAL '20 days' FROM senderzz_portal_users WHERE email='produtor.demo@senderzz.com'
ON CONFLICT DO NOTHING;

INSERT INTO tpc_transacoes (user_id, tipo, valor, saldo_apos, descricao, status, created_at)
SELECT id, 'credito', 2000.00, 2000.00, 'Recarga PIX',        'confirmado', NOW() - INTERVAL '18 days' FROM senderzz_portal_users WHERE email='produtor.demo@senderzz.com'
UNION ALL
SELECT id, 'debito',   124.60, 1875.40, 'Frete pedido #1003', 'confirmado', NOW() - INTERVAL '3 days'  FROM senderzz_portal_users WHERE email='produtor.demo@senderzz.com';

-- CD + motoboy
INSERT INTO sz_motoboy_cds (nome, cidade, uf, endereco, lat, lng, ativo, created_at, updated_at)
VALUES ('CD Central','São Paulo','SP','Av. Paulista, 1000', -23.5614, -46.6559, TRUE, NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO sz_motoboys (cd_id, nome, telefone, ativo, created_at)
SELECT id, 'Carlos Entregas','11988887777', TRUE, NOW() FROM sz_motoboy_cds WHERE nome='CD Central' LIMIT 1
ON CONFLICT DO NOTHING;

-- Pedidos variados (status/datas/valores) para dashboard, orders, cod-livro
INSERT INTO sz_orders
 (order_number, user_id, produtor_id, customer_name, billing_email, total, subtotal, shipping,
  senderzz_fee, delivery_fee, transaction_fee, affiliate_amount, producer_net,
  shipping_class_id, status, payment_status, payment_method, currency, created_at)
SELECT * FROM (
  SELECT '1001'::varchar, p.id, p.id, 'Maria Silva',   'maria@ex.com',  189.90, 159.90, 30.00,  9.50, 12.00, 5.70, 0.00, 162.70, 101, 'completo',   'paid',    'pix', 'BRL', NOW() - INTERVAL '6 days' FROM senderzz_portal_users p WHERE p.email='produtor.demo@senderzz.com'
  UNION ALL SELECT '1002', p.id, p.id, 'João Souza',    'joao@ex.com',   249.00, 219.00, 30.00, 12.45, 12.00, 7.47, 24.90, 192.18, 101, 'enviado',    'paid',    'cod', 'BRL', NOW() - INTERVAL '2 days' FROM senderzz_portal_users p WHERE p.email='produtor.demo@senderzz.com'
  UNION ALL SELECT '1003', p.id, p.id, 'Ana Costa',     'ana@ex.com',    99.90,  79.90,  20.00,  5.00, 12.00, 3.00, 0.00,  79.90,  101, 'aguardando', 'pending', 'pix', 'BRL', NOW() - INTERVAL '1 day'  FROM senderzz_portal_users p WHERE p.email='produtor.demo@senderzz.com'
  UNION ALL SELECT '1004', p.id, p.id, 'Pedro Lima',    'pedro@ex.com',  320.00, 290.00, 30.00, 16.00, 12.00, 9.60, 32.00, 250.40, 102, 'entregue',   'paid',    'cod', 'BRL', NOW() - INTERVAL '4 days' FROM senderzz_portal_users p WHERE p.email='produtor.sul@senderzz.com'
  UNION ALL SELECT '1005', p.id, p.id, 'Beatriz Rocha', 'bia@ex.com',    75.00,  55.00,  20.00,  3.75, 12.00, 2.25, 0.00,  57.00,  102, 'frustrado',  'paid',    'cod', 'BRL', NOW() - INTERVAL '5 days' FROM senderzz_portal_users p WHERE p.email='produtor.sul@senderzz.com'
  UNION ALL SELECT '1006', p.id, p.id, 'Rafael Dias',   'rafa@ex.com',  410.50, 380.50, 30.00, 20.52, 12.00, 12.31,41.05, 324.62, 101, 'processing','paid',    'pix', 'BRL', NOW()                      FROM senderzz_portal_users p WHERE p.email='produtor.demo@senderzz.com'
) v(order_number, user_id, produtor_id, customer_name, billing_email, total, subtotal, shipping,
    senderzz_fee, delivery_fee, transaction_fee, affiliate_amount, producer_net,
    shipping_class_id, status, payment_status, payment_method, currency, created_at)
WHERE NOT EXISTS (SELECT 1 FROM sz_orders o WHERE o.order_number = v.order_number);

COMMIT;
