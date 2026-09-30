// Integrações — paridade com o portal WP (templates/portal/v2/sections/integrations.php).
// 2 abas: "Endpoint" (URL + token + status + logs) e "Configurações" (toggles + regras + mapeamento).
//
// Contrato do backend go/portal hoje (/wp-json/senderzz/v1):
//   GET    /portal/integrations          → { ok, integrations:{flags JSONB} }  (só as flags!)
//   POST   /portal/integrations/toggle   → alterna 1 flag (whitelist: active, paused,
//                                           require_paid, ignore_duplicates, auto_cheapest)
//   DELETE /portal/integrations/logs     → limpa logs do usuário
//   POST   /portal/integrations/rotate   → 501 (sem storage de secret no espelho)
//   POST   /portal/integrations/reprocess→ 501 (pipeline de recebimento não migrado)
//
// LACUNAS de backend (UI construída assumindo o contrato do WP; ver backendReqs no relatório):
//   - GET não devolve token/endpoint_url, last_received_at, total_recv, mapping_json.
//   - Não existe GET de logs (só DELETE) — lista de "Logs de recebimento" fica vazia.
//   - Não existe save de mapeamento — botão "Salvar mapeamento" chama contrato assumido.
//   - complete_sku / save_pending_error NÃO estão na whitelist (toggle daria 400) — no WP
//     ficam hardcoded em off (o enriquecimento por SKU roda incondicionalmente no
//     pipeline PHP; o pipeline de recebimento NÃO foi migrado p/ go), então aqui
//     renderizamos desabilitados com nota "(em breve)" — gap honesto de backend.
//
// Pausa = inverso de `active` (mesma chave) — espelha szV2IntTogglePause do WP. Nunca
// modelamos `paused` como flag independente na UI para não dessincronizar de `active`.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import { dtTime } from '../utils/format'

type IntegrationsBlob = Record<string, any>
type ListResp = {
  ok: boolean
  integrations: IntegrationsBlob | null
  has_api_key?: boolean
  token_prefix?: string | null
  endpoint_url?: string
}

// Log de recebimento (shape esperado do WP — backend ainda não expõe GET).
type IntLog = {
  id: number
  received_at: string
  external_id: string
  status: string
  message: string
  payload: string
}

// Campos de mapeamento Senderzz ← payload da plataforma (espelha $sz9in_map_fields do WP).
const MAP_FIELDS: { key: string; label: string }[] = [
  { key: 'pedido.id_externo', label: 'ID externo do pedido' },
  { key: 'pedido.status_pagamento', label: 'Status do pagamento' },
  { key: 'cliente.nome', label: 'Nome do cliente' },
  { key: 'cliente.telefone', label: 'Telefone' },
  { key: 'cliente.email', label: 'E-mail' },
  { key: 'cliente.documento', label: 'CPF/CNPJ' },
  { key: 'endereco.cep', label: 'CEP' },
  { key: 'endereco.rua', label: 'Rua' },
  { key: 'endereco.numero', label: 'Número' },
  { key: 'endereco.complemento', label: 'Complemento' },
  { key: 'endereco.bairro', label: 'Bairro' },
]

// Toggles da aba Configurações. `whitelisted=false` ⇒ não está na whitelist do backend
// (toggle daria 400) — renderiza desabilitado, igual ao WP que os deixa hardcoded off.
const CONFIG_TOGGLES: { key: string; label: string; help: string; whitelisted: boolean }[] = [
  { key: 'active', label: 'Integração ativa', help: 'Recebe e processa webhooks automaticamente.', whitelisted: true },
  { key: 'require_paid', label: 'Exigir pagamento confirmado', help: 'Só processa pedidos com status de pagamento confirmado.', whitelisted: true },
  { key: 'ignore_duplicates', label: 'Ignorar pedido duplicado pelo ID externo', help: 'Evita criação de pedidos duplicados usando o ID externo.', whitelisted: true },
  { key: 'auto_cheapest', label: 'Menor frete automático', help: 'Escolhe automaticamente o frete mais barato disponível.', whitelisted: true },
  { key: 'complete_sku', label: 'Completar dados com cadastro da FALK LOG (SKU)', help: 'Se o produto existir, usamos peso, medidas e classe do cadastro.', whitelisted: false },
  { key: 'save_pending_error', label: 'Salvar pendentes com erro', help: 'Se faltar dado crítico, o pedido fica pendente para revisão.', whitelisted: false },
]

