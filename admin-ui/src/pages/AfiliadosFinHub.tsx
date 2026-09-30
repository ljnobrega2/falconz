// Tela ÚNICA "Afiliados $" — Carteira + Regras numa só tela OTIMIZADA.
//
// Antes empilhava AffiliateWallet + AffiliateRules inteiros (2 headers, KPIs
// duplicados, 2 formulários dominando → scroll de 2 páginas). Agora:
//   1. UM cabeçalho só.
//   2. UMA faixa de KPIs compacta (Pendente · Disponível · Dívida · Total
//      afiliados · Comissão média) — buscada AQUI em /affiliates-wallet/summary
//      + /affiliate-rules/stats, não bubblada do estado interno dos filhos.
//   3. Conteúdo principal = a LISTA de afiliados (<AffiliateWallet embedded>).
//   4. Card COMPACTO "Recompensa de indicação" (regra fixa 2,5% / 1%).
//   5. <details> RECOLHIDO com as configurações (<AffiliateRules embedded>).
//
// Os filhos em modo `embedded` omitem seus headers/KPIs/recompensa duplicados;
// `onChanged` re-busca o resumo da faixa após sync/liberação na lista.
import { useEffect, useState } from 'react'
import { api } from '../api'
import AffiliateWallet from './AffiliateWallet'
import AffiliateRules from './AffiliateRules'

type Summary = {
  total_pendente: number
  total_disponivel: number
  total_debt: number
  affiliates_count: number
}

type Stats = {
  // total_affiliates = COUNT(*) de senderzz_affiliates (TODOS os cadastrados).
  // Difere de summary.affiliates_count, que conta só quem tem atividade de
  // carteira (COUNT DISTINCT afiliado_id nas transações) — subconjunto menor.
  // O KPI "Total afiliados" usa total_affiliates (leitura natural do rótulo).
  total_affiliates: number
  avg_commission_pct: number
}

const fmt = (v: number) =>
  v.toLocaleString('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const money = (v: number) => 'R$ ' + fmt(v)

// KPI compacto da faixa unificada. `tone` controla a cor do valor.
// Sem verde em UI (identidade FALK) — Disponível usa o azul da marca.
function Kpi({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: 'brand' | 'warning' | 'danger' | 'muted'
}) {
  const color =
    tone === 'warning' ? 'var(--szv2-warning, #d97706)'
    : tone === 'danger' ? 'var(--szv2-danger)'
    : tone === 'muted'  ? 'var(--szv2-text)'
                        : 'var(--szv2-brand)'
  return (
    <div className="szv2-card" style={{ padding: '14px 16px' }}>
      <div className="szv2-kpi">
        <span className="szv2-kpi-label">{label}</span>
        <span className="szv2-kpi-value" style={{ color, fontSize: 22 }}>{value}</span>
      </div>
    </div>
  )
}

export default function AfiliadosFinHub({
  initialTab,
}: {
  initialTab?: 'carteira' | 'regras'
} = {}) {
  const [summary, setSummary] = useState<Summary | null>(null)
  const [stats, setStats] = useState<Stats | null>(null)

  // Faixa de KPIs: resumo da carteira (4 KPIs) + comissão média (das stats de
  // regras). Tolerante a falha — a tela não quebra se um dos dois falhar.
  async function loadStrip() {
    const [s, st] = await Promise.all([
      api<Summary>('/affiliates-wallet/summary').catch(() => null),
      api<Stats>('/affiliate-rules/stats').catch(() => null),
    ])
    if (s) setSummary(s)
    if (st) setStats(st)
  }

  useEffect(() => { loadStrip() }, [])

  return (
    <div>
      {/* 1 ── Cabeçalho único */}
      <div className="szv2-section-head">
        <div>
          <h1>Afiliados $</h1>
          <p>Carteiras, saldos e regras do programa de afiliados</p>
        </div>
      </div>

      {/* 2 ── Faixa de KPIs compacta (5 essenciais em uma linha responsiva) */}
      <div
        className="szv2-kpi-grid"
        style={{ gridTemplateColumns: 'repeat(5, minmax(0,1fr))', gap: 12, marginBottom: 20 }}
      >
        <Kpi label="Pendente" value={summary ? money(summary.total_pendente) : '—'} tone="warning" />
        <Kpi label="Disponível" value={summary ? money(summary.total_disponivel) : '—'} tone="brand" />
        <Kpi label="Dívida total" value={summary ? money(summary.total_debt) : '—'} tone="danger" />
        <Kpi
          label="Total afiliados"
          value={stats ? stats.total_affiliates.toLocaleString('pt-BR') : '—'}
          tone="muted"
        />
        <Kpi
          label="Comissão média"
          value={stats ? `${fmt(stats.avg_commission_pct)}%` : '—'}
          tone="brand"
        />
      </div>

      {/* 3 ── Conteúdo principal: lista de afiliados (carteira) */}
      <AffiliateWallet embedded onChanged={loadStrip} />

      {/* 4 ── Card compacto "Recompensa de indicação" (regra fixa) */}
      <div className="szv2-card" style={{ marginTop: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Recompensa de indicação</h2>
            <p className="szv2-card-sub">
              Regra FIXA: cada usuário tem um link individual
              (<span style={{ fontFamily: 'var(--szv2-font-mono)' }}>falklog.com.br/r/&#123;código&#125;</span>);
              o indicador recebe quando o indicado gera pedido.
            </p>
          </div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
          <div style={{ padding: '12px 14px', background: 'var(--szv2-brand-light)', borderRadius: 10 }}>
            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>COD concluído (pago na entrega)</div>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
              <span style={{ fontSize: 26, fontWeight: 800, color: 'var(--szv2-brand)', lineHeight: 1.1 }}>2,5%</span>
              <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>do valor do pedido</span>
            </div>
          </div>
          <div
            style={{
              padding: '12px 14px',
              background: 'var(--szv2-bg-soft, #f8fafc)',
              borderRadius: 10,
              border: '1px solid var(--szv2-divider)',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Expedição (frete)</div>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
              <span style={{ fontSize: 26, fontWeight: 800, color: 'var(--szv2-text)', lineHeight: 1.1 }}>1%</span>
              <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>do valor do pedido</span>
            </div>
          </div>
        </div>
      </div>

      {/* 5 ── Configurações em seção recolhível (recolhida por padrão).
             Abre por padrão SÓ quando a rota é /affiliate-rules (initialTab). */}
      <details
        open={initialTab === 'regras'}
        style={{
          marginTop: 24,
          border: '1px solid var(--szv2-divider)',
          borderRadius: 12,
          background: 'var(--szv2-card-bg, transparent)',
        }}
      >
        <summary
          style={{
            cursor: 'pointer',
            padding: '14px 18px',
            fontWeight: 700,
            fontSize: 15,
            color: 'var(--szv2-brand)',
            userSelect: 'none',
            listStyle: 'revert',
          }}
        >
          ⚙ Regras, penalidades e taxas (clique para editar)
        </summary>
        <div style={{ padding: '4px 18px 18px' }}>
          <AffiliateRules embedded />
        </div>
      </details>
    </div>
  )
}
