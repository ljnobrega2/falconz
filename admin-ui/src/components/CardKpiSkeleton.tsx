// Esqueleto de cards KPI horizontais com animação shimmer.
// Usado enquanto fetches de KPIs (Dashboard, MotoboyDashboard, Products, etc.)
// ainda não retornaram.
//
// Reutiliza as classes .szv2-skeleton-block / @keyframes shimmer injetadas
// por TableSkeleton — garante que ambos compartilhem a mesma definição.
//
// Uso:
//   {stats === null
//     ? <CardKpiSkeleton count={3} />
//     : <div className="szv2-kpi-grid">...</div>}

import { useEffect } from 'react'

type CardKpiSkeletonProps = {
  count?: number
}

const STYLE_ID = 'szv2-shimmer-keyframes'

// Garante que o keyframes esteja disponível mesmo se o usuário renderizar
// CardKpiSkeleton antes de qualquer TableSkeleton.
function ensureShimmerKeyframes() {
  if (typeof document === 'undefined') return
  if (document.getElementById(STYLE_ID)) return
  const styleEl = document.createElement('style')
  styleEl.id = STYLE_ID
  styleEl.textContent = `
@keyframes shimmer {
  0%   { background-position: -400px 0; }
  100% { background-position: 400px 0; }
}
.szv2-skeleton-cell {
  display: block;
  width: 100%;
  height: 14px;
  border-radius: 4px;
  background: linear-gradient(90deg, var(--szv2-surface-alt) 0%, var(--szv2-divider) 50%, var(--szv2-surface-alt) 100%);
  background-size: 800px 100%;
  animation: shimmer 1.5s infinite linear;
}
.szv2-skeleton-block {
  display: block;
  border-radius: 8px;
  background: linear-gradient(90deg, var(--szv2-surface-alt) 0%, var(--szv2-divider) 50%, var(--szv2-surface-alt) 100%);
  background-size: 800px 100%;
  animation: shimmer 1.5s infinite linear;
}
`
  document.head.appendChild(styleEl)
}

export default function CardKpiSkeleton({ count = 3 }: CardKpiSkeletonProps) {
  useEffect(() => {
    ensureShimmerKeyframes()
  }, [])

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: `repeat(${count}, minmax(0,1fr))`,
        gap: 16,
        marginBottom: 16,
      }}
    >
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="szv2-card"
          style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 10 }}
        >
          <span
            className="szv2-skeleton-block"
            style={{ height: 11, width: '55%' }}
          />
          <span
            className="szv2-skeleton-block"
            style={{ height: 28, width: '70%' }}
          />
          <span
            className="szv2-skeleton-block"
            style={{ height: 10, width: '40%' }}
          />
        </div>
      ))}
    </div>
  )
}
