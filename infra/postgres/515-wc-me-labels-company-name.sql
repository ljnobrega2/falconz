-- 515-wc-me-labels-company-name.sql
--
-- AUDIT-2026-07-28 (dono): coluna "Transportadora" mostrava o SERVIÇO (PAC,
-- Express, .Package, .Package Centralizado) em vez da EMPRESA (Correios,
-- Jadlog) — wc_me_labels só guardava service_name, nunca a company da ME
-- (ME.ServiceOption.Company.Name, distinto de Name/serviço). Idempotente.
ALTER TABLE wc_me_labels ADD COLUMN IF NOT EXISTS company_name VARCHAR(100);
