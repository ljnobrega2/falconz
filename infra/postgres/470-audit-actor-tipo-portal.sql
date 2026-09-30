-- =============================================================================
-- 470-audit-actor-tipo-portal.sql — relaxa o CHECK de sz_motoboy_audit.actor_tipo
-- para aceitar os atores de PORTAL ('produtor','afiliado').
--
-- MOTIVO (SEC-RBAC-MOTOBOY-MUTATIONS-2026-06-24): as mutações de pedido motoboy do
-- admin (reagendar / cancelar / reagendar-clone) passaram a aceitar TOKEN DE PORTAL
-- (produtor/afiliado) com ownership-gate. O audit (sz_motoboy_audit) agora grava
-- actor_tipo = 'admin' | 'produtor' | 'afiliado' conforme o ator autenticado.
--
-- O CHECK original (030-motoboy.sql) só permitia ('sistema','alan','motoboy','admin').
-- Sem este relaxamento, todo INSERT de auditoria com actor_tipo='produtor'/'afiliado'
-- é REJEITADO pelo CHECK e — como o INSERT é best-effort (erro engolido nos handlers)
-- — a linha de auditoria é SILENCIOSAMENTE descartada (perda de trilha de auditoria).
--
-- OBS: o go/portal JÁ escreve 'produtor'/'afiliado' nesta tabela hoje
-- (go/portal motoboy_portal.go / orders_mutations.go via writeMotoboyAuditPortal);
-- portanto este fix também corrige um bug LATENTE de auditoria silenciosamente
-- descartada que já existia em produção para as mutações do PORTAL.
--
-- Idempotente: DROP IF EXISTS + recria. Apenas DDL de constraint — não toca dados.
-- =============================================================================

ALTER TABLE sz_motoboy_audit
  DROP CONSTRAINT IF EXISTS sz_motoboy_audit_actor_tipo_check;

ALTER TABLE sz_motoboy_audit
  ADD CONSTRAINT sz_motoboy_audit_actor_tipo_check
  CHECK (actor_tipo IN ('sistema','alan','motoboy','admin','produtor','afiliado'));
