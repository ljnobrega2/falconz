import { useEffect, useMemo, useState } from 'react'
import { api as adminApi } from '../admin-orders/api'
import { useToast } from '../hooks/useToast'
import EmptyState from '../components/EmptyState'
import SzStatusBadge from '../components/StatusBadge'

type Producer = { id:number; user_email:string; amount:number; fee:number; net:number; pix_key:string; pix_type:string; holder_name:string; holder_cpf:string; status:string; proof_url?:string|null; created_at:string }
type Affiliate = { id:number; affiliate_name:string; amount:number; fee:number; net_amount:number; pix_key:string; bank_info:string; status:string; proof_url?:string|null; created_at:string }
type Row = { id:number; kind:'producer'|'affiliate'; name:string; holder?:string; amount:number; fee:number; net:number; account:string; status:string; created_at:string; proof_url?:string|null }

const open = (s:string) => ['analysis','em_analise','pending'].includes(s)
const fmt = (n:number) => n.toLocaleString('pt-BR', { minimumFractionDigits:2, maximumFractionDigits:2 })
const label: Record<string,string> = { analysis:'Em análise', em_analise:'Em análise', pending:'Pendente', paid:'Pago', approved:'Aprovado', rejected:'Rejeitado' }

export default function Saques() {
  const [tab, setTab] = useState<'producer'|'affiliate'>('producer')
  const [producer, setProducer] = useState<Producer[]>([])
  const [affiliate, setAffiliate] = useState<Affiliate[]>([])
  const [status, setStatus] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const toast = useToast()

  async function load() {
    setLoading(true); setError('')
    try {
      const qs = status ? `?status=${encodeURIComponent(status)}&limit=120` : '?limit=120'
      const [p, a] = await Promise.all([
        adminApi<{items:Producer[]}>(`/operator/saques/producer${qs}`),
        adminApi<{items:Affiliate[]}>(`/operator/saques/affiliate${(status === 'paid' ? '?status=approved' : qs)}`),
      ])
      setProducer(p.items || []); setAffiliate(a.items || [])
    } catch (e:any) { setError(e.message || 'Erro ao carregar saques') }
    finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [status])

  const rows = useMemo<Row[]>(() => [
    ...producer.map(p => ({ id:p.id, kind:'producer' as const, name:p.user_email || `#${p.id}`, holder:p.holder_name, amount:p.amount, fee:p.fee, net:p.net, account:[p.pix_type, p.pix_key].filter(Boolean).join(' · ') || '—', status:p.status, created_at:p.created_at, proof_url:p.proof_url })),
    ...affiliate.map(a => ({ id:a.id, kind:'affiliate' as const, name:a.affiliate_name || `#${a.id}`, amount:a.amount, fee:a.fee, net:a.net_amount, account:[a.pix_key, a.bank_info].filter(Boolean).join(' · ') || '—', status:a.status, created_at:a.created_at, proof_url:a.proof_url })),
  ].filter(r => r.kind === tab).sort((a,b) => b.created_at.localeCompare(a.created_at)), [producer, affiliate, tab])

  async function action(row: Row, type:'pay'|'reject', proof_url = '') {
    setBusy(true)
    try {
      const path = row.kind === 'producer'
        ? `/operator/saques/producer/${row.id}/${type === 'pay' ? 'mark-paid' : 'reject'}`
        : `/operator/saques/affiliate/${row.id}/${type === 'pay' ? 'approve' : 'reject'}`
      await adminApi(path, { method:'POST', body:JSON.stringify(type === 'pay' ? { proof_url, admin_note:'Processado pelo operador logístico.' } : { admin_note:'Rejeitado pelo operador logístico.' }) })
      toast('ok', type === 'pay' ? 'Saque processado.' : 'Saque rejeitado.')
      await load()
    } catch (e:any) { toast('err', e.message || 'Falha ao processar saque') }
    finally { setBusy(false) }
  }

  return <div>
    <div className="szv2-tabs" style={{ marginBottom:16 }}>
      <button className="szv2-tab" aria-selected={tab === 'producer'} onClick={() => setTab('producer')}>Produtor</button>
      <button className="szv2-tab" aria-selected={tab === 'affiliate'} onClick={() => setTab('affiliate')}>Afiliado</button>
    </div>
    {error && <div className="sz-alert-danger" style={{ marginBottom:16 }}>{error}</div>}
    <div className="szv2-card">
      <div className="szv2-card-head">
        <div><h2>Saques — {tab === 'producer' ? 'Produtores' : 'Afiliados'}</h2><p className="szv2-card-sub">Aprovação, pagamento e rejeição da fila financeira.</p></div>
        <select className="szv2-input" style={{ width:160 }} value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">Todos</option><option value="analysis">Em análise</option><option value="pending">Pendente</option><option value="paid">Pago</option><option value="rejected">Rejeitado</option>
        </select>
      </div>
      {loading ? <div className="szv2-card" style={{ padding:24 }}>Carregando saques…</div> : rows.length === 0 ? <EmptyState icon="💸" title="Nenhum saque encontrado" description="A fila está vazia para este filtro." /> : <div style={{ overflowX:'auto' }}><table className="szv2-table"><thead><tr><th>ID</th><th>Usuário</th><th style={{textAlign:'right'}}>Valor</th><th style={{textAlign:'right'}}>Taxa</th><th style={{textAlign:'right'}}>Líquido</th><th>PIX / Conta</th><th>Status</th><th>Data</th><th>Ações</th></tr></thead><tbody>{rows.map(r => <tr key={`${r.kind}-${r.id}`}><td><strong>#{r.id}</strong></td><td><div>{r.name}</div>{r.holder && <small>{r.holder}</small>}</td><td style={{textAlign:'right'}}>R$ {fmt(r.amount)}</td><td style={{textAlign:'right'}}>R$ {fmt(r.fee)}</td><td style={{textAlign:'right',fontWeight:700}}>R$ {fmt(r.net)}</td><td style={{fontSize:11}}>{r.account}</td><td><SzStatusBadge status={r.status} label={label[r.status] || r.status} /></td><td>{r.created_at?.slice(0,16).replace('T',' ')}</td><td>{open(r.status) && <div style={{display:'flex',gap:6}}><button className="szv2-btn szv2-btn-brand szv2-btn-sm" disabled={busy} onClick={() => void action(r,'pay')}>{r.kind === 'producer' ? 'Marcar pago' : 'Aprovar'}</button><button className="szv2-btn szv2-btn-danger szv2-btn-sm" disabled={busy} onClick={() => void action(r,'reject')}>Rejeitar</button></div>}</td></tr>)}</tbody></table></div>}
    </div>
  </div>
}
