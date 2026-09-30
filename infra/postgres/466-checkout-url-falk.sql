-- 466-checkout-url-falk.sql
-- ============================================================================
-- Repoint dos links de checkout para o checkout FALK (checkout-ui React).
--
-- BUG: senderzz_checkout_links.url era gravado/importado apontando para o
-- WordPress legado senderzz (https://app.senderzz.com.br/checkouts/<seg>/?sz=...),
-- que renderiza o checkout antigo FunnelKit ("modelo antigo senderzz"). O checkout
-- aprovado é o FALK em /checkout/?sz=<token> (gateway → falk-checkout-ui), que
-- resolve correio/motoboy pelo `tipo` da oferta via /checkout-api/offer.
--
-- Os geradores em Go (go/portal links_portal.go, go/admin checkout_links.go e
-- affiliates.go) já passam a emitir a URL FALK para NOVAS criações; esta migração
-- corrige o BACKLOG existente. Idempotente: só toca linhas cujo url ainda não está
-- no formato FALK e que têm token. Domínio default = produção FALK; em ambientes
-- com base própria, ajuste depois via UPDATE pontual (a coluna é só o link público).
-- ============================================================================

UPDATE senderzz_checkout_links
   SET url = 'https://app.falklog.com.br/checkout/?sz=' || token
 WHERE token IS NOT NULL
   AND token <> ''
   AND url IS DISTINCT FROM 'https://app.falklog.com.br/checkout/?sz=' || token
   AND (
        url LIKE '%/checkouts/%'                 -- /checkouts/checkout|codsfpc|lp/...
     OR url LIKE '%app.senderzz.com.br%'         -- domínio WP legado
     OR url IS NULL
     OR url = ''
   );
