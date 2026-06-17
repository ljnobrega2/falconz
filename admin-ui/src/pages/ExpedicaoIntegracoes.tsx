// ExpedicaoIntegracoes — tela de markup de frete por classe de entrega.
// Paridade com includes/senderzz-markup.php :: senderzz_markup_admin_page().
// Estrutura: (1) markup padrão global, (2) regras por classe (tabela inline — todas as classes),
// (3) preview calculadora, (4) botão salvar único no rodapé. Visual: AuditEngine.tsx pattern.
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'

// ---------- tipos ----------

type MarkupPair = { pct: number; fixed: number }

type MarkupRule = {
  class_id: number
  class_name: string
  pct: number
  fixed: number
}

type MarkupResponse = {
  default: MarkupPair
  rules: MarkupRule[]
}

type ShippingClass = { id: number; name: string }

type PreviewResponse = {
  base_cost: number
  pct: number
  fixed: number
  final_cost: number
}

// ---------- helpers ----------

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

const fmtPct = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 0, maximumFractionDigits: 2 })

// Aplica a fórmula do PHP em JS (para preview da tabela sem ida ao servidor).
const calcFinal = (base: number, pct: number, fixed: number): number => {
  const raw = base * (1 + pct / 100) + fixed
  return Math.round(raw * 100) / 100
}

// strToNum: aceita "5,5" / "5.5" / "" → number ou NaN para distinguir "vazio" de "zero".
const strToNum = (v: string): number => {
  const s = (v ?? '').toString().trim().replace(',', '.')
  if (s === '') return NaN
  const n = parseFloat(s)
  if (!isFinite(n) || n < 0) return 0
  return n
}

// ---------- componente ----------

