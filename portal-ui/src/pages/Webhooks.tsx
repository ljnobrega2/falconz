// Conexões / Webhooks — porte fiel de templates/portal/v2/sections/webhooks.php.
// Sub-abas: "Webhooks" (payload exemplo + novo webhook + configurados) e "Histórico".
// Ligada ao go/portal:
//   GET    /portal/webhooks                — lista (filtra url='' soft-deleted)
//   POST   /portal/webhooks                — cria (url, active, event_types, shipping_class_id)
//   DELETE /portal/webhooks/{id}           — soft-delete (url='', active=0)
//   POST   /portal/webhooks/{id}/test      — disparo de teste síncrono (response_code)
//   GET    /portal/webhooks/{id}/history   — histórico de disparos do webhook
//   POST   /portal/webhooks/clear-history  — limpa logs (opcional webhook_id)
// Classes de entrega vêm de GET /portal/localidades (CDs) — não há endpoint dedicado
// que exponha os shipping_class_id do usuário; ver backendReqs no relatório.
import { ChangeEvent, FormEvent, useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import FalkSelect from '../components/FalkSelect'
import { dtTime } from '../utils/format'

// ── Tipos ───────────────────────────────────────────────────────────────────

type WebhookRow = {
  id: number
  url: string
  active: boolean
  event_types: string[]
  shipping_class_id?: number | null
  created_at: string
  updated_at: string
}

type HistoryEntry = {
  id: number
  event_type: string
  response_code?: number | null
  response_body?: string | null
  created_at: string
}

// Produto do produtor (GET /portal/products → { ok, data: productItem[] }).
// Substitui o antigo seletor de "Classe de entrega" (pedido do dono): o webhook é
// referente a um PRODUTO, não a uma classe de entrega.
type Produto = {
  id: number
  name: string
}

type ListResp = { ok: boolean; data: WebhookRow[]; total: number }
type HistoryResp = { ok: boolean; data: HistoryEntry[]; total: number }
type ProductsResp = { ok: boolean; data: Produto[]; total: number }

// ── Tipo de webhook (pedido do dono) ──────────────────────────────────────────
// "Expedição" (correio/transportadora) x "Cash on Delivery" (motoboy/COD).
// Os EVENTOS e os campos mudam conforme o tipo, espelhando o que o backend dispara:
//   - Expedição : eventos order_status_* (senderzz-producer-webhooks.php) — na
//     whitelist DT-CODE-02 do go/portal → SALVA de verdade (POST /portal/webhooks).
//   - COD       : eventos motoboy_* (senderzz-producer-webhooks.php:889-897) — agora
//     na whitelist do go/portal (allowedEventTypes) + colunas tipo/product_id no
//     schema → SALVA de verdade também (POST /portal/webhooks com tipo='cod').
// As chaves de evento abaixo precisam casar EXATAMENTE a whitelist do go/portal
// (handlers/webhooks.go: allowedEventTypes) — exact match, nunca substring.
type WebhookTipo = 'expedicao' | 'cod'

// Eventos de EXPEDIÇÃO — order_status_* (whitelist DT-CODE-02 do .php / go).
// 7 chaves; só o rótulo é cosmético. NÃO inventar chaves order_status_* novas —
// o backend só aceita estas e qualquer outra retorna 400 no Create.
const EVENT_TYPES_EXPEDICAO: { key: string; label: string }[] = [
  { key: 'order_status_embalado', label: 'Separado / Embalado' },
  // AUDIT-2026-07-31 (dono): status 'coletado' (531-status-coletado.sql) —
  // faltava na whitelist/UI, dono cobrou.
  { key: 'order_status_coletado', label: 'Coletado' },
  { key: 'order_status_enviado', label: 'Enviado' },
  { key: 'order_status_em_rota', label: 'A caminho / Em rota' },
  { key: 'order_status_entregue', label: 'Entregue' },
  { key: 'order_status_frustrado', label: 'Mal sucedido / Frustrado' },
  { key: 'order_status_cancelado', label: 'Cancelado / Devolvido' },
]

// Eventos de CASH ON DELIVERY — motoboy_* (senderzz-producer-webhooks.php:889-897).
// Disparados pelo backend WP em sz_motoboy_status_changed e AGORA aceitos pelo
// go/portal (allowedEventTypes). Chaves casam a whitelist 1:1.
const EVENT_TYPES_COD: { key: string; label: string }[] = [
  { key: 'motoboy_agendado', label: 'Agendado' },
  { key: 'motoboy_pre_agendado', label: 'Pré-agendado' },
  { key: 'motoboy_embalado', label: 'Embalado' },
  { key: 'motoboy_enviado', label: 'Enviado' },
  { key: 'motoboy_coletado', label: 'Coletado' },
  { key: 'motoboy_a_caminho', label: 'A caminho' },
  { key: 'motoboy_em_rota', label: 'Em rota' },
  { key: 'motoboy_completo', label: 'Completo' },
  { key: 'motoboy_frustrado', label: 'Frustrado' },
]

function eventsForTipo(tipo: WebhookTipo) {
  return tipo === 'cod' ? EVENT_TYPES_COD : EVENT_TYPES_EXPEDICAO
}

// Rótulo de evento para o histórico — cobre os dois conjuntos.
const EVENT_LABEL: Record<string, string> = Object.fromEntries(
  [...EVENT_TYPES_EXPEDICAO, ...EVENT_TYPES_COD].map(e => [e.key, e.label]),
)

// Payload de exemplo — espelha exatamente a estrutura do .php (JSON_PRETTY_PRINT).
const PAYLOAD_EXEMPLO = {
  event: 'order_status_enviado',
  status_ativo: true,
  pedido: {
    id: 1,
    numero: '1',
    status: 'enviado',
    subtotal: 197.0,
    subtotal_formatado: 'R$ 197,00',
    total: 226.9,
    total_formatado: 'R$ 226,90',
    desconto: 0,
    desconto_formatado: '',
    metodo_pagamento: 'PIX',
    criado_em: '2026-06-15T01:14:08-03:00',
    atualizado_em: '2026-06-15T01:14:08-03:00',
    pago_em: '2026-06-15T01:14:08-03:00',
    enviado_em: '',
    entregue_em: '',
  },
  classe_entrega: { id: 10, nome: 'São Paulo', slug: 'sao-paulo' },
  frete: {
    valor: 29.9,
    valor_formatado: 'R$ 29,90',
    prazo_dias_uteis: 3,
    transportadora: 'Loggi',
    servico: 'Express',
  },
  cliente: {
    nome: 'Cliente Teste',
    telefone: '11999999999',
    telefone_completo: '5511999999999',
    email: 'cliente@email.com',
    cpf: '000.000.000-00',
  },
  entrega: {
    nome: 'Cliente Teste',
    cep: '01001000',
    endereco: 'Rua Exemplo',
    numero: '100',
    complemento: '',
    bairro: 'Centro',
    cidade: 'São Paulo',
    estado: 'SP',
  },
  rastreamento: ['BR123456789'],
  link_rastreamento: 'https://testes.falklog.com.br/rastreio/BR123456789/', // ALIGN-2026-06-21: dominio de exemplo senderzz.com.br -> falklog.com.br
  itens: [{ nome: 'Produto', quantidade: 1, subtotal: 197.0 }],
  transportadora: 'Loggi',
  servico: 'Express',
}

export default function Webhooks() {
  const toast = useToast()

  // sub-aba ativa: 'webhooks' | 'history' (espelha szV2ConnTab).
  const [tab, setTab] = useState<'webhooks' | 'history'>('webhooks')

  // listagem
  const [rows, setRows] = useState<WebhookRow[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // produtos do produtor para o select (pedido do dono: webhook é por PRODUTO).
  const [produtos, setProdutos] = useState<Produto[]>([])

  // formulário "novo webhook"
  const [tipo, setTipo] = useState<WebhookTipo>('cod') // default COD até o /portal/me resolver
  // expedicaoAtiva: espelha Layout.tsx — settings.expedicao_ativa do /portal/me. OFF
  // (false/'false') → o tipo "Expedição" some (produtor só-COD não emite webhook de
  // expedição). null = CARREGANDO → NÃO renderiza o seletor de tipo (evita o flicker de
  // "Expedição aparece e some"). Ver links_portal.go:533.
  const [expedicaoAtiva, setExpedicaoAtiva] = useState<boolean | null>(null)
  const [productId, setProductId] = useState('')
  const [url, setUrl] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [active, setActive] = useState(true)
  const [creating, setCreating] = useState(false)
  const [saveErr, setSaveErr] = useState('')

  // ação por linha (testar/excluir) — desabilita botões durante o request
  const [busyId, setBusyId] = useState<number | null>(null)

  // histórico (carregado sob demanda quando a aba é aberta ou em "Atualizar")
  const [history, setHistory] = useState<HistoryEntry[] | null>(null)
  const [histLoading, setHistLoading] = useState(false)
  const [histErr, setHistErr] = useState('')

  // ── Carregadores ────────────────────────────────────────────────────────────

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/webhooks')
      .then(r => setRows(r.data || []))
      .catch(e => setErr((e && e.message) || 'Erro ao carregar webhooks'))
      .finally(() => setLoading(false))
  }

  function loadProdutos() {
    // Produtos do produtor — populam o select de PRODUTO do formulário (pedido do
    // dono: webhook é referente a um produto, não a uma classe de entrega).
    api<ProductsResp>('/portal/products')
      .then(r => setProdutos(r.data || []))
      .catch(() => setProdutos([]))
  }

  useEffect(() => {
    load()
    loadProdutos()
    // Flag de expedição do produtor — gateia o tipo "Expedição". Fail-soft: erro → mantém
    // o default ATIVA (não esconde por engano numa falha de rede).
    api<{ role?: string; settings?: { expedicao_ativa?: boolean | string } | null }>('/portal/me')
      .then(me => {
        const role = (me?.role ?? '').toLowerCase()
        const isProducer = role === 'produtor' || role === 'producer'
        if (!isProducer) {
          // afiliado/operator/cliente: nunca vê expedição
          setExpedicaoAtiva(false)
          setTipo('cod')
          return
        }
        const f = me?.settings?.expedicao_ativa
        const ativa = !(f === false || f === 'false')
        setExpedicaoAtiva(ativa)
        setTipo(ativa ? 'expedicao' : 'cod')
      })
      .catch(() => { setExpedicaoAtiva(false); setTipo('cod') })
  }, [])

  // Carrega o histórico agregando os disparos de TODOS os webhooks do usuário.
  // O backend expõe histórico por webhook (GET /portal/webhooks/{id}/history);
  // unimos as respostas e ordenamos por data desc para a visão "Histórico de disparos".
  async function loadHistory() {
    setHistLoading(true)
    setHistErr('')
    try {
      const lista = rows.filter(r => r.url) // só webhooks ativos/com URL
      if (lista.length === 0) {
        setHistory([])
        return
      }
      const partes = await Promise.all(
        lista.map(r =>
          api<HistoryResp>(`/portal/webhooks/${r.id}/history`)
            .then(resp => resp.data || [])
            .catch(() => [] as HistoryEntry[]),
        ),
      )
      const todos = partes.flat()
      todos.sort((a, b) => (a.created_at < b.created_at ? 1 : -1))
      setHistory(todos)
    } catch (e: any) {
      setHistErr((e && e.message) || 'Erro ao carregar histórico')
      setHistory([])
    } finally {
      setHistLoading(false)
    }
  }

  function openHistoryTab() {
    setTab('history')
    if (history === null) loadHistory() // primeira abertura → carrega
  }

  // ── Ações ─────────────────────────────────────────────────────────────────

  // Multi-select de eventos: o <select multiple> entrega selectedOptions; tomamos
  // todos os valores marcados. Vazio = todos os eventos (semântica do backend).
  function onEventsChange(e: ChangeEvent<HTMLSelectElement>) {
    setSelected(Array.from(e.target.selectedOptions, o => o.value))
  }

  // Troca de tipo limpa os eventos selecionados (os conjuntos não se sobrepõem).
  function changeTipo(next: WebhookTipo) {
    setTipo(next)
    setSelected([])
    setSaveErr('')
  }

  async function create(e: FormEvent) {
    e.preventDefault()
    setSaveErr('')

    // COD agora persiste: o go/portal aceita os eventos motoboy_* (allowedEventTypes)
    // e tem as colunas tipo/product_id no schema. Expedição e COD seguem o mesmo POST.
    if (!url.trim()) {
      setSaveErr('Informe a URL de destino.')
      return
    }
    setCreating(true)
    try {
      const body: Record<string, any> = {
        url: url.trim(),
        active,
        event_types: selected, // vazio = todos os eventos
        tipo, // 'expedicao' | 'cod' — persistido na coluna `tipo`.
      }
      // product_id (pedido do dono): webhook é por PRODUTO. Enviado como campo próprio.
      // NÃO escrevemos em shipping_class_id — o dispatch casa pedidos por
      // shipping_class_id (producer-webhooks.php:690-707) e reaproveitar esse campo
      // corromperia o roteamento. O backend persiste product_id na coluna própria.
      if (productId) body.product_id = Number(productId)
      await api('/portal/webhooks', { method: 'POST', body: JSON.stringify(body) })
      toast('ok', 'Webhook salvo com sucesso.')
      setUrl('')
      setProductId('')
      setSelected([])
      setActive(true)
      load()
      // novo disparo pode aparecer no histórico → invalida o cache
      setHistory(null)
    } catch (e: any) {
      const msg = (e && e.message) || 'Erro ao salvar webhook.'
      setSaveErr(msg)
      toast('err', msg)
    } finally {
      setCreating(false)
    }
  }

  async function testar(row: WebhookRow) {
    setBusyId(row.id)
    try {
      const resp = await api<{ ok: boolean; response_code?: number; success?: boolean }>(
        `/portal/webhooks/${row.id}/test`,
        { method: 'POST' },
      )
      if (resp.success) {
        toast('ok', `Teste enviado — endpoint respondeu HTTP ${resp.response_code}.`)
      } else {
        toast('warn', `Teste enviado, mas o endpoint respondeu HTTP ${resp.response_code ?? '—'}.`)
      }
      setHistory(null) // gerou novo registro no histórico
    } catch (e: any) {
      toast('err', (e && e.message) || 'Falha ao disparar o webhook de teste.')
    } finally {
      setBusyId(null)
    }
  }

  async function remove(row: WebhookRow) {
    const ok = await confirmAsync({
      title: 'Excluir webhook',
      message: `Excluir o webhook ${row.url}? Esta ação desativa o envio de eventos para este endpoint.`,
      confirmLabel: 'Excluir',
      danger: true,
    })
    if (!ok) return
    setBusyId(row.id)
    try {
      await api(`/portal/webhooks/${row.id}`, { method: 'DELETE' })
      toast('ok', 'Webhook excluído.')
      load()
    } catch (e: any) {
      toast('err', (e && e.message) || 'Erro ao excluir webhook.')
    } finally {
      setBusyId(null)
    }
  }

  async function clearHistory() {
    const ok = await confirmAsync({
      title: 'Limpar histórico de webhooks?',
      message: 'Todos os disparos registrados serão apagados.',
      confirmLabel: 'Limpar',
      danger: true,
    })
    if (!ok) return
    try {
      await api('/portal/webhooks/clear-history', { method: 'POST', body: JSON.stringify({}) })
      toast('ok', 'Histórico limpo.')
      setHistory([])
    } catch (e: any) {
      toast('err', (e && e.message) || 'Erro ao limpar histórico.')
    }
  }

  // ── Helpers de exibição ───────────────────────────────────────────────────

  // Rótulo do produto na tabela. O backend devolve product_id na linha (List) —
  // resolvemos o nome via lista de produtos; NULL/0 = todos os produtos → '—'.
  function produtoLabel(row: WebhookRow): string {
    const pid = (row as any).product_id as number | null | undefined
    if (!pid) return '—'
    const p = produtos.find(x => x.id === pid)
    return p ? p.name : `Produto #${pid}`
  }

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <section id="sec-webhooks" className="sz-sec szv2-conn-panel" data-szv2-label="Conexões">
      <div className="szv2-page-head" style={{ marginBottom: 12 }}>
        <h2
          className="szv2-page-title"
          style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}
        >
          Conexões
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Envie eventos dos seus pedidos para sistemas externos via webhook.
        </p>
      </div>

      {/* Sub-abas: Webhooks / Histórico (espelha szv2-prod-subtabs) */}
      <div className="szv2-prod-subtabs" role="tablist" style={{ marginBottom: 16 }}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'webhooks'}
          className={`szv2-prod-subtab${tab === 'webhooks' ? ' szv2-prod-subtab--active' : ''}`}
          onClick={() => setTab('webhooks')}
        >
          Webhooks
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'history'}
          className={`szv2-prod-subtab${tab === 'history' ? ' szv2-prod-subtab--active' : ''}`}
          onClick={openHistoryTab}
        >
          Histórico
        </button>
      </div>

      {/* ───────────────────────── SUB: Webhooks ───────────────────────── */}
      {tab === 'webhooks' && (
        <>
          {err && <div className="sz-alert-danger">{err}</div>}

          {/* Payload exemplo — recolhido */}
          <details
            className="szv2-payload-details"
            style={{
              marginBottom: 16,
              border: '1px solid var(--szv2-border)',
              borderRadius: 12,
              overflow: 'hidden',
              background: 'var(--szv2-surface)',
            }}
          >
            <summary
              className="szv2-payload-summary"
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                gap: 8,
                padding: '12px 16px',
                cursor: 'pointer',
                fontSize: 13,
                fontWeight: 600,
                color: 'var(--szv2-text)',
              }}
            >
              <span>📦 Payload exemplo — estrutura completa do webhook</span>
              <span style={{ fontSize: 11, fontWeight: 400, color: 'var(--szv2-text-muted)' }}>
                clique para expandir
              </span>
            </summary>
            <pre
              className="szv2-code-pre"
              style={{
                margin: 0,
                padding: 16,
                maxHeight: 360,
                overflow: 'auto',
                fontSize: 12,
                lineHeight: 1.5,
                background: 'var(--szv2-surface-alt)',
                color: 'var(--szv2-text)',
                borderTop: '1px solid var(--szv2-border)',
              }}
            >
              {JSON.stringify(PAYLOAD_EXEMPLO, null, 2)}
            </pre>
          </details>

          {/* Formulário: Novo webhook */}
          <div className="szv2-card" style={{ padding: 20, marginBottom: 16 }}>
            <div style={{ marginBottom: 14 }}>
              <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>
                Novo webhook
              </h3>
              <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Receba eventos dos pedidos em tempo real.
              </p>
            </div>

            <form onSubmit={create}>
              {/* Tipo do webhook (pedido do dono): Expedição x Cash on Delivery.
                  Os campos e eventos abaixo mudam conforme o tipo selecionado. */}
              {/* Seletor de tipo só quando expedição ATIVA. Enquanto carrega (null) ou
                  quando inativa, NÃO renderiza — evita o flicker ("Expedição aparece e
                  some") e o botão único inútil (produtor só-COD nem escolhe: tipo='cod'). */}
              {expedicaoAtiva === true && (
              <div style={{ marginBottom: 14 }}>
                <label className="sz-login-label">Tipo de webhook</label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', paddingTop: 4 }}>
                  {(([
                    { key: 'expedicao', label: 'Expedição' },
                    { key: 'cod', label: 'Cash on Delivery' },
                  ] as { key: WebhookTipo; label: string }[])
                    // Tipo "Expedição" só p/ produtor com expedição ATIVA (pedido do dono).
                    .filter(opt => opt.key !== 'expedicao' || expedicaoAtiva)
                  ).map(opt => {
                    const on = tipo === opt.key
                    return (
                      <button
                        key={opt.key}
                        type="button"
                        onClick={() => changeTipo(opt.key)}
                        aria-pressed={on}
                        style={{
                          padding: '7px 14px',
                          fontSize: 13,
                          fontWeight: 600,
                          cursor: 'pointer',
                          borderRadius: 8,
                          border: `1.5px solid ${on ? 'var(--szv2-brand)' : 'var(--szv2-border)'}`,
                          background: on ? 'var(--szv2-brand)' : 'transparent',
                          color: on ? 'var(--szv2-on-brand)' : 'var(--szv2-text)',
                          transition: 'all .15s',
                        }}
                      >
                        {opt.label}
                      </button>
                    )
                  })}
                </div>
              </div>
              )}

              {/* Produto (pedido do dono): webhook é referente a um PRODUTO. */}
              <div style={{ marginBottom: 14 }}>
                <label className="sz-login-label">Produto</label>
                <FalkSelect
                  aria-label="Produto"
                  value={productId}
                  onChange={v => setProductId(v)}
                  placeholder="Todos os produtos"
                  options={[
                    { value: '', label: 'Todos os produtos' },
                    ...produtos.map(p => ({ value: String(p.id), label: p.name })),
                  ]}
                />
              </div>

              <div style={{ marginBottom: 14 }}>
                <label className="sz-login-label">URL de destino</label>
                <input
                  className="sz-login-input"
                  type="url"
                  placeholder="https://seusistema.com.br/webhook/falk" /* ALIGN-2026-06-21: exemplo de marca senderzz -> falk */
                  value={url}
                  onChange={e => setUrl(e.target.value)}
                  required
                  style={{ marginBottom: 0 }}
                />
              </div>

              {/* Eventos: SELECT MÚLTIPLO (vários OU todos). Vazio = todos os
                  eventos. Ctrl/Cmd-clique p/ marcar vários. Estilo global do tema
                  (.sz-login-input); height:auto p/ mostrar as linhas. Ao lado,
                  o toggle "Ativo" e o botão "Salvar webhook" — linha compacta. */}
              <div
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'flex-end',
                  gap: 14,
                  marginBottom: 14,
                }}
              >
                <div style={{ flex: '1 1 260px', minWidth: 240 }}>
                  <label className="sz-login-label">Eventos (vazio = todos)</label>
                  <select
                    className="sz-login-input"
                    multiple
                    size={Math.min(eventsForTipo(tipo).length, 6)}
                    value={selected}
                    onChange={onEventsChange}
                    style={{ height: 'auto', marginBottom: 0, paddingTop: 6, paddingBottom: 6 }}
                  >
                    {eventsForTipo(tipo).map(ev => (
                      <option key={ev.key} value={ev.key}>
                        {ev.label}
                      </option>
                    ))}
                  </select>
                </div>

                <label
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    cursor: 'pointer',
                    paddingBottom: 10,
                  }}
                >
                  <input
                    type="checkbox"
                    checked={active}
                    onChange={e => setActive(e.target.checked)}
                  />
                  <span style={{ fontSize: 13, color: 'var(--szv2-text)' }}>Ativo</span>
                </label>

                <button
                  type="submit"
                  className="szv2-btn szv2-btn-brand"
                  disabled={creating}
                  style={{ marginBottom: 2 }}
                >
                  {creating ? 'Salvando…' : 'Salvar webhook'}
                </button>
              </div>

              {saveErr && (
                <div
                  style={{
                    color: 'var(--szv2-danger)',
                    fontSize: 13,
                    marginBottom: 8,
                  }}
                >
                  {saveErr}
                </div>
              )}
            </form>
          </div>

          {/* Webhooks configurados */}
          <div className="szv2-card" style={{ padding: 20 }}>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'baseline',
                marginBottom: 14,
              }}
            >
              <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>
                Webhooks configurados
              </h3>
              <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                {rows.length} endpoint(s)
              </span>
            </div>

            {loading ? (
              <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
            ) : rows.length === 0 ? (
              <EmptyState
                icon="🔌"
                title="Nenhum webhook configurado"
                description="Use o formulário acima para adicionar seu primeiro endpoint."
              />
            ) : (
              <div className="szv2-table-wrap">
                <table className="szv2-table" style={{ width: '100%' }}>
                  <thead>
                    <tr>
                      <th>Produto</th>
                      <th>URL destino</th>
                      <th>Status</th>
                      <th style={{ textAlign: 'right' }}>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map(r => (
                      <tr key={r.id}>
                        <td className="szv2-td-sub">{produtoLabel(r)}</td>
                        <td style={{ wordBreak: 'break-all' }}>
                          {r.url ? (
                            <a
                              href={r.url}
                              target="_blank"
                              rel="noopener noreferrer"
                              style={{ color: 'var(--szv2-brand)' }}
                            >
                              {r.url}
                            </a>
                          ) : (
                            <span style={{ color: 'var(--szv2-text-faint)' }}>Aguardando URL</span>
                          )}
                        </td>
                        <td>
                          <span
                            className={`szv2-role-badge ${r.active ? 'role-affiliate' : 'role-default'}`}
                          >
                            {r.active ? 'Ativo' : 'Inativo'}
                          </span>
                        </td>
                        <td style={{ textAlign: 'right' }}>
                          <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                            {r.url && (
                              <button
                                type="button"
                                className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                                disabled={busyId === r.id}
                                onClick={() => testar(r)}
                              >
                                {busyId === r.id ? '…' : 'Testar'}
                              </button>
                            )}
                            <button
                              type="button"
                              className="szv2-btn szv2-btn-sm szv2-btn-danger"
                              disabled={busyId === r.id}
                              onClick={() => remove(r)}
                            >
                              Excluir
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}

      {/* ───────────────────────── SUB: Histórico ───────────────────────── */}
      {tab === 'history' && (
        <div className="szv2-card" style={{ padding: 20 }}>
          <div
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'flex-start',
              gap: 12,
              marginBottom: 14,
            }}
          >
            <div>
              <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>
                Histórico de disparos
              </h3>
              <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Eventos recentes, status e payloads enviados.
              </p>
            </div>
            {/* #26: 'Atualizar' e 'Limpar histórico' lado a lado, alinhados à direita
                (antes estavam empilhados em coluna). */}
            <div style={{ display: 'flex', flexDirection: 'row', gap: 8, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                onClick={loadHistory}
                disabled={histLoading}
              >
                {histLoading ? 'Atualizando…' : 'Atualizar'}
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-danger szv2-btn-sm"
                onClick={clearHistory}
              >
                Limpar histórico
              </button>
            </div>
          </div>

          {histErr && <div className="sz-alert-danger">{histErr}</div>}

          {histLoading && history === null ? (
            <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando disparos…</p>
          ) : !history || history.length === 0 ? (
            <EmptyState
              icon="📡"
              title="Nenhum disparo registrado"
              description="Os disparos de webhook aparecem aqui após o primeiro evento ou teste."
            />
          ) : (
            <div className="szv2-table-wrap">
              <table className="szv2-table" style={{ width: '100%' }}>
                <thead>
                  <tr>
                    <th>Evento</th>
                    <th>Status HTTP</th>
                    <th>Data</th>
                  </tr>
                </thead>
                <tbody>
                  {history.map(h => {
                    const code = h.response_code ?? 0
                    const ok2xx = code >= 200 && code < 300
                    return (
                      <tr key={h.id}>
                        <td className="szv2-td-sub">
                          {EVENT_LABEL[h.event_type] || h.event_type}
                        </td>
                        <td>
                          <span
                            className={`szv2-role-badge ${ok2xx ? 'role-affiliate' : 'role-default'}`}
                          >
                            {code > 0 ? code : 'falha'}
                          </span>
                        </td>
                        <td className="szv2-td-mono">{dtTime(h.created_at)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
