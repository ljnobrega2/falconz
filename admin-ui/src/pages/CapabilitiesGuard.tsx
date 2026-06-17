import { useEffect, useState } from 'react'
import { api } from '../api'

// ----- tipos ---------------------------------------------------------------

type GuardConfig = {
  trigger: string
  auto_grants: string[]
}

type CustomCapability = {
  cap: string
  description: string
  default_roles: string[]
}

type ScopeType = {
  scope: string
  description: string
  check: string
}

type CapabilitiesData = {
  guard: GuardConfig
  custom_capabilities: CustomCapability[]
  scope_types: ScopeType[]
}

type CapabilityUser = {
  email: string
  nome: string
  capabilities: string[]
}

type CapabilityUsersData = {
  users: CapabilityUser[]
  note: string
}

// ----- componente principal ------------------------------------------------

export default function CapabilitiesGuard() {
  const [data, setData] = useState<CapabilitiesData | null>(null)
  const [usersData, setUsersData] = useState<CapabilityUsersData | null>(null)
  const [loading, setLoading] = useState(true)
  const [usersLoading, setUsersLoading] = useState(true)
  const [err, setErr] = useState('')

  useEffect(() => {
    api<CapabilitiesData>('/capabilities')
      .then(d => setData(d))
      .catch((e: any) => setErr(e.message || 'Erro ao carregar capabilities'))
      .finally(() => setLoading(false))

    api<CapabilityUsersData>('/capabilities/users')
      .then(d => setUsersData(d))
      .catch(() => setUsersData({ users: [], note: '' }))
      .finally(() => setUsersLoading(false))
  }, [])

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* Aviso somente leitura */}
      <div
        style={{
          marginBottom: 20,
          padding: '12px 16px',
          background: 'rgba(234,88,12,.08)',
          border: '1px solid rgba(234,88,12,.25)',
          borderRadius: 8,
          color: 'var(--szv2-text)',
          fontSize: 13,
        }}
      >
        Esta tela é <strong>somente leitura</strong>. Para alterar capabilities, edite{' '}
        <code>senderzz-access-scope.php</code> ou <code>senderzz_admin_capability_guard()</code>.
      </div>

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : data ? (
        <>
          {/* Card: Guard Automático */}
          <div className="szv2-card" style={{ marginBottom: 24 }}>
            <div className="szv2-card-head">
              <div>
                <h2>Guard Automático</h2>
                <p className="szv2-card-sub">
                  Qualquer usuário WordPress com a capability trigger recebe automaticamente todas as capabilities listadas abaixo.
                </p>
              </div>
            </div>

            <div style={{ marginBottom: 12 }}>
              <span className="szv2-field-label" style={{ marginRight: 8 }}>Trigger capability:</span>
              <code
                style={{
                  padding: '3px 8px',
                  background: 'rgba(234,88,12,.10)',
                  borderRadius: 4,
                  color: 'var(--szv2-brand)',
                  fontFamily: 'monospace',
                  fontSize: 13,
                }}
              >
                {data.guard.trigger}
              </code>
            </div>

            <div>
              <span className="szv2-field-label" style={{ display: 'block', marginBottom: 8 }}>
                Auto-grants:
              </span>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {data.guard.auto_grants.map(cap => (
                  <span
                    key={cap}
                    style={{
                      padding: '4px 10px',
                      background: 'rgba(234,88,12,.10)',
                      border: '1px solid rgba(234,88,12,.25)',
                      borderRadius: 16,
                      fontFamily: 'monospace',
                      fontSize: 12,
                      color: 'var(--szv2-brand)',
                    }}
                  >
                    {cap}
                  </span>
                ))}
              </div>
            </div>
          </div>

          {/* Card: Capabilities Customizadas */}
          <div className="szv2-card" style={{ marginBottom: 24 }}>
            <div className="szv2-card-head">
              <div>
                <h2>Capabilities Customizadas</h2>
                <p className="szv2-card-sub">
                  Capabilities registradas pelo plugin em senderzz-access-scope.php.
                </p>
              </div>
            </div>
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Capability</th>
                    <th>Descrição</th>
                    <th>Roles padrão</th>
                  </tr>
                </thead>
                <tbody>
                  {data.custom_capabilities.map(c => (
                    <tr key={c.cap}>
                      <td>
                        <code style={{ fontFamily: 'monospace', fontSize: 13 }}>{c.cap}</code>
                      </td>
                      <td>{c.description}</td>
                      <td>
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                          {c.default_roles.map(role => (
                            <span
                              key={role}
                              className="sz-badge"
                              style={{ fontFamily: 'monospace', fontSize: 11 }}
                            >
                              {role}
                            </span>
                          ))}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Card: Tipos de Scope */}
          <div className="szv2-card" style={{ marginBottom: 24 }}>
            <div className="szv2-card-head">
              <div>
                <h2>Tipos de Scope</h2>
                <p className="szv2-card-sub">
                  Como o portal detecta o nível de acesso de cada usuário.
                </p>
              </div>
            </div>
            <div style={{ overflowX: 'auto' }}>
              <table className="szv2-table">
                <thead>
                  <tr>
                    <th>Scope</th>
                    <th>Descrição</th>
                    <th>Como detectado</th>
                  </tr>
                </thead>
                <tbody>
                  {data.scope_types.map(s => (
                    <tr key={s.scope}>
                      <td>
                        <span
                          className="sz-badge"
                          style={{
                            background: 'rgba(234,88,12,.10)',
                            color: 'var(--szv2-brand)',
                            border: '1px solid rgba(234,88,12,.20)',
                            fontFamily: 'monospace',
                          }}
                        >
                          {s.scope}
                        </span>
                      </td>
                      <td>{s.description}</td>
                      <td>
                        <code style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--szv2-text-muted)' }}>
                          {s.check}
                        </code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {/* Card: Quais usuários têm capabilities */}
          <div className="szv2-card">
            <div className="szv2-card-head">
              <div>
                <h2>Quais usuários têm</h2>
                <p className="szv2-card-sub">
                  Usuários admin ativos e as capabilities que detêm via auto-grant.
                </p>
              </div>
            </div>

            {usersData?.note && (
              <div
                style={{
                  marginBottom: 12,
                  padding: '8px 12px',
                  background: 'var(--szv2-bg-alt, rgba(0,0,0,.04))',
                  borderRadius: 6,
                  fontSize: 12,
                  color: 'var(--szv2-text-muted)',
                }}
              >
                {usersData.note}
              </div>
            )}

            {usersLoading ? (
              <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
                Carregando…
              </div>
            ) : usersData && usersData.users.length > 0 ? (
              <div style={{ overflowX: 'auto' }}>
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Usuário</th>
                      <th>E-mail</th>
                      <th>Capabilities</th>
                    </tr>
                  </thead>
                  <tbody>
                    {usersData.users.map(u => (
                      <tr key={u.email}>
                        <td>{u.nome || '—'}</td>
                        <td>
                          <code style={{ fontFamily: 'monospace', fontSize: 12 }}>{u.email}</code>
                        </td>
                        <td>
                          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                            {u.capabilities.map(cap => (
                              <span
                                key={cap}
                                style={{
                                  padding: '2px 7px',
                                  background: 'rgba(234,88,12,.10)',
                                  border: '1px solid rgba(234,88,12,.20)',
                                  borderRadius: 12,
                                  fontFamily: 'monospace',
                                  fontSize: 11,
                                  color: 'var(--szv2-brand)',
                                }}
                              >
                                {cap}
                              </span>
                            ))}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div style={{ padding: 24, textAlign: 'center', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
                Nenhum usuário admin ativo encontrado.
              </div>
            )}
          </div>
        </>
      ) : null}
    </div>
  )
}
