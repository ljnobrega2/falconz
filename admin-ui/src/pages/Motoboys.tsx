import { useEffect, useState } from 'react'
import { api } from '../api'

type CD = { id: number; nome: string; ativo: boolean }
type Zona = { id: number; cd_id: number; nome: string; ativo: boolean }

type M = {
  id: number
  nome: string
  telefone: string
  cpf: string
  email: string
  tipo_pgto: string
  ativo: boolean
  cd_id?: number | null
  zona_id?: number | null
  token_app: string
  pin_set: boolean
}

type FormState = {
  id: number
  nome: string
  telefone: string
  cpf: string
  email: string
  tipo_pgto: string
  ativo: boolean
  cd_id: number | null
  pin: string
  zona_ids: number[]
}

const emptyForm = (): FormState => ({
  id: 0,
  nome: '',
  telefone: '',
  cpf: '',
  email: '',
  tipo_pgto: 'autonomo',
  ativo: true,
  cd_id: null,
  pin: '',
  zona_ids: [],
})

export default function Motoboys() {
  const [items, setItems] = useState<M[]>([])
  const [cds, setCds] = useState<CD[]>([])
  const [zonas, setZonas] = useState<Zona[]>([])
  const [err, setErr] = useState('')
  const [form, setForm] = useState<FormState>(emptyForm())
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)

  async function load() {
    try {
      const [r, rCds, rZonas] = await Promise.all([
        api<{ items: M[] }>('/motoboys'),
        api<{ items: CD[] }>('/cds'),
        api<{ items: Zona[] }>('/zonas'),
      ])
      setItems(r.items || [])
      setCds((rCds.items || []).filter(c => c.ativo))
      setZonas(rZonas.items || [])
    } catch (e: any) {
      setErr(e.message)
    }
  }

  useEffect(() => { load() }, [])

  async function openEdit(m: M) {
    // Carrega zonas atuais do motoboy via endpoint dedicado
    let zonaIds: number[] = []
    try {
      const r = await api<{ zona_ids: number[] }>(`/motoboys/${m.id}/zonas`)
      zonaIds = r.zona_ids || []
    } catch (_) {
      // graceful: se endpoint falhar, inicia sem zonas pré-selecionadas
    }
    setForm({
      id: m.id,
      nome: m.nome,
      telefone: m.telefone,
      cpf: m.cpf,
      email: m.email,
      tipo_pgto: m.tipo_pgto || 'autonomo',
      ativo: m.ativo,
      cd_id: m.cd_id ?? null,
      pin: '',
      zona_ids: zonaIds,
    })
    setShowForm(true)
  }

  function toggleZona(zonaId: number) {
    setForm(f => {
      const already = f.zona_ids.includes(zonaId)
      return {
        ...f,
        zona_ids: already
          ? f.zona_ids.filter(z => z !== zonaId)
          : [...f.zona_ids, zonaId],
      }
    })
  }

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setErr('')
    try {
      const payload = {
        nome: form.nome,
        telefone: form.telefone,
        cpf: form.cpf,
        email: form.email,
        tipo_pgto: form.tipo_pgto,
        ativo: form.ativo,
        cd_id: form.cd_id,
        pin: form.pin,
        zona_ids: form.zona_ids,
      }
      if (form.id) {
        await api(`/motoboys/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      } else {
        await api('/motoboys', { method: 'POST', body: JSON.stringify(payload) })
      }
      setShowForm(false)
      setForm(emptyForm())
      load()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }

  async function del(id: number) {
    if (!confirm('Desativar este motoboy? O histórico de pedidos será preservado.')) return
    try {
      await api(`/motoboys/${id}`, { method: 'DELETE' })
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  function cdNome(cdId?: number | null) {
    if (!cdId) return '—'
    const c = cds.find(c => c.id === cdId) || { nome: `CD #${cdId}` }
    return c.nome
  }

  // Agrupa zonas por cd_id para o optgroup
  const zonasPorCD: Record<number, Zona[]> = {}
  for (const z of zonas) {
    if (!zonasPorCD[z.cd_id]) zonasPorCD[z.cd_id] = []
    zonasPorCD[z.cd_id].push(z)
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Motoboys</h1>
          <p>{items.length} motoboys ativos</p>
        </div>
        <button
          className="szv2-btn szv2-btn-brand"
          onClick={() => { setForm(emptyForm()); setShowForm(true) }}
        >
          + Novo Motoboy
        </button>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {showForm && (
        <div className="szv2-card" style={{ marginBottom: '24px' }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? 'Editar Motoboy' : 'Novo Motoboy'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            {/* Linha 1: Nome, Telefone, CPF */}
            <div className="sz-form-grid sz-form-grid-3" style={{ marginBottom: '16px' }}>
              <div className="szv2-field">
                <label className="szv2-label">Nome *</label>
                <input
                  className="szv2-input"
                  required
                  value={form.nome}
                  onChange={e => setForm({ ...form, nome: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Telefone (com DDD)</label>
                <input
                  className="szv2-input"
                  value={form.telefone}
                  onChange={e => setForm({ ...form, telefone: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">CPF</label>
                <input
                  className="szv2-input"
                  value={form.cpf}
                  placeholder="000.000.000-00"
                  onChange={e => setForm({ ...form, cpf: e.target.value })}
                />
              </div>
            </div>

            {/* Linha 2: Email, CD, Tipo */}
            <div className="sz-form-grid sz-form-grid-3" style={{ marginBottom: '16px' }}>
              <div className="szv2-field">
                <label className="szv2-label">E-mail</label>
                <input
                  className="szv2-input"
                  type="email"
                  value={form.email}
                  onChange={e => setForm({ ...form, email: e.target.value })}
                />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">CD *</label>
                <select
                  className="szv2-input"
                  required
                  value={form.cd_id ?? ''}
                  onChange={e => setForm({ ...form, cd_id: e.target.value ? Number(e.target.value) : null })}
                >
                  <option value="">Selecione...</option>
                  {cds.map(c => (
                    <option key={c.id} value={c.id}>{c.nome}</option>
                  ))}
                </select>
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Tipo</label>
                <select
                  className="szv2-input"
                  value={form.tipo_pgto}
                  onChange={e => setForm({ ...form, tipo_pgto: e.target.value })}
                >
                  <option value="autonomo">Autônomo</option>
                  <option value="pj">PJ / MEI</option>
                  <option value="clt">CLT</option>
                </select>
              </div>
            </div>

            {/* Zonas de atuação */}
            {zonas.length > 0 && (
              <div className="szv2-field" style={{ marginBottom: '16px' }}>
                <label className="szv2-label">
                  Zonas de atuação{' '}
                  <span style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>
                    (clique para selecionar/desmarcar)
                  </span>
                </label>
                <div
                  style={{
                    border: '1px solid var(--szv2-border)',
                    borderRadius: '6px',
                    maxHeight: '180px',
                    overflowY: 'auto',
                    padding: '8px',
                    background: 'var(--szv2-bg-card, #fff)',
                  }}
                >
                  {Object.entries(zonasPorCD).map(([cdId, zs]) => {
                    const cdObj = cds.find(c => c.id === Number(cdId))
                    const cdLabel = cdObj ? cdObj.nome : `CD ${cdId}`
                    return (
                      <div key={cdId} style={{ marginBottom: '8px' }}>
                        <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--szv2-text-muted)', textTransform: 'uppercase', marginBottom: '4px' }}>
                          {cdLabel}
                        </div>
                        {zs.map(z => (
                          <label
                            key={z.id}
                            style={{ display: 'flex', alignItems: 'center', gap: '6px', padding: '3px 4px', cursor: 'pointer', fontSize: '13px' }}
                          >
                            <input
                              type="checkbox"
                              checked={form.zona_ids.includes(z.id)}
                              onChange={() => toggleZona(z.id)}
                            />
                            {z.nome}
                          </label>
                        ))}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}

            {/* PIN PWA */}
            <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: '16px' }}>
              <div className="szv2-field">
                <label className="szv2-label">
                  PIN de acesso ao PWA{' '}
                  {form.id && (
                    <span style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>
                      (deixe em branco para não alterar)
                    </span>
                  )}
                </label>
                <input
                  className="szv2-input"
                  type="password"
                  placeholder="4 a 8 dígitos"
                  maxLength={8}
                  autoComplete="new-password"
                  value={form.pin}
                  onChange={e => setForm({ ...form, pin: e.target.value })}
                />
                {form.id && (
                  <div style={{ fontSize: '12px', color: 'var(--szv2-text-muted)', marginTop: '4px' }}>
                    {/* pin_set vem do item original — não fica no form state */}
                    Deixe em branco para manter o PIN atual.
                  </div>
                )}
              </div>
              <div className="szv2-field" style={{ display: 'flex', alignItems: 'flex-end', paddingBottom: '4px' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '14px', cursor: 'pointer', color: 'var(--szv2-text-soft)' }}>
                  <input
                    type="checkbox"
                    checked={form.ativo}
                    onChange={e => setForm({ ...form, ativo: e.target.checked })}
                  />
                  Ativo
                </label>
              </div>
            </div>

            {/* Token App (somente leitura na edição) */}
            {form.id > 0 && (() => {
              const item = items.find(m => m.id === form.id)
              return item?.token_app ? (
                <div className="szv2-field" style={{ marginBottom: '16px' }}>
                  <label className="szv2-label">Token App <span style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>(somente leitura)</span></label>
                  <input
                    className="szv2-input"
                    readOnly
                    value={item.token_app}
                    style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '13px', background: 'var(--szv2-bg-muted, #f9fafb)', color: 'var(--szv2-text-muted)' }}
                  />
                </div>
              ) : null
            })()}

            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Salvando…' : form.id ? 'Atualizar' : 'Criar Motoboy'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowForm(false)}>
                Cancelar
              </button>
            </div>
          </form>
        </div>
      )}

      <div className="szv2-table-wrap">
        <table className="szv2-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Nome</th>
              <th>Telefone</th>
              <th>Email</th>
              <th>CD</th>
              <th>Token</th>
              <th>PIN</th>
              <th>Status</th>
              <th style={{ textAlign: 'right' }}>Ações</th>
            </tr>
          </thead>
          <tbody>
            {items.map(m => (
              <tr key={m.id}>
                <td style={{ color: 'var(--szv2-text-muted)', fontSize: '12px' }}>#{m.id}</td>
                <td style={{ fontWeight: 600 }}>{m.nome}</td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '13px' }}>{m.telefone}</td>
                <td style={{ fontSize: '13px', color: 'var(--szv2-text-muted)' }}>{m.email}</td>
                <td style={{ fontSize: '12px' }}>{cdNome(m.cd_id)}</td>
                <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', color: 'var(--szv2-text-muted)' }}>
                  {m.token_app ? `${m.token_app.slice(0, 10)}…` : '—'}
                </td>
                <td>
                  {m.pin_set
                    ? <span className="sz-badge szv2-badge-success">Configurado</span>
                    : <span className="sz-badge szv2-badge-neutral">Sem PIN</span>}
                </td>
                <td>
                  {m.ativo
                    ? <span className="sz-badge szv2-badge-success">Ativo</span>
                    : <span className="sz-badge szv2-badge-neutral">Inativo</span>}
                </td>
                <td style={{ textAlign: 'right' }}>
                  <div style={{ display: 'flex', gap: '8px', justifyContent: 'flex-end' }}>
                    <button
                      className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                      onClick={() => openEdit(m)}
                    >
                      Editar
                    </button>
                    <button
                      className="szv2-btn szv2-btn-sm szv2-btn-danger"
                      onClick={() => del(m.id)}
                    >
                      Desativar
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr>
                <td colSpan={9}>
                  <div className="szv2-empty">
                    <h3>Nenhum motoboy ativo</h3>
                    <p>Clique em "Novo Motoboy" para adicionar.</p>
                  </div>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
