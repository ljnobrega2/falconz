// Afiliação — port fiel de templates/portal/v2/sections/affiliates.php.
// Painel do PRODUTOR: Pendentes / Aprovados / Minha afiliação / Configurações
// (aprovar/recusar, comissão por vínculo, comissão padrão, aprovação automática,
//  links de convite). VISÃO AFILIADO: suas afiliações + links de venda.
//
// Backend (go/portal, namespace /wp-json/senderzz/v1) — inferido das ações
// sz_aff_panel_action do WP, convenção de path /portal/affiliates:
//   GET    /portal/affiliates                    — payload completo (escopo por sessão)
//   POST   /portal/affiliates/{id}/approve       — aprovar afiliado (produtor)
//   POST   /portal/affiliates/{id}/reject        — recusar afiliado (produtor)
//   POST   /portal/affiliates/{id}/commission    — { commission_pct } por vínculo
//   DELETE /portal/affiliates/{id}               — excluir afiliado
//   POST   /portal/affiliates/default-commission — { commission_pct } padrão
//   POST   /portal/affiliates/auto-approve       — { enabled } aprovação automática
//
// #71: o link de convite/indicação saiu desta tela (card "Convite de associado"
// movido p/ Perfil ▸ aba "Convite de associado" — Settings.tsx, via
// GET /portal/affiliates/referral). Endpoints /affiliates/invites não são mais
// consumidos por este componente.
//
// Ownership / role escopados pela sessão do portal (auth.FromContext): produtor
// vê os afiliados dele (producer_id = portal.id), afiliado vê os vínculos dele
// (afiliado = wp_user_id). Comissão padrão e aprovar/recusar = PRODUTOR.
// Taxa de saque é global/admin — não aparece aqui.
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { PortalMe } from '../components/Layout'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import SectionLoading from '../components/SectionLoading'
import AlertError from '../components/AlertError'
import { dt, brl } from '../utils/format'

// ── Tipos ─────────────────────────────────────────────────────────────────────

type AffiliateRow = {
  id: number
  name: string
  email: string
  status: string // pending | active
  commission_pct: number
  created_at: string
  approved_at?: string | null
}

type MyAffiliationRow = {
  id: number
  producer_name: string
  status: string
  commission_pct: number
  created_at: string
  approved_at?: string | null
}

type AffLink = {
  name: string
  url: string
  commission_pct: number
  // FEAT-AFF-PRODUCT-VIEW (2026-06-24): preço da oferta + base_name ("{qtd} {produto}",
  // sem estágio) p/ agrupar por produto, filtrar por quantidade e mostrar valor/comissão.
  display_value: number
  base_name: string
}

type AffiliatesResp = {
  ok: boolean
  pending?: AffiliateRow[]
  approved?: AffiliateRow[]
  default_commission_pct?: number
  auto_approve?: boolean
  per_affiliate_override?: boolean
  my_affiliations?: MyAffiliationRow[]
  my_links?: AffLink[]
}

type ViewTab = 'pending' | 'approved' | 'minhas' | 'config'

// ── Helpers de exibição (espelham number_format / wp_date do PHP) ───────────────

/** "12,00" — duas casas, vírgula decimal, sem separador de milhar (igual ao PHP). */
function pct2(v: number): string {
  return (Number.isFinite(v) ? v : 0).toFixed(2).replace('.', ',')
}

/** "12.00" para o value do input number (ponto decimal). */
function pctInput(v: number): string {
  return (Number.isFinite(v) ? v : 0).toFixed(2)
}

/**
 * Nome de exibição do afiliado com fallback robusto:
 *   COALESCE(nome, email, 'Afiliado #'+id).
 * O backend (affiliates_portal.go) já colapsa o LEFT JOIN sem match (afiliado fora
 * de senderzz_portal_users) para o literal '—' (em-dash) — que é truthy, então
 * `name || '—'` no render NÃO o detecta. Aqui tratamos '—' (e '-') como ausente,
 * caímos para o email e, por fim, para 'Afiliado #{id}', para nunca exibir só o
 * traço numa linha de afiliado real.
 */
function affName(row: { name?: string; email?: string; id: number }): string {
  const isBlank = (s: string) => !s || s === '—' || s === '-'
  const n = (row.name ?? '').trim()
  if (!isBlank(n)) return n
  const e = (row.email ?? '').trim()
  if (!isBlank(e)) return e
  return `Afiliado #${row.id}`
}

