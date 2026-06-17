-- =============================================================================
-- Senderzz — Seed default das classes de envio (v460)
--
-- A tabela senderzz_shipping_classes foi criada vazia via schema-fixes-v460.sql.
-- Várias páginas (BulkActions, Expedição) dependem dela estar populada.
-- Este seed popula 5 classes default e é idempotente (ON CONFLICT DO NOTHING).
-- =============================================================================

INSERT INTO senderzz_shipping_classes (slug, name, description, active) VALUES
    ('padrao',      'Padrão',      'Classe padrão sem especificações',                TRUE),
    ('fragil',      'Frágil',      'Itens frágeis com embalagem reforçada',           TRUE),
    ('grande',      'Grande',      'Volumes grandes / acima de 30cm',                 TRUE),
    ('refrigerado', 'Refrigerado', 'Produtos que precisam de refrigeração',           TRUE),
    ('perigoso',    'Perigoso',    'Itens com restrições logísticas (líquidos, etc)', TRUE)
ON CONFLICT (slug) DO NOTHING;
