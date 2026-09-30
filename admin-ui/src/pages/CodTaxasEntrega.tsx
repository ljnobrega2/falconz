import { useEffect, useState } from 'react'
import { useToast } from '../hooks/useToast'
import { api } from '../api'

// Tela COD · Taxas de Entrega — paridade com tab_fin_taxas_entrega() (PHP).
// 4 taxas principais + 4 penalidades globais + 3 tabelas particulares
// (motoboy, produtor, afiliado). Save: 4 POSTs em paralelo.

type GlobalRates = {
  taxa_cliente_cod: number
  cod_delivery_fee: number
  taxa_transacao_percentual: number
  taxa_motoboy_entrega: number
  taxa_motoboy_frustrado: number
  aff_first_global: number
  aff_repeat_global: number
  prod_first_global: number
  prod_repeat_global: number
}

type MotoboyRow = {
  id: number
  nome: string
  taxa_entrega: number | null
  taxa_frustrado: number | null
}

type ProducerRow = {
  user_id: number
  nome: string
  email: string
  motoboy_ativo: boolean
  taxa_entrega: number | null
  taxa_manuseio: number | null
  taxa_percentual: number | null
  frustracao_first: number | null
  frustracao_repeat: number | null
}

type AffiliateRow = {
  id: number
  nome: string
  first: number | null
  repeat: number | null
}

// Inputs ficam em string para suportar campo vazio = "padrão" (inerit global).
type MotoboyEditable = {
  id: number
  nome: string
  taxa_entrega: string
  taxa_frustrado: string
}
type ProducerEditable = {
  user_id: number
  nome: string
  email: string
  motoboy_ativo: boolean
  taxa_entrega: string
  taxa_manuseio: string
  taxa_percentual: string
  frustracao_first: string
  frustracao_repeat: string
}
type AffiliateEditable = {
  id: number
  nome: string
  first: string
  repeat: string
}

// String(5.0) === "5", String(5.5) === "5.5" — sem strip manual (regex agressivo).
const numToStr = (v: number | null): string =>
  v == null ? '' : String(v)

// strToNum: '' → null (padrão = herda global); senão parseFloat normalizado.
const strToNum = (v: string): number | null => {
  const s = v.trim().replace(',', '.')
  if (s === '') return null
  const n = parseFloat(s)
  if (!isFinite(n) || n < 0) return null
  return n
}