// ── Badge de status — port fiel de sz_v2_status_badge (status-badge.php) ─────────
// Mesma marcação <span class="sz-badge szv2-badge-{variante}" data-status="{slug}">
// e mesmo mapa slug → [variante, rótulo]. Slugs ausentes (ex.: "active") caem no
// fallback neutral + ucfirst — idêntico ao PHP.
const STATUS_BADGE_MAP: Record<string, [string, string]> = {
  aprovado: ['info', 'Aprovado'],
  agendado: ['info', 'Agendado'],
  processing: ['info', 'Processando'],
  separado: ['warning', 'Separado'],
  embalado: ['warning', 'Embalado'],
  coletado: ['warning', 'Coletado'],
  emretirada: ['warning', 'Em retirada'],
  acaminho: ['brand', 'A caminho'],
  a_caminho: ['brand', 'A caminho'],
  em_rota: ['brand', 'Em rota'],
  'em-rota': ['brand', 'Em rota'],
  emrota: ['brand', 'Em rota'],
  enviado: ['brand', 'Enviado'],
  entregue: ['success', 'Entregue'],
  completed: ['success', 'Concluído'],
  completo: ['success', 'Concluído'],
  frustrado: ['danger', 'Frustrado'],
  failed: ['danger', 'Falhou'],
  saldoinsuficiente: ['danger', 'Saldo insuf.'],
  cancelled: ['neutral', 'Cancelado'],
  cancelado: ['neutral', 'Cancelado'],
  emcancelamento: ['neutral', 'Em cancelamento'],
  devolvido: ['neutral', 'Devolvido'],
  avariado: ['neutral', 'Avariado'],
  asuspender: ['neutral', 'A suspender'],
  refunded: ['neutral', 'Reembolsado'],
  'on-hold': ['warning', 'Em espera'],
  pending: ['neutral', 'Pendente'],
  pendente: ['warning', 'Pendente'],
}

function ucfirst(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s
}

function StatusBadge({ status }: { status: string }) {
  let slug = (status || '').toLowerCase().trim()
  if (slug.startsWith('wc-')) slug = slug.slice(3)
  const item = STATUS_BADGE_MAP[slug] ?? ['neutral', ucfirst(slug.replace(/-/g, ' '))]
  return (
    <span className={`sz-badge szv2-badge-${item[0]}`} data-status={slug}>
      {item[1]}
    </span>
  )
}

// ── Lista de links de venda (reusada na visão afiliado e na aba "Minha afiliação") ─

// offerQty — quantidade à frente do nome ("3 Datalaprox" → 3). Espelha Products.tsx
// (filtro de quantidade do produtor). Sem número à frente → null (grupo "Outros").
function offerQty(name: string): number | null {
  const m = /^\s*(\d+)\b/.exec(name || '')
  return m ? parseInt(m[1], 10) : null
}

// productOf — nome do PRODUTO a partir do base_name ("{qtd} {produto}", migração 471):
// remove a quantidade da frente ("1 Datalaprox" → "Datalaprox"). Fallback p/ o name.
// É o ÚNICO sinal confiável de produto nos links do afiliado (post_id = canal, não
// produto; e o vínculo do afiliado é produto-agnóstico — produto_id=0).
function productOf(lk: { base_name?: string; name?: string }): string {
  const src = (lk.base_name || lk.name || '').trim()
  const stripped = src.replace(/^\s*\d+\s+/, '').trim()
  return stripped || 'Produto'
}

