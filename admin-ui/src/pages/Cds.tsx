import { useEffect, useState } from 'react'
import { api } from '../api'

type CD = { id: number; nome: string; cidade: string; uf: string; endereco: string | null; lat: number | null; lng: number | null; ativo: boolean; zona_count: number }
const empty = (): CD => ({ id: 0, nome: '', cidade: '', uf: 'SP', endereco: null, lat: null, lng: null, ativo: true, zona_count: 0 })

export default function Cds() {
  const [items, setItems] = useState<CD[]>([])
  const [err, setErr] = useState('')
  const [form, setForm] = useState<CD>(empty())
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)

  async function load() {
    try { const r = await api<{ items: CD[] }>('/cds'); setItems(r.items) }
    catch (e: any) { setErr(e.message) }
  }
  useEffect(() => { load() }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault(); setSaving(true)
    try {
      if (form.id) await api(`/cds/${form.id}`, { method: 'PUT', body: JSON.stringify(form) })
      else await api('/cds', { method: 'POST', body: JSON.stringify(form) })
      setShowForm(false); setForm(empty()); load()
    } catch (e: any) { setErr(e.message) }
    finally { setSaving(false) }
  }

  async function toggle(cd: CD) {
    if (cd.ativo && !window.confirm(`Desativar o CD "${cd.nome}"? As zonas vinculadas continuarão cadastradas.`)) return
    try { await api(`/cds/${cd.id}`, { method: 'PUT', body: JSON.stringify({ ...cd, ativo: !cd.ativo }) }); load() }
    catch (e: any) { setErr(e.message) }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Centros de Distribuição</h1>
          <p>{items.length} CDs cadastrados</p>
        </div>
        <button className="szv2-btn szv2-btn-brand" onClick={() => { setForm(empty()); setShowForm(true) }}>
          + Novo CD
        </button>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {showForm && (
        <div className="szv2-card" style={{ marginBottom: '24px' }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? 'Editar CD' : 'Novo Centro de Distribuição'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            <div className="sz-form-grid sz-form-grid-3" style={{ marginBottom: '16px' }}>
              <div className="szv2-field">
                <label className="szv2-label">Nome *</label>
                <input className="szv2-input" required value={form.nome} onChange={e => setForm({ ...form, nome: e.target.value })} />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Cidade *</label>
                <input className="szv2-input" required value={form.cidade} onChange={e => setForm({ ...form, cidade: e.target.value })} />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">UF</label>
                <input className="szv2-input" maxLength={2} value={form.uf} onChange={e => setForm({ ...form, uf: e.target.value.toUpperCase() })} />
              </div>
              <div className="szv2-field" style={{ gridColumn: '1 / 3' }}>
                <label className="szv2-label">Endereço</label>
                <input className="szv2-input" value={form.endereco ?? ''} onChange={e => setForm({ ...form, endereco: e.target.value || null })} />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Lat / Lng</label>
                <div style={{ display: 'flex', gap: '8px' }}>
                  <input className="szv2-input" type="number" step="any" placeholder="Lat" value={form.lat ?? ''} onChange={e => setForm({ ...form, lat: e.target.value ? +e.target.value : null })} />
                  <input className="szv2-input" type="number" step="any" placeholder="Lng" value={form.lng ?? ''} onChange={e => setForm({ ...form, lng: e.target.value ? +e.target.value : null })} />
                </div>
              </div>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '16px', marginBottom: '16px' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '14px', cursor: 'pointer', color: 'var(--szv2-text-soft)' }}>
                <input type="checkbox" checked={form.ativo} onChange={e => setForm({ ...form, ativo: e.target.checked })} />
                Ativo
              </label>
            </div>
            <div className="sz-form-actions">
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={saving}>
                {saving ? 'Salvando…' : form.id ? 'Salvar' : 'Criar CD'}
              </button>
              <button type="button" className="szv2-btn szv2-btn-secondary" onClick={() => setShowForm(false)}>Cancelar</button>
            </div>
          </form>
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0,1fr))', gap: '16px' }}>
        {items.map(cd => (
          <div key={cd.id} className="szv2-card" style={!cd.ativo ? { opacity: 0.6 } : {}}>
            <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: '8px' }}>
              <div>
                <h3 style={{ margin: '0 0 2px', fontSize: '15px', fontWeight: 700, color: 'var(--szv2-text)' }}>{cd.nome}</h3>
                <p style={{ margin: 0, fontSize: '13px', color: 'var(--szv2-text-muted)' }}>{cd.cidade} — {cd.uf}</p>
                <p style={{ margin: '4px 0 0', fontSize: '12px', color: 'var(--szv2-text-faint)' }}>
                  {cd.zona_count} zona{cd.zona_count !== 1 ? 's' : ''}
                </p>
              </div>
              <span className={`sz-badge ${cd.ativo ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                {cd.ativo ? 'Ativo' : 'Inativo'}
              </span>
            </div>
            {cd.endereco && <p style={{ fontSize: '12px', color: 'var(--szv2-text-faint)', margin: '0 0 12px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{cd.endereco}</p>}
            {cd.lat && cd.lng && (
              <p style={{ fontSize: '11px', color: 'var(--szv2-text-faint)', margin: '0 0 12px', fontFamily: 'var(--szv2-font-mono)' }}>
                {cd.lat?.toFixed(4)}, {cd.lng?.toFixed(4)}
              </p>
            )}
            <div style={{ display: 'flex', gap: '12px', paddingTop: '12px', borderTop: '1px solid var(--szv2-divider)' }}>
              <button className="szv2-btn szv2-btn-sm szv2-btn-secondary" onClick={() => { setForm(cd); setShowForm(true) }}>
                Editar
              </button>
              <button className="szv2-btn szv2-btn-sm" onClick={() => toggle(cd)}
                style={{ background: 'transparent', borderColor: cd.ativo ? 'var(--szv2-danger)' : 'var(--szv2-success)', color: cd.ativo ? 'var(--szv2-danger)' : 'var(--szv2-success)' }}>
                {cd.ativo ? 'Desativar' : 'Ativar'}
              </button>
            </div>
          </div>
        ))}
        {items.length === 0 && (
          <div style={{ gridColumn: '1 / 4' }}>
            <div className="szv2-card">
              <div className="szv2-empty">
                <h3>Nenhum CD cadastrado</h3>
                <p>Clique em "Novo CD" para começar.</p>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