export default function CodTaxasEntrega() {
  const [global, setGlobal] = useState<GlobalRates>({
    taxa_cliente_cod: 25,
    cod_delivery_fee: 23.98,
    taxa_transacao_percentual: 0,
    taxa_motoboy_entrega: 18,
    taxa_motoboy_frustrado: 5,
    aff_first_global: 5,
    aff_repeat_global: 5,
    prod_first_global: 0,
    prod_repeat_global: 8,
  })
  const [motoboys, setMotoboys] = useState<MotoboyEditable[]>([])
  const [producers, setProducers] = useState<ProducerEditable[]>([])
  const [affiliates, setAffiliates] = useState<AffiliateEditable[]>([])

  // Taxa de transação do produtor (%) — option global sz_producer_transaction_fee_pct
  // (endpoint próprio /producer-fee-config; default 4.99). Mantida fora do
  // Promise.all de loadAll para não derrubar a tela se o endpoint falhar/404.
  const PRODUCER_FEE_DEFAULT = 4.99
  const [producerFeePct, setProducerFeePct] = useState<string>(String(PRODUCER_FEE_DEFAULT))
  const [savingProducerFee, setSavingProducerFee] = useState(false)

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const showToast = useToast() // AUDIT-2026-06-18 Onda3


  async function loadAll() {
    setLoading(true)
    setErr('')
    try {
      const [g, mb, pr, af] = await Promise.all([
        api<GlobalRates>('/cod-taxas/global'),
        api<{ items: MotoboyRow[] }>('/cod-taxas/motoboys'),
        api<{ items: ProducerRow[] }>('/cod-taxas/producers'),
        api<{ items: AffiliateRow[] }>('/cod-taxas/affiliates'),
      ])
      setGlobal(g)
      setMotoboys((mb.items || []).map(r => ({
        id: r.id,
        nome: r.nome,
        taxa_entrega: numToStr(r.taxa_entrega),
        taxa_frustrado: numToStr(r.taxa_frustrado),
      })))
      setProducers((pr.items || []).map(r => ({
        user_id: r.user_id,
        nome: r.nome,
        email: r.email,
        motoboy_ativo: !!r.motoboy_ativo,
        taxa_entrega: numToStr(r.taxa_entrega),
        taxa_manuseio: numToStr(r.taxa_manuseio),
        taxa_percentual: numToStr(r.taxa_percentual),
        frustracao_first: numToStr(r.frustracao_first),
        frustracao_repeat: numToStr(r.frustracao_repeat),
      })))
      setAffiliates((af.items || []).map(r => ({
        id: r.id,
        nome: r.nome,
        first: numToStr(r.first),
        repeat: numToStr(r.repeat),
      })))
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar taxas')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { loadAll() }, [])

  // Carrega a taxa de transação do produtor separadamente — falha aqui não
  // derruba o restante da tela (default 4.99 permanece).
  useEffect(() => {
    let alivo = true
    // O back-end (cod_taxas.go::GetProducerFeeConfig) responde com a chave
    // `producer_transaction_fee_pct` (sem prefixo sz_). Ler a chave errada fazia
    // o valor salvo nunca recarregar — mantemos os fallbacks legados por garantia.
    api<{ producer_transaction_fee_pct?: number; sz_producer_transaction_fee_pct?: number; value?: number; pct?: number }>('/producer-fee-config')
      .then(r => {
        if (!alivo) return
        const v = r?.producer_transaction_fee_pct ?? r?.sz_producer_transaction_fee_pct ?? r?.value ?? r?.pct
        if (v != null && isFinite(Number(v))) setProducerFeePct(String(v))
      })
      .catch(() => { /* mantém default 4.99 */ })
    return () => { alivo = false }
  }, [])

  async function handleSaveProducerFee() {
    const n = strToNum(producerFeePct)
    if (n == null) {
      showToast('err', 'Informe um percentual válido.')
      return
    }
    setSavingProducerFee(true)
    try {
      // POST espera `producer_transaction_fee_pct` (sem prefixo sz_). Enviar a
      // chave prefixada fazia o back-end ignorar o valor e persistir 0 — a taxa
      // do produtor era zerada a cada save. Ver cod_taxas.go::SaveProducerFeeConfig.
      await api('/producer-fee-config', {
        method: 'POST',
        body: JSON.stringify({ producer_transaction_fee_pct: n }),
      })
      showToast('ok', 'Taxa de transação do produtor salva.')
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setSavingProducerFee(false)
    }
  }

  async function handleSave() {
    setSaving(true)
    try {
      await Promise.all([
        api('/cod-taxas/global', { method: 'POST', body: JSON.stringify(global) }),
        api('/cod-taxas/motoboys', {
          method: 'POST',
          body: JSON.stringify({
            items: motoboys.map(r => ({
              id: r.id,
              taxa_entrega: strToNum(r.taxa_entrega),
              taxa_frustrado: strToNum(r.taxa_frustrado),
            })),
          }),
        }),
        api('/cod-taxas/producers', {
          method: 'POST',
          body: JSON.stringify({
            items: producers.map(r => ({
              user_id: r.user_id,
              motoboy_ativo: r.motoboy_ativo,
              taxa_entrega: strToNum(r.taxa_entrega),
              taxa_manuseio: strToNum(r.taxa_manuseio),
              taxa_percentual: strToNum(r.taxa_percentual),
              frustracao_first: strToNum(r.frustracao_first),
              frustracao_repeat: strToNum(r.frustracao_repeat),
            })),
          }),
        }),
        api('/cod-taxas/affiliates', {
          method: 'POST',
          body: JSON.stringify({
            items: affiliates.map(r => ({
              id: r.id,
              first: strToNum(r.first),
              repeat: strToNum(r.repeat),
            })),
          }),
        }),
      ])
      showToast('ok', 'Taxas de entrega salvas.')
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  // ----- helpers de render --------------------------------------------------

  function KpiInput({
    label,
    sub,
    value,
    onChange,
  }: {
    label: string
    sub?: string
    value: number
    onChange: (v: number) => void
  }) {
    return (
      <div className="szv2-card">
        <div className="szv2-kpi">
          <span className="szv2-kpi-label">{label}</span>
          <input
            className="szv2-input"
            type="number"
            step="0.01"
            min="0"
            value={isFinite(value) ? value : 0}
            onChange={e => onChange(parseFloat(e.target.value) || 0)}
            style={{ marginTop: 4, fontSize: 22, fontWeight: 600, color: 'var(--szv2-brand)', width: '100%' }}
          />
          {sub && <span className="szv2-kpi-meta">{sub}</span>}
        </div>
      </div>
    )
  }

  function RateInput({
    value,
    onChange,
    placeholder = 'padrão',
  }: {
    value: string
    onChange: (v: string) => void
    placeholder?: string
  }) {
    return (
      <input
        className="szv2-input"
        type="number"
        step="0.01"
        min="0"
        placeholder={placeholder}
        value={value}
        onChange={e => onChange(e.target.value)}
        style={{ width: 110 }}
      />
    )
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>COD · Taxas de Entrega</h1>
          <p>Configura tarifas cobradas do cliente, do motoboy e penalidades de frustração</p>
        </div>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando taxas…
        </div>
      ) : (
        <>
          {/* KPI Grid 1 — Taxas Principais */}
          <h2 style={{ marginTop: 8, marginBottom: 12, fontSize: 16 }}>Taxas principais</h2>
          <div
            className="szv2-kpi-grid"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', gap: 16 }}
          >
            <KpiInput
              label="Cliente COD · entrega"
              sub="cobrado no pedido/checkout"
              value={global.taxa_cliente_cod}
              onChange={v => setGlobal(p => ({ ...p, taxa_cliente_cod: v }))}
            />
            <KpiInput
              label="Taxa de entrega COD (R$)"
              sub="usada no breakdown financeiro (padrão 23,98)"
              value={global.cod_delivery_fee}
              onChange={v => setGlobal(p => ({ ...p, cod_delivery_fee: v }))}
            />
            <KpiInput
              label="Venda · taxa de transação (%)"
              sub="replica por participante em pedidos novos; sem afiliado cobra uma vez do produtor"
              value={global.taxa_transacao_percentual}
              onChange={v => setGlobal(p => ({ ...p, taxa_transacao_percentual: v }))}
            />
            <KpiInput
              label="Motoboy · entrega"
              sub="valor a pagar por entrega"
              value={global.taxa_motoboy_entrega}
              onChange={v => setGlobal(p => ({ ...p, taxa_motoboy_entrega: v }))}
            />
            <KpiInput
              label="Motoboy · frustrado"
              sub="valor validado no frustrado"
              value={global.taxa_motoboy_frustrado}
              onChange={v => setGlobal(p => ({ ...p, taxa_motoboy_frustrado: v }))}
            />
          </div>

          {/* Taxa de transação do produtor — option global sz_producer_transaction_fee_pct */}
          <h2 style={{ marginTop: 24, marginBottom: 12, fontSize: 16 }}>Taxa de transação do produtor</h2>
          <div className="szv2-card" style={{ padding: 20 }}>
            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                gap: 16,
                alignItems: 'flex-end',
              }}
            >
              <div style={{ flex: '1 1 280px', minWidth: 240 }}>
                <label
                  htmlFor="sz-producer-fee-pct"
                  className="szv2-kpi-label"
                  style={{ display: 'block', marginBottom: 6 }}
                >
                  Taxa de transação do produtor (%)
                </label>
                <input
                  id="sz-producer-fee-pct"
                  className="szv2-input"
                  type="number"
                  step="0.01"
                  min="0"
                  value={producerFeePct}
                  onChange={e => setProducerFeePct(e.target.value)}
                  style={{
                    fontSize: 22,
                    fontWeight: 600,
                    color: 'var(--szv2-brand)',
                    width: '100%',
                  }}
                />
                <span className="szv2-kpi-meta" style={{ display: 'block', marginTop: 4 }}>
                  Percentual cobrado do produtor por transação (padrão 4,99%).
                </span>
              </div>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={handleSaveProducerFee}
                disabled={savingProducerFee}
                style={{ minWidth: 160 }}
              >
                {savingProducerFee ? 'Salvando…' : 'Salvar'}
              </button>
            </div>
          </div>

          {/* KPI Grid 2 — Penalidades */}
          <h2 style={{ marginTop: 24, marginBottom: 12, fontSize: 16 }}>Penalidades de frustração</h2>
          <div
            className="szv2-kpi-grid"
            style={{ gridTemplateColumns: 'repeat(4, minmax(0,1fr))', gap: 16 }}
          >
            <KpiInput
              label="Afiliado · 1ª frustração"
              value={global.aff_first_global}
              onChange={v => setGlobal(p => ({ ...p, aff_first_global: v }))}
            />
            <KpiInput
              label="Afiliado · reincidente"
              value={global.aff_repeat_global}
              onChange={v => setGlobal(p => ({ ...p, aff_repeat_global: v }))}
            />
            <KpiInput
              label="Produtor · 1ª frustração"
              value={global.prod_first_global}
              onChange={v => setGlobal(p => ({ ...p, prod_first_global: v }))}
            />
            <KpiInput
              label="Produtor · reincidente"
              value={global.prod_repeat_global}
              onChange={v => setGlobal(p => ({ ...p, prod_repeat_global: v }))}
            />
          </div>

          {/* Particulares por motoboy */}
          <details className="szv2-card" style={{ marginTop: 24, padding: 0 }}>
            <summary
              style={{
                cursor: 'pointer',
                padding: '16px 20px',
                fontWeight: 600,
                listStyle: 'revert',
                userSelect: 'none',
              }}
            >
              Taxas particulares por motoboy ({motoboys.length})
            </summary>
            <div style={{ padding: '0 20px 20px', overflowX: 'auto' }}>
              {motoboys.length === 0 ? (
                <div style={{ padding: 20, color: 'var(--szv2-text-muted)' }}>
                  Nenhum motoboy ativo encontrado.
                </div>
              ) : (
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Motoboy</th>
                      <th style={{ width: 130 }}>Entrega</th>
                      <th style={{ width: 130 }}>Frustrado</th>
                    </tr>
                  </thead>
                  <tbody>
                    {motoboys.map((m, idx) => (
                      <tr key={m.id}>
                        <td>
                          {m.nome}{' '}
                          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                            ID {m.id}
                          </span>
                        </td>
                        <td>
                          <RateInput
                            value={m.taxa_entrega}
                            onChange={v =>
                              setMotoboys(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], taxa_entrega: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={m.taxa_frustrado}
                            onChange={v =>
                              setMotoboys(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], taxa_frustrado: v }
                                return next
                              })
                            }
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </details>

          {/* Configuração Senderzz por produtor */}
          <details className="szv2-card" style={{ marginTop: 12, padding: 0 }}>
            <summary
              style={{
                cursor: 'pointer',
                padding: '16px 20px',
                fontWeight: 600,
                listStyle: 'revert',
                userSelect: 'none',
              }}
            >
              Configuração FALK LOG por produtor ({producers.length})
            </summary>
            <div style={{ padding: '0 20px 20px', overflowX: 'auto' }}>
              {producers.length === 0 ? (
                <div style={{ padding: 20, color: 'var(--szv2-text-muted)' }}>
                  Nenhum produtor encontrado.
                </div>
              ) : (
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Produtor</th>
                      <th style={{ width: 110, textAlign: 'center' }}>Motoboy ativo</th>
                      <th style={{ width: 120 }}>Entrega cliente</th>
                      <th style={{ width: 110 }}>Manuseio</th>
                      <th style={{ width: 110 }}>Transação %</th>
                      <th style={{ width: 110 }}>Frustração 1ª</th>
                      <th style={{ width: 110 }}>Frustração reinc.</th>
                    </tr>
                  </thead>
                  <tbody>
                    {producers.map((p, idx) => (
                      <tr key={p.user_id}>
                        <td>
                          <div style={{ fontWeight: 500 }}>{p.nome || `Produtor #${p.user_id}`}</div>
                          {p.email && (
                            <div style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                              {p.email}
                            </div>
                          )}
                        </td>
                        <td style={{ textAlign: 'center' }}>
                          <input
                            type="checkbox"
                            checked={p.motoboy_ativo}
                            onChange={e =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], motoboy_ativo: e.target.checked }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={p.taxa_entrega}
                            onChange={v =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], taxa_entrega: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={p.taxa_manuseio}
                            onChange={v =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], taxa_manuseio: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={p.taxa_percentual}
                            onChange={v =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], taxa_percentual: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={p.frustracao_first}
                            onChange={v =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], frustracao_first: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={p.frustracao_repeat}
                            onChange={v =>
                              setProducers(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], frustracao_repeat: v }
                                return next
                              })
                            }
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </details>

          {/* Regras particulares por afiliado */}
          <details className="szv2-card" style={{ marginTop: 12, padding: 0 }}>
            <summary
              style={{
                cursor: 'pointer',
                padding: '16px 20px',
                fontWeight: 600,
                listStyle: 'revert',
                userSelect: 'none',
              }}
            >
              Regras particulares por afiliado ({affiliates.length})
            </summary>
            <div style={{ padding: '0 20px 20px', overflowX: 'auto' }}>
              {affiliates.length === 0 ? (
                <div style={{ padding: 20, color: 'var(--szv2-text-muted)' }}>
                  Nenhum afiliado encontrado.
                </div>
              ) : (
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Afiliado</th>
                      <th style={{ width: 130 }}>1ª frustração</th>
                      <th style={{ width: 130 }}>Reincidente</th>
                    </tr>
                  </thead>
                  <tbody>
                    {affiliates.map((a, idx) => (
                      <tr key={a.id}>
                        <td>
                          {a.nome}{' '}
                          <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                            ID {a.id}
                          </span>
                        </td>
                        <td>
                          <RateInput
                            value={a.first}
                            onChange={v =>
                              setAffiliates(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], first: v }
                                return next
                              })
                            }
                          />
                        </td>
                        <td>
                          <RateInput
                            value={a.repeat}
                            onChange={v =>
                              setAffiliates(arr => {
                                const next = [...arr]
                                next[idx] = { ...next[idx], repeat: v }
                                return next
                              })
                            }
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </details>

          {/* Save */}
          <div style={{ marginTop: 24, display: 'flex', gap: 12, alignItems: 'center' }}>
            <button
              type="button"
              className="szv2-btn szv2-btn-brand"
              onClick={handleSave}
              disabled={saving}
              style={{ minWidth: 200 }}
            >
              {saving ? 'Salvando…' : 'Salvar taxas de entrega'}
            </button>
            <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
              Campos vazios herdam o valor global.
            </span>
          </div>
        </>
      )}
    </div>
  )
}
