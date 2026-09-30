// Usuários (equipe / sub-usuários) — ligada ao go/portal:
//   GET    /portal/users        → UsersHandler.List
//   POST   /portal/users        → UsersHandler.Create
//   DELETE /portal/users/{id}   → UsersHandler.Delete
//
// Port fiel de templates/portal/v2/sections/users.php (+ painel completo em
// settings.php → szv2-panel-st-users). Só o usuário principal gerencia equipe
// (gate empty(parent_user_id) no PHP / isManager no Go — subconta recebe 403).
// Permissões (whitelist DT-CODE-02): approve, cancel, suspend, wallet, links.
import { FormEvent, useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'

type SubUser = {
  id: number
  nome: string
  email: string
  role: string
  ativo: boolean
  permissions?: Record<string, boolean>
  created_at: string
}

type ListResp = { ok: boolean; data: SubUser[]; total: number }

// Whitelist de permissões — espelha allowedUserPermissions (users_portal.go)
// e $perms_list de Portal_Page.php::render_users. Exact match, nunca substring.
const PERMS: { key: string; label: string }[] = [
  { key: 'approve', label: 'Autorizar envio' },
  { key: 'cancel', label: 'Cancelar pedido' },
  { key: 'suspend', label: 'Perda / extravio' },
  { key: 'wallet', label: 'Ver carteira' },
  { key: 'links', label: 'Gerenciar links' },
]

export default function Users() {
  const toast = useToast()
  const [rows, setRows] = useState<SubUser[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [denied, setDenied] = useState(false)

  // formulário "novo acesso"
  const [email, setEmail] = useState('')
  const [senha, setSenha] = useState('')
  const [nome, setNome] = useState('')
  const [perms, setPerms] = useState<string[]>([])
  const [formErr, setFormErr] = useState('')
  const [creating, setCreating] = useState(false)

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/users')
      .then(r => setRows(r.data || []))
      .catch(e => {
        // 403 → subconta sem permissão de gerenciar equipe.
        if (/permiss/i.test(e.message || '')) setDenied(true)
        else setErr(e.message || 'Erro ao carregar usuários')
      })
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  function togglePerm(key: string) {
    setPerms(prev => (prev.includes(key) ? prev.filter(x => x !== key) : [...prev, key]))
  }

  async function create(e: FormEvent) {
    e.preventDefault()
    setFormErr('')
    if (senha.length < 8) {
      setFormErr('Senha mínima 8 caracteres.')
      return
    }
    setCreating(true)
    try {
      await api('/portal/users', {
        method: 'POST',
        body: JSON.stringify({ email, senha, nome, permissions: perms }),
      })
      toast('ok', 'Acesso criado com sucesso.')
      setEmail(''); setSenha(''); setNome(''); setPerms([])
      load()
    } catch (e: any) {
      setFormErr(e.message || 'Erro ao criar acesso.')
      toast('err', e.message || 'Erro ao criar acesso.')
    } finally {
      setCreating(false)
    }
  }

  async function remove(row: SubUser) {
    const ok = await confirmAsync({
      title: 'Remover acesso',
      message: `Remover o acesso de ${row.nome || row.email}? Esta pessoa perderá o acesso ao painel.`,
      confirmLabel: 'Remover',
      danger: true,
    })
    if (!ok) return
    try {
      await api(`/portal/users/${row.id}`, { method: 'DELETE' })
      toast('ok', 'Acesso excluído com sucesso.')
      load()
    } catch (e: any) {
      toast('err', e.message || 'Erro ao remover acesso.')
    }
  }

  return (
    <section>
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Usuários
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Gerencie quem da sua equipe acessa o painel e o que cada um pode fazer.
        </p>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {denied ? (
        <div className="szv2-card" style={{ padding: 20 }}>
          <EmptyState
            icon="🔒"
            title="Sem permissão"
            description="Apenas o usuário principal da conta pode gerenciar a equipe."
          />
        </div>
      ) : (
        <div
          className="szv2-fr-carrier-grid"
          style={{ display: 'grid', gridTemplateColumns: '380px 1fr', gap: 16, alignItems: 'start' }}
        >
          {/* Novo acesso */}
          <div className="szv2-card" style={{ padding: 20 }}>
            <div style={{ marginBottom: 14 }}>
              <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Novo acesso</h3>
              <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                Cadastre uma pessoa da equipe e defina as permissões.
              </p>
            </div>
            <form onSubmit={create}>
              <div style={{ marginBottom: 12 }}>
                <label className="sz-login-label">E-mail</label>
                <input
                  className="sz-login-input"
                  type="email"
                  placeholder="funcionario@email.com"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  autoComplete="off"
                  required
                  style={{ marginBottom: 0 }}
                />
              </div>
              <div style={{ marginBottom: 12 }}>
                <label className="sz-login-label">Senha</label>
                <input
                  className="sz-login-input"
                  type="password"
                  placeholder="Mínimo 8 caracteres"
                  value={senha}
                  onChange={e => setSenha(e.target.value)}
                  autoComplete="new-password"
                  required
                  minLength={8}
                  style={{ marginBottom: 0 }}
                />
              </div>
              <div style={{ marginBottom: 12 }}>
                <label className="sz-login-label">Nome</label>
                <input
                  className="sz-login-input"
                  type="text"
                  placeholder="Nome do funcionário"
                  value={nome}
                  onChange={e => setNome(e.target.value)}
                  style={{ marginBottom: 0 }}
                />
              </div>
              <div style={{ marginBottom: 14 }}>
                <label className="sz-login-label">Permissões</label>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  {PERMS.map(p => (
                    <label
                      key={p.key}
                      style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--szv2-text)' }}
                    >
                      <input type="checkbox" checked={perms.includes(p.key)} onChange={() => togglePerm(p.key)} />
                      {p.label}
                    </label>
                  ))}
                </div>
              </div>
              {formErr && (
                <div style={{ color: 'var(--szv2-danger)', fontSize: 13, marginBottom: 10 }}>{formErr}</div>
              )}
              <button type="submit" className="szv2-btn szv2-btn-brand" disabled={creating} style={{ width: '100%' }}>
                {creating ? 'Criando…' : 'Criar acesso'}
              </button>
            </form>
          </div>

          {/* Acessos criados */}
          <div className="szv2-card" style={{ padding: 20 }}>
            <div
              style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 14 }}
            >
              <div>
                <h3 style={{ margin: 0, fontSize: 15, fontWeight: 700, color: 'var(--szv2-text)' }}>Acessos criados</h3>
                <p style={{ margin: '4px 0 0', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                  Usuários autorizados a acessar este painel.
                </p>
              </div>
              <span className="szv2-badge szv2-badge-neutral">{rows.length}</span>
            </div>

            {loading ? (
              <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
            ) : rows.length === 0 ? (
              <EmptyState
                icon="👥"
                title="Nenhum acesso criado"
                description="Quando você cadastrar membros da equipe, eles aparecerão aqui."
              />
            ) : (
              <div className="szv2-table-wrap">
                <table className="szv2-table" style={{ width: '100%' }}>
                  <thead>
                    <tr>
                      <th>Nome</th>
                      <th>E-mail</th>
                      <th>Status</th>
                      <th style={{ textAlign: 'right' }}>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map(r => (
                      <tr key={r.id}>
                        <td className="szv2-td-main">{r.nome || '—'}</td>
                        <td className="szv2-td-sub">{r.email || '—'}</td>
                        <td>
                          <span className={`sz-badge ${r.ativo ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                            {r.ativo ? 'Ativo' : 'Inativo'}
                          </span>
                        </td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-sm szv2-btn-danger"
                            onClick={() => remove(r)}
                          >
                            Remover
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
