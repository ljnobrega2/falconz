import { useEffect, useMemo, useRef, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import FalkSelect from '../components/FalkSelect'

// Tela Senderzz · Notificações PWA — paridade com
// sz_app_pwa_render_notifications_admin() (includes/senderzz-app-pwa.php:397).
// Configura por evento: template (producer/affiliate/admin), status WC,
// destinatários (toggles), admins específicos e flag "incluir número do pedido".
// Salvar tudo dispara 5 POSTs em paralelo.

// ----- tipos do backend -------------------------------------------------------

type EventDef = { key: string; label: string; default_status: string }
type WcStatus = { slug: string; label: string }

type Template = {
  // title/body: campos genéricos de fallback preservados no round-trip para o
  // dispatcher PHP (sz_app_pwa_apply_template linha 713-714). Não expostos na UI.
  title?: string
  body?: string
  producer_title: string
  producer_body: string
  affiliate_title: string
  affiliate_body: string
  admin_title: string
  admin_body: string
}

type Recipients = { producer: number; affiliate: number; admin: number }

type AdminUser = { id: number; nome: string; email: string }

type VarDef = { key: string; label: string; available_for: string[] }

type Role = 'producer' | 'affiliate' | 'admin'

// ----- helpers ----------------------------------------------------------------

const ROLE_LABEL: Record<Role, string> = {
  producer: 'Produtor',
  affiliate: 'Afiliado',
  admin: 'Admin',
}

// Valores fake para preview — espelham o que sz_app_pwa_apply_template injeta.
const PREVIEW_FAKE: Record<string, string> = {
  numero_pedido: '1042',
  pedido_id: '1042',
  cliente: 'Maria Souza',
  valor_total_pedido: 'R$ 189,90',
  total: 'R$ 189,90',
  valor_envio: 'R$ 25,00',
  taxa_entrega_percentual_admin: 'R$ 27,50',
  percentual_envio: '12%',
  comissao_final_afiliado: 'R$ 12,40',
  comissao_final_produtor: 'R$ 110,30',
  comissao: 'R$ 12,40',
  cidade: 'São Paulo',
  produto: 'Suplemento Whey 900g',
  produtos: '1x Suplemento Whey 900g',
  comissao_produtor: 'R$ 110,30',
  comissao_afiliado: 'R$ 12,40',
  comissao_admin_liquida: 'R$ 23,00',
  comissao_admin_liquida_total: 'R$ 46,00',
  comissao_admin: 'R$ 23,00',
  comissao_admin_total: 'R$ 46,00',
  fundo_operacional: 'R$ 2,00',
  taxa_motoboy_admin: 'R$ 18,00',
  status: 'agendado',
  transportadora: 'Correios PAC',
  tipo_entrega: 'Motoboy',
}

// renderPreview substitui {{var}} pelos valores fake (template literal seguro).
function renderPreview(s: string): string {
  return s.replace(/\{\{([a-zA-Z0-9_]+)\}\}/g, (_, k) => PREVIEW_FAKE[k] ?? `{{${k}}}`)
}

// fieldKeys retorna par (title, body) por role.
function fieldKeys(role: Role): { title: keyof Template; body: keyof Template } {
  if (role === 'producer') return { title: 'producer_title', body: 'producer_body' }
  if (role === 'affiliate') return { title: 'affiliate_title', body: 'affiliate_body' }
  return { title: 'admin_title', body: 'admin_body' }
}

// ----- agrupamento de eventos (pedido do dono #71) ----------------------------
// Os eventos vêm do backend Go (eventList) como array plano [key,label,status].
// Aqui apenas BUCKETIZAMOS por chave para renderizar a sidebar em três seções —
// não reshape do estado `events` (a ordem do array Go é load-bearing para o
// desempate de status duplicado em usedStatusesByOther/setStatusForActive).
type EventGroup = 'fulfillment' | 'cod' | 'geral'

const GROUP_LABEL: Record<EventGroup, string> = {
  fulfillment: 'Fulfillment',
  cod: 'Cash on Delivery',
  geral: 'Geral',
}

// Ordem de exibição das seções na sidebar.
const GROUP_ORDER: EventGroup[] = ['fulfillment', 'cod', 'geral']

// Chaves COD (motoboy): agendado/em rota/completo/frustrado + cobrança pendente.
const COD_KEYS = new Set([
  'agendamento_cod',
  'em_rota_cod',
  'completo_cod',
  'frustrado_cod',
  'cobranca_pendente',
])

// Chaves Fulfillment (expedição/Melhor Envio): pedido feito/enviado/entregue/etiqueta.
const FULFILLMENT_KEYS = new Set([
  'pedido_feito',
  'enviado_pad',
  'entregue',
  'label_gerada',
])

// eventGroup classifica uma chave. Catch-all → 'geral' para que um evento
// futuro adicionado no Go nunca suma da sidebar (ficaria inconfigurável).
function eventGroup(key: string): EventGroup {
  if (COD_KEYS.has(key)) return 'cod'
  if (FULFILLMENT_KEYS.has(key)) return 'fulfillment'
  return 'geral' // saldo_baixo, manutencao e qualquer chave nova
}

// LABEL_OVERRIDE — rótulo de exibição na sidebar. Pedido do dono (#77): os eventos
// do grupo COD/Motoboy devem ler EXATAMENTE igual ao menu de Pedidos (Orders.tsx →
// filterStatusLabel + statusLabel). Aqui só muda o LABEL exibido; a chave do evento
// e o status binding (wc-agendado/wc-emrota/wc-completo/wc-frustrado) permanecem
// intactos para não quebrar o desempate de status em statusMap.
//   agendamento_cod → "Agendado"   (Orders: agendado)
//   em_rota_cod     → "A caminho"  (Orders: em_rota → "A caminho", override do dono)
//   completo_cod    → "Entregue"   (Orders: entregue)
//   frustrado_cod   → "Frustrado"  (Orders: frustrado)
// Os status do menu de Pedidos que NÃO têm evento de notificação cadastrado
// (embalado, pre_agendado, cancelado — sem entrada em eventList/wcStatuses no Go)
// não aparecem aqui: "(o que estiver cadastrado)" = só os que existem como evento.
const LABEL_OVERRIDE: Record<string, string> = {
  agendamento_cod: 'Agendado',
  em_rota_cod: 'A caminho',
  completo_cod: 'Entregue',
  frustrado_cod: 'Frustrado',
}

function displayLabel(ev: EventDef): string {
  return LABEL_OVERRIDE[ev.key] ?? ev.label
}

// ----- componente -------------------------------------------------------------

export default function NotificacoesPWA() {
  // catálogo (carregado uma vez)
  const [events, setEvents] = useState<EventDef[]>([])
  const [wcStatuses, setWcStatuses] = useState<WcStatus[]>([])
  const [variables, setVariables] = useState<VarDef[]>([])
  const [availableAdmins, setAvailableAdmins] = useState<AdminUser[]>([])

  // estado editável — chave = event_key
  const [templates, setTemplates] = useState<Record<string, Template>>({})
  const [statusMap, setStatusMap] = useState<Record<string, string>>({})
  const [recipients, setRecipients] = useState<Record<string, Recipients>>({})
  const [adminRecipients, setAdminRecipients] = useState<Record<string, number[]>>({})
  const [orderNumberFlags, setOrderNumberFlags] = useState<Record<string, number>>({})

  // UI state
  const [activeEvent, setActiveEvent] = useState<string>('')
  const [activeTab, setActiveTab] = useState<Role>('producer')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const showToast = useToast() // AUDIT-2026-06-18 Onda3

  // refs para inserir variáveis na posição do cursor.
  const producerBodyRef = useRef<HTMLTextAreaElement | null>(null)
  const affiliateBodyRef = useRef<HTMLTextAreaElement | null>(null)
  const adminBodyRef = useRef<HTMLTextAreaElement | null>(null)

  // ----- load inicial ---------------------------------------------------------

  async function loadAll() {
    setLoading(true)
    setErr('')
    try {
      const [ev, tpl, sm, rc, ar, of, vr] = await Promise.all([
        api<{ items: EventDef[]; wc_statuses: WcStatus[] }>('/notificacoes-pwa/events'),
        api<{ templates: Record<string, Template> }>('/notificacoes-pwa/templates'),
        api<Record<string, string>>('/notificacoes-pwa/status-map'),
        api<{ recipients: Record<string, Recipients> }>('/notificacoes-pwa/recipients'),
        api<{ admin_recipients: Record<string, number[]>; available_admins: AdminUser[] }>('/notificacoes-pwa/admin-recipients'),
        api<Record<string, number>>('/notificacoes-pwa/order-number-flags'),
        api<{ items: VarDef[] }>('/notificacoes-pwa/variables'),
      ])
      setEvents(ev.items || [])
      setWcStatuses(ev.wc_statuses || [])
      setTemplates(tpl.templates || {})
      setStatusMap(sm || {})
      setRecipients(rc.recipients || {})
      setAdminRecipients(ar.admin_recipients || {})
      setAvailableAdmins(ar.available_admins || [])
      setOrderNumberFlags(of || {})
      setVariables(vr.items || [])
      if ((ev.items || []).length && !activeEvent) {
        setActiveEvent(ev.items[0].key)
      }
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadAll() /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [])


  // ----- save -----------------------------------------------------------------

  async function handleSaveAll() {
    setBusy(true)
    try {
      await Promise.all([
        api('/notificacoes-pwa/templates', { method: 'POST', body: JSON.stringify({ templates }) }),
        api('/notificacoes-pwa/status-map', { method: 'POST', body: JSON.stringify(statusMap) }),
        api('/notificacoes-pwa/recipients', { method: 'POST', body: JSON.stringify({ recipients }) }),
        api('/notificacoes-pwa/admin-recipients', { method: 'POST', body: JSON.stringify({ admin_recipients: adminRecipients }) }),
        api('/notificacoes-pwa/order-number-flags', { method: 'POST', body: JSON.stringify(orderNumberFlags) }),
      ])
      showToast('ok', 'Configurações de notificação salvas.')
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setBusy(false)
    }
  }

  // ----- mutators -------------------------------------------------------------

  function updateTemplateField(role: Role, kind: 'title' | 'body', value: string) {
    if (!activeEvent) return
    const fk = fieldKeys(role)
    const key = kind === 'title' ? fk.title : fk.body
    setTemplates(prev => ({
      ...prev,
      [activeEvent]: { ...(prev[activeEvent] ?? emptyTemplate()), [key]: value },
    }))
  }

  function emptyTemplate(): Template {
    return {
      producer_title: '', producer_body: '',
      affiliate_title: '', affiliate_body: '',
      admin_title: '', admin_body: '',
    }
  }

  function getActiveTemplate(): Template {
    return templates[activeEvent] ?? emptyTemplate()
  }

  function getActiveRecipients(): Recipients {
    return recipients[activeEvent] ?? { producer: 1, affiliate: 1, admin: 0 }
  }

  function toggleRecipient(field: keyof Recipients) {
    if (!activeEvent) return
    const cur = getActiveRecipients()
    setRecipients(prev => ({
      ...prev,
      [activeEvent]: { ...cur, [field]: cur[field] ? 0 : 1 },
    }))
  }

  function setStatusForActive(slug: string) {
    if (!activeEvent) return
    // Exclusão mútua client-side: se outro evento já usa este slug, limpa lá.
    const next = { ...statusMap }
    if (slug) {
      for (const ev of events) {
        if (ev.key !== activeEvent && next[ev.key] === slug) next[ev.key] = ''
      }
    }
    next[activeEvent] = slug
    setStatusMap(next)
  }

  function toggleAdminRecipient(adminId: number) {
    if (!activeEvent) return
    const cur = adminRecipients[activeEvent] ?? []
    const has = cur.includes(adminId)
    setAdminRecipients(prev => ({
      ...prev,
      [activeEvent]: has ? cur.filter(x => x !== adminId) : [...cur, adminId],
    }))
  }

  function toggleOrderNumber() {
    if (!activeEvent) return
    setOrderNumberFlags(prev => ({
      ...prev,
      [activeEvent]: prev[activeEvent] ? 0 : 1,
    }))
  }

  // insertVariable injeta {{var}} no textarea de body do role ativo.
  function insertVariable(varKey: string) {
    if (!activeEvent || !varKey) return
    const ref = activeTab === 'producer' ? producerBodyRef
      : activeTab === 'affiliate' ? affiliateBodyRef
      : adminBodyRef
    const el = ref.current
    if (!el) return
    const fk = fieldKeys(activeTab)
    const cur = getActiveTemplate()[fk.body] as string
    const start = el.selectionStart ?? cur.length
    const end = el.selectionEnd ?? cur.length
    const ins = `{{${varKey}}}`
    const next = cur.slice(0, start) + ins + cur.slice(end)
    updateTemplateField(activeTab, 'body', next)
    // Reposiciona o cursor depois do insert (próximo tick).
    setTimeout(() => {
      el.focus()
      el.selectionStart = el.selectionEnd = start + ins.length
    }, 0)
  }

  // ----- derivados ------------------------------------------------------------

  // Eventos agrupados para a sidebar (preserva a ordem do array plano dentro de
  // cada grupo; grupos vazios são omitidos no render). Não altera `events`.
  const groupedEvents = useMemo(() => {
    const buckets: Record<EventGroup, EventDef[]> = { fulfillment: [], cod: [], geral: [] }
    for (const ev of events) {
      buckets[eventGroup(ev.key)].push(ev)
    }
    return GROUP_ORDER.map(g => ({ group: g, label: GROUP_LABEL[g], items: buckets[g] }))
      .filter(sec => sec.items.length > 0)
  }, [events])

  // Status já usados em OUTROS eventos — usados para desabilitar no select.
  const usedStatusesByOther = useMemo(() => {
    const used = new Set<string>()
    for (const ev of events) {
      if (ev.key === activeEvent) continue
      const slug = statusMap[ev.key]
      if (slug) used.add(slug)
    }
    return used
  }, [events, statusMap, activeEvent])

  // Variáveis disponíveis no role ativo.
  const availableVars = useMemo(() => {
    return variables.filter(v => v.available_for.includes(activeTab))
  }, [variables, activeTab])

  // Preview do role ativo (interpola fakes).
  const previewTitle = useMemo(() => {
    const fk = fieldKeys(activeTab)
    return renderPreview((getActiveTemplate()[fk.title] as string) || '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templates, activeEvent, activeTab])
  const previewBody = useMemo(() => {
    const fk = fieldKeys(activeTab)
    return renderPreview((getActiveTemplate()[fk.body] as string) || '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templates, activeEvent, activeTab])

  // ----- render ---------------------------------------------------------------

  if (loading) {
    return (
      <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
        Carregando…
      </div>
    )
  }

  const tpl = getActiveTemplate()
  const rec = getActiveRecipients()
  const currentStatus = statusMap[activeEvent] || ''
  const orderNumberOn = !!orderNumberFlags[activeEvent]
  const selectedAdmins = adminRecipients[activeEvent] || []
  const fk = fieldKeys(activeTab)

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {/* Top bar — Salvar tudo */}
      <div className="szv2-card">
        <div className="szv2-card-head">
          <div>
            <h2>Notificações PWA</h2>
            <p className="szv2-card-sub">
              Configure templates, status, destinatários e admins por evento. Cada status pode ser vinculado a apenas um evento.
            </p>
          </div>
          <button type="button" className="szv2-btn-brand" onClick={handleSaveAll} disabled={busy}>
            {busy ? 'Salvando…' : 'Salvar tudo'}
          </button>
        </div>
      </div>

      {/* Layout: sidebar (eventos) + main (configuração do evento ativo) */}
      <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr', gap: 16, marginTop: 16 }}>
        {/* Sidebar — lista de eventos agrupada (Fulfillment / Cash on Delivery / Geral) */}
        <div className="szv2-card" style={{ padding: 8 }}>
          {groupedEvents.map((section, idx) => (
            <div key={section.group} style={{ marginTop: idx === 0 ? 0 : 10 }}>
              <div style={{ padding: '8px 8px 4px', fontSize: 11, fontWeight: 700, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', letterSpacing: 0.6 }}>
                {section.label}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                {section.items.map(ev => {
                  const isActive = ev.key === activeEvent
                  const slug = statusMap[ev.key] || ''
                  return (
                    <button
                      key={ev.key}
                      type="button"
                      onClick={() => setActiveEvent(ev.key)}
                      style={{
                        textAlign: 'left',
                        padding: '8px 10px',
                        border: 'none',
                        borderRadius: 8,
                        cursor: 'pointer',
                        background: isActive ? 'rgba(30, 111, 242,.10)' : 'transparent',
                        color: isActive ? 'var(--szv2-brand)' : 'var(--szv2-text)',
                        fontWeight: isActive ? 700 : 500,
                        fontSize: 13,
                      }}
                    >
                      <div>{displayLabel(ev)}</div>
                      {slug && (
                        <div style={{ fontSize: 10, color: 'var(--szv2-text-muted)', marginTop: 2 }}>
                          {slug}
                        </div>
                      )}
                    </button>
                  )
                })}
              </div>
            </div>
          ))}
        </div>

        {/* Main area */}
        {activeEvent ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            {/* --- Section: Template --- */}
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Template</h2>
                  <p className="szv2-card-sub">
                    Mensagem do push por papel do destinatário. Producer/Affiliate só aceitam variáveis seguras (número do pedido + comissão própria).
                  </p>
                </div>
              </div>

              {/* Tabs Producer / Affiliate / Admin */}
              <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid var(--szv2-border)', marginBottom: 16 }}>
                {(['producer', 'affiliate', 'admin'] as Role[]).map(role => (
                  <button
                    key={role}
                    type="button"
                    onClick={() => setActiveTab(role)}
                    style={{
                      padding: '8px 16px',
                      border: 'none',
                      background: 'transparent',
                      borderBottom: activeTab === role ? '2px solid var(--szv2-brand)' : '2px solid transparent',
                      color: activeTab === role ? 'var(--szv2-brand)' : 'var(--szv2-text-muted)',
                      fontWeight: 700,
                      cursor: 'pointer',
                      fontSize: 13,
                    }}
                  >
                    {ROLE_LABEL[role]}
                  </button>
                ))}
              </div>

              {/* Tab content (renderizado uma única vez, conteúdo muda por activeTab) */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <div>
                  <label style={{ display: 'block', fontWeight: 700, marginBottom: 6, fontSize: 12 }}>
                    Título do push
                  </label>
                  <input
                    type="text"
                    value={(tpl[fk.title] as string) || ''}
                    onChange={e => updateTemplateField(activeTab, 'title', e.target.value)}
                    className="szv2-input"
                    style={{ width: '100%' }}
                  />
                </div>

                <div>
                  <label style={{ display: 'block', fontWeight: 700, marginBottom: 6, fontSize: 12 }}>
                    Variáveis disponíveis (clique para inserir no corpo)
                  </label>
                  <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                    <FalkSelect
                      value=""
                      onChange={v => { if (v) insertVariable(v) }}
                      options={[
                        { value: '', label: 'Inserir variável…' },
                        ...availableVars.map(v => ({ value: v.key, label: `{{${v.key}}} — ${v.label}` })),
                      ]}
                      aria-label="Inserir variável no corpo da mensagem"
                    />
                  </div>
                </div>

                <div>
                  <label style={{ display: 'block', fontWeight: 700, marginBottom: 6, fontSize: 12 }}>
                    Mensagem
                  </label>
                  <textarea
                    ref={activeTab === 'producer' ? producerBodyRef
                       : activeTab === 'affiliate' ? affiliateBodyRef
                       : adminBodyRef}
                    value={(tpl[fk.body] as string) || ''}
                    onChange={e => updateTemplateField(activeTab, 'body', e.target.value)}
                    className="szv2-input"
                    style={{ width: '100%', minHeight: 90, fontFamily: 'inherit' }}
                    rows={3}
                  />
                </div>

                {/* Live preview */}
                <div style={{
                  marginTop: 4,
                  padding: 14,
                  background: 'rgba(30, 111, 242,.06)',
                  border: '1px solid rgba(30, 111, 242,.20)',
                  borderRadius: 10,
                }}>
                  <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--szv2-brand)', textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 6 }}>
                    Preview ({ROLE_LABEL[activeTab]})
                  </div>
                  <div style={{ fontWeight: 700, color: 'var(--szv2-text)', marginBottom: 4 }}>
                    {previewTitle || <em style={{ color: 'var(--szv2-text-muted)' }}>(sem título)</em>}
                  </div>
                  <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>
                    {previewBody || <em>(sem mensagem)</em>}
                  </div>
                </div>
              </div>
            </div>

            {/* --- Section: Status binding --- */}
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Status que dispara esta notificação</h2>
                  <p className="szv2-card-sub">
                    Cada status de pedido pode pertencer a apenas um evento. Status já vinculados em outros eventos aparecem como (em uso).
                  </p>
                </div>
              </div>
              <FalkSelect
                value={currentStatus}
                onChange={v => setStatusForActive(v)}
                options={[
                  { value: '', label: '— Não disparar por status —' },
                  ...wcStatuses.map(s => {
                    const usedByOther = usedStatusesByOther.has(s.slug)
                    return {
                      value: s.slug,
                      label: `${s.label} — ${s.slug}${usedByOther ? ' (em uso)' : ''}`,
                      disabled: usedByOther,
                    }
                  }),
                ]}
                style={{ maxWidth: 480 }}
              />
            </div>

            {/* --- Section: Recipients --- */}
            <div className="szv2-card">
              <div className="szv2-card-head">
                <div>
                  <h2>Destinatários</h2>
                  <p className="szv2-card-sub">
                    Marque quem deve receber este evento.
                  </p>
                </div>
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {(['producer', 'affiliate', 'admin'] as (keyof Recipients)[]).map(field => (
                  <label
                    key={field}
                    style={{
                      display: 'inline-flex',
                      gap: 8,
                      alignItems: 'center',
                      padding: '8px 12px',
                      border: '1px solid var(--szv2-border)',
                      borderRadius: 10,
                      background: rec[field] ? 'rgba(30, 111, 242,.06)' : '#fff',
                      cursor: 'pointer',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={!!rec[field]}
                      onChange={() => toggleRecipient(field)}
                    />
                    <span style={{ fontWeight: 600 }}>{ROLE_LABEL[field as Role]}</span>
                  </label>
                ))}
              </div>
            </div>

            {/* --- Section: Admin recipients (visible only if admin toggle on) --- */}
            {rec.admin > 0 && (
              <div className="szv2-card">
                <div className="szv2-card-head">
                  <div>
                    <h2>Administradores que recebem</h2>
                    <p className="szv2-card-sub">
                      Selecione quais admins do painel recebem este push. Se nenhum for selecionado, o evento é registrado como skipped no histórico.
                    </p>
                  </div>
                </div>
                {availableAdmins.length === 0 ? (
                  <div style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>
                    Nenhum administrador cadastrado no portal.
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                    {availableAdmins.map(a => {
                      const checked = selectedAdmins.includes(a.id)
                      return (
                        <label
                          key={a.id}
                          style={{
                            display: 'inline-flex',
                            gap: 8,
                            alignItems: 'center',
                            padding: '8px 12px',
                            border: '1px solid var(--szv2-border)',
                            borderRadius: 10,
                            background: checked ? 'rgba(30, 111, 242,.06)' : '#fff',
                            cursor: 'pointer',
                          }}
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggleAdminRecipient(a.id)}
                          />
                          <span style={{ fontWeight: 600 }}>
                            {a.nome || a.email || `Admin #${a.id}`}
                          </span>
                          {a.email && (
                            <span style={{ color: 'var(--szv2-text-muted)', fontSize: 11 }}>
                              · {a.email}
                            </span>
                          )}
                        </label>
                      )
                    })}
                  </div>
                )}
              </div>
            )}

            {/* --- Section: Order number flag --- */}
            <div className="szv2-card">
              <label
                style={{
                  display: 'inline-flex',
                  gap: 8,
                  alignItems: 'center',
                  padding: '8px 12px',
                  border: '1px solid var(--szv2-border)',
                  borderRadius: 10,
                  background: orderNumberOn ? 'rgba(30, 111, 242,.06)' : '#fff',
                  cursor: 'pointer',
                }}
              >
                <input
                  type="checkbox"
                  checked={orderNumberOn}
                  onChange={toggleOrderNumber}
                />
                <span style={{ fontWeight: 600 }}>Incluir número do pedido no push</span>
              </label>
            </div>

            {/* Footer save */}
            <div className="szv2-card" style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
              <button type="button" className="szv2-btn-brand" onClick={handleSaveAll} disabled={busy}>
                {busy ? 'Salvando…' : 'Salvar tudo'}
              </button>
            </div>
          </div>
        ) : (
          <div className="szv2-card" style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Selecione um evento à esquerda para configurar.
          </div>
        )}
      </div>
    </div>
  )
}
