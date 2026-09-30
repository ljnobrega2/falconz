-- =============================================================================
-- 473-lgpd-anonimizacao-extend.sql
-- AUDIT-LGPD-CHECKOUT-2026-06-24 #A4 + #M6: estende a anonimização de 2 anos para
-- alvos de PII que sobreviviam INDEFINIDAMENTE à função atual.
--
-- A versão anterior de sz_anonymize_old_order_pii() (441-lgpd-retencao-cpf-endereco
-- .sql) mascarava sz_order_addresses e sz_motoboy_pedidos, mas DEIXAVA:
--   • A4 (Art.16): o CPF do comprador correio gravado em sz_order_meta._billing_cpf
--     (dígitos crus, sem cifragem) — nunca tocado pela função, retido para sempre.
--   • M6 (Art.6/16): sz_orders.ip_address / user_agent / customer_note — `sz_orders`
--     aparecia só como FONTE de JOIN nos UPDATEs antigos, nunca como `UPDATE SET`.
--
-- Esta migração REDEFINE a função (CREATE OR REPLACE, MESMA assinatura
-- `() RETURNS integer`, MESMO nome — o cron em go/cron/cmd/server/main.go segue
-- chamando `SELECT sz_anonymize_old_order_pii()` sem mudança). Os DOIS UPDATEs
-- existentes (sz_order_addresses, sz_motoboy_pedidos) são preservados VERBATIM;
-- esta migração só ADICIONA dois ramos novos:
--   • _billing_cpf em sz_order_meta → NULL  (mesma escolha de recebedor_cpf=NULL;
--     não deleta a linha de meta, só zera o valor sensível).
--   • sz_orders.ip_address  → '0.0.0.0'
--     sz_orders.user_agent  → '[redacted]'
--     sz_orders.customer_note → '[anonimizado]' (só quando havia nota; ver abaixo).
--
-- GUARDA preservada nos ramos novos: MESMO conjunto de status finalizado e MESMA
-- idade (> 2 anos via COALESCE(created_at, NOW())) dos UPDATEs atuais. NUNCA deleta
-- linha; NÃO toca valor financeiro nem cidade/uf.
--
-- IDEMPOTÊNCIA: cada ramo só seleciona linhas ainda não-anonimizadas (NULL-safe,
-- IS DISTINCT FROM). Cuidado especial em customer_note: a maioria dos pedidos tem
-- nota NULL — um guard ingênuo `customer_note IS DISTINCT FROM '[anonimizado]'`
-- deixaria toda linha-NULL elegível p/ sempre (NULL é sempre "distinto" do
-- sentinela), re-contando a cada run. Por isso só anonimizamos a nota QUANDO ela
-- existe (`customer_note IS NOT NULL`), preservando NULL como NULL — não inventa
-- dado e a contagem assenta em 0 ao re-rodar. O UPDATE em sz_orders dispara
-- trg_orders_updated_at (bump de updated_at) — esperado e inócuo.
-- =============================================================================

