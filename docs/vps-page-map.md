# VPS Page Map

Snapshot based on the current VPS-aligned code in this workspace.
Goal: identify canonical pages, safe merges, and redirect candidates without
changing runtime behavior yet.

## Scope

- `portal-ui`: customer/produtor/afiliado/operator portal
- `admin-ui`: internal admin/ops console
- `checkout-ui`: public checkout + tracking

## Canonical Shape

### `portal-ui`

Role-aware app. It already consolidates some historical duplicates:

- `Orders` is the canonical orders screen for portal roles.
- `/motoboy` is a deep-link alias to `Orders`.
- Menu is role-scoped in `portal-ui/src/components/Layout.tsx`.

### `admin-ui`

Backoffice app with the widest surface. It already uses hubs and redirects to
reduce duplication.

### `checkout-ui`

Public-only flow. Keep isolated.

## Route Counts

- `portal-ui`: 24 routes
- `admin-ui`: 69 routes
- `checkout-ui`: 4 routes

## Current Route Map

### `portal-ui`

- Auth: `/login`
- Base: `/`, `/dashboard`, `/orders`, `/motoboy`, `/motoboys-dia`, `/expedicao`
- Vendas: `/products`, `/stock`, `/vitrine`, `/affiliates`, `/links`
- Financeiro: `/wallet`, `/wallet-expedition`, `/label-credits`
- Plataforma/config: `/webhooks`, `/reports`, `/integrations`, `/freight`, `/localidades`, `/users`, `/settings`, `/support`

### `admin-ui`

- Público/auth: `/login`, `/reset`, `/setup`
- Hub principal: `/`, `/faturamento`, `/usuarios`, `/administracao`, `/carteira-cod`, `/carteira-expedicao`, `/sistema`
- Logística: `/cds`, `/zonas`
- Expedição ME: `/labels`
- Produtos: `/products`, `/products-approval`, `/stock`, `/checkout-links`
- Operação comercial: `/producers`, `/clientes`, `/operators-users`, `/admins`, `/affiliates`, `/commissions`, `/onboarding-requests`, `/document-changes`
- Pedidos: `/orders`, `/orders/:id`
- Motoboy: `/motoboys`, `/motoboys-dia`, `/motoboy-config`, `/motoboy-dashboard`, `/motoboy-carteira`, `/motoboy-fechamento`, `/motoboy-comprovantes`, `/motoboy-saques`, `/motoboy-custodia`, `/motoboy-conciliacao`, `/motoboy-mapa`
- Financeiro COD / TPC: `/cod-livro`, `/cod-saques`, `/cod-taxas`, `/config-taxas`, `/cod-wallet-producer`, `/cod-wallet-transactions`, `/pix`, `/tpc-clientes`, `/tpc-transacoes`, `/tpc-config`
- Sistema / suporte / auditoria: `/logs`, `/tools`, `/maintenance`, `/crons`, `/audit-log`, `/api-docs`, `/push-tecnico`, `/capabilities`, `/pwa-config`, `/support`

### `checkout-ui`

- `/`
- `/:token`
- `/rastreio/:code`
- `/pedido/:code`

## Already-Consolidated

These are good examples of the pattern we want to keep:

- `portal-ui`: `/motoboy` -> `Orders`
- `admin-ui`: `/users` -> `/usuarios`
- `admin-ui`: `/pix` -> `/carteira-expedicao?tab=pix`
- `admin-ui`: `/audit` -> `/sistema?tab=auditoria`
- `admin-ui`: `/affiliates-wallet` -> `/usuarios?tab=afiliados&sub=carteira`
- `admin-ui`: `/affiliate-rules` -> `/usuarios?tab=afiliados&sub=regras`
- `admin-ui`: `/etiquetas-lote`, `/motoboy-etiquetas`, `/bulk-actions` -> `/orders`

## Safe Merge Candidates

These are the highest-value consolidations because they reduce duplicated UI
without changing the product boundary.

### Portal

1. `Orders` as the single canonical screen for order/motoboy flow.
2. `portal-ui` finance pages can be grouped into one hub:
   - `Wallet`
   - `WalletExpedition`
   - `LabelCredits`
3. `portal-ui` platform/support pages can be grouped into one hub:
   - `Webhooks`
   - `Integrations`
   - `Freight`
   - `Localidades`
   - `Users`
   - `Settings`
   - `Support`

### Admin

1. `UsuariosHub` can absorb:
   - `/producers`
   - `/clientes`
   - `/operators-users`
   - `/admins`
   - `/affiliates`
   - `/commissions`
   - `/onboarding-requests`
   - `/document-changes`
   - redirects already in place
2. `CarteiraCodHub` can absorb:
   - `/wallet`
   - `/cod-livro`
   - `/cod-saques`
   - `/cod-taxas`
   - `/config-taxas`
   - `/cod-wallet-producer`
   - `/cod-wallet-transactions`
   - `/pix`
3. `CarteiraExpedicaoHub` can absorb:
   - `/carteira-expedicao`
   - `/tpc-clientes`
   - `/tpc-transacoes`
   - `/tpc-config`
4. `LogisticaHub` can absorb:
   - `/cds`
   - `/zonas`
5. `ExpedicaoMEHub` can absorb:
   - `/labels`
   - `/expedicao-integracoes`
   - `/expedicao-webhooks`
   - `/tracking-brand`
6. `ProdutosHub` can absorb:
   - `/products`
   - `/products-approval`
   - `/stock`
   - `/checkout-links`
7. `SistemaHub` can absorb:
   - `/logs`
   - `/tools`
   - `/maintenance`
   - `/crons`
   - `/audit-log`
   - `/api-docs`
   - `/push-tecnico`
   - `/capabilities`
   - `/pwa-config`
   - `/support`
8. `Motoboy` pages can be reduced to a single hub for internal ops:
   - `/motoboys`
   - `/motoboys-dia`
   - `/motoboy-config`
   - `/motoboy-dashboard`
   - `/motoboy-carteira`
   - `/motoboy-fechamento`
   - `/motoboy-comprovantes`
   - `/motoboy-saques`
   - `/motoboy-custodia`
   - `/motoboy-conciliacao`
   - `/motoboy-mapa`

### Checkout

- No merge target. Keep isolated.

## Pages That Should Not Be Merged Across Apps

- `checkout-ui` with backoffice apps
- `portal-ui` with `admin-ui` as a single deployment
- `OrderDetail` with list screens before validating all deep-link flows
- Motoboy operational screens with portal customer-facing screens

## Safe Redirect Candidates

These are the next low-risk cuts after hub consolidation:

- `portal-ui` legacy duplicates of `Orders`
- `admin-ui` standalone route copies that only mirror a hub tab
- any item removed from the sidebar but still valid as deep-link

## Recommended Cut Order

1. Freeze the canonical hubs and route ownership.
2. Move duplicate standalone pages behind redirects.
3. Remove sidebar items that now duplicate hub tabs.
4. Fold repeated permission checks into a single `role`/`capabilities` layer.
5. Rebuild and smoke test the affected container only.

## Safety Notes

- Prefer redirects over deletions for one release cycle.
- Keep deep-links working until bookmarks and operator habits are confirmed.
- Back up the target file before each VPS deploy.
- Validate each cut with a build and a quick navigation smoke test.

