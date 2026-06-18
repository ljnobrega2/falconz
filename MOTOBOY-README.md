# Senderzz — Módulo Motoboy

## Arquivos do módulo

| Arquivo | Função |
|---------|--------|
| `includes/motoboy/database.php` | 7 tabelas SQL: CDs, zonas, CEPs, motoboys, pedidos, fechamento, auditoria |
| `includes/motoboy/router.php` | Roteamento CEP→CD→Zona→Motoboy, geofence automático (Haversine) |
| `includes/motoboy/rest-api.php` | 15 endpoints REST: login, lote, GPS, entregar, frustrar, fechamento, dashboard Alan, zona-cep, link-expedicao |
| `includes/motoboy/admin.php` | Painel WP: Dashboard, Pedidos, Motoboys, CDs & Zonas, Configurações |
| `includes/motoboy/routes.php` | URLs `/motoboy-app/` e `/rastreio-motoboy/` via rewrite rules |
| `includes/motoboy/motoboy-producer.php` | Toggle por produtor, taxas configuráveis, método de frete WC, bloqueio ME |
| `includes/motoboy/checkout-link-motoboy.php` | Link motoboy + expedição, seletor de data, status agendado, bloqueio CEP |
| `includes/motoboy/wc-translations-ptbr.php` | Traduções PT-BR de erros WooCommerce/FunnelKit |
| `templates/motoboy/pwa.php` | PWA do motoboy (login, lote, GPS, assinatura, foto, fechamento) |
| `templates/motoboy/tracking.php` | Tracking público por pedido |

## Fluxo do pedido motoboy

```
Cliente acessa link /?sz=TOKEN&frete=motoboy
         ↓
CEP verificado → cobertura confirmada
         ↓
Data de entrega selecionada (3 dias úteis + sábado)
         ↓
Pedido criado → status: AGENDADO (automático)
         ↓
Alan muda para EMBALADO (valida motoboy)
         ↓
Motoboy dá OK geral → EM ROTA (automático)
         ↓
Geofence 500m → A CAMINHO (automático, cron 1 min)
         ↓
Motoboy confirma: CPF + assinatura + pagamento
         ↓
ENTREGUE ✅  ou  FRUSTRADO ❌ (foto + geo)
         ↓
Fechamento de caixa diário (PIX/cartão → Senderzz, dinheiro → motoboy repassa)
```

## Configurações necessárias (WP Admin)

1. **WooCommerce → 🏍️ Motoboy → CDs & Zonas** — cadastrar CDs, zonas e faixas de CEP
2. **WooCommerce → 🏍️ Motoboy → Motoboys** — cadastrar motoboys
3. **WooCommerce → 🏍️ Motoboy → Configurações** — taxas e geofence
4. **WooCommerce → Configurações → Entrega → Zona** — adicionar método "🏍️ Motoboy Senderzz"
5. **Usuários → [produtor] → Editar** — ativar motoboy e configurar taxas por produtor

## URLs públicas

- PWA Motoboy: `https://app.senderzz.com.br/motoboy-app/`
- Tracking: `https://app.senderzz.com.br/rastreio-motoboy/?pedido=ORDER_ID`
- REST API: `https://app.senderzz.com.br/wp-json/sz-motoboy/v1/`

## Endpoints REST

| Método | Endpoint | Auth | Função |
|--------|----------|------|--------|
| POST | `/login` | público | Login motoboy via telefone |
| GET | `/motoboy/lote` | token | Pedidos do dia |
| POST | `/motoboy/iniciar-rota` | token | OK geral → em rota |
| POST | `/motoboy/ping` | token | Atualiza GPS |
| POST | `/motoboy/entregar` | token | Confirma entrega |
| POST | `/motoboy/frustrar` | token | Registra frustração |
| GET | `/motoboy/fechamento` | token | Resumo do dia |
| POST | `/motoboy/confirmar-repasse` | token | Confirma PIX do dinheiro |
| GET | `/alan/pedidos` | WP admin | Todos pedidos ativos |
| POST | `/alan/embalar` | WP admin | Muda para embalado |
| POST | `/alan/confirmar-fechamento` | WP admin | Confirma fechamento |
| GET | `/alan/dashboard` | WP admin | Métricas gerais |
| GET | `/tracking/{order_id}` | público | Tracking do pedido |
| GET | `/zona-cep?cep=` | público | Verifica cobertura |
| GET | `/link-expedicao?sz=` | público | URL link de expedição |

## Cron necessário (cPanel)

```
* * * * * wget -q -O /dev/null https://app.senderzz.com.br/wp-cron.php?doing_wp_cron
```

E no `wp-config.php`:
```php
define('DISABLE_WP_CRON', true);
```
