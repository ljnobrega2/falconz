import { useEffect, useState } from 'react'
import { confirmAsync } from '../components/ConfirmDialog'
import { useToast } from '../hooks/useToast'
import { api } from '../api'
import CardKpiSkeleton from '../components/CardKpiSkeleton'
import EmptyState from '../components/EmptyState'
import ErrorState from '../components/ErrorState'

type CD = { id: number; nome: string; cidade: string; uf: string; endereco: string | null; lat: number | null; lng: number | null; ativo: boolean; zona_count: number }
const empty = (): CD => ({ id: 0, nome: '', cidade: '', uf: 'SP', endereco: null, lat: null, lng: null, ativo: true, zona_count: 0 })

export default function Cds() {
  const [items, setItems] = useState<CD[]>([])
  // `err` é canal EXCLUSIVO de erro de carregamento (alimenta ErrorState/banner).
  // Erros de mutação (salvar/ativar) vão para toast — assim uma falha de save com
  // lista vazia NÃO é confundida com falha de load (que mostraria "Tentar novamente").
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)
  const [form, setForm] = useState<CD>(empty())
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const showToast = useToast()

  async function load() {
    setLoading(true); setErr('')
    try { const r = await api<{ items: CD[] }>('/cds'); setItems(r.items ?? []) }
    catch (e: any) { setErr(e.message) }
    finally { setLoading(false) }
  }
  useEffect(() => { load() }, [])

  async function save(e: React.FormEvent) {
    e.preventDefault(); setSaving(true)
    try {
      // cidade/uf seguem NOT NULL no banco e alimentam seletores de CD do portal; o form
      // não os coleta mais → default seguro (nome vira cidade no create; SP no uf). Na edição,
      // form.cidade/uf vêm preservados do registro (setForm(cd)), então nada é apagado.
      const payload = { ...form, cidade: form.cidade?.trim() || form.nome, uf: form.uf?.trim() || 'SP' }
      if (form.id) await api(`/cds/${form.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      else await api('/cds', { method: 'POST', body: JSON.stringify(payload) })
      setShowForm(false); setForm(empty()); load()
      showToast('ok', form.id ? 'CD atualizado.' : 'CD criado.')
    } catch (e: any) { showToast('err', e.message || 'Falha ao salvar CD') }
    finally { setSaving(false) }
  }

  async function toggle(cd: CD) {
    if (cd.ativo && !await confirmAsync({ message: `Desativar o CD "${cd.nome}"? As zonas vinculadas continuarão cadastradas.`, danger: true })) return
    try { await api(`/cds/${cd.id}`, { method: 'PUT', body: JSON.stringify({ ...cd, ativo: !cd.ativo }) }); load(); showToast('ok', !cd.ativo ? 'CD ativado.' : 'CD desativado.') }
    catch (e: any) { showToast('err', e.message || 'Falha ao alterar status do CD') }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Centros de Distribuição</h1>
          <p>{loading ? 'Carregando…' : `${items.length} CD${items.length !== 1 ? 's' : ''} cadastrado${items.length !== 1 ? 's' : ''}`}</p>
        </div>
        <button className="szv2-btn szv2-btn-brand" onClick={() => { setForm(empty()); setShowForm(true) }}>
          + Novo CD
        </button>
      </div>

      {/* Banner só com dados na tela (erro de refresh/ação). Falha de
          carregamento inicial vira ErrorState abaixo. */}
      {err && items.length > 0 && <div className="sz-alert-danger">{err}</div>}

      {showForm && (
        <div className="szv2-card" style={{ marginBottom: '24px' }}>
          <div className="szv2-card-head">
            <div><h2>{form.id ? 'Editar CD' : 'Novo Centro de Distribuição'}</h2></div>
            <button className="szv2-modal-x" onClick={() => setShowForm(false)}>✕</button>
          </div>
          <form onSubmit={save}>
            {/* CD = só Nome (pedido do dono 2026-06-26): Cidade/UF/Endereço/Lat/Lng saíram —
                as cidades de cobertura são definidas pelas ZONAS, não pelo CD. cidade/uf
                continuam no payload (default no save) só p/ não quebrar NOT NULL nem os
                seletores de CD do portal. */}
            <div className="szv2-field" style={{ marginBottom: '16px' }}>
              <label className="szv2-label">Nome *</label>
              <input className="szv2-input" required value={form.nome} onChange={e => setForm({ ...form, nome: e.target.value })} />
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

      {loading && items.length === 0 ? (
        <CardKpiSkeleton count={3} />
      ) : err && items.length === 0 ? (
        <ErrorState message={err} onRetry={() => { setErr(''); load() }} />
      ) : !loading && items.length === 0 ? (
        <EmptyState
          icon="🏢"
          title="Nenhum CD cadastrado"
          description='Clique em "+ Novo CD" para começar.'
          action={{ label: '+ Novo CD', onClick: () => { setForm(empty()); setShowForm(true) } }}
        />
      ) : (
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0,1fr))', gap: '16px' }}>
        {items.map(cd => (
          <div key={cd.id} className="szv2-card" style={!cd.ativo ? { opacity: 0.6 } : {}}>
            <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: '8px' }}>
              <div>
                <h3 style={{ margin: '0 0 2px', fontSize: '15px', fontWeight: 700, color: 'var(--szv2-text)' }}>{cd.nome}</h3>
                <p style={{ margin: '2px 0 0', fontSize: '12px', color: 'var(--szv2-text-faint)' }}>
                  {cd.zona_count} zona{cd.zona_count !== 1 ? 's' : ''}
                </p>
              </div>
              <span className={`sz-badge ${cd.ativo ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                {cd.ativo ? 'Ativo' : 'Inativo'}
              </span>
            </div>
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
      </div>
      )}
    </div>
  )
}