// AffLinksList — visão rica dos links de venda do afiliado (espelha a tela Produtos
// do produtor): submenu por PRODUTO + filtro por QUANTIDADE + valor do link + valor
// da comissão (= preço × %, BRUTA, consistente com a Vitrine).
function AffLinksList({ links }: { links: AffLink[] }) {
  const toast = useToast()
  // Guard defensivo: caller sempre passa array, mas blindamos contra shape
  // divergente do backend para nunca quebrar o render (.map sobre não-array).
  const safeLinks = Array.isArray(links) ? links : []

  function copy(url: string) {
    if (!url) return
    navigator.clipboard?.writeText(url).then(
      () => toast('ok', 'Link copiado.'),
      () => toast('err', 'Não foi possível copiar o link.'),
    )
  }

  // Agrupa por PRODUTO (base_name sem a quantidade). Ordem = 1ª aparição (estável).
  const products = useMemo(() => {
    const order: string[] = []
    const map = new Map<string, AffLink[]>()
    for (const lk of safeLinks) {
      const p = productOf(lk)
      if (!map.has(p)) {
        map.set(p, [])
        order.push(p)
      }
      map.get(p)!.push(lk)
    }
    return order.map(name => ({ name, links: map.get(name)! }))
  }, [safeLinks])

  // Aba de produto ativa (submenu) + filtro de quantidade da aba + busca por nome.
  const [activeProd, setActiveProd] = useState<string>('')
  const active = products.find(p => p.name === activeProd) || products[0] || null
  const [qtyFilter, setQtyFilter] = useState<number | null>(null)
  // Troca de produto zera o filtro (espelha Products.tsx — evita filtro "preso").
  useEffect(() => {
    setQtyFilter(null)
  }, [active?.name])

  if (!active) return <div className="szv2-aff-links-list" />

  const prodLinks = active.links
  const qtys = Array.from(
    new Set(prodLinks.map(l => offerQty(l.name)).filter((q): q is number => q != null)),
  ).sort((a, b) => a - b)
  const hasOutros = prodLinks.some(l => offerQty(l.name) == null)
  const filteredByQty =
    qtyFilter == null
      ? prodLinks
      : qtyFilter === -1
      ? prodLinks.filter(l => offerQty(l.name) == null)
      : prodLinks.filter(l => offerQty(l.name) === qtyFilter)
  const visible = filteredByQty

  return (
    <div>
      {/* Submenu de PRODUTOS (abas) — sempre visível quando há produtos. */}
      <div className="szv2-prod-tabs" role="tablist" style={{ marginBottom: 12 }}>
        {products.map(p => {
          const on = p.name === active.name
          return (
            <button
              key={p.name}
              type="button"
              role="tab"
              aria-selected={on}
              className={`szv2-prod-tab${on ? ' szv2-prod-tab--active' : ''}`}
              onClick={() => setActiveProd(p.name)}
            >
              {p.name}
            </button>
          )
        })}
      </div>

      {/* Nome do produto ativo */}
      <h3 style={{ fontSize: 16, fontWeight: 700, margin: '0 0 12px', color: 'var(--szv2-text)' }}>
        {active.name}
      </h3>

      {/* Filtro por QUANTIDADE — igual ao do produtor: sem "Todos", sem contadores;
          clicar de novo no chip ativo limpa. Só quando há mais de uma quantidade. */}
      {(qtys.length > 1 || (qtys.length >= 1 && hasOutros)) && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center', marginBottom: 12 }}>
          <span style={{ fontSize: 12, color: 'var(--szv2-text-faint)', marginRight: 2 }}>Quantidade:</span>
          {[
            ...qtys.map(q => ({ key: `q${q}`, label: `${q}`, val: q as number | null })),
            ...(hasOutros ? [{ key: 'outros' as const, label: 'Outros', val: -1 as number | null }] : []),
          ].map(chip => {
            const on = qtyFilter === chip.val
            return (
              <button
                key={chip.key}
                type="button"
                onClick={() => setQtyFilter(on ? null : chip.val)}
                style={{
                  padding: '4px 11px',
                  borderRadius: 999,
                  fontSize: 12.5,
                  lineHeight: 1.4,
                  cursor: 'pointer',
                  whiteSpace: 'nowrap',
                  border: `1px solid ${on ? 'var(--szv2-brand)' : 'var(--szv2-border, #d4d9e0)'}`,
                  background: on ? 'var(--szv2-brand)' : 'transparent',
                  color: on ? '#fff' : 'var(--szv2-text)',
                  fontWeight: on ? 700 : 500,
                }}
              >
                {chip.label}
              </button>
            )
          })}
        </div>
      )}

      {/* Linhas — agora com VALOR do link + VALOR da comissão (preço × %, bruta). */}
      <div className="szv2-aff-links-list">
        {visible.map((lk, i) => {
          const commValue =
            lk.display_value > 0 && lk.commission_pct > 0 ? (lk.display_value * lk.commission_pct) / 100 : 0
          return (
            <div className="szv2-aff-link-row" key={i}>
              <div className="szv2-aff-link-info">
                <span className="szv2-aff-link-name">{lk.name || '—'}</span>
                <span className="szv2-link-info-label">
                  {lk.display_value > 0 && <>Valor: {brl(lk.display_value)}</>}
                  {commValue > 0 ? (
                    <>
                      {lk.display_value > 0 ? ' · ' : ''}Comissão: <strong>{brl(commValue)}</strong>
                      {lk.commission_pct > 0 ? ` (${pct2(lk.commission_pct)}%)` : ''}
                    </>
                  ) : (
                    lk.commission_pct > 0 && <>{lk.display_value > 0 ? ' · ' : ''}{pct2(lk.commission_pct)}% comissão</>
                  )}
                </span>
              </div>
              {lk.url ? (
                <button
                  type="button"
                  className="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                  title="Copiar link de venda"
                  onClick={() => copy(lk.url)}
                >
                  Copiar link
                </button>
              ) : (
                <span className="szv2-text-faint-val">Link indisponível</span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ── Página ──────────────────────────────────────────────────────────────────────

export default function Affiliates() {
  const toast = useToast()

  const [me, setMe] = useState<PortalMe | null>(null)
  const [data, setData] = useState<AffiliatesResp | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const [tab, setTab] = useState<ViewTab>('pending')

  // estado por-linha das ações de aprovação/recusa/exclusão
  const [busy, setBusy] = useState<Record<number, boolean>>({})

  // edição de comissão por vínculo (id -> valor string do input)
  const [commDraft, setCommDraft] = useState<Record<number, string>>({})

  // configurações editáveis
  const [autoApprove, setAutoApprove] = useState(false)
  const [perAffOverride, setPerAffOverride] = useState(false)
  const [defaultComm, setDefaultComm] = useState('0.00')
  const [savingDefault, setSavingDefault] = useState(false)

  const role = (me?.role || 'cliente').toLowerCase()
  // /portal/me devolve role cru do banco — senderzz_portal_users.role usa
  // 'produtor' (CHECK produtor|afiliado|operator|cliente). ALLOWLIST least-privilege
  // (casa Layout.tsx): SÓ produtor vê o chrome de gestão de afiliados (Pendentes/
  // Aprovados/Configurações). Qualquer outro papel (afiliado, cliente, operator,
  // role desconhecido/vazio) cai na VISÃO AFILIADO — que renderiza apenas a própria
  // afiliação ("Minha afiliação" + links de venda). Denylist por isAffiliate vazaria
  // os tabs de produtor p/ cliente/operator.
  const isProducer = role === 'produtor' || role === 'producer'
  // WP: $sz7af_can_act = !is_aff && !is_sub (subconta também não age). PortalMe não
  // expõe parent_user_id, então a checagem de subconta NÃO é replicada aqui — o
  // backend (ownership por sessão) ainda barra mutações indevidas. Só restringimos
  // por role (apenas produtor aprova/edita/exclui).
  const canAct = isProducer

  function load() {
    setLoading(true)
    setErr('')
    api<AffiliatesResp>('/portal/affiliates')
      .then(r => {
        setData(r)
        setAutoApprove(!!r.auto_approve)
        setPerAffOverride(!!r.per_affiliate_override)
        setDefaultComm(pctInput(r.default_commission_pct || 0))
        const draft: Record<number, string> = {}
        ;(Array.isArray(r.approved) ? r.approved : []).forEach(a => {
          draft[a.id] = pctInput(a.commission_pct ?? r.default_commission_pct ?? 0)
        })
        setCommDraft(draft)
      })
      .catch(e => setErr(e.message || 'Erro ao carregar afiliados'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    api<PortalMe>('/portal/me').then(setMe).catch(() => {})
    load()
  }, [])

  // Resiliência: TODO acesso a array do backend passa por Array.isArray (nunca
  // .map sobre null/undefined ou objeto truthy não-array → ErrorBoundary/tela
  // branca). O handler go/portal sempre materializa [] em cada chave, mas o guard
  // fecha o buraco teórico de shape divergente (conta NOVA / 200-mas-vazio).
  const pending = Array.isArray(data?.pending) ? data!.pending! : []
  const approved = Array.isArray(data?.approved) ? data!.approved! : []
  const myAffiliations = Array.isArray(data?.my_affiliations) ? data!.my_affiliations! : []
  const myLinks = Array.isArray(data?.my_links) ? data!.my_links! : []

  // ── Ações: aprovar / recusar ──────────────────────────────────────────────────

  async function affAction(id: number, action: 'approve' | 'reject', name: string) {
    if (action === 'reject') {
      const ok = await confirmAsync({
        title: 'Recusar afiliado',
        message: `Tem certeza que deseja recusar ${name}?`,
        confirmLabel: 'Recusar',
        danger: true,
      })
      if (!ok) return
    }
    setBusy(b => ({ ...b, [id]: true }))
    try {
      await api(`/portal/affiliates/${id}/${action}`, { method: 'POST' })
      toast('ok', action === 'approve' ? 'Afiliado aprovado.' : 'Afiliado recusado.')
      load()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao processar afiliado.')
      setBusy(b => ({ ...b, [id]: false }))
    }
  }

  // ── Ação: salvar comissão por vínculo ─────────────────────────────────────────

  async function saveCommission(id: number) {
    const raw = commDraft[id]
    const pct = parseFloat(raw)
    if (!Number.isFinite(pct) || pct < 0 || pct > 100) {
      toast('err', 'Informe uma comissão entre 0 e 100.')
      return
    }
    setBusy(b => ({ ...b, [id]: true }))
    try {
      await api(`/portal/affiliates/${id}/commission`, {
        method: 'POST',
        body: JSON.stringify({ commission_pct: pct }),
      })
      toast('ok', 'Comissão atualizada.')
      // reflete localmente
      setData(prev =>
        prev
          ? { ...prev, approved: (prev.approved || []).map(a => (a.id === id ? { ...a, commission_pct: pct } : a)) }
          : prev,
      )
    } catch (e: any) {
      toast('err', e.message || 'Erro ao salvar comissão.')
    } finally {
      setBusy(b => ({ ...b, [id]: false }))
    }
  }

  // ── Ação: excluir afiliado ────────────────────────────────────────────────────

  async function deleteAffiliate(id: number, name: string) {
    const ok = await confirmAsync({
      title: 'Excluir afiliado',
      message: `Tem certeza que deseja excluir ${name}? Esta ação não pode ser desfeita.`,
      confirmLabel: 'Excluir',
      danger: true,
    })
    if (!ok) return
    setBusy(b => ({ ...b, [id]: true }))
    try {
      await api(`/portal/affiliates/${id}`, { method: 'DELETE' })
      toast('ok', 'Afiliado excluído.')
      setData(prev => (prev ? { ...prev, approved: (prev.approved || []).filter(a => a.id !== id) } : prev))
    } catch (e: any) {
      toast('err', e.message || 'Erro ao excluir.')
      setBusy(b => ({ ...b, [id]: false }))
    }
  }

  // ── Ação: aprovação automática ────────────────────────────────────────────────

  async function toggleAuto(next: boolean) {
    setAutoApprove(next) // otimista
    try {
      await api('/portal/affiliates/auto-approve', {
        method: 'POST',
        body: JSON.stringify({ enabled: next }),
      })
      toast('ok', next ? 'Auto-aprovação ativada.' : 'Auto-aprovação desativada.')
    } catch (e: any) {
      setAutoApprove(!next) // reverte
      toast('err', e.message || 'Erro ao alterar aprovação automática.')
    }
  }

  // ── Ação: override de comissão por afiliado (toggle GLOBAL) ────────────────────

  async function togglePerAffOverride(next: boolean) {
    setPerAffOverride(next) // otimista
    try {
      await api('/portal/affiliates/per-affiliate-override', {
        method: 'POST',
        body: JSON.stringify({ enabled: next }),
      })
      toast('ok', next ? 'Override por afiliado ativado.' : 'Override por afiliado desativado.')
    } catch (e: any) {
      setPerAffOverride(!next) // reverte
      toast('err', e.message || 'Erro ao alterar override de comissão.')
    }
  }

  // ── Ação: salvar comissão padrão ──────────────────────────────────────────────

  async function saveDefaultComm() {
    const pct = parseFloat(defaultComm)
    if (!Number.isFinite(pct) || pct < 0 || pct > 100) {
      toast('err', 'Informe uma comissão entre 0 e 100.')
      return
    }
    setSavingDefault(true)
    try {
      await api('/portal/affiliates/default-commission', {
        method: 'POST',
        body: JSON.stringify({ commission_pct: pct }),
      })
      toast('ok', 'Comissão padrão salva.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao salvar.')
    } finally {
      setSavingDefault(false)
    }
  }

  // #71: as ações de link de convite (createInvite/copyInvite/revokeInvite) saíram
  // desta tela junto com o card — o convite agora é o link de indicação fixo em
  // Perfil ▸ "Convite de associado" (Settings.tsx).

  // ── Loading / erro ────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <section id="sec-affiliates" className="sz-sec" aria-busy="true">
        {/* #68 — título "Afiliação" presente em TODOS os branches (loading + afiliado +
            produtor), idêntico, p/ a tela não "piscar": sem ele aqui, o cabeçalho
            apareceria só depois do fetch e empurraria o conteúdo (flicker reverso).
            Texto casa o label da nav (Layout.tsx: affiliates → 'Afiliação'). */}
        <div className="szv2-page-head" style={{ marginBottom: 16 }}>
          <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
            Afiliação
          </h2>
        </div>
        <SectionLoading label="Carregando afiliados…" />
      </section>
    )
  }

  // ════════════════════════════════════════════════════════════════════════════
  // VISÃO NÃO-PRODUTOR (afiliado / cliente / operator) — apenas a PRÓPRIA afiliação
  // + links de venda. Sem sub-abas de gestão (Pendentes/Aprovados/Configurações):
  // essas são exclusivas do produtor que gerencia seus afiliados.
  // ════════════════════════════════════════════════════════════════════════════
  if (!isProducer) {
    return (
      <section id="sec-affiliates" className="sz-sec">
        {/* #68 — título da tela (mesmo posicionamento dos demais branches). */}
        <div className="szv2-page-head" style={{ marginBottom: 16 }}>
          <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
            Afiliação
          </h2>
          <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Acompanhe suas afiliações e copie seus links de venda para divulgar.
          </p>
        </div>

        {err && <AlertError message={err} onRetry={load} />}

        {/* #71 — o card "Convite de associado" (link de indicação fixo) MOVIDO para
            Perfil ▸ aba "Convite de associado" (Settings.tsx) — pedido do dono. */}

        {myAffiliations.length === 0 ? (
          <EmptyState
            title="Nenhuma afiliação ativa"
            description="Aguarde a aprovação do produtor para começar a promover os produtos dele e ganhar comissão."
          />
        ) : (
          <>
            {/* PRODUTOS em primeiro lugar (pedido do dono): submenu por produto no
                lugar da tabela de afiliações. As afiliações viram resumo abaixo. */}
            {myLinks.length > 0 ? (
              <div className="szv2-card">
                <div className="szv2-card-head">
                  <h2>Produtos para promover</h2>
                  <p className="szv2-card-sub">
                    Escolha o produto e copie o link de venda — sua comissão já vai embutida.
                  </p>
                </div>
                <AffLinksList links={myLinks} />
              </div>
            ) : (
              <EmptyState
                title="Nenhum produto liberado ainda"
                description="Assim que o produtor liberar as ofertas, seus links de venda aparecem aqui, organizados por produto."
              />
            )}

            {/* Card "Suas afiliações" (produtores que aprovaram o afiliado + comissão)
                removido a pedido do dono (2026-06-25): não é necessário na visão do afiliado. */}
          </>
        )}
      </section>
    )
  }

  // ════════════════════════════════════════════════════════════════════════════
  // VISÃO PRODUTOR — tabs Pendentes / Aprovados / Minha afiliação / Configurações
  // ════════════════════════════════════════════════════════════════════════════
  return (
    <section id="sec-affiliates" className="sz-sec">
      {/* #68 — título da tela (mesmo posicionamento dos demais branches). */}
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Afiliação
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Gerencie o programa de afiliados dos seus produtos: aprove parceiros, defina a comissão e
          acompanhe as vendas geradas por indicação.
        </p>
      </div>

      {err && <AlertError message={err} onRetry={load} />}

      {/* Tabs */}
      <div className="szv2-dash-switcher" role="tablist" aria-label="Visão de afiliados">
        <button
          type="button"
          className={`szv2-dash-tab ${tab === 'pending' ? 'szv2-dash-tab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'pending'}
          onClick={() => setTab('pending')}
        >
          Pendentes
          {pending.length > 0 && <span className="szv2-aff-count-badge">{pending.length}</span>}
        </button>
        <button
          type="button"
          className={`szv2-dash-tab ${tab === 'approved' ? 'szv2-dash-tab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'approved'}
          onClick={() => setTab('approved')}
        >
          Aprovados
        </button>
        <button
          type="button"
          className={`szv2-dash-tab ${tab === 'minhas' ? 'szv2-dash-tab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'minhas'}
          onClick={() => setTab('minhas')}
        >
          Minha afiliação
        </button>
        <button
          type="button"
          className={`szv2-dash-tab ${tab === 'config' ? 'szv2-dash-tab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'config'}
          onClick={() => setTab('config')}
        >
          Configurações
        </button>
      </div>

      {/* ── Painel: Pendentes ──────────────────────────────────────────────── */}
      {tab === 'pending' && (
        <div className="szv2-dash-panel">
          {pending.length === 0 ? (
            <EmptyState
              title="Nenhum afiliado pendente"
              description="Novos pedidos de afiliação aparecem aqui."
            />
          ) : (
            <div className="szv2-card szv2-card-table">
              <div className="szv2-table-wrap szv2-table-flush">
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Nome</th>
                      <th>E-mail</th>
                      <th>Solicitação</th>
                      <th className="szv2-aff-actions-col">Ação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pending.map(p => (
                      <tr key={p.id} id={`szv2-aff-row-${p.id}`}>
                        <td>{affName(p)}</td>
                        <td>{p.email || '—'}</td>
                        <td>{dt(p.created_at)}</td>
                        <td className="szv2-aff-actions">
                          {canAct && (
                            <>
                              <button
                                type="button"
                                className="szv2-btn szv2-btn-sm szv2-btn-brand szv2-aff-action-btn"
                                disabled={!!busy[p.id]}
                                onClick={() => affAction(p.id, 'approve', affName(p))}
                              >
                                Aprovar
                              </button>
                              <button
                                type="button"
                                className="szv2-btn szv2-btn-sm szv2-btn-danger szv2-aff-action-btn"
                                disabled={!!busy[p.id]}
                                onClick={() => affAction(p.id, 'reject', affName(p))}
                              >
                                Recusar
                              </button>
                            </>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ── Painel: Aprovados ──────────────────────────────────────────────── */}
      {tab === 'approved' && (
        <div className="szv2-dash-panel">
          {approved.length === 0 ? (
            <EmptyState
              title="Nenhum afiliado aprovado ainda"
              description="Os afiliados aprovados aparecem aqui."
            />
          ) : (
            <div className="szv2-card szv2-card-table">
              <div className="szv2-table-wrap szv2-table-flush">
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Nome</th>
                      <th>E-mail</th>
                      <th className="szv2-td-num">Comissão %</th>
                      <th>Desde</th>
                      {canAct && (
                        <>
                          <th className="szv2-aff-actions-col">Comissão</th>
                          <th style={{ textAlign: 'right' }}>Excluir</th>
                        </>
                      )}
                    </tr>
                  </thead>
                  <tbody>
                    {approved.map(a => (
                      <tr key={a.id} id={`szv2-aff-row-${a.id}`}>
                        <td>{affName(a)}</td>
                        <td>{a.email || '—'}</td>
                        <td className="szv2-td-num szv2-num">{pct2(a.commission_pct)}%</td>
                        <td>{dt(a.approved_at || a.created_at)}</td>
                        {canAct && (
                          <>
                            <td>
                              <div className="szv2-aff-comm-row">
                                <input
                                  type="number"
                                  className="szv2-input szv2-aff-comm-input"
                                  value={commDraft[a.id] ?? pctInput(a.commission_pct)}
                                  min={0}
                                  max={100}
                                  step={0.01}
                                  onChange={e => setCommDraft(d => ({ ...d, [a.id]: e.target.value }))}
                                />
                                <button
                                  type="button"
                                  className="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-aff-comm-save"
                                  disabled={!!busy[a.id]}
                                  onClick={() => saveCommission(a.id)}
                                >
                                  Salvar
                                </button>
                              </div>
                            </td>
                            <td style={{ textAlign: 'right' }}>
                              <button
                                type="button"
                                className="szv2-btn szv2-btn-sm szv2-btn-danger"
                                disabled={!!busy[a.id]}
                                onClick={() => deleteAffiliate(a.id, affName(a))}
                              >
                                Excluir
                              </button>
                            </td>
                          </>
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Widget "Repasse mensal por afiliado" REMOVIDO a pedido do dono (2026-06-23). */}

      {/* ── Painel: Minha afiliação (produtor como afiliado de outros) ──────── */}
      {tab === 'minhas' && (
        <div className="szv2-dash-panel">
          {myAffiliations.length === 0 ? (
            <EmptyState
              title="Nenhuma afiliação"
              description="Acesse a Vitrine para se afiliar a produtos disponíveis."
            />
          ) : (
            <>
              <div className="szv2-card" style={{ marginBottom: 'var(--szv2-space-4)' }}>
                <div className="szv2-card-head">
                  <h2>Meus vínculos como afiliado</h2>
                </div>
                <div className="szv2-table-wrap szv2-table-flush">
                  <table className="szv2-table">
                    <thead>
                      <tr>
                        <th>Produtor</th>
                        <th>Status</th>
                        <th className="szv2-td-num">Comissão</th>
                        <th>Desde</th>
                      </tr>
                    </thead>
                    <tbody>
                      {myAffiliations.map(mar => (
                        <tr key={mar.id}>
                          <td>{mar.producer_name || '—'}</td>
                          <td>
                            <StatusBadge status={mar.status} />
                          </td>
                          <td className="szv2-td-num szv2-num">{pct2(mar.commission_pct)}%</td>
                          <td>{dt(mar.approved_at || mar.created_at)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>

              {myLinks.length > 0 && (
                <div className="szv2-card">
                  <div className="szv2-card-head">
                    <h2>Links de venda disponíveis</h2>
                  </div>
                  <AffLinksList links={myLinks} />
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* ── Painel: Configurações ──────────────────────────────────────────── */}
      {tab === 'config' && (
        <div className="szv2-dash-panel">
          <div className="szv2-card">
            <div className="szv2-card-head">
              <h2>Configurações do programa de afiliados</h2>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
              {/* Aprovação automática */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '14px 0',
                  borderBottom: '1px solid var(--szv2-divider)',
                }}
              >
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>
                    Aprovação automática
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    Novos afiliados são aprovados automaticamente ao se cadastrar.
                  </div>
                </div>
                <label className="szv2-toggle-lbl">
                  <input
                    type="checkbox"
                    checked={autoApprove}
                    onChange={e => toggleAuto(e.target.checked)}
                  />
                  <span className="szv2-toggle-slider" />
                </label>
              </div>

              {/* Comissão padrão */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '14px 0',
                  borderBottom: '1px solid var(--szv2-divider)',
                }}
              >
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>
                    Comissão padrão dos afiliados
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                    Aplicada automaticamente quando nenhuma comissão específica é definida no checkout.
                  </div>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <input
                    type="number"
                    className="szv2-input szv2-input-sm"
                    value={defaultComm}
                    min={0}
                    max={100}
                    step={0.01}
                    onChange={e => setDefaultComm(e.target.value)}
                    style={{ width: 80, textAlign: 'right' }}
                  />
                  <span style={{ fontSize: 13, color: 'var(--szv2-text-muted)' }}>%</span>
                  <button
                    type="button"
                    className="szv2-btn szv2-btn-brand szv2-btn-sm"
                    disabled={savingDefault}
                    onClick={saveDefaultComm}
                  >
                    {savingDefault ? 'Salvando…' : 'Salvar'}
                  </button>
                </div>
              </div>

              {/* Override de comissão por afiliado — sz_aff_per_affiliate_override.
                  Toggle GLOBAL (senderzz_options) LIDO por go/orders (checkout) para
                  resolver a % da venda. Estado vem do GET /portal/affiliates
                  (per_affiliate_override); alterna via POST .../per-affiliate-override.
                  Azul FALK (var(--szv2-info)) para distinguir do toggle laranja de
                  aprovação automática: este é uma EXCEÇÃO ao modelo (a comissão vem da
                  oferta/link), não uma ação do fluxo padrão. */}
              <div
                style={{
                  display: 'flex',
                  alignItems: 'flex-start',
                  justifyContent: 'space-between',
                  gap: 16,
                  padding: '14px 0 4px',
                }}
              >
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>
                    Override de comissão por afiliado
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', lineHeight: 1.5 }}>
                    A comissão é definida pela <strong>oferta / link de checkout</strong> (pré-preenchida com o
                    padrão do produto). O override por-afiliado é uma <strong>exceção</strong>: quando ligado, a %
                    específica do afiliado aprovado tem precedência sobre o link. Quando desligado, o link manda.
                  </div>
                </div>
                <label className="szv2-toggle-lbl" style={{ flexShrink: 0 }}>
                  <input
                    type="checkbox"
                    checked={perAffOverride}
                    onChange={e => togglePerAffOverride(e.target.checked)}
                  />
                  <span
                    className="szv2-toggle-slider"
                    style={perAffOverride ? { background: 'var(--szv2-info)' } : undefined}
                  />
                </label>
              </div>
            </div>
          </div>

          {/* #71 — o card "Convite de associado" (link de indicação fixo /r/{code})
              MOVIDO para Perfil ▸ aba "Convite de associado" (Settings.tsx) — pedido
              do dono. Antes aparecia aqui e na visão afiliado; agora só no perfil. */}
        </div>
      )}
    </section>
  )
}
