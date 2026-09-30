-- =============================================================================
-- 441-lgpd-retencao-cpf-endereco.sql
-- AUDIT-2026-06-21 #17: estende a retenção/anonimização de PII de pedido/entrega.
--
-- A versão anterior de sz_anonymize_old_order_pii() (401-fix-auditoria-infra.sql)
-- mascarava SÓ nome/telefone/email após 2 anos — deixando CPF do recebedor,
-- endereço residencial completo e coordenadas precisas retidos INDEFINIDAMENTE
-- (LGPD Art. 15/16 — eliminação após fim da finalidade; Art. 6º — minimização).
--
-- Esta migração REDEFINE a função (CREATE OR REPLACE, mesma assinatura
-- `() RETURNS integer`, mesmo nome — o cron em go/cron/cmd/server/main.go segue
-- chamando `SELECT sz_anonymize_old_order_pii()` sem mudança) para também:
--   • recebedor_cpf      → NULL          (dado sensível de identificação direta)
--   • recebedor_nome     → '[anonimizado]'
--   • recebedor_assinatura → NULL        (assinatura base64 inline — sem arquivo)
--   • dest_endereco/complemento/bairro/numero → '[anonimizado]'
--   • dest_cep           → CEP parcial (left 5 + '000') — coluna é CHAR(8) NOT NULL,
--                          não pode ser NULL; preserva região, descarta sufixo.
--   • dest_lat/dest_lng  → NULL          (geolocalização precisa da residência)
--   • sz_order_addresses.logradouro/numero/complemento/bairro → '[anonimizado]'
--   • sz_order_addresses.cep → CEP parcial (coluna nullable, mas mantemos parcial
--                          p/ consistência e analítica de região)
--
-- GUARDA FISCAL preservada: NUNCA deleta linha; NÃO toca valor_*/pgto_* nem nada
-- financeiro. cidade/uf ficam (granularidade grosseira, não identificam titular).
--
-- FOTOS de comprovante (sz_motoboy_comprovantes.foto_url/foto_path) NÃO são
-- tocadas aqui DE PROPÓSITO: o expurgo dessas fotos (AUDIT #25) precisa do
-- foto_path p/ apagar o arquivo físico do disco (os.Remove). Anular o path antes
-- orfanaria o arquivo permanentemente. Assinatura (recebedor_assinatura) é base64
-- inline na linha do pedido (sem arquivo), por isso é seguro e correto anular aqui.
--
-- IDEMPOTÊNCIA: o guard de re-execução foi AMPLIADO. O guard antigo
-- (`dest_nome <> '[anonimizado]'`) faria a 1ª execução pós-deploy PULAR todas as
-- linhas que a versão antiga já mascarou (nome/tel feitos, mas CPF/endereço ainda
-- presentes) — exatamente o bug que #17 corrige. Agora a linha é reprocessada
-- enquanto QUALQUER alvo continuar não-anonimizado, com IS DISTINCT FROM (NULL-safe).
-- =============================================================================

CREATE OR REPLACE FUNCTION sz_anonymize_old_order_pii()
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n1 integer; n2 integer;
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

    RETURN n1 + n2;
END; $$;

COMMENT ON FUNCTION sz_anonymize_old_order_pii() IS
  'LGPD Art.6/11/15/16: anonimiza PII de pedidos finalizados > 2 anos — nome, '
  'telefone, email (mask), CPF (NULL), nome/assinatura do recebedor, endereço '
  'residencial completo, CEP parcial e coordenadas. NÃO deleta linha nem toca '
  'valor financeiro (guarda fiscal); cidade/uf preservadas (granular grosseira). '
  'Fotos de comprovante NÃO são tocadas aqui (#25 precisa do path p/ os.Remove). '
  'Idempotente (guard NULL-safe IS DISTINCT FROM). Chamada via go/cron junto de '
  'sz_anonymize_old_pii().';

-- Aplica a retenção já no deploy (idempotente — não espera até 24h do cron).
SELECT sz_anonymize_old_order_pii();