CREATE OR REPLACE FUNCTION sz_anonymize_old_order_pii()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n1 integer; n2 integer; n3 integer; n4 integer;
BEGIN
    -- ── Endereços de pedido (sz_order_addresses) de pedidos finalizados > 2 anos ──
    UPDATE sz_order_addresses a
       SET nome        = '[anonimizado]',
           telefone    = '0000000000',
           email       = CASE WHEN a.email IS NOT NULL AND a.email <> ''
                              THEN sz_mask_email(a.email) ELSE a.email END,
           -- AUDIT-2026-06-21 #17: endereço residencial completo (Art. 6/15/16).
           logradouro  = '[anonimizado]',
           numero      = '[anonimizado]',
           complemento = '[anonimizado]',
           bairro      = '[anonimizado]',
           -- CEP parcial: preserva região (5 dígitos), descarta sufixo. Nullable
           -- aqui, mas mantido parcial p/ consistência com dest_cep (CHAR(8) NOT NULL).
           cep         = CASE WHEN a.cep IS NOT NULL AND length(a.cep) >= 5
                              THEN left(regexp_replace(a.cep, '[^0-9]', '', 'g'), 5) || '000'
                              ELSE a.cep END
      FROM sz_orders o
     WHERE a.order_id = o.id
       AND o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years'
       -- Reprocessa enquanto qualquer alvo continuar não-anonimizado (NULL-safe).
       AND ( a.nome        IS DISTINCT FROM '[anonimizado]'
          OR a.logradouro  IS DISTINCT FROM '[anonimizado]'
          OR a.numero      IS DISTINCT FROM '[anonimizado]'
          OR a.complemento IS DISTINCT FROM '[anonimizado]'
          OR a.bairro      IS DISTINCT FROM '[anonimizado]' );
    GET DIAGNOSTICS n1 = ROW_COUNT;

    -- ── Destinatário/recebedor no pedido motoboy (dest_*/recebedor_*) ────────────
    UPDATE sz_motoboy_pedidos m
       SET dest_nome      = '[anonimizado]',
           dest_telefone  = '0000000000',
           -- AUDIT-2026-06-21 #17: CPF, endereço residencial, coords e dados do
           -- recebedor (Art. 6/11/15/16). Assinatura base64 inline → NULL (sem arquivo).
           recebedor_cpf        = NULL,
           recebedor_nome       = '[anonimizado]',
           recebedor_assinatura = NULL,
           dest_endereco    = '[anonimizado]',
           dest_numero      = '[anonimizado]',
           dest_complemento = '[anonimizado]',
           dest_bairro      = '[anonimizado]',
           -- dest_cep é CHAR(8) NOT NULL → não pode ser NULL; CEP parcial (8 chars).
           dest_cep         = CASE WHEN length(m.dest_cep) >= 5
                                   THEN left(m.dest_cep, 5) || '000'
                                   ELSE m.dest_cep END,
           dest_lat         = NULL,
           dest_lng         = NULL
      FROM sz_orders o
     WHERE COALESCE(o.wp_order_id, o.id) = m.wc_order_id
       AND o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years'
       -- Reprocessa enquanto qualquer alvo continuar não-anonimizado (NULL-safe).
       AND ( m.dest_nome            IS DISTINCT FROM '[anonimizado]'
          OR m.recebedor_cpf        IS NOT NULL
          OR m.recebedor_nome       IS DISTINCT FROM '[anonimizado]'
          OR m.recebedor_assinatura IS NOT NULL
          OR m.dest_endereco        IS DISTINCT FROM '[anonimizado]'
          OR m.dest_lat             IS NOT NULL
          OR m.dest_lng             IS NOT NULL );
    GET DIAGNOSTICS n2 = ROW_COUNT;

    -- ── A4 (Art.16): CPF do comprador em sz_order_meta._billing_cpf → NULL ────────
    -- Mesma guarda de status finalizado + idade > 2 anos dos UPDATEs acima. Não
    -- deleta a linha de meta (mantém a chave p/ trilha); só zera o valor sensível.
    -- `meta_value IS NOT NULL` faz a contagem assentar em 0 ao re-rodar (idempotente).
    UPDATE sz_order_meta meta
       SET meta_value = NULL
      FROM sz_orders o
     WHERE meta.order_id = o.id
       AND meta.meta_key = '_billing_cpf'
       AND meta.meta_value IS NOT NULL
       AND o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years';
    GET DIAGNOSTICS n3 = ROW_COUNT;

    -- ── M6 (Art.6/16): IP / User-Agent / nota livre em sz_orders ─────────────────
    -- UPDATE direto em sz_orders (sem JOIN). customer_note só é anonimizada QUANDO
    -- existe — preservar NULL como NULL evita re-contagem infinita (NULL é sempre
    -- "distinto" do sentinela) e não fabrica nota onde não havia.
    UPDATE sz_orders o
       SET ip_address    = CASE WHEN o.ip_address IS DISTINCT FROM '0.0.0.0'
                                THEN '0.0.0.0' ELSE o.ip_address END,
           user_agent    = CASE WHEN o.user_agent IS DISTINCT FROM '[redacted]'
                                THEN '[redacted]' ELSE o.user_agent END,
           customer_note = CASE WHEN o.customer_note IS NOT NULL
                                THEN '[anonimizado]' ELSE o.customer_note END
     WHERE o.status IN ('entregue','completo','cancelled','cancelado','frustrado','reembolsado')
       AND COALESCE(o.created_at, NOW()) < NOW() - INTERVAL '2 years'
       -- Reprocessa enquanto qualquer alvo continuar não-anonimizado (NULL-safe).
       AND ( o.ip_address IS DISTINCT FROM '0.0.0.0'
          OR o.user_agent IS DISTINCT FROM '[redacted]'
          OR ( o.customer_note IS NOT NULL
               AND o.customer_note IS DISTINCT FROM '[anonimizado]' ) );
    GET DIAGNOSTICS n4 = ROW_COUNT;

    RETURN n1 + n2 + n3 + n4;
END; $$;

COMMENT ON FUNCTION sz_anonymize_old_order_pii() IS
  'LGPD Art.6/11/15/16: anonimiza PII de pedidos finalizados > 2 anos — nome, '
  'telefone, email (mask), CPF (NULL) em sz_order_addresses/sz_motoboy_pedidos, '
  'nome/assinatura do recebedor, endereço residencial completo, CEP parcial e '
  'coordenadas; A4: _billing_cpf em sz_order_meta → NULL; M6: ip_address (0.0.0.0), '
  'user_agent ([redacted]) e customer_note ([anonimizado]) em sz_orders. NÃO deleta '
  'linha nem toca valor financeiro (guarda fiscal); cidade/uf preservadas. Fotos de '
  'comprovante NÃO são tocadas aqui (#25 precisa do path p/ os.Remove). Idempotente '
  '(guards NULL-safe IS DISTINCT FROM; nota NULL preservada). Chamada via go/cron '
  'junto de sz_anonymize_old_pii().';

-- Aplica a retenção já no deploy (idempotente — não espera até 24h do cron).
SELECT sz_anonymize_old_order_pii();
