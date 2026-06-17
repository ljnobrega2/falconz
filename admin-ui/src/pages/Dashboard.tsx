import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import CardKpiSkeleton from '../components/CardKpiSkeleton'

type KPIs = {
  pedidos_hoje: number
  agendados: number
  em_rota: number
  entregues_hoje: number
  frustrados_hoje: number
  alertas_total: number
}

type Alertas = {
  saldo_divergente: number
  aff_sem_transacao: number
  wallet_divergente: number
  split_divergente: number
  webhooks_falhando_7d: number
  pedidos_parados_24h: number
}

function KpiCard({
  label,
  value,
  sub,
  variant,
}: {
  label: string
  value: string | number
  sub?: string
  variant?: 'warn' | 'ok' | 'danger'
}) {
  const color =
    variant === 'danger'
      ? 'var(--szv2-danger)'
      : variant === 'warn'
      ? 'var(--szv2-warning, #f59e0b)'
      : variant === 'ok'
      ? 'var(--szv2-success, #22c55e)'
      : 'var(--szv2-brand)'
  return (
    <div className="szv2-card">
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color }}>
          {value}
        </span>
        {sub && <span className="szv2-kpi-meta">{sub}</span>}
      </div>
    </div>
  )
}

function AlertRow({
  label,
  count,
  linkTo,
}: {
  label: string
  count: number
  linkTo?: string
}) {
  const badge =
    count > 0 ? (
      <span className="sz-badge szv2-badge-warn" style={{ color: 'var(--szv2-warning, #f59e0b)' }}>
        ⚠ {count}
      </span>
    ) : (
      <span className="sz-badge szv2-badge-success">OK</span>
    )
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '10px 0',
        borderBottom: '1px solid var(--szv2-border, #f1f5f9)',
      }}
    >
      <strong style={{ fontSize: '14px' }}>{label}</strong>
      {count > 0 && linkTo ? (
        <Link to={linkTo} style={{ textDecoration: 'none' }}>
          {badge}
        </Link>
      ) : (
        badge
      )}
    </div>
  )
}

export default function Dashboard() {
  const [k, setK] = useState<KPIs | null>(null)
  const [al, setAl] = useState<Alertas | null>(null)
  const [errK, setErrK] = useState('')
  const [errAl, setErrAl] = useState('')

  useEffect(() => {
    api<KPIs>('/dashboard').then(setK).catch(e => setErrK(e.message))
    api<Alertas>('/dashboard/alerts').then(setAl).catch(e => setErrAl(e.message))
  }, [])

  const alertasTotal = k?.alertas_total ?? 0
  // Botão: se há alertas financeiros (saldo/aff/wallet/split) → auditoria; senão → pedidos parados
  const temAlertaFinanceiro =
    al && (al.saldo_divergente + al.aff_sem_transacao + al.wallet_divergente + al.split_divergente) > 0
  const alertBtn = temAlertaFinanceiro
    ? { label: 'Abrir auditoria', to: '/audit' }
    : { label: 'Ver pedidos', to: '/orders?stopped=1' }

  return (
    <div>
      {errK && <div className="sz-alert-danger">{errK}</div>}

      {/* 6 KPIs operacionais — espelha tab_overview_operacao() */}
      {k ? (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(6, minmax(0,1fr))' }}>
          <KpiCard label="Pedidos Hoje" value={k.pedidos_hoje} sub="Operação do dia" />
          <KpiCard label="Agendados" value={k.agendados} sub="Aguardando separação/rota" variant="warn" />
          <KpiCard label="Em Rota" value={k.em_rota} sub="Motoboy em entrega" />
          <KpiCard label="Entregues" value={k.entregues_hoje} sub="Concluídos hoje" variant="ok" />
          <KpiCard label="Frustrados" value={k.frustrados_hoje} sub="Ocorrências hoje" variant={k.frustrados_hoje > 0 ? 'danger' : undefined} />
          <KpiCard
            label="Alertas"
            value={alertasTotal}
            sub="Financeiro e operação"
            variant={alertasTotal > 0 ? 'warn' : 'ok'}
          />
        </div>
      ) : (
        !errK && <CardKpiSkeleton count={6} />
      )}

      {/* Seção Alertas operacionais — espelha alert_line() */}
      <div className="szv2-card" style={{ marginTop: '24px' }}>
        <div className="szv2-card-head" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <h2>Alertas operacionais</h2>
          {alertasTotal > 0 && (
            <Link to={alertBtn.to} className="szv2-btn szv2-btn-brand szv2-btn-sm">
              {alertBtn.label}
            </Link>
          )}
        </div>
        {errAl && <div className="sz-alert-danger">{errAl}</div>}
        {al ? (
          <div style={{ marginTop: '8px' }}>
            <AlertRow label="Saldo divergente" count={al.saldo_divergente} linkTo="/audit" />
            <AlertRow label="Afiliado sem transação" count={al.aff_sem_transacao} linkTo="/audit" />
            <AlertRow label="Wallet divergente" count={al.wallet_divergente} linkTo="/audit" />
            <AlertRow label="Split financeiro divergente" count={al.split_divergente} linkTo="/audit" />
            <AlertRow label="Webhook falhando (7d)" count={al.webhooks_falhando_7d} />
            <AlertRow
              label="Pedido parado (24h+)"
              count={al.pedidos_parados_24h}
              linkTo="/orders?stopped=1"
            />
          </div>
        ) : (
          !errAl && <CardKpiSkeleton count={3} />
        )}
      </div>
    </div>
  )
}
