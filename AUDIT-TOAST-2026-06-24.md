# Auditoria — Toast lateral temático (`.szv2-toasts`) em saves/mutações

> 2026-06-24. Pergunta do dono: "ao salvar comissão e **qualquer pequena alteração** deve lançar o
> pop up lateral no tema do site". Audita onde já dispara vs onde falta.
> Método: 1 agente por arquivo (62 arquivos, 171 mutações) classificando sucesso/erro de cada ação.

## O que conta como "pop up lateral no tema"

Stack fixo top-right `.szv2-toasts` (component `ToastHost`), alimentado por `hooks/useToast.ts`:
- dentro de componente: `const showToast = useToast()` → `showToast(kind, msg)` (portal nomeia `toast`).
- fora do React: `emitToast(kind, msg)`. `kind ∈ 'ok' | 'err' | 'warn' | 'info'`.

**Mecanismo paralelo que NÃO é isso**: várias páginas admin definem um `showToast` LOCAL que pinta um
banner inline `sz-alert-success/danger` no topo do card — visível, mas **não** é o toast lateral. São o
maior balde de "converter".

## Placar

| | arquivos | mutações |
|---|---|---|
| ✅ Já no toast lateral (sucesso+erro) | 38 | maioria |
| ⚠️ Sucesso SILENCIOSO (sem confirmação) | — | 7 + 2 checkout |
| 🔵 Banner inline local (não-lateral) | ~10 arq | ~16 |
| ⏭️ Redirect/navegação confirma (login/reset/signup) | — | ok, manter |
| ⏭️ `info` honesto (backend não persiste) | AffiliateRules | manter |
| ⛔ checkout-ui sem sistema de toast | 1 | 2 |

---

## ⚠️ A — Sucesso SILENCIOSO (prioridade alta)

Nenhuma confirmação visível no sucesso (só refetch / flip otimista de checkbox). Erro já tratado.

### A1 — toast lateral JÁ no arquivo, só faltou 1 linha (trivial)
| Arquivo | Linha | Ação | Fix |
|---|---|---|---|
| `admin-ui/pages/Cds.tsx` | 45 | toggle ativar/desativar CD | `showToast('ok', ativo?'CD ativado.':'CD desativado.')` |
| `portal-ui/pages/Affiliates.tsx` | 348 | toggle auto-aprovação afiliado | `toast('ok', next?'Auto-aprovação ligada.':'…desligada.')` |
| `portal-ui/pages/Settings.tsx` | 562 | toggle preferência de notificação | `toast('ok','Preferência salva.')` |

### A2 — arquivo sem `useToast` importado (silencioso de verdade)
| Arquivo | Linha | Ação | Hoje |
|---|---|---|---|
| `admin-ui/pages/Motoboys.tsx` | 218 | excluir/desativar motoboy | só `load()` refetch — badge muda, sem aviso |
| `admin-ui/pages/Pix.tsx` | 149 | confirmar/cancelar recarga (botões da linha) | só refetch, badge muda |
| `admin-ui/pages/Products.tsx` | 203 | sincronizar produtos do histórico | sucesso>0 silencioso (só `synced===0` mostra banner) |
| `admin-ui/pages/Products.tsx` | 273 | excluir produto | só refetch, linha some |

→ Precisa importar `useToast` (ou `emitToast`) e disparar no sucesso.

### A3 — checkout-ui (decisão do dono)
`checkout-ui` **não tem sistema de toast nenhum**. Mutações:
| Arquivo | Linha | Ação | Hoje |
|---|---|---|---|
| `checkout-ui/src/api.ts` | 495 | `postConsent` (aceite LGPD, best-effort) | silencioso por design |
| `checkout-ui/src/api.ts` | 726 | `createOrder` (finaliza pedido COD) | sucesso → navega p/ ThankYou; erro → throw (caller exibe) |

→ `createOrder` já confirma via página ThankYou. Falta: feedback de **erro** padronizado. Adicionar
infra de toast no checkout é decisão de escopo (tela do cliente final).

---

## 🔵 B — Banner inline local em vez do toast lateral (converter p/ consistência)

Dão confirmação visível, mas via `sz-alert` inline / `setMsg` / `flashMsg` — não o lateral temático.

| Arquivo | Linhas | Ações | Mecanismo atual |
|---|---|---|---|
| `admin-ui/pages/BulkActions.tsx` | 357, 429 | gerar etiquetas ME / motoboy | `showToast` LOCAL → banner `sz-alert` |
| `admin-ui/pages/Orders.tsx` | 499, 520, 573 | reagendar / clonar frustrado / cancelar | `showToast` LOCAL → banner `sz-alert` |
| `admin-ui/pages/Zonas.tsx` | 151 | salvar edição de zona | `showToast` LOCAL → banner `sz-alert` |
| `admin-ui/pages/CodWalletProducer.tsx` | 414, 439, 816 | salvar regras/overrides / liberar saldo | `showToast` LOCAL → banner `sz-alert` |
| `admin-ui/pages/Pix.tsx` | 160 | verificar PIX pendentes | `setVerifyMsg` → banner `sz-alert-success` |
| `admin-ui/pages/Settings.tsx` | 47 | salvar configurações do sistema | `setMsg` → '✓' inline |
| `admin-ui/pages/Producers.tsx` | 139 | salvar dados do produtor | `setSavedOk` → 'Salvo ✓' inline |
| `admin-ui/pages/Products.tsx` | 258, 260 | criar/editar produto | fecha form + refetch (sem toast) |
| `portal-ui/pages/Freight.tsx` | 102, 118 | salvar modalidades / bloqueios | `flashMsg` → inline colorido |

> `MotoboyCarteira.tsx:482` e `Wallet.tsx:198/322` **parecem** locais mas o `onSuccess` do pai chama o
> `showToast` SHARED → já cobertos (lateral). Não mexer.

---

## ⏭️ C — Manter como está (confirmação por outro caminho válido)

- **Login/2FA/Signup/Forgot** (`admin-ui/pages/Login.tsx`, `portal-ui/pages/Login.tsx`),
  **`ResetPassword.tsx`**, **`OnboardingSetup.tsx`**: sucesso = redirect / troca de etapa / card
  full-screen. Toast seria redundante.
- **`portal-ui/components/CookieConsent.tsx`**: sucesso = banner some. Ok.
- **`AffiliateRules.tsx`** (reward/taxa frustração): usa `info` de propósito porque o backend ainda
  não persiste — honesto, não trocar por 'salvo'.

---

## Recomendação de execução

1. **A1** (3 toggles, toast já no arquivo) — 1 linha cada. Imediato.
2. **A2** (4 ações: importar `useToast` + disparar) — Motoboys/Pix/Products.
3. **B** (~16 ações em ~9 arquivos) — trocar `showToast` local / `setMsg` por `useToast` lateral.
   Padroniza o site inteiro no pop-up lateral. Pode manter o banner inline OU substituir.
4. **A3 checkout-ui** — decisão do dono (infra de toast na tela do cliente).

⚠️ Itens em `portal-ui` (Affiliates, Settings, Freight) tocam **portal de role** — confirmar antes.
