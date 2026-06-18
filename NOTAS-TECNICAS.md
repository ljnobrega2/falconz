# Senderzz — Notas Técnicas e Dívidas de Código

**Última atualização:** 2026-04-30

---

## Resumo

A análise detalhada do codebase identificou **2 itens de dívida técnica real** em comentários de código. Os 142 matches do grep original eram quase todos strings CSS/JS dentro de arquivos PHP (ex: `remove()`, `TODOS os rates`) — não comentários de dívida.

---

## DT-CODE-01 — Workaround: Mini Envios desabilitados (ID 17)

**Arquivo:** `src/Methods/Method.php` — linha 209  
**Tipo:** Workaround com código comentado  

```php
// workaround: desligar mini envios, muito bugado e restrito
if ( 17 === intval( $method->id ) ) {
//   continue;
}
```

O `continue` está comentado, o que significa que o workaround **não está ativo** — mini envios (método ID 17 do ME) estão sendo incluídos normalmente. O comentário é um lembrete de que esse método já foi problemático.

**Ação recomendada:** Testar se Mini Envios (Correios Mini Envios) está funcionando corretamente com os clientes atuais. Se não for usado ou se ainda apresentar bugs, reativar o `continue` e remover o comentário morto.

---

## DT-CODE-02 — Webhook de tracking dispara para TODOS os webhooks do OL (MVP)

**Arquivo:** `src/Webhook/Tracking_Webhook.php` — linha 218  
**Tipo:** Simplificação de MVP que pode precisar de refinamento em escala  

```php
// Para simplificar no MVP, dispara para TODOS webhooks ativos vinculados ao class owner
```

O comportamento atual dispara o webhook de tracking para todos os webhooks ativos do operador logístico dono do pedido. Isso é correto para a maioria dos casos, mas em escala pode gerar disparos desnecessários se o OL tiver webhooks configurados para sistemas diferentes (ex: um webhook para ERP e outro para sistema de rastreio).

**Ação recomendada (futuro):** Adicionar um campo `event_types` na tabela `senderzz_webhooks` para filtrar por tipo de evento. Por enquanto o comportamento atual é adequado.

---

## Notas de Qualidade (não são bugs)

### Portal_Page.php — Arquivo monolítico (5.834 linhas)

O arquivo mistura PHP server-side, HTML, CSS e ~3.000 linhas de JavaScript. Funciona corretamente, mas é difícil de auditar e manter. 

**Caminho para refatoração:**
1. Extrair o JavaScript para `tpc-assets/js/portal.js` (padrão já adotado para `painel.js` e `my-account.js`)
2. Mover templates HTML para `templates/portal/`
3. Manter o PHP de lógica em `Portal_Page.php`

Não urgente — fazer na próxima janela de refatoração de maior porte.

---

## O que NÃO é dívida (confirmado como intencional)

- **`// CRIT-03: fail-closed`** — comentário de segurança documenta decisão de design, não dívida.
- **`// SEC-02: usar wrapper SSRF-safe`** — comentário de patch de segurança aplicado.
- **`// Idempotência: se referencia já existe`** — documentação inline de lógica de negócio.
- **`// SELECT FOR UPDATE`** — comentário documenta uso intencional de locking.
- **Strings `remove()` em JS inline** — JavaScript legítimo, não comentário de dívida.
- **Strings `TODOS os rates` em comentários PT-BR** — documentação funcional em português.
