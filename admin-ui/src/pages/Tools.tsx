import { useEffect, useState } from 'react'
import { api } from '../api'
import EmptyState from '../components/EmptyState'

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
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setStatsErr('')
    api<{ tables: TableStat[] }>('/tools/stats')
      // Null-safe: resposta sem `tables` (ou null) não deve estourar o .map abaixo.
      .then(r => setStats(r.tables ?? []))
      .catch(e => setStatsErr(e.message))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Ferramentas</h1><p>Diagnóstico e operações administrativas</p></div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr', gap: '20px' }}>
        {/* Stats */}
        <div className="szv2-card">
          <div className="szv2-card-head">
            <div><h2>Contagem de Registros</h2><p className="szv2-card-sub">Total por tabela no PostgreSQL</p></div>
          </div>
          {statsErr && <div className="sz-alert-danger" style={{ marginBottom: '16px' }}>{statsErr}</div>}
          {loading ? (
            <p style={{ fontSize: '13px', color: 'var(--szv2-text-faint)' }}>Carregando…</p>
          ) : !statsErr && stats.length === 0 ? (
            <EmptyState
              icon="🗄️"
              title="Nenhuma contagem disponível"
              description="O serviço não retornou estatísticas de tabelas no momento."
            />
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {stats.map(s => (
                <div key={s.table} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <span style={{ fontSize: '13px', color: 'var(--szv2-text-soft)' }}>{TABLE_LABELS[s.table] || s.table}</span>
                  <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '14px', fontWeight: 700, color: s.count > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)' }}>
                    {s.count.toLocaleString('pt-BR')}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

      </div>
    </div>
  )
}