const FREIGHT_RULE = 'Mais barato disponível'

export default function Integrations() {
  const toast = useToast()
  const [tab, setTab] = useState<'endpoint' | 'config'>('endpoint')
  const [blob, setBlob] = useState<IntegrationsBlob>({})
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Endpoint / token — AUDIT-2026-07-28: backend agora devolve de verdade
  // (senderzz_producer_api_keys, migration 510). token só existe em memória
  // logo após rotate() — o backend nunca guarda/devolve o valor em claro de novo.
  const [endpointUrl, setEndpointUrl] = useState('')
  const [hasToken, setHasToken] = useState(false)
  const [tokenPrefix, setTokenPrefix] = useState('')
  const [freshToken, setFreshToken] = useState('')
  const [lastReceived] = useState('')
  const [totalRecv] = useState(0)

  // Logs de recebimento (sem GET no backend → lista vazia; mostra empty-state).
  const [logs] = useState<IntLog[]>([])
  const [openLogId, setOpenLogId] = useState<number | null>(null)

  // Mapeamento de campos — controlado; salva via contrato assumido do WP.
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [savingMapping, setSavingMapping] = useState(false)
  const [renewing, setRenewing] = useState(false)
  const [reprocessing, setReprocessing] = useState(false)

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/integrations')
      .then(r => {
        const data = r.integrations || {}
        setBlob(data)
        setHasToken(!!r.has_api_key)
        setTokenPrefix(r.token_prefix || '')
        setEndpointUrl(r.endpoint_url || '')
        if (data.mapping_json && typeof data.mapping_json === 'object') {
          setMapping(data.mapping_json as Record<string, string>)
        }
      })
      .catch(e => setErr(e.message || 'Erro ao carregar integrações'))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  // active = 1/true ⇒ integração ativa. Default: tratada como inativa se ausente.
  const isActive = (() => {
    const v = blob.active
    return v === true || v === 1 || v === '1'
  })()

  function isOn(key: string): boolean {
    const v = blob[key]
    return v === true || v === 1 || v === '1'
  }

  // Toggle genérico de chave whitelistada (otimista + revert no erro).
  async function toggleKey(key: string, nextOn: boolean) {
    const prevVal = blob[key]
    setBlob(prev => ({ ...prev, [key]: nextOn ? 1 : 0 }))
    try {
      await api('/portal/integrations/toggle', {
        method: 'POST',
        body: JSON.stringify({ key, value: nextOn ? 1 : 0 }),
      })
      toast('ok', nextOn ? 'Ativado.' : 'Desativado.')
    } catch (e: any) {
      setBlob(prev => ({ ...prev, [key]: prevVal })) // reverte
      toast('err', e.message || 'Erro ao salvar.')
    }
  }

  // "Pausar recebimento" = inverso de active. Marcado (pausar) ⇒ active=0.
  async function togglePause(pauseChecked: boolean) {
    await toggleKey('active', !pauseChecked)
  }

  // Renovar token → POST /rotate. Backend devolve o token em CLARO só nesta
  // resposta — depois disso é irrecuperável (mesmo padrão de todo secret do
  // sistema). Guarda em memória pra mostrar uma vez com botão de copiar.
  async function renewToken() {
    setRenewing(true)
    try {
      const r = await api<{ token: string; token_prefix: string; endpoint_url: string }>(
        '/portal/integrations/rotate', { method: 'POST' },
      )
      setFreshToken(r.token)
      setTokenPrefix(r.token_prefix)
      setEndpointUrl(r.endpoint_url)
      setHasToken(true)
      toast('ok', 'Chave gerada! Copie agora — não será mostrada de novo.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao renovar token.')
    } finally {
      setRenewing(false)
    }
  }

  // Reprocessar último → POST /reprocess (hoje 501; mostra a mensagem do backend).
  async function reprocessLast() {
    setReprocessing(true)
    try {
      await api('/portal/integrations/reprocess', { method: 'POST' })
      toast('ok', 'Reprocessamento solicitado.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao reprocessar.')
    } finally {
      setReprocessing(false)
    }
  }

  async function copyUrl() {
    if (!endpointUrl) {
      toast('warn', 'Endpoint ainda não disponível.')
      return
    }
    try {
      await navigator.clipboard.writeText(endpointUrl)
      toast('ok', 'URL copiada!')
    } catch {
      toast('err', 'Não foi possível copiar.')
    }
  }

  async function clearLogs() {
    const ok = await confirmAsync({
      title: 'Limpar logs de integração?',
      message: 'Todos os registros de recebimento serão apagados permanentemente.',
      confirmLabel: 'Limpar',
      danger: true,
    })
    if (!ok) return
    try {
      await api('/portal/integrations/logs', { method: 'DELETE' })
      toast('ok', 'Logs limpos.')
      load()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao limpar logs.')
    }
  }

  function setMapField(key: string, value: string) {
    setMapping(prev => ({ ...prev, [key]: value }))
  }

  function clearMapField(key: string) {
    setMapping(prev => {
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  // Salva o mapeamento — backend ainda não tem endpoint; assume contrato do WP
  // (szaction=integrations_save, mapping_json). Falha graciosamente.
  async function saveMapping() {
    setSavingMapping(true)
    // Remove entradas vazias antes de enviar (espelha szV2IntSaveMapping do WP).
    const clean: Record<string, string> = {}
    Object.entries(mapping).forEach(([k, v]) => {
      if (v && v.trim()) clean[k] = v.trim()
    })
    try {
      await api('/portal/integrations/mapping', {
        method: 'POST',
        body: JSON.stringify({ mapping_json: clean }),
      })
      toast('ok', 'Mapeamento salvo!')
    } catch (e: any) {
      toast('err', e.message || 'Salvar mapeamento ainda não disponível neste ambiente.')
    } finally {
      setSavingMapping(false)
    }
  }

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <section className="szv2-conn-panel">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Integrações
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Conecte sua plataforma à FALK e receba pedidos automaticamente.
        </p>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* Sub-abas */}
      <div className="szv2-prod-subtabs" role="tablist" style={{ marginBottom: 16 }}>
        <button
          type="button"
          className={`szv2-prod-subtab${tab === 'endpoint' ? ' szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'endpoint'}
          onClick={() => setTab('endpoint')}
        >
          Endpoint
        </button>
        <button
          type="button"
          className={`szv2-prod-subtab${tab === 'config' ? ' szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'config'}
          onClick={() => setTab('config')}
        >
          Configurações
        </button>
      </div>

      {loading ? (
        <div className="szv2-card" style={{ padding: 20 }}>
          <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
        </div>
      ) : tab === 'endpoint' ? (
        <EndpointTab
          endpointUrl={endpointUrl}
          hasToken={hasToken}
          tokenPrefix={tokenPrefix}
          freshToken={freshToken}
          isActive={isActive}
          lastReceived={lastReceived}
          totalRecv={totalRecv}
          logs={logs}
          openLogId={openLogId}
          renewing={renewing}
          reprocessing={reprocessing}
          onCopyUrl={copyUrl}
          onRenewToken={renewToken}
          onReprocess={reprocessLast}
          onTogglePause={togglePause}
          onRefreshLogs={load}
          onClearLogs={clearLogs}
          onToggleLog={(id) => setOpenLogId(prev => (prev === id ? null : id))}
        />
      ) : (
        <ConfigTab
          isOn={isOn}
          onToggle={toggleKey}
          mapping={mapping}
          onSetMapField={setMapField}
          onClearMapField={clearMapField}
          onSaveMapping={saveMapping}
          savingMapping={savingMapping}
        />
      )}
    </section>
  )
}

// ── Aba Endpoint ────────────────────────────────────────────────────────────

function EndpointTab(props: {
  endpointUrl: string
  hasToken: boolean
  tokenPrefix: string
  freshToken: string
  isActive: boolean
  lastReceived: string
  totalRecv: number
  logs: IntLog[]
  openLogId: number | null
  renewing: boolean
  reprocessing: boolean
  onCopyUrl: () => void
  onRenewToken: () => void
  onReprocess: () => void
  onTogglePause: (pauseChecked: boolean) => void
  onRefreshLogs: () => void
  onClearLogs: () => void
  onToggleLog: (id: number) => void
}) {
  const {
    endpointUrl, hasToken, tokenPrefix, freshToken, isActive, lastReceived, totalRecv, logs, openLogId,
    renewing, reprocessing, onCopyUrl, onRenewToken, onReprocess, onTogglePause,
    onRefreshLogs, onClearLogs, onToggleLog,
  } = props

  return (
    <>
      <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        {/* Card: Endpoint da integração */}
        <div className="szv2-card" style={{ flex: 1, minWidth: 280, padding: 20 }}>
          <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>
            Endpoint da integração
          </h3>
          <p style={{ margin: '0 0 14px', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Use este endpoint para enviar pedidos da sua plataforma para a FALK LOG.
          </p>

          <label className="sz-login-label">URL do seu endpoint</label>
          <div style={{ display: 'flex', gap: 8, width: '100%', alignItems: 'center' }}>
            <input
              type="text"
              className="sz-login-input"
              readOnly
              value={endpointUrl}
              placeholder="Endpoint indisponível neste ambiente"
              style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12, flex: 1, minWidth: 0, marginBottom: 0 }}
            />
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
              style={{ flexShrink: 0 }}
              onClick={onCopyUrl}
              disabled={!endpointUrl}
            >
              Copiar
            </button>
          </div>

          {freshToken && (
            <div style={{ margin: '12px 0', padding: 12, borderRadius: 8, background: 'var(--szv2-warning-bg)', border: '1px solid var(--szv2-warning)' }}>
              <label className="sz-login-label" style={{ marginBottom: 4 }}>
                Sua chave de API (copie agora — não será mostrada de novo)
              </label>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input
                  type="text"
                  className="sz-login-input"
                  readOnly
                  value={freshToken}
                  style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: 12, flex: 1, minWidth: 0, marginBottom: 0 }}
                  onFocus={(e) => e.currentTarget.select()}
                />
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  onClick={async () => { await navigator.clipboard.writeText(freshToken) }}
                >
                  Copiar
                </button>
              </div>
            </div>
          )}
          {!freshToken && hasToken && tokenPrefix && (
            <p style={{ margin: '8px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)', fontFamily: 'var(--szv2-font-mono)' }}>
              Chave ativa: {tokenPrefix}••••••••
            </p>
          )}

          {/* Badges + ações na MESMA região (pedido do dono): "Renovar token" e
              "Reprocessar último" alinhados ao lado dos badges POST/JSON. */}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginTop: 8 }}>
            <span className="sz-badge szv2-badge-neutral">POST</span>
            <span className="sz-badge szv2-badge-neutral">JSON</span>
            {hasToken && <span className="sz-badge szv2-badge-success">Token ativo</span>}
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginLeft: 'auto' }}>
              <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={onRenewToken} disabled={renewing}>
                {renewing ? 'Renovando…' : 'Renovar token'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={onReprocess} disabled={reprocessing}>
                {reprocessing ? 'Reprocessando…' : 'Reprocessar último'}
              </button>
            </div>
          </div>
        </div>

        {/* Card: Status da integração */}
        <div className="szv2-card" style={{ minWidth: 240, flex: '0 0 auto', padding: 20 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
            <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Status da integração</h3>
            <span className={`sz-badge ${isActive ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
              {isActive ? 'Ativa' : 'Pausada'}
            </span>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 13 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
              <span style={{ color: 'var(--szv2-text-muted)' }}>Último recebimento</span>
              <span>{lastReceived ? dtTime(lastReceived) : '—'}</span>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
              <span style={{ color: 'var(--szv2-text-muted)' }}>Total recebido</span>
              <span>{totalRecv} registros</span>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
              <span style={{ color: 'var(--szv2-text-muted)' }}>Regra de frete</span>
              <span>{FREIGHT_RULE}</span>
            </div>
          </div>

          <div style={{ borderTop: '1px solid var(--szv2-divider)', marginTop: 14, paddingTop: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 13 }}>Pausar recebimento</span>
            <label className="szv2-toggle-lbl">
              <input
                type="checkbox"
                checked={!isActive}
                onChange={e => onTogglePause(e.target.checked)}
              />
              <span className="szv2-toggle-slider" />
            </label>
          </div>
        </div>
      </div>

      {/* Logs de recebimento */}
      <div className="szv2-card" style={{ padding: 20, marginTop: 16 }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, marginBottom: 14 }}>
          <div>
            <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Logs de recebimento</h3>
            <p style={{ margin: 0, fontSize: 13, color: 'var(--szv2-text-muted)' }}>Clique em um registro para expandir o payload.</p>
          </div>
          {/* Botões lado a lado, alinhados à direita (pedido do dono): antes
              estavam empilhados (flex-direction: column). */}
          <div style={{ display: 'flex', flexDirection: 'row', gap: 8, justifyContent: 'flex-end', flexShrink: 0 }}>
            <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm" onClick={onRefreshLogs}>
              Atualizar
            </button>
            <button type="button" className="szv2-btn szv2-btn-danger szv2-btn-sm" onClick={onClearLogs}>
              Limpar logs
            </button>
          </div>
        </div>

        {logs.length === 0 ? (
          <EmptyState
            icon="📥"
            title="Nenhum log encontrado"
            description="Os registros de recebimento aparecem aqui após o primeiro envio da sua plataforma."
          />
        ) : (
          <div>
            {logs.map(log => {
              const ok = ['processed', 'processado', 'ok'].includes(log.status)
              const open = openLogId === log.id
              let pretty = log.payload
              try {
                pretty = JSON.stringify(JSON.parse(log.payload), null, 2)
              } catch {
                /* mantém raw */
              }
              return (
                <div
                  key={log.id}
                  onClick={() => onToggleLog(log.id)}
                  style={{ cursor: 'pointer', padding: '10px 0', borderBottom: '1px solid var(--szv2-divider)' }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                    <span style={{ width: 8, height: 8, borderRadius: '50%', flexShrink: 0, display: 'inline-block', background: ok ? 'var(--szv2-success)' : 'var(--szv2-danger)' }} />
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)', minWidth: 90 }}>{dtTime(log.received_at)}</span>
                    <span style={{ fontSize: 13, fontFamily: 'var(--szv2-font-mono)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {log.external_id || '—'}
                    </span>
                    <span className={`sz-badge ${ok ? 'szv2-badge-success' : 'szv2-badge-danger'}`}>{ok ? 'Processado' : 'Erro'}</span>
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)', maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {log.message}
                    </span>
                    <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', transition: 'transform .2s', transform: open ? 'rotate(180deg)' : undefined }}>▾</span>
                  </div>
                  {open && (
                    <div style={{ marginTop: 10 }}>
                      {pretty ? (
                        <pre style={{ overflow: 'auto', fontSize: 12, fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-surface-alt)', padding: 12, borderRadius: 8, margin: 0 }}>
                          {pretty}
                        </pre>
                      ) : (
                        <p style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Nenhum payload armazenado.</p>
                      )}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </>
  )
}

// ── Aba Configurações ───────────────────────────────────────────────────────

function ConfigTab(props: {
  isOn: (key: string) => boolean
  onToggle: (key: string, nextOn: boolean) => void
  mapping: Record<string, string>
  onSetMapField: (key: string, value: string) => void
  onClearMapField: (key: string) => void
  onSaveMapping: () => void
  savingMapping: boolean
}) {
  const { isOn, onToggle, mapping, onSetMapField, onClearMapField, onSaveMapping, savingMapping } = props

  return (
    <div style={{ display: 'flex', gap: 16, alignItems: 'stretch', flexWrap: 'wrap' }}>
      {/* Coluna esquerda: toggles + regras */}
      <div style={{ flex: 1, minWidth: 320, display: 'flex', flexDirection: 'column', gap: 16 }}>
        {/* Configurações da integração */}
        <div className="szv2-card" style={{ padding: 20 }}>
          <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Configurações da integração</h3>
          <p style={{ margin: '0 0 12px', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Defina como os pedidos recebidos são processados.</p>
          {CONFIG_TOGGLES.map(t => (
            <div
              key={t.key}
              style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', padding: '12px 0', borderBottom: '1px solid var(--szv2-divider)', gap: 12 }}
            >
              <div>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--szv2-text)', marginBottom: 2 }}>{t.label}</div>
                <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  {t.help}
                  {!t.whitelisted && ' (em breve)'}
                </div>
              </div>
              <label className="szv2-toggle-lbl" style={{ flexShrink: 0, opacity: t.whitelisted ? 1 : 0.45 }}>
                <input
                  type="checkbox"
                  checked={t.whitelisted ? isOn(t.key) : false}
                  disabled={!t.whitelisted}
                  onChange={e => onToggle(t.key, e.target.checked)}
                />
                <span className="szv2-toggle-slider" />
              </label>
            </div>
          ))}
        </div>

        {/* Regras de processamento (placeholder estático, igual ao WP) */}
        <div className="szv2-card" style={{ padding: 20 }}>
          <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Regras de processamento</h3>
          <p style={{ margin: '0 0 14px', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Como o frete é cotado a cada pedido recebido.</p>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', border: '1px solid var(--szv2-border)', borderRadius: 8, overflow: 'hidden' }}>
            {['Regra de frete', 'Classe de entrega permitida', 'Transportadoras'].map((h, i) => (
              <div
                key={h}
                style={{ padding: '8px 12px', fontSize: 12, fontWeight: 600, color: 'var(--szv2-text-muted)', background: 'var(--szv2-surface-alt)', borderBottom: '1px solid var(--szv2-border)', borderLeft: i > 0 ? '1px solid var(--szv2-border)' : undefined }}
              >
                {h}
              </div>
            ))}
            {[0, 1, 2].map(i => (
              <div key={i} style={{ padding: '10px 12px', fontSize: 13, color: 'var(--szv2-text-muted)', borderLeft: i > 0 ? '1px solid var(--szv2-border)' : undefined }}>
                —
              </div>
            ))}
          </div>
          <div style={{ marginTop: 10, padding: '10px 12px', background: 'var(--szv2-surface-alt)', borderLeft: '3px solid var(--szv2-border)', borderRadius: '0 6px 6px 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
            O menor frete é escolhido no momento da cotação, entre as transportadoras e classes liberadas para o cadastro.
          </div>
        </div>
      </div>

      {/* Coluna direita: mapeamento de campos */}
      <div className="szv2-card" style={{ flex: 1, minWidth: 320, padding: 20 }}>
        <h3 style={{ margin: '0 0 4px', fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Mapeamento de campos</h3>
        <p style={{ margin: '0 0 14px', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Associe os campos recebidos da plataforma aos campos da FALK LOG.
        </p>

        {MAP_FIELDS.map(f => (
          <div key={f.key} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 0', borderBottom: '1px solid var(--szv2-divider)' }}>
            <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)', minWidth: 150, flexShrink: 0 }}>{f.label}</span>
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 12, fontFamily: 'var(--szv2-font-mono)', background: 'var(--szv2-surface-alt)', padding: '4px 8px', borderRadius: 6, color: 'var(--szv2-text-muted)' }}>
              {f.key} →
            </div>
            <input
              type="text"
              className="sz-login-input"
              value={mapping[f.key] || ''}
              placeholder="campo.do.payload"
              onChange={e => onSetMapField(f.key, e.target.value)}
              style={{ flex: 1, fontSize: 12, fontFamily: 'var(--szv2-font-mono)', marginBottom: 0 }}
            />
            <button
              type="button"
              className="szv2-btn szv2-btn-secondary szv2-btn-sm"
              title="Limpar"
              onClick={() => onClearMapField(f.key)}
              style={{ flexShrink: 0, padding: '4px 10px' }}
            >
              ×
            </button>
          </div>
        ))}

        <div style={{ marginTop: 12 }}>
          <button type="button" className="szv2-btn szv2-btn-brand" onClick={onSaveMapping} disabled={savingMapping}>
            {savingMapping ? 'Salvando…' : 'Salvar mapeamento'}
          </button>
        </div>
      </div>
    </div>
  )
}
