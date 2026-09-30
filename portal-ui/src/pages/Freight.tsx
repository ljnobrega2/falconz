// Frete / Logística — porte fiel de templates/portal/v2/sections/freight.php.
// Ligada ao go/portal: GET /portal/freight, POST /portal/freight/preferred,
// POST /portal/freight/blocked.
//
// Regras de negócio espelhadas do WP (freight.php + szV2FrSave/szV2FrAutoCheck):
//  - 2 subtabs visíveis: Favoritas / Bloqueadas (+ painel Remetente OCULTO, sem tab).
//  - "Mais barata entre todas" vem checada quando preferred_ids está vazio
//    (pref_cheapest derivado no backend). Marcar o toggle limpa as individuais
//    (szV2FrAutoCheck). Marcar individuais não desmarca o toggle na UI — paridade
//    literal com a JS do WP; o backend deriva cheapest=preferred vazio ao salvar.
//  - Bloqueio tem prioridade sobre permitidas (texto de rodapé do WP).
//  - Mensagem de save é INLINE (div ao lado do botão), verde no sucesso / vermelha
//    no erro, some após 3s — igual ao WP (não usa toast). A mensagem vem do backend
//    (varia entre set/reset).
//  - carriers vem [] (catálogo Melhor Envio sem mirror Go ainda) → empty-state fiel.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { emitToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'

// ── Tipos do contrato go/portal (envelope achatado: {ok, ...}) ──────────────────

type FreightMethod = { id: number; name: string }
type FreightCarrier = { company: string; methods: FreightMethod[] }
type FreightSender = { name: string; document: string; telephone: string }

type GetResp = {
  ok: boolean
  class_id: number
  sender: FreightSender
  carriers: FreightCarrier[]
  preferred_ids: number[]
  blocked_ids: number[]
  pref_cheapest: boolean
}

type SaveResp = { ok: boolean; success: boolean; message: string }

type InlineMsg = { text: string; kind: 'ok' | 'err' } | null

