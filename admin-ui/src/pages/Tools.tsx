import { useEffect, useState } from 'react'
import { api } from '../api'

type TableStat = { table: string; count: number }

const TABLE_LABELS: Record<string, string> = {
  senderzz_portal_users:    'Usuários Portal',
  sz_motoboys:              'Motoboys',
  sz_motoboy_pedidos:       'Pedidos Motoboy',
  sz_motoboy_cds:           'Centros de Distribuição',
  tpc_carteira:             'Carteiras',
  tpc_transacoes:           'Transações',
  tpc_recargas:             'Recargas PIX',
  senderzz_affiliates:      'Afiliados',
  wc_me_labels:             'Etiquetas ME',
  senderzz_webhook_log:     'Logs Webhook',
  senderzz_integration_log: 'Logs Integração',
}

export default function Tools() {
  const [stats, setStats] = useState<TableStat[]>([])
  const [statsErr, setStatsErr] = useState('')
  const [userForm, setUserForm] = useState({ wp_user_id: '', email: '', nome: '', role: 'produtor', plano: 'free' })
  const [userMsg, setUserMsg] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    api<{ tables: TableStat[] }>('/tools/stats')
      .then(r => setStats(r.tables))
      .catch(e => setStatsErr(e.message))
  }, [])

  async function insertUser(e: React.FormEvent) {
    e.preventDefault(); setSaving(true); setUserMsg('')
    try {
      const r = await api<{ id: number }>('/tools/users', {
        method: 'POST',
        body: JSON.stringify({ ...userForm, wp_user_id: userForm.wp_user_id ? +userForm.wp_user_id : null }),
      })
      setUserMsg(`✓ Usuário criado/atualizado — ID #${r.id}`)
      setUserForm({ wp_user_id: '', email: '', nome: '', role: 'produtor', plano: 'free' })
    } catch (e: any) { setUserMsg(`Erro: ${e.message}`) }
    finally { setSaving(false) }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Ferramentas</h1><p>Diagnóstico e operações administrativas</p></div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '20px' }}>
        {/* Stats */}
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div><h2>Contagem de Registros</h2><p className="szv2-card-sub">Total por tabela no PostgreSQL</p></div>
          </div>
          {statsErr && <div className="sz-alert-danger" style={{ marginBottom: '16px' }}>{statsErr}</div>}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
            {stats.map(s => (
              <div key={s.table} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={{ fontSize: '13px', color: 'var(--szv2-text-soft)' }}>{TABLE_LABELS[s.table] || s.table}</span>
                <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '14px', fontWeight: 700, color: s.count > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)' }}>
                  {s.count.toLocaleString('pt-BR')}
                </span>
              </div>
            ))}
            {stats.length === 0 && !statsErr && (
              <p style={{ fontSize: '13px', color: 'var(--szv2-text-faint)' }}>Carregando…</p>
            )}
          </div>
        </div>

        {/* Insert user */}
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div>
              <h2>Inserir / Corrigir Usuário</h2>
              <p className="szv2-card-sub">Para usuários que não vieram na migração</p>
            </div>
          </div>
          <form onSubmit={insertUser}>
            <div className="sz-form-grid sz-form-grid-2" style={{ marginBottom: '12px' }}>
              <div className="szv2-field">
                <label className="szv2-label">WP User ID</label>
                <input className="szv2-input" type="number" placeholder="Ex: 15" value={userForm.wp_user_id}
                  onChange={e => setUserForm({ ...userForm, wp_user_id: e.target.value })} />
              </div>
              <div className="szv2-field">
                <label className="szv2-label">Plano</label>
                <select className="szv2-select" value={userForm.plano} onChange={e => setUserForm({ ...userForm, plano: e.target.value })}>
                  <option value="free">Free</option>
                  <option value="basic">Basic</option>
                  <option value="pro">Pro</option>
                  <option value="enterprise">Enterprise</option>
                </select>
              </div>
            </div>
            <div className="szv2-field" style={{ marginBottom: '12px' }}>
              <label className="szv2-label">Email *</label>
              <input className="szv2-input" type="email" required value={userForm.email} onChange={e => setUserForm({ ...userForm, email: e.target.value })} />
            </div>
            <div className="szv2-field" style={{ marginBottom: '12px' }}>
              <label className="szv2-label">Nome *</label>
              <input className="szv2-input" required value={userForm.nome} onChange={e => setUserForm({ ...userForm, nome: e.target.value })} />
            </div>
            <div className="szv2-field" style={{ marginBottom: '16px' }}>
              <label className="szv2-label">Role</label>
              <select className="szv2-select" value={userForm.role} onChange={e => setUserForm({ ...userForm, role: e.target.value })}>
                <option value="produtor">Produtor</option>
                <option value="afiliado">Afiliado</option>
                <option value="operator">Operator (OL)</option>
              </select>
            </div>
            <button type="submit" disabled={saving} className="szv2-btn szv2-btn-brand" style={{ width: '100%' }}>
              {saving ? 'Salvando…' : 'Inserir / Atualizar'}
            </button>
            {userMsg && (
              <div style={{ marginTop: '12px', padding: '10px 14px', borderRadius: '10px', fontSize: '13px', background: userMsg.startsWith('✓') ? 'var(--szv2-success-bg)' : 'var(--szv2-danger-bg)', color: userMsg.startsWith('✓') ? 'var(--szv2-success)' : 'var(--szv2-danger)' }}>
                {userMsg}
              </div>
            )}
          </form>
        </div>
      </div>
    </div>
  )
}
