-- =============================================================================
-- 427-portal-users-document-phone.sql
-- Senderzz — colunas DOCUMENT (CPF) e PHONE no cadastro do usuário do portal.
--
-- POR QUÊ: o cadastro do portal (Portal_Page.php :: ajax_portal_register) coleta
--   CPF e telefone no WordPress (usermeta `sz_document` / `billing_phone`), mas o
--   serviço Go é STANDALONE e NÃO espelha wp_usermeta. Logo /portal/me não tinha
--   de onde devolver document/phone. O Portal V2 (portal-ui/src/pages/Settings.tsx)
--   já LÊ `me.document` para auto-preencher a chave PIX quando tipo='cpf' e exibe
--   `me.phone` na aba Conta — sem essas colunas o CPF aparece como "—" e o
--   cadastro de conta PIX por CPF dá 422 ("CPF do cadastro indisponível").
--
-- ONDE: colunas nomeadas EXATAMENTE como as chaves JSON esperadas pelo front
--   (`document` / `phone`) → o handler Me faz um SELECT trivial sem mapeamento.
--   Nullable, sem default: registros antigos ficam NULL e o handler devolve "".
--   A POPULAÇÃO é responsabilidade do fluxo de cadastro/edição (não há backfill
--   aqui — o ambiente atual não tem CPF/telefone de origem confiável a migrar).
--
-- IDEMPOTENTE: ADD COLUMN IF NOT EXISTS — re-rodar é no-op.
-- =============================================================================

ALTER TABLE senderzz_portal_users
    ADD COLUMN IF NOT EXISTS document varchar(32);

ALTER TABLE senderzz_portal_users
    ADD COLUMN IF NOT EXISTS phone varchar(32);