export default function Freight() {
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const [sender, setSender] = useState<FreightSender>({ name: '', document: '', telephone: '' })
  const [carriers, setCarriers] = useState<FreightCarrier[]>([])

  // subtab ativa: 'favoritas' | 'bloqueadas' (Favoritas é a inicial, como no WP)
  const [tab, setTab] = useState<'favoritas' | 'bloqueadas'>('favoritas')

  // Favoritas. "Mais barata" não tem estado próprio — é derivada (prefIds vazio).
  const [prefIds, setPrefIds] = useState<Set<number>>(new Set())
  const [prefSaving, setPrefSaving] = useState(false)
  const [prefMsg, setPrefMsg] = useState<InlineMsg>(null)

  // Bloqueadas
  const [blkIds, setBlkIds] = useState<Set<number>>(new Set())
  const [blkSaving, setBlkSaving] = useState(false)
  const [blkMsg, setBlkMsg] = useState<InlineMsg>(null)

  useEffect(() => {
    api<GetResp>('/portal/freight')
      .then(r => {
        setSender(r.sender || { name: '', document: '', telephone: '' })
        setCarriers(r.carriers || [])
        setPrefIds(new Set(r.preferred_ids || []))
        setBlkIds(new Set(r.blocked_ids || []))
      })
      .catch(e => setErr(e.message || 'Erro ao carregar configurações de frete'))
      .finally(() => setLoading(false))
  }, [])

  function togglePref(id: number) {
    setPrefIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function toggleBlk(id: number) {
    setBlkIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // szV2FrSave('pref'/'blk'): POST → mostra r.message inline 3s.
  async function savePref() {
    setPrefSaving(true)
    try {
      const r = await api<SaveResp>('/portal/freight/preferred', {
        method: 'POST',
        body: JSON.stringify({ method_ids: Array.from(prefIds) }),
      })
      flashMsg(setPrefMsg, { text: r.message || 'Salvo!', kind: r.success ? 'ok' : 'err' })
      emitToast(r.success ? 'ok' : 'err', r.message || 'Preferências salvas.')
    } catch (e: any) {
      flashMsg(setPrefMsg, { text: e.message || 'Erro.', kind: 'err' })
      emitToast('err', e.message || 'Erro ao salvar preferências.')
    } finally {
      setPrefSaving(false)
    }
  }

  async function saveBlk() {
    setBlkSaving(true)
    try {
      const r = await api<SaveResp>('/portal/freight/blocked', {
        method: 'POST',
        body: JSON.stringify({ method_ids: Array.from(blkIds) }),
      })
      flashMsg(setBlkMsg, { text: r.message || 'Salvo!', kind: r.success ? 'ok' : 'err' })
      emitToast(r.success ? 'ok' : 'err', r.message || 'Bloqueios salvos.')
    } catch (e: any) {
      flashMsg(setBlkMsg, { text: e.message || 'Erro.', kind: 'err' })
      emitToast('err', e.message || 'Erro ao salvar bloqueios.')
    } finally {
      setBlkSaving(false)
    }
  }

  // Mostra a mensagem inline e some após 3s (igual ao setTimeout do WP).
  function flashMsg(setter: (m: InlineMsg) => void, m: InlineMsg) {
    setter(m)
    window.setTimeout(() => setter(null), 3000)
  }

  if (loading) {
    return (
      <section>
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      </section>
    )
  }

  return (
    <section id="sec-freight">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Frete
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Defina quais modalidades de frete sua loja prioriza ou bloqueia no checkout.
        </p>
      </div>

      {!!err && <div className="sz-alert-danger">{err}</div>}

      <div className="szv2-prod-subtabs" role="tablist">
        <button
          type="button"
          className={`szv2-prod-subtab ${tab === 'favoritas' ? 'szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'favoritas'}
          onClick={() => setTab('favoritas')}
        >
          Favoritas
        </button>
        <button
          type="button"
          className={`szv2-prod-subtab ${tab === 'bloqueadas' ? 'szv2-prod-subtab--active' : ''}`}
          role="tab"
          aria-selected={tab === 'bloqueadas'}
          onClick={() => setTab('bloqueadas')}
        >
          Bloqueadas
        </button>
      </div>

      {/* Remetente — painel OCULTO no WP (sem tab que o revele). Mantido fiel. */}
      <div className="szv2-conn-panel szv2-prod-sub--hidden" id="szv2-panel-fr-remetente">
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Dados do remetente</h2>
              <p className="szv2-card-sub">Informações utilizadas para etiquetas de envio.</p>
            </div>
            <button type="button" className="szv2-btn szv2-btn-secondary szv2-btn-sm">
              Editar em Configurações
            </button>
          </div>
          <div className="szv2-info-grid">
            <div className="szv2-info-cell">
              <div className="szv2-info-label">Nome / Razão Social</div>
              <div className="szv2-info-value">{sender.name || '—'}</div>
            </div>
            <div className="szv2-info-cell">
              <div className="szv2-info-label">CNPJ / CPF</div>
              <div className="szv2-info-value szv2-num">{sender.document || '—'}</div>
            </div>
            <div className="szv2-info-cell">
              <div className="szv2-info-label">Telefone</div>
              <div className="szv2-info-value">{sender.telephone || '—'}</div>
            </div>
          </div>
          <p style={{ marginTop: 12, fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            Para alterar estes dados, acesse <strong>Configurações → Conta</strong> ou abra um chamado em{' '}
            <strong>Suporte</strong>.
          </p>
        </div>
      </div>

      {/* Favoritas */}
      <div
        className={`szv2-conn-panel ${tab === 'favoritas' ? '' : 'szv2-prod-sub--hidden'}`}
        id="szv2-panel-fr-favoritas"
      >
        <div className="szv2-card">
          <div className="szv2-card-head" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
            <div>
              <h2>Modalidades de frete favoritas</h2>
              <p className="szv2-card-sub">
                Escolha as opções priorizadas. A FALK LOG recomenda a menor entre as selecionadas.
              </p>
            </div>
            {carriers.length > 0 && (
              <label
                className={`szv2-fr-cheapest-toggle${prefIds.size === 0 ? ' szv2-fr-cheapest-toggle--active' : ''}`}
                style={{ whiteSpace: 'nowrap' }}
                title="Marcada automaticamente quando nenhuma modalidade abaixo está selecionada."
              >
                <input type="checkbox" id="szv2-fr-pref-cheapest" checked={prefIds.size === 0} readOnly disabled />
                <span>Mais barata entre todas as modalidades disponíveis</span>
              </label>
            )}
          </div>
          {carriers.length === 0 ? (
            <EmptyState
              title="Nenhuma transportadora disponível"
              description="As transportadoras são configuradas pelo operador FALK LOG."
            />
          ) : (
            <>
              <div className="szv2-fr-carrier-grid" id="szv2-fr-pref-grid">
                {carriers.map(carrier => (
                  <div className="szv2-fr-carrier-card" key={carrier.company}>
                    <div className="szv2-fr-carrier-name">{carrier.company}</div>
                    {carrier.methods.map(m => (
                      <label className="szv2-fr-method-label" key={m.id}>
                        <input
                          type="checkbox"
                          className="szv2-fr-pref-cb"
                          name="fr_pref[]"
                          value={m.id}
                          checked={prefIds.has(m.id)}
                          onChange={() => togglePref(m.id)}
                        />
                        <span className="szv2-fr-method-name">{m.name || '—'}</span>
                      </label>
                    ))}
                  </div>
                ))}
              </div>
              <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', gap: 12 }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand"
                  onClick={savePref}
                  disabled={prefSaving}
                >
                  {prefSaving ? 'Salvando…' : 'Salvar modalidades'}
                </button>
                {!!prefMsg && (
                  <div
                    id="szv2-fr-pref-msg"
                    style={{
                      fontSize: 13,
                      color: prefMsg.kind === 'ok' ? 'var(--szv2-success)' : 'var(--szv2-danger)',
                    }}
                  >
                    {prefMsg.text}
                  </div>
                )}
              </div>
              <p style={{ marginTop: 10, fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Selecionando mais de uma, a FALK LOG mantém somente essas modalidades e recomenda a menor
                disponível.
              </p>
            </>
          )}
        </div>
      </div>

      {/* Bloqueadas */}
      <div
        className={`szv2-conn-panel ${tab === 'bloqueadas' ? '' : 'szv2-prod-sub--hidden'}`}
        id="szv2-panel-fr-bloqueadas"
      >
        <div className="szv2-card">
          <div className="szv2-card-head" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
            <div>
              <h2>Transportadoras bloqueadas</h2>
              <p className="szv2-card-sub">
                As modalidades marcadas aqui NÃO aparecem no checkout nem entram na recomendação.
              </p>
            </div>
            {carriers.length > 0 && (
              <label
                className={`szv2-fr-cheapest-toggle${blkIds.size === 0 ? ' szv2-fr-cheapest-toggle--active' : ''}`}
                style={{ whiteSpace: 'nowrap' }}
                title="Marcada automaticamente quando nenhuma transportadora está bloqueada."
              >
                <input type="checkbox" checked={blkIds.size === 0} readOnly disabled />
                <span>Todas as transportadoras liberadas</span>
              </label>
            )}
          </div>
          {carriers.length === 0 ? (
            <EmptyState
              title="Nenhuma transportadora disponível"
              description="As transportadoras são configuradas pelo operador FALK."
            />
          ) : (
            <>
              <div className="szv2-fr-carrier-grid" id="szv2-fr-blk-grid">
                {carriers.map(carrier => (
                  <div className="szv2-fr-carrier-card" key={carrier.company}>
                    <div className="szv2-fr-carrier-name">{carrier.company}</div>
                    {carrier.methods.map(m => (
                      <label className="szv2-fr-method-label" key={m.id}>
                        <input
                          type="checkbox"
                          className="szv2-fr-blk-cb"
                          name="fr_blk[]"
                          value={m.id}
                          checked={blkIds.has(m.id)}
                          onChange={() => toggleBlk(m.id)}
                        />
                        <span className="szv2-fr-method-name">{m.name || '—'}</span>
                      </label>
                    ))}
                  </div>
                ))}
              </div>
              <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', gap: 12 }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand"
                  onClick={saveBlk}
                  disabled={blkSaving}
                >
                  {blkSaving ? 'Salvando…' : 'Salvar bloqueios'}
                </button>
                {!!blkMsg && (
                  <div
                    id="szv2-fr-blk-msg"
                    style={{
                      fontSize: 13,
                      color: blkMsg.kind === 'ok' ? 'var(--szv2-success)' : 'var(--szv2-danger)',
                    }}
                  >
                    {blkMsg.text}
                  </div>
                )}
              </div>
              <p style={{ marginTop: 10, fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Bloqueio tem prioridade sobre modalidades permitidas. Modalidades marcadas aqui não aparecem
                no checkout.
              </p>
            </>
          )}
        </div>
      </div>
    </section>
  )
}
