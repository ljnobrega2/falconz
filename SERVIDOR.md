# Senderzz Logistics — Requisitos de Servidor

**Versão do plugin:** 2.5.45  
**Última atualização:** 2026-04-30

---

## Requisitos Mínimos

| Componente | Versão mínima | Observação |
|---|---|---|
| PHP | 8.1 | Requer `union types`, `fibers` não usados |
| WordPress | 6.0 | HPOS (High-Performance Order Storage) suportado desde 6.1 |
| WooCommerce | 7.0 | Testado até 9.3.1 |
| MySQL / MariaDB | 8.0 / 10.4 | Requer `FOR UPDATE` em InnoDB — MyISAM **não funciona** |
| HTTPS | Obrigatório | Webhooks PIX e Melhor Envio exigem endpoint HTTPS |

---

## Extensões PHP Obrigatórias

```
php-curl        → chamadas à API do Melhor Envio e provedores PIX
php-json        → serialização de payloads
php-mbstring    → strings UTF-8 nos nomes de remetentes/destinatários
php-openssl     → HMAC SHA-256 para assinaturas de webhook
php-pdo         → não usada diretamente, mas requerida por dependências
php-intl        → formatação de valores monetários (opcional mas recomendada)
```

Verificar no servidor:
```bash
php -m | grep -E "curl|json|mbstring|openssl"
```

---

## Dependências de Sistema (Funcionalidades Opcionais)

As dependências abaixo **não são obrigatórias** para o funcionamento principal do plugin. Quando ausentes, o sistema opera em modo degradado com aviso no painel admin.

### Python 3 + PyMuPDF + Pillow

**Para que serve:** branding de etiquetas — sobrepõe o logo Senderzz sobre as etiquetas PDF do Melhor Envio antes da impressão.

**O que acontece se ausente:** etiquetas são entregues com a marca Melhor Envio. Um aviso laranja aparece no painel admin uma vez por hora. O fluxo de emissão e impressão **não é interrompido**.

**Instalação (Ubuntu/Debian):**
```bash
apt-get install python3 python3-pip
pip3 install pymupdf pillow
```

**Verificar:**
```bash
python3 -c "import fitz, PIL; print('OK')"
```

**Hospedagens que geralmente têm:** VPS própria, servidores dedicados, Cloudways, Spinupwp.  
**Hospedagens que geralmente NÃO têm:** WP Engine, Kinsta, Flywheel (managed WordPress).

