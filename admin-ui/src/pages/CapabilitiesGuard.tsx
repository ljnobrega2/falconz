import { useEffect, useState } from 'react'
import { api } from '../api'

// CapabilitiesGuard — modelo de PAPÉIS (read-only). Atualizado 2026-06-18:
// reflete os 4 papéis atuais (operator/produtor/afiliado/cliente) + admin do
// full-Postgres, não mais capabilities WordPress.

type RoleAccess = {
  role: string
  label: string
  description: string
  fonte: string
  acessos: string[]
  sem_acesso: string[]
}

type RoleCount = { role: string; total: number }

type CapabilitiesData = { roles: RoleAccess[] }
type DistData = { distribuicao: RoleCount[]; admins_ativos: number; note: string }

const ROLE_LABEL: Record<string, string> = {
  admin: 'Admin', operator: 'Operador', produtor: 'Produtor',
  afiliado: 'Afiliado', cliente: 'Cliente',
}

export default function CapabilitiesGuard() {
  const [data, setData] = useState<CapabilitiesData | null>(null)
  const [dist, setDist] = useState<DistData | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  useEffect(() => {
    api<CapabilitiesData>('/capabilities')
      .then(d => setData(d))
      .catch((e: any) => setErr(e.message || 'Erro ao carregar papéis'))
      .finally(() => setLoading(false))

    api<DistData>('/capabilities/users')
      .then(d => setDist(d))
      .catch(() => setDist(null))
  }, [])

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      <div
        style={{
          marginBottom: 20, padding: '12px 16px',
          background: 'rgba(30, 111, 242,.08)', border: '1px solid rgba(30, 111, 242,.25)',
          borderRadius: 8, color: 'var(--szv2-text)', fontSize: 13,
        }}
      >
        Somente leitura. O papel de cada usuário é definido por <strong>vínculo/produto</strong> em{' '}
        <code>senderzz_portal_users.role</code> — não por capability do WordPress.
      </div>

      {/* Distribuição real de usuários por papel */}
      {dist && (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(140px,1fr))', marginBottom: 24 }}>
          {dist.distribuicao.map(d => (
            <div className="szv2-card" key={d.role}>
              <div className="szv2-kpi">
                <span className="szv2-kpi-label">{ROLE_LABEL[d.role] || d.role}</span>
                <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{d.total}</span>
                <span className="szv2-kpi-meta">usuários de portal</span>
              </div>
            </div>
          ))}
          <div className="szv2-card">
            <div className="szv2-kpi">
              <span className="szv2-kpi-label">Admins ativos</span>
              <span className="szv2-kpi-value" style={{ color: 'var(--szv2-brand)' }}>{dist.admins_ativos}</span>
              <span className="szv2-kpi-meta">painel admin</span>
            </div>
          </div>
        </div>
      )}

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>Carregando…</div>
      ) : data && data.roles.length > 0 ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {data.roles.map(role => (
            <div className="szv2-card" key={role.role}>
              <div className="szv2-card-head">
                <div>
                  <h2 style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    {role.label}
                    <code
                      style={{
                        padding: '2px 8px', background: 'rgba(30, 111, 242,.10)',
                        borderRadius: 4, color: 'var(--szv2-brand)', fontFamily: 'monospace', fontSize: 12,
                      }}
                    >
                      {role.role}
                    </code>
                  </h2>
                  <p className="szv2-card-sub">{role.description}</p>
                </div>
              </div>

              <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 12 }}>
                <strong>Como é determinado:</strong> <code style={{ fontFamily: 'monospace' }}>{role.fonte}</code>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: role.sem_acesso.length ? '1fr 1fr' : '1fr', gap: 16 }}>
                <div>
                  <span className="szv2-field-label" style={{ display: 'block', marginBottom: 6 }}>Acessa</span>
                  <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13, lineHeight: 1.7 }}>
                    {role.acessos.map((a, i) => <li key={i}>{a}</li>)}
                  </ul>
                </div>
                {role.sem_acesso.length > 0 && (
                  <div>
                    <span className="szv2-field-label" style={{ display: 'block', marginBottom: 6 }}>Não acessa</span>
                    <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13, lineHeight: 1.7, color: 'var(--szv2-text-muted)' }}>
                      {role.sem_acesso.map((a, i) => <li key={i}>{a}</li>)}
                    </ul>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : !err ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Nenhum papel definido no momento.
        </div>
      ) : null}
    </div>
  )
}
