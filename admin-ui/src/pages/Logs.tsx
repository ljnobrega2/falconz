import { useEffect, useState } from 'react'
import { api } from '../api'

type Tab = 'webhooks' | 'integrations' | 'motoboy'
type WH = { id: number; webhook_id: number | null; event_type: string; response_code: number | null; response_body: string | null; created_at: string }
type IL = { id: number; user_id: number | null; event: string; payload: string; created_at: string }
type AU = { id: number; pedido_id: number | null; acao: string; descricao: string | null; created_at: string }

export default function Logs() {
  const [tab, setTab] = useState<Tab>('webhooks')
  const [wh, setWh] = useState<WH[]>([])
  const [il, setIl] = useState<IL[]>([])
  const [au, setAu] = useState<AU[]>([])
  const [totals, setTotals] = useState({ wh: 0, il: 0, au: 0 })
  const [err, setErr] = useState('')

  useEffect(() => {
    setErr('')
    if (tab === 'webhooks')
      api<{ items: WH[]; total: number }>('/logs/webhooks?limit=100').then(r => { setWh(r.items); setTotals(t => ({ ...t, wh: r.total })) }).catch(e => setErr(e.message))
    if (tab === 'integrations')
      api<{ items: IL[]; total: number }>('/logs/integrations?limit=100').then(r => { setIl(r.items); setTotals(t => ({ ...t, il: r.total })) }).catch(e => setErr(e.message))
    if (tab === 'motoboy')
      api<{ items: AU[]; total: number }>('/logs/motoboy?limit=100').then(r => { setAu(r.items); setTotals(t => ({ ...t, au: r.total })) }).catch(e => setErr(e.message))
  }, [tab])

  const tabs: { key: Tab; label: string; count: number }[] = [
    { key: 'webhooks',     label: 'Webhooks',        count: totals.wh },
    { key: 'integrations', label: 'Integrações',     count: totals.il },
    { key: 'motoboy',      label: 'Auditoria Motoboy', count: totals.au },
  ]

  return (
    <div>
      <div className="szv2-section-head">
        <div><h1>Logs do Sistema</h1><p>Histórico de eventos e auditoria</p></div>
      </div>

      <div className="szv2-tabs">
        {tabs.map(t => (
          <button key={t.key} className="szv2-tab" aria-selected={tab === t.key} onClick={() => setTab(t.key)}>
            {t.label}
            {t.count > 0 && <span style={{ marginLeft: '6px', fontSize: '11px', color: 'var(--szv2-text-faint)' }}>({t.count})</span>}
          </button>
        ))}
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {tab === 'webhooks' && (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Webhook</th><th>Evento</th><th>HTTP</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {wh.map(l => (
                <tr key={l.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{l.id}</td>
                  <td style={{ fontSize: '13px' }}>{l.webhook_id ?? '—'}</td>
                  <td style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px' }}>{l.event_type}</td>
                  <td>
                    <span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '12px', color: l.response_code && l.response_code < 300 ? 'var(--szv2-success)' : 'var(--szv2-danger)' }}>
                      {l.response_code ?? '—'}
                    </span>
                  </td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
              {wh.length === 0 && <tr><td colSpan={6}><div className="szv2-empty"><h3>Sem logs de webhook</h3></div></td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === 'integrations' && (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Usuário</th><th>Tipo</th><th>Mensagem</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {il.map(l => (
                <tr key={l.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{l.id}</td>
                  <td style={{ fontSize: '13px' }}>{l.user_id ?? '—'}</td>
                  <td><span style={{ fontFamily: 'var(--szv2-font-mono)', fontSize: '11px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px' }}>{l.event}</span></td>
                  <td style={{ fontSize: '13px', color: 'var(--szv2-text-soft)', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.payload}</td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{l.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
              {il.length === 0 && <tr><td colSpan={5}><div className="szv2-empty"><h3>Sem logs de integração</h3></div></td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === 'motoboy' && (
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr>
              <th>ID</th><th>Pedido</th><th>Ação</th><th>Descrição</th><th>Quando</th>
            </tr></thead>
            <tbody>
              {au.map(a => (
                <tr key={a.id}>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>#{a.id}</td>
                  <td style={{ fontSize: '13px' }}>{a.pedido_id ? `#${a.pedido_id}` : '—'}</td>
                  <td><span style={{ background: 'var(--szv2-brand-light)', color: 'var(--szv2-brand)', fontSize: '11px', fontWeight: 700, padding: '2px 8px', borderRadius: '6px' }}>{a.acao}</span></td>
                  <td style={{ fontSize: '13px', color: 'var(--szv2-text-soft)', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.descricao ?? '—'}</td>
                  <td style={{ fontSize: '12px', color: 'var(--szv2-text-muted)' }}>{a.created_at.slice(0, 16).replace('T', ' ')}</td>
                </tr>
              ))}
              {au.length === 0 && <tr><td colSpan={5}><div className="szv2-empty"><h3>Sem auditoria motoboy</h3></div></td></tr>}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