export default function ExpedicaoIntegracoes() {
  const navigate = useNavigate()

  // Estado principal.
  const [defPair, setDefPair] = useState<MarkupPair>({ pct: 0, fixed: 0 })
  const [rules, setRules] = useState<MarkupRule[]>([])
  const [classes, setClasses] = useState<ShippingClass[]>([])

  // Edição inline por classe: class_id → {pct: string, fixed: string} (strings para suportar campo vazio = herdar).
  const [classEdits, setClassEdits] = useState<Record<number, { pct: string; fixed: string }>>({})

  // Loading / saving / feedback.
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  // Preview calculator.
  const [previewClassID, setPreviewClassID] = useState<number | ''>('')
  const [previewBase, setPreviewBase] = useState('20')
  const [previewBusy, setPreviewBusy] = useState(false)
  const [previewResult, setPreviewResult] = useState<PreviewResponse | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const [m, c] = await Promise.all([
        api<MarkupResponse>('/expedicao/markup'),
        api<{ items: ShippingClass[] }>('/expedicao/shipping-classes'),
      ])
      const loadedDef = {
        pct: Number(m?.default?.pct) || 0,
        fixed: Number(m?.default?.fixed) || 0,
      }
      setDefPair(loadedDef)

      const loadedRules = (m?.rules || []).map(r => ({
        class_id: Number(r.class_id),
        class_name: r.class_name || `Classe #${r.class_id}`,
        pct: Number(r.pct) || 0,
        fixed: Number(r.fixed) || 0,
      }))
      setRules(loadedRules)
      setClasses(c?.items || [])

      // Inicializa edits inline: regras existentes pré-preenchem os campos.
      const edits: Record<number, { pct: string; fixed: string }> = {}
      for (const r of loadedRules) {
        edits[r.class_id] = {
          pct: String(r.pct),
          fixed: String(r.fixed),
        }
      }
      setClassEdits(edits)
    } catch (e: any) {
      setErr(e?.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  // ---------- helpers de edição inline ----------

  function setClassPct(classID: number, val: string) {
    setClassEdits(prev => ({
      ...prev,
      [classID]: { ...(prev[classID] || { pct: '', fixed: '' }), pct: val },
    }))
  }

  function setClassFixed(classID: number, val: string) {
    setClassEdits(prev => ({
      ...prev,
      [classID]: { ...(prev[classID] || { pct: '', fixed: '' }), fixed: val },
    }))
  }

  // Compila as rules a partir dos classEdits (campos vazios = herdar padrão = omitir).
  const compiledRules = useMemo((): MarkupRule[] => {
    const result: MarkupRule[] = []
    for (const cls of classes) {
      const edit = classEdits[cls.id]
      if (!edit) continue
      const pct = strToNum(edit.pct)
      const fixed = strToNum(edit.fixed)
      // Qualquer campo preenchido com valor >= 0 significa regra explícita.
      const hasPct = !isNaN(pct)
      const hasFixed = !isNaN(fixed)
      if (!hasPct && !hasFixed) continue // ambos vazios = herdar padrão
      result.push({
        class_id: cls.id,
        class_name: cls.name,
        pct: hasPct ? pct : 0,
        fixed: hasFixed ? fixed : 0,
      })
    }
    return result
  }, [classes, classEdits])

  // ---------- handlers ----------

  async function handleSave() {
    // Valida default.
    if (defPair.pct < 0 || defPair.pct > 500) {
      showToast('err', 'Markup % padrão deve estar entre 0 e 500.')
      return
    }
    if (defPair.fixed < 0) {
      showToast('err', 'Valor fixo padrão não pode ser negativo.')
      return
    }
    for (const r of compiledRules) {
      if (r.pct < 0 || r.pct > 500) {
        showToast('err', `Classe ${r.class_name}: % fora do intervalo (0-500).`)
        return
      }
      if (r.fixed < 0) {
        showToast('err', `Classe ${r.class_name}: valor fixo negativo.`)
        return
      }
    }

    setSaving(true)
    try {
      await api('/expedicao/markup', {
        method: 'POST',
        body: JSON.stringify({
          default: { pct: defPair.pct, fixed: defPair.fixed },
          rules: compiledRules.map(r => ({
            class_id: r.class_id,
            pct: r.pct,
            fixed: r.fixed,
          })),
        }),
      })
      showToast('ok', 'Markup de frete salvo com sucesso.')
      // Recarrega para confirmar persistência e atualizar class_name.
      await load()
    } catch (e: any) {
      showToast('err', e?.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  async function runPreview() {
    const base = parseFloat(previewBase.replace(',', '.'))
    if (!isFinite(base) || base <= 0) {
      showToast('err', 'Informe um custo base maior que zero.')
      return
    }
    setPreviewBusy(true)
    try {
      const r = await api<PreviewResponse>('/expedicao/markup/preview', {
        method: 'POST',
        body: JSON.stringify({
          class_id: previewClassID === '' ? 0 : Number(previewClassID),
          base_cost: base,
        }),
      })
      setPreviewResult(r)
    } catch (e: any) {
      showToast('err', e?.message || 'Falha no cálculo')
    } finally {
      setPreviewBusy(false)
    }
  }

  // ---------- render ----------

  return (
    <div>
      {/* ============ Atalhos de navegação rápida ============ */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 20, flexWrap: 'wrap' }}>
        <button
          type="button"
          className="szv2-btn-secondary"
          onClick={() => navigate('/expedicao-webhooks')}
        >
          Ver Webhooks
        </button>
        <button
          type="button"
          className="szv2-btn-secondary"
          onClick={() => navigate('/tpc-clientes')}
        >
          Carteira Frete
        </button>
      </div>

      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {loading ? (
        <div className="szv2-card">
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
            Carregando…
          </div>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>

          {/* ============ Card 1 : Markup Padrão ============ */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Markup Padrão</h2>
                <p className="szv2-card-sub">
                  Aplica-se a todas as classes de entrega que não tiverem regra específica.
                  Fórmula: <strong>custo × (1 + %/100) + fixo</strong>.
                </p>
              </div>
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 1fr',
                gap: 16,
                paddingTop: 8,
              }}
            >
              <div>
                <label
                  htmlFor="sz-mkp-def-pct"
                  style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                >
                  Markup % padrão
                </label>
                <input
                  id="sz-mkp-def-pct"
                  type="number"
                  min="0"
                  max="500"
                  step="0.1"
                  className="szv2-input"
                  value={defPair.pct}
                  onChange={e =>
                    setDefPair(p => ({ ...p, pct: parseFloat(e.target.value) || 0 }))
                  }
                  style={{ width: '100%' }}
                />
                <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Percentual aplicado sobre o custo bruto (Melhor Envio). Máx: 500%.
                </span>
              </div>
              <div>
                <label
                  htmlFor="sz-mkp-def-fixed"
                  style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                >
                  Valor fixo padrão (R$)
                </label>
                <input
                  id="sz-mkp-def-fixed"
                  type="number"
                  min="0"
                  step="0.01"
                  className="szv2-input"
                  value={defPair.fixed}
                  onChange={e =>
                    setDefPair(p => ({ ...p, fixed: parseFloat(e.target.value) || 0 }))
                  }
                  style={{ width: '100%' }}
                />
                <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Adicional fixo somado ao final.
                </span>
              </div>
            </div>
          </div>

          {/* ============ Card 2 : Regras por Classe ============ */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Taxa por classe de entrega</h2>
                <p className="szv2-card-sub">
                  Deixe em branco para usar a taxa padrão global. Preencha para sobrescrever.
                </p>
              </div>
            </div>

            {classes.length === 0 ? (
              /* Aviso: nenhuma classe cadastrada — espelha alerta amarelo do PHP */
              <div
                style={{
                  background: '#fffbeb',
                  border: '1px solid #fcd34d',
                  padding: '12px 16px',
                  borderRadius: 6,
                  marginTop: 8,
                }}
              >
                ⚠️ Nenhuma classe de entrega encontrada. Cadastre classes de entrega no painel para vincular integrações.
              </div>
            ) : (
              <div style={{ overflowX: 'auto', marginTop: 8 }}>
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Classe de entrega</th>
                      <th style={{ width: 140 }}>Taxa % <span style={{ fontWeight: 400, color: 'var(--szv2-text-muted)' }}>(sobre custo ME)</span></th>
                      <th style={{ width: 140 }}>Taxa fixa R$</th>
                      <th style={{ width: 210, textAlign: 'right' }}>
                        Exemplo: custo ME R$20,00 → cobra
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {classes.map(cls => {
                      const edit = classEdits[cls.id] || { pct: '', fixed: '' }
                      const pctVal = isNaN(strToNum(edit.pct)) ? defPair.pct : strToNum(edit.pct)
                      const fixedVal = isNaN(strToNum(edit.fixed)) ? defPair.fixed : strToNum(edit.fixed)
                      const preview = calcFinal(20, pctVal, fixedVal)
                      const hasRule = !isNaN(strToNum(edit.pct)) || !isNaN(strToNum(edit.fixed))
                      return (
                        <tr key={cls.id}>
                          <td>
                            <strong>{cls.name}</strong>
                            <br />
                            <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                              #{cls.id}
                            </span>
                          </td>
                          <td>
                            <input
                              type="number"
                              min="0"
                              max="500"
                              step="0.01"
                              className="szv2-input"
                              value={edit.pct}
                              onChange={e => setClassPct(cls.id, e.target.value)}
                              placeholder={String(defPair.pct)}
                              style={{ width: '100%' }}
                            />
                          </td>
                          <td>
                            <input
                              type="number"
                              min="0"
                              step="0.01"
                              className="szv2-input"
                              value={edit.fixed}
                              onChange={e => setClassFixed(cls.id, e.target.value)}
                              placeholder={fmt(defPair.fixed)}
                              style={{ width: '100%' }}
                            />
                          </td>
                          <td style={{ textAlign: 'right', color: '#16a34a', fontWeight: 600 }}>
                            R$ {fmt(preview)}
                            {!hasRule && (
                              <span style={{ display: 'block', fontWeight: 400, fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                                (padrão)
                              </span>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* ============ Card 3 : Preview de cálculo ============ */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Preview de cálculo</h2>
                <p className="szv2-card-sub">
                  Simule o custo final cobrado do cliente para uma classe específica.
                </p>
              </div>
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 1fr auto',
                gap: 12,
                alignItems: 'end',
              }}
            >
              <div>
                <label
                  htmlFor="sz-mkp-pv-class"
                  style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                >
                  Classe de entrega
                </label>
                <select
                  id="sz-mkp-pv-class"
                  className="szv2-select"
                  value={previewClassID}
                  onChange={e =>
                    setPreviewClassID(e.target.value === '' ? '' : Number(e.target.value))
                  }
                  style={{ width: '100%' }}
                >
                  <option value="">— Padrão (sem classe) —</option>
                  {classes.map(c => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label
                  htmlFor="sz-mkp-pv-base"
                  style={{ display: 'block', fontWeight: 600, marginBottom: 6 }}
                >
                  Custo base (R$)
                </label>
                <input
                  id="sz-mkp-pv-base"
                  type="number"
                  min="0"
                  step="0.01"
                  className="szv2-input"
                  value={previewBase}
                  onChange={e => setPreviewBase(e.target.value)}
                  style={{ width: '100%' }}
                />
              </div>

              <button
                type="button"
                className="szv2-btn-brand"
                onClick={runPreview}
                disabled={previewBusy}
                style={{ height: 40 }}
              >
                {previewBusy ? 'Calculando…' : 'Calcular'}
              </button>
            </div>

            {previewResult && (
              <div
                style={{
                  marginTop: 18,
                  padding: '14px 18px',
                  background: 'rgba(234,88,12,0.06)',
                  border: '1px solid rgba(234,88,12,0.20)',
                  borderRadius: 10,
                  display: 'flex',
                  flexWrap: 'wrap',
                  gap: 18,
                  alignItems: 'center',
                  justifyContent: 'space-between',
                }}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                  <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Base</span>
                  <strong style={{ fontSize: 16 }}>R$ {fmt(previewResult.base_cost)}</strong>
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                  <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Markup</span>
                  <strong style={{ fontSize: 16 }}>
                    {fmtPct(previewResult.pct)}% + R$ {fmt(previewResult.fixed)}
                  </strong>
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                  <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Diferença</span>
                  <strong style={{ fontSize: 16, color: 'var(--szv2-brand)' }}>
                    R$ {fmt(previewResult.final_cost - previewResult.base_cost)}
                  </strong>
                </div>
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 4,
                    padding: '8px 16px',
                    background: 'var(--szv2-brand)',
                    color: '#fff',
                    borderRadius: 8,
                  }}
                >
                  <span style={{ fontSize: 12, opacity: 0.85 }}>Final cobrado</span>
                  <strong style={{ fontSize: 22 }}>
                    R$ {fmt(previewResult.final_cost)}
                  </strong>
                </div>
              </div>
            )}
          </div>

          {/* ============ Rodapé: Salvar ============ */}
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="button"
              className="szv2-btn-brand"
              onClick={handleSave}
              disabled={saving}
            >
              {saving ? 'Salvando…' : 'Salvar markup de frete'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
