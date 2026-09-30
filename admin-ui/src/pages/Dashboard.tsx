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

export default function Dashboard() {
  const [k, setK] = useState<KPIs | null>(null)
  const [errK, setErrK] = useState('')

  useEffect(() => {
    api<KPIs>('/dashboard').then(setK).catch(e => setErrK(e.message))
  }, [])

  return (
    <div>
      {errK && <div className="sz-alert-danger">{errK}</div>}

      {/* Atalho p/ a aba Faturamento (números financeiros vivem lá). Dashboard =
          visão operacional do dia. O <Link to="/faturamento"> abre a aba
          Faturamento da VisaoGeral (a tab é derivada da rota). */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          gap: 12,
          marginBottom: 24,
        }}
      >
        <div>
          <h2 style={{ margin: 0 }}>Operação do dia</h2>
          <p style={{ margin: '4px 0 0', color: 'var(--szv2-text-muted)', fontSize: 13 }}>
            KPIs operacionais de hoje. Números financeiros na aba Faturamento.
          </p>
        </div>
        <Link to="/faturamento" className="szv2-btn szv2-btn-brand" style={{ textDecoration: 'none' }}>
          Ver faturamento completo
        </Link>
      </div>

      {/* 5 KPIs operacionais do dia — espelha tab_overview_operacao(). O card
          "Alertas" (auditoria financeira/operação) foi MOVIDO para Sistema. */}
      {k ? (
        <div className="szv2-kpi-grid" style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))' }}>
          <KpiCard label="Pedidos Hoje" value={k.pedidos_hoje} sub="Operação do dia" />
          <KpiCard label="Agendados" value={k.agendados} sub="Aguardando separação/rota" variant="warn" />
          <KpiCard label="Em Rota" value={k.em_rota} sub="Motoboy em entrega" />
          <KpiCard label="Entregues" value={k.entregues_hoje} sub="Concluídos hoje" variant="ok" />
          <KpiCard label="Frustrados" value={k.frustrados_hoje} sub="Ocorrências hoje" variant={k.frustrados_hoje > 0 ? 'danger' : undefined} />
        </div>
      ) : (
        !errK && <CardKpiSkeleton count={5} />
      )}
    </div>
  )
}
