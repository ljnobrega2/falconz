-- ============================================================================
-- 480 — Estoque canônico por PRODUTO (sem ambiguidade COD vs FF).
--
-- Problema: um produto tem N posts WP (ex.: Datalaprox COD=post 1075, FF=1278).
-- O estoque vivia por post; o checkout COD lia o post 1075 (fantasma, 0 disp) e
-- mostrava "esgotado" mesmo o produto tendo 289 no post 1278. Ofertas do MESMO
-- produto devem compartilhar UM pool.
--
-- Solução (não-destrutiva): função que resolve QUALQUER chave (sz_products.id,
-- wp_post_id, ou post_id de um link de checkout) para o sz_products.id canônico.
-- O checkout passa a somar o estoque do produto pelas chaves do produto (id +
-- wp_post_id), ignorando posts órfãos/fantasma. Sem migração de dado, sem cascata.
-- ============================================================================

CREATE OR REPLACE FUNCTION sz_resolve_product_id(p_key bigint) RETURNS bigint
LANGUAGE sql STABLE AS $$
  SELECT COALESCE(
    -- 1) já é um sz_products.id
    (SELECT id FROM sz_products WHERE id = p_key LIMIT 1),
    -- 2) é o wp_post_id do produto
    (SELECT id FROM sz_products WHERE wp_post_id = p_key LIMIT 1),
    -- 3) é o post_id de um link de checkout (oferta) → resolve pelo produtor + nome
    --    (ex.: post 1075, link "3 Datalaprox" do produtor 15 → produto "Datalaprox").
    --    ORDER BY nome mais longo = match mais específico (reduz colisão).
    (SELECT p.id
       FROM senderzz_checkout_links cl
       JOIN sz_products p
         ON p.produtor_id = cl.producer_id
        AND cl.base_name ILIKE '%' || p.nome || '%'
      WHERE cl.post_id = p_key
      ORDER BY length(p.nome) DESC
      LIMIT 1),
    -- 4) fallback: a própria chave (degrada ao comportamento antigo)
    p_key
  )
$$;
