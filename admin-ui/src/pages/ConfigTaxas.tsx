// Taxas & Cálculo — porta de escrita central da "regra de cálculo" que governa
// o breakdown financeiro de TODOS os pedidos (líquido produtor, take, repasse
// motoboy). Pedido do dono: "regra de cálculo muda SÓ via admin".
//
// Backend: GET/POST /wp-json/senderzz/v1/admin/config/taxas (config_taxas.go).
//   - producer_transaction_fee_pct  — editável (0..100), grava sz_producer_transaction_fee_pct
//   - motoboy_repasse_padrao        — editável (>= 0 R$), grava motoboy_repasse_padrao
//   - affiliate_transaction_fee_pct — READ-ONLY (const fixa hoje, 4,99%)
//
// Molde de UI: MotoboyConfig.tsx (form + validação client-side + toast + loading).
import { useEffect, useMemo, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'

type ProducerFeeRow = { producer_id: number; name: string; pct: number | null }

type TaxasConfig = {
  producer_transaction_fee_pct: number
  motoboy_repasse_padrao: number
  affiliate_transaction_fee_pct: number
  affiliate_transaction_fee_read_only: boolean
}

type Editable = Pick<TaxasConfig, 'producer_transaction_fee_pct' | 'motoboy_repasse_padrao'>
type Errors = Partial<Record<keyof Editable, string>>

const DEFAULTS: TaxasConfig = {
  producer_transaction_fee_pct: 4.99,
  motoboy_repasse_padrao: 18,
  affiliate_transaction_fee_pct: 4.99,
  affiliate_transaction_fee_read_only: true,
}

function validate(c: Editable): Errors {
  const errs: Errors = {}
  if (!Number.isFinite(c.producer_transaction_fee_pct)
    || c.producer_transaction_fee_pct < 0
    || c.producer_transaction_fee_pct > 100) {
    errs.producer_transaction_fee_pct = 'Taxa deve estar entre 0 e 100'
  }
  if (!Number.isFinite(c.motoboy_repasse_padrao) || c.motoboy_repasse_padrao < 0) {
    errs.motoboy_repasse_padrao = 'Repasse deve ser maior ou igual a zero'
  }
  return errs
}

export default function ConfigTaxas() {
  const [cfg, setCfg] = useState<TaxasConfig>(DEFAULTS)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [errs, setErrs] = useState<Errors>({})
  const showToast = useToast()

  // Taxa de transação por PRODUTOR (override individual, fallback = taxa global acima).
  const [producers, setProducers] = useState<ProducerFeeRow[]>([])
  const [producerLoading, setProducerLoading] = useState(true)
  const [producerEdits, setProducerEdits] = useState<Record<number, string>>({})
  const [producerSaving, setProducerSaving] = useState(false)
  const [removingProducer, setRemovingProducer] = useState<number | null>(null)

  async function loadProducers() {
    setProducerLoading(true)
    try {
      const data = await api<{ items: ProducerFeeRow[] }>('/config/taxas/produtores')
      const items = data?.items || []
      setProducers(items)
      const edits: Record<number, string> = {}
      for (const p of items) {
        if (p.pct != null) edits[p.producer_id] = String(p.pct)
      }
      setProducerEdits(edits)
    } catch (e: any) {
      showToast('err', e?.message || 'Erro ao carregar taxas por produtor')
    } finally {
      setProducerLoading(false)
    }
  }

  useEffect(() => { loadProducers() }, [])

  const compiledProducerRules = useMemo(
    () =>
      producers
        .map(p => {
          const raw = producerEdits[p.producer_id]
          if (raw === undefined || raw.trim() === '') return null
          const pct = parseFloat(raw.replace(',', '.'))
          if (!Number.isFinite(pct)) return null
          return { producer_id: p.producer_id, pct }
        })
        .filter((r): r is { producer_id: number; pct: number } => r !== null),
    [producers, producerEdits],
  )

  async function handleSaveProducerFees() {
    for (const rule of compiledProducerRules) {
      if (rule.pct < 0 || rule.pct > 100) {
        showToast('err', `Produtor #${rule.producer_id}: taxa deve estar entre 0 e 100.`)
        return
      }
    }
    setProducerSaving(true)
    try {
      await api('/config/taxas/produtores', {
        method: 'POST',
        body: JSON.stringify({ rules: compiledProducerRules }),
      })
      showToast('ok', 'Taxas por produtor salvas.')
      await loadProducers()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao salvar taxas por produtor')
    } finally {
      setProducerSaving(false)
    }
  }

  async function handleRemoveProducerFee(p: ProducerFeeRow) {
    if (!window.confirm(`Remover a taxa individual de "${p.name}"? Volta a usar a taxa global (${cfg.producer_transaction_fee_pct}%).`)) {
      return
    }
    setRemovingProducer(p.producer_id)
    try {
      await api(`/config/taxas/produtores/${p.producer_id}`, { method: 'DELETE' })
      showToast('ok', `Taxa individual de "${p.name}" removida.`)
      await loadProducers()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao remover taxa individual')
    } finally {
      setRemovingProducer(null)
    }
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const data = await api<TaxasConfig>('/config/taxas')
      setCfg({
        producer_transaction_fee_pct:
          Number.isFinite(Number(data.producer_transaction_fee_pct))
            ? Number(data.producer_transaction_fee_pct)
            : DEFAULTS.producer_transaction_fee_pct,
        motoboy_repasse_padrao:
          Number.isFinite(Number(data.motoboy_repasse_padrao))
            ? Number(data.motoboy_repasse_padrao)
            : DEFAULTS.motoboy_repasse_padrao,
        affiliate_transaction_fee_pct:
          Number.isFinite(Number(data.affiliate_transaction_fee_pct))
            ? Number(data.affiliate_transaction_fee_pct)
            : DEFAULTS.affiliate_transaction_fee_pct,
        affiliate_transaction_fee_read_only:
          data.affiliate_transaction_fee_read_only ?? true,
      })
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar configurações de taxas')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function update<K extends keyof Editable>(key: K, value: Editable[K]) {
    setCfg(prev => ({ ...prev, [key]: value }))
    setErrs(prev => {
      if (!prev[key]) return prev
      const { [key]: _omit, ...rest } = prev
      return rest
    })
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    const validation = validate(cfg)
    setErrs(validation)
    if (Object.keys(validation).length > 0) {
      showToast('err', 'Corrija os campos destacados antes de salvar')
      return
    }
    setSaving(true)
    try {
      // Só os campos editáveis vão no POST — a taxa do afiliado é read-only e o
      // backend ignora qualquer valor enviado para ela.
      await api<TaxasConfig>('/config/taxas', {
        method: 'POST',
        body: JSON.stringify({
          producer_transaction_fee_pct: cfg.producer_transaction_fee_pct,
          motoboy_repasse_padrao: cfg.motoboy_repasse_padrao,
        }),
      })
      showToast('ok', 'Taxas salvas com sucesso')
      await load()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar as taxas')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Taxas &amp; Cálculo</h1>
          <p>Regra de cálculo central — alterar aqui reflete em todos os pedidos</p>
        </div>
      </div>

      {/* Nota de impacto — deixa explícito que isto é a fonte da verdade. */}
      <div
        className="szv2-card"
        style={{
          marginBottom: 16,
          borderLeft: '3px solid var(--szv2-brand)',
          background: 'rgba(234,88,12,.10)',
        }}
      >
        <p style={{ margin: 0, fontSize: 13, lineHeight: 1.5 }}>
          <strong>Atenção:</strong> estas taxas governam o cálculo financeiro de{' '}
          <strong>todos os pedidos</strong> — líquido do produtor, take da
          plataforma e repasse ao motoboy. Qualquer alteração passa a valer
          imediatamente para os cálculos seguintes, sem necessidade de deploy.
        </p>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {loading ? (
        <div
          className="szv2-card"
          style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}
          role="status"
          aria-live="polite"
        >
          Carregando taxas…
        </div>
      ) : (
        <form onSubmit={handleSave}>
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Parâmetros de cálculo</h2>
                <p className="szv2-card-sub">
                  Valores aplicados ao breakdown financeiro de todo o sistema.
                </p>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
              {/* Taxa transação produtor (%) — editável */}
              <div className="szv2-field">
                <label className="szv2-label">Taxa transação produtor (%)</label>
                <input
                  className="szv2-input"
                  type="number"
                  min={0}
                  max={100}
                  step={0.01}
                  value={
                    Number.isFinite(cfg.producer_transaction_fee_pct)
                      ? cfg.producer_transaction_fee_pct
                      : ''
                  }
                  onChange={e => update('producer_transaction_fee_pct', Number(e.target.value))}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Percentual descontado do total de cada pedido do produtor
                  (taxa de transação). Compõe o take da plataforma.
                </small>
                {errs.producer_transaction_fee_pct && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.producer_transaction_fee_pct}
                  </small>
                )}
              </div>

              {/* Repasse padrão ao motoboy (R$) — editável */}
              <div className="szv2-field">
                <label className="szv2-label">Repasse padrão ao motoboy (R$)</label>
                <input
                  className="szv2-input"
                  type="number"
                  min={0}
                  step={0.01}
                  value={
                    Number.isFinite(cfg.motoboy_repasse_padrao)
                      ? cfg.motoboy_repasse_padrao
                      : ''
                  }
                  onChange={e => update('motoboy_repasse_padrao', Number(e.target.value))}
                  disabled={saving}
                  autoComplete="off"
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Valor padrão (R$) repassado ao motoboy por entrega quando não há
                  valor específico definido.
                </small>
                {errs.motoboy_repasse_padrao && (
                  <small style={{ color: 'var(--szv2-danger)', fontSize: 12 }}>
                    {errs.motoboy_repasse_padrao}
                  </small>
                )}
              </div>

              {/* Taxa transação afiliado (%) — READ-ONLY (const fixa hoje) */}
              <div className="szv2-field">
                <label className="szv2-label">Taxa transação afiliado (%)</label>
                <input
                  className="szv2-input"
                  type="number"
                  value={
                    Number.isFinite(cfg.affiliate_transaction_fee_pct)
                      ? cfg.affiliate_transaction_fee_pct
                      : ''
                  }
                  readOnly
                  disabled
                  aria-readonly="true"
                  autoComplete="off"
                  style={{ background: 'var(--szv2-bg)', cursor: 'not-allowed' }}
                />
                <small style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                  Fixa hoje em {cfg.affiliate_transaction_fee_pct.toLocaleString('pt-BR', {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                  })}% — constante do serviço, não editável pelo painel.
                </small>
              </div>
            </div>
          </div>

          <div style={{ marginTop: 20, display: 'flex', gap: 12, alignItems: 'center' }}>
            <button
              type="submit"
              className="szv2-btn szv2-btn-brand"
              disabled={saving || loading}
              style={{ minWidth: 200 }}
            >
              {saving ? 'Salvando…' : 'Salvar taxas'}
            </button>
          </div>
        </form>
      )}

      {/* ============ Taxa de transação POR PRODUTOR (override individual) ============ */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Taxa de transação por produtor</h2>
            <p className="szv2-card-sub">
              Sobrescreve a taxa global acima para um produtor específico. Deixe em
              branco para usar a taxa padrão ({cfg.producer_transaction_fee_pct}%).
            </p>
          </div>
        </div>

        {producerLoading ? (
          <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando produtores…
          </div>
        ) : producers.length === 0 ? (
          <div
            style={{
              background: '#fffbeb',
              border: '1px solid #fcd34d',
              padding: '12px 16px',
              borderRadius: 6,
              marginTop: 8,
            }}
          >
            ⚠️ Nenhum produtor cadastrado.
          </div>
        ) : (
          <>
            <div style={{ overflowX: 'auto', marginTop: 8 }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Produtor</th>
                    <th style={{ width: 160 }}>Taxa % (individual)</th>
                    <th style={{ width: 100 }}></th>
                  </tr>
                </thead>
                <tbody>
                  {producers.map(p => {
                    const raw = producerEdits[p.producer_id] ?? ''
                    const hasRule = raw.trim() !== ''
                    return (
                      <tr key={p.producer_id}>
                        <td>
                          <strong>{p.name}</strong>
                          <br />
                          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                            #{p.producer_id}
                          </span>
                        </td>
                        <td>
                          <input
                            type="number"
                            min="0"
                            max="100"
                            step="0.01"
                            className="szv2-input"
                            value={raw}
                            placeholder={String(cfg.producer_transaction_fee_pct)}
                            onChange={e =>
                              setProducerEdits(prev => ({ ...prev, [p.producer_id]: e.target.value }))
                            }
                            style={{ width: '100%' }}
                          />
                        </td>
                        <td>
                          {hasRule && (
                            <button
                              type="button"
                              className="szv2-btn-danger"
                              style={{ padding: '6px 12px', fontSize: 12.5 }}
                              disabled={removingProducer === p.producer_id}
                              onClick={() => handleRemoveProducerFee(p)}
                            >
                              {removingProducer === p.producer_id ? 'Removendo…' : 'Remover'}
                            </button>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div style={{ marginTop: 16, display: 'flex', gap: 12, alignItems: 'center' }}>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                disabled={producerSaving}
                style={{ minWidth: 200 }}
                onClick={handleSaveProducerFees}
              >
                {producerSaving ? 'Salvando…' : 'Salvar taxas por produtor'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
