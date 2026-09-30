// Painel de "Alertas" (auditoria financeira/operação) — MOVIDO do Dashboard para
// o hub Sistema (regra do dono: "o resto relacionado a AUDITORIA move para CONFIGS").
// Dashboard fica só com KPIs operacionais do dia; este painel agrega as
// divergências (saldo/afiliado/wallet/split) + ocorrências operacionais e faz
// drill-down para a Auditoria (?audit_type=). Fetch idêntico ao que vivia no
// Dashboard (/dashboard + /dashboard/alerts) — não duplica lógica, apenas mudou de lugar.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import CardKpiSkeleton from '../components/CardKpiSkeleton'

// Destino canônico da Auditoria (rota /audit redireciona aqui — usamos o destino
// final direto para evitar o double-hop e poder passar ?audit_type=).
const AUDIT_TO = '/sistema?tab=auditoria'

type KPIs = {
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

export default function AlertasAuditoria() {
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
    ? { label: 'Abrir auditoria', to: AUDIT_TO }
    : { label: 'Ver pedidos', to: '/orders?stopped=1' }

  return (
    <div>
      {errK && <div className="sz-alert-danger">{errK}</div>}

      <div className="szv2-card">
        <div className="szv2-card-head" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <h2>Alertas — financeiro e operação</h2>
          {alertasTotal > 0 && (
            <Link to={alertBtn.to} className="szv2-btn szv2-btn-brand szv2-btn-sm">
              {alertBtn.label}
            </Link>
          )}
        </div>
        {errAl && <div className="sz-alert-danger">{errAl}</div>}
        {al ? (
          <div style={{ marginTop: '8px' }}>
            <AlertRow label="Saldo divergente" count={al.saldo_divergente} linkTo={`${AUDIT_TO}&audit_type=wallet`} />
            <AlertRow label="Afiliado sem transação" count={al.aff_sem_transacao} linkTo={`${AUDIT_TO}&audit_type=aff_missing`} />
            <AlertRow label="Wallet divergente" count={al.wallet_divergente} linkTo={`${AUDIT_TO}&audit_type=wallet`} />
            {/* Drill-down acionável: deep-link pré-filtra a Auditoria no tipo "split"
                (lista os pedidos divergentes esperado×encontrado + Corrigir). */}
            <AlertRow label="Split financeiro divergente" count={al.split_divergente} linkTo={`${AUDIT_TO}&audit_type=split`} />
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
