# RUNBOOK — Senderzz Logistics

Operações de manutenção e emergência em produção.

---

## 1. Rotação de secrets

### tpc_webhook_secret e tpc_jwt_secret

Auto-gerados na primeira visita admin se ausentes. Para rotacionar manualmente:

```bash
# Via WP-CLI
wp option delete tpc_webhook_secret
wp option delete tpc_jwt_secret
# Na próxima visita admin, serão regerados automaticamente.

# Ou via env vars (prefere env sobre option):
# Adicionar em wp-config.php ou .env:
# define('SENDERZZ_WEBHOOK_SECRET', 'novo-secret-aqui');
# define('SENDERZZ_JWT_SECRET', 'novo-jwt-secret-aqui');
```

**Após rotacionar:**
- Atualizar todos webhooks configurados por clientes (URL não muda, só o secret de verificação)
- Reiniciar workers PHP se usar PHP-FPM com opcache

---

## 2. Reconciliação PIX manual

O cron `sz_pix_auto_reconcile_cron` roda a cada 5 minutos. Para forçar manualmente:

```bash
wp cron event run sz_pix_auto_reconcile_cron
```

Para ver o histórico das últimas reconciliações:
```bash
wp option get sz_pix_reconcile_last_run
```

Logs detalhados em:
```
wp-content/uploads/wc-logs/sz-pix-reconcile-YYYY-MM-DD-*.log
```

**Se cron parou (admin notice laranja):**
1. Verificar se WP-Cron está habilitado: `wp cron test`
2. Se `DISABLE_WP_CRON=true` em wp-config.php, garantir cron de sistema:
   ```bash
   # crontab -e
   */5 * * * * wget -q -O /dev/null https://seusite.com.br/wp-cron.php?doing_wp_cron
   # ou
   */5 * * * * wp --path=/var/www/html cron event run --due-now
   ```
3. Verificar lock preso: `wp transient delete sz_pix_reconcile_lock`

---

## 3. PIX: divergência saldo ME vs saldo interno

Se `sz_pix_reconcile_last_run['result']['errors']` não vazio:

```bash
# Ver erros da última reconciliação
wp option get sz_pix_reconcile_last_run --format=json | python3 -m json.tool

# Forçar reconciliação e ver saída
wp eval 'sz_pix_reconcile_run(); print_r(get_option("sz_pix_reconcile_last_run"));'
```

**Checklist de diagnóstico:**
- Token ME válido: `wp option get tpc_me_token` (deve ser != 'env-managed' se não usa env)
- Saldo ME via API: `GET https://melhorenvio.com.br/api/v2/me/balance` com token
- Recargas pendentes na tabela: `wp db query "SELECT * FROM wp_tpc_recargas WHERE status='analise'"`

---

## 4. Carteira COD: saque travado

Se saque em `analysis` por mais de X dias:

```bash
# Ver saques pendentes
wp db query "SELECT id, user_id, amount, net, status, created_at FROM wp_sz_cod_withdrawals WHERE status IN ('analysis','pending') ORDER BY created_at ASC"

# Aprovar manualmente (substituir ID)
wp db query "UPDATE wp_sz_cod_withdrawals SET status='paid', paid_at=NOW() WHERE id=X"
```

---

## 5. Motoboy: slot de webhook não recriado

O cron `senderzz_pw_ensure_user_webhook_slots` roda a cada 10 min. Se slots sumiram:

```bash
wp cron event run senderzz_pw_ensure_user_webhook_slots
```

---

## 6. Rate limits do portal: restaurar para produção

**ATENÇÃO:** Limites estão em valores de dev? Verificar:

```bash
grep -n "ip_max\|email_max\|attempts >=" \
  includes/senderzz-security-patches.php \
  src/Portal/Portal_Auth.php
```

Valores corretos para produção:
- `ip_max`: 10 (por 5 min)
- `email_max`: 5 (por 5 min)
- login step1: `>= 5`
- 2FA: `>= 3`
- password reset: `>= 3` (já correto)

---

## 7. Secret regenerado em produção (detectar)

Se admin ver aviso de "secret regenerado", um deploy sobrescreveu options. Verificar:

```bash
# Secrets devem existir e não ser strings genéricas
wp option get tpc_webhook_secret | wc -c  # deve ser > 40 chars
wp option get tpc_jwt_secret | wc -c
```

Se strings curtas ou default → rodar o procedimento do item 1.

---

## 8. Sessões portal vazias / login quebrado

```bash
# Limpar sessões expiradas manualmente
wp db query "DELETE FROM wp_senderzz_portal_sessions WHERE expires_at < NOW()"

# Ver sessões ativas
wp db query "SELECT user_id, COUNT(*) as sessions FROM wp_senderzz_portal_sessions WHERE expires_at > NOW() GROUP BY user_id LIMIT 20"
```

---

## 9. Labels PDF: Python branding pulado

O branding de etiqueta requer Python + PyMuPDF. Se admin notice aparece:

```bash
which python3
pip3 show pymupdf

# Instalar
pip3 install pymupdf
```

Se exec() desabilitado no PHP (`disable_functions`), branding é pulado silenciosamente — etiquetas funcionam sem branding.

---

## 10. Diagnóstico rápido (checklist de deploy)

```bash
# 1. Cron PIX ativo
wp option get sz_pix_reconcile_last_run | grep -o '"at":"[^"]*"'

# 2. Secrets ok
wp option get tpc_webhook_secret | wc -c

# 3. Token ME ok  
wp option get tpc_me_token

# 4. Rate limits produção
grep "ip_max" includes/senderzz-security-patches.php

# 5. Webhook secret não vazio
wp option get tpc_webhook_secret

# 6. Sessões portal (tabela existe)
wp db query "SELECT COUNT(*) FROM wp_senderzz_portal_sessions"

# 7. DB version TPC
wp option get tpc_db_version
```

---

## 11. Migração off-WordPress (quando iniciar)

Ordem: Motoboy → TPC Wallet/PIX → Affiliates/COD → Labels → Portal SPA → Checkout → desligar WP.

**Primeiro passo (Motoboy):**
1. Provisionar Postgres 16 + Redis em paralelo ao WP
2. Adicionar nginx/Caddy como gateway na frente
3. Migrar tabelas `sz_motoboy_*` com pgloader
4. Implementar 41 rotas `/sz-motoboy/v1/*` em Go
5. Redirecionar rotas no nginx: `/wp-json/sz-motoboy/v1/*` → Go
6. Double-write 30 dias, reconcile diário
7. Cutover: desligar rotas WP

Ver `ROADMAP-V2.md` Fase 4 para detalhes completos.