> **Alternativa para hospedagem managed:** mova o processamento de PDF para um microserviço externo. Veja a seção [Microserviço de Branding](#microservi%C3%A7o-de-branding-alternativa) abaixo.

---

### Ghostscript (`gs`)

**Para que serve:** fusão de múltiplas etiquetas PDF em um único arquivo para impressão em lote A4.

**O que acontece se ausente:** a impressão em lote usa a lib PHP FPDI (já incluída no `vendor/`) como fallback. O resultado é funcionalmente equivalente, mas pode ser ligeiramente mais lento para lotes grandes (>50 etiquetas).

**Instalação (Ubuntu/Debian):**
```bash
apt-get install ghostscript
```

**Verificar:**
```bash
gs --version
```

---

### `exec()` PHP habilitado

**Para que serve:** executar o script Python de branding de etiquetas via linha de comando.

**O que acontece se ausente:** o branding de etiquetas é silenciosamente ignorado (aviso no admin). Tudo mais funciona normalmente.

**Como verificar:**
```bash
php -r "var_dump(function_exists('exec'));"
# true = habilitado
```

**Como habilitar (se seu provedor permitir):** no `php.ini`, remover `exec` da diretiva `disable_functions`.

---

## Configuração de Banco de Dados

O plugin cria automaticamente as tabelas abaixo na ativação. Todas requerem **InnoDB** para suporte a transações (`FOR UPDATE`).

| Tabela | Uso |
|---|---|
| `wp_tpc_carteira` | Saldo da carteira por usuário |
| `wp_tpc_transacoes` | Ledger de movimentações financeiras |
| `wp_tpc_recargas` | Recargas PIX pendentes/confirmadas |
| `wp_tpc_webhook_events` | Idempotência de webhooks (dedup por event_key) |
| `wp_senderzz_portal_users` | Usuários do portal de OLs |
| `wp_senderzz_portal_sessions` | Sessões ativas do portal |
| `wp_senderzz_portal_2fa` | Códigos 2FA temporários |
| `wp_senderzz_webhooks` | Webhooks de saída cadastrados pelos OLs |
| `wp_senderzz_webhook_log` | Log de disparos de webhook |

Se as tabelas não forem criadas automaticamente, desative e reative o plugin. Logs de erro ficarão em `wp-content/debug.log` se `WP_DEBUG_LOG` estiver ativo.

---

## Variáveis de Configuração (wp-options)

| Option name | Descrição | Como configurar |
|---|---|---|
| `tpc_webhook_secret` | Secret HMAC para webhooks PIX (≥ 32 chars) | Gerado automaticamente; copiar do aviso admin para o painel ME |
| `tpc_jwt_secret` | Secret para JWT do portal de OLs | Gerado automaticamente na primeira visita ao admin |
| `woocommerce_wc-melhor-envio_settings[token]` | Token OAuth do Melhor Envio | Configurar em WooCommerce → Configurações → Frete → Melhor Envio |

---

## Checklist de Deploy

```
[ ] PHP 8.1+ com extensões curl, json, mbstring, openssl
[ ] MySQL 8+ ou MariaDB 10.4+ com InnoDB
[ ] HTTPS válido (não self-signed) no domínio
[ ] Ativar o plugin → verificar aviso do webhook_secret no admin
[ ] Configurar tpc_webhook_secret no painel do Melhor Envio (Webhooks)
[ ] Configurar token OAuth ME em WooCommerce → Frete → Melhor Envio
[ ] (Opcional) Instalar python3 + pymupdf + pillow para branding de etiquetas
[ ] (Opcional) Instalar ghostscript para impressão em lote
[ ] Confirmar que exec() está habilitado se quiser branding de etiquetas
[ ] Rodar os testes: cd plugin/ && phpunit (requer PHP CLI + PHPUnit 9)
```

---

## Microserviço de Branding (Alternativa)

Para quem usa hospedagem managed que bloqueia `exec()` e Python, o processamento de etiquetas pode ser delegado a um microserviço externo:

**Opção recomendada:** Render.com (plano gratuito suficiente para < 500 etiquetas/dia)

1. Criar um serviço web em `render.com` com o script `includes/senderzz_clean_label.py` exposto via Flask/FastAPI
2. Configurar a URL do serviço em `wp-options` como `senderzz_branding_service_url`
3. O plugin chama o serviço via `wp_remote_post` em vez de `exec()`

> Esta integração não está implementada na versão atual — é uma sugestão de roadmap para escalar em hospedagens managed.

---

## Headers de Segurança HTTP

Configurar no nginx/Apache em produção. O plugin não seta esses headers — responsabilidade do servidor.

### nginx (dentro de `server {}`)

```nginx
# HSTS — força HTTPS por 1 ano, inclui subdomínios
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;

# Evita clickjacking
add_header X-Frame-Options "SAMEORIGIN" always;

# Bloqueia MIME sniffing
add_header X-Content-Type-Options "nosniff" always;

# Referrer para requisições cross-origin
add_header Referrer-Policy "strict-origin-when-cross-origin" always;

# CSP — ajustar src conforme domínios usados (ME, qrserver, WhatsApp embed)
add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://api.qrserver.com https://melhorenvio.com.br; connect-src 'self' https://melhorenvio.com.br; frame-ancestors 'none';" always;

# Permissions Policy — desabilitar features não usadas
add_header Permissions-Policy "camera=(), microphone=(), geolocation=(self)" always;
```

> **Nota `unsafe-inline`:** WP e o portal V2 têm `<script>` inline nos templates. Para remover `unsafe-inline`, migrar todos os scripts inline para arquivos `.js` (D6/D7 em andamento). Até lá, `unsafe-inline` é necessário.

> **PWA motoboy (`/motoboy-app/`):** usa câmera (BarcodeDetector) e geolocalização (GPS ping). Ajustar `Permissions-Policy` para `camera=(self), geolocation=(self)` nesse path.

### Verificar headers em produção

```bash
curl -I https://seusite.com.br | grep -E "Strict|X-Frame|X-Content|Content-Security|Referrer"
```

---

## Suporte e Diagnóstico

**Logs do plugin:** `wp-content/debug.log` (com `WP_DEBUG=true` e `WP_DEBUG_LOG=true`)  
**Prefixos de log:** `[tpc_*]` para financeiro/PIX, `[senderzz_*]` para rastreio/etiquetas  
**Teste de webhooks:** `POST /wp-json/senderzz/v1/webhooks/{id}/test` (requer auth de OL)  
**Painel de diagnóstico:** WooCommerce → Senderzz → Status (lista tabelas, saldos, eventos recentes)
