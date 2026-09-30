-- 467-aff-ref-salt-seed.sql
-- FEAT-AFF-ATTRIBUTION (2026-06-24): semeia `sz_aff_ref_salt` em senderzz_options.
--
-- POR QUE: o portal só EMITE o token de rastreio do afiliado (?r=) quando esse salt
-- existe — `appendRefToken` (go/portal links_portal.go) degrada para URL sem ?r=
-- quando o salt é vazio. Em prod o salt nunca foi semeado, então afiliado nenhum
-- recebia link rastreável e a venda não era creditada.
--
-- O VALOR é aleatório e estável; o DECODE (go/orders checkout.go decodeAffRefToken)
-- IGNORA o salt (só lê os 4 primeiros bytes = id do vínculo), então o valor não
-- precisa casar com nada nem ser secreto-forte — é só presença + leve ofuscação.
--
-- Idempotente: NÃO sobrescreve um salt já existente (WHERE NOT EXISTS) — preserva o
-- valor atual em qualquer ambiente que já tenha um, sem depender da coluna do PK.
INSERT INTO senderzz_options (name, value)
SELECT 'sz_aff_ref_salt', replace(gen_random_uuid()::text, '-', '')
 WHERE NOT EXISTS (
   SELECT 1 FROM senderzz_options WHERE name = 'sz_aff_ref_salt'
 );
