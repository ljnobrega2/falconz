-- 468-enable-saque-v2.sql
-- Habilita o SAQUE V2 do painel (produtor COD) — pedido do dono ("é pra funcionar").
--
-- O handler go/portal wallet.go::Withdraw é fail-closed: só libera o saque quando a
-- option `senderzz_dashboard_v2_withdraw_enabled` = 'yes'; ausente/'no' → 403
-- "Saque V2 não habilitado. Utilize o painel clássico." (era o estado: option não
-- existia). Os demais guards (subconta, conta PIX do dono, mínimo R$10, lock anti
-- double-spend, saldo disponível, status 'analysis' p/ revisão) seguem valendo.
--
-- Idempotente e NÃO-destrutivo: ON CONFLICT DO NOTHING — só semeia 'yes' quando a
-- option não existe; se o dono depois setar 'no' manualmente, o migrate-runner NÃO
-- reverte (não força 'yes' a cada run).
INSERT INTO senderzz_options (name, value)
VALUES ('senderzz_dashboard_v2_withdraw_enabled', 'yes')
ON CONFLICT (name) DO NOTHING;
