-- 517-labels-quotes-snapshot.sql
--
-- AUDIT-2026-07-30 (dono): "ajusta para mostrar a que realmente foi emitida e
-- sempre usar a cotação do momento da solicitação do pedido" — GetFreightQuotes
-- (freight_quotes.go) recotava a ME AO VIVO (preço de HOJE) e marcava
-- "selecionado" = mais barata disponível HOJE, o que diverge do que foi
-- realmente emitido no passado (preços/disponibilidade da ME mudam dia a dia).
-- Passa a persistir o snapshot completo das cotações no momento da emissão.
ALTER TABLE wc_me_labels ADD COLUMN IF NOT EXISTS quotes_snapshot JSONB;
