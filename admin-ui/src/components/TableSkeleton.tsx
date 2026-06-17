// Esqueleto de tabela com animação shimmer.
// Renderiza linhas/colunas placeholder enquanto dados carregam,
// evitando "tela branca" e mantendo a estrutura visual da tabela final.
//
// Uso:
//   {loading && items.length === 0
//     ? <TableSkeleton rows={6} cols={5} />
//     : <table>...</table>}
//
// O <style> com @keyframes shimmer é injetado uma única vez no <head>,
// independente de quantas instâncias de TableSkeleton existirem.

import { useEffect } from 'react'

type TableSkeletonProps = {
  rows?: number
  cols?: number
}

const STYLE_ID = 'szv2-shimmer-keyframes'

// Injeta keyframes só uma vez no DOM, mesmo com múltiplas instâncias.
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

export default function TableSkeleton({ rows = 6, cols = 5 }: TableSkeletonProps) {
  useEffect(() => {
    ensureShimmerKeyframes()
  }, [])

  // Larguras pseudo-aleatórias variando entre 60% e 100% para parecer natural.
  const widths = ['90%', '70%', '80%', '60%', '85%', '75%', '95%', '65%']

  return (
    <div className="szv2-table-wrap">
      <table className="szv2-table" style={{ width: '100%' }}>
        <thead>
          <tr>
            {Array.from({ length: cols }).map((_, c) => (
              <th key={c}>
                <span
                  className="szv2-skeleton-cell"
                  style={{ width: widths[c % widths.length], height: 12 }}
                />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }).map((_, r) => (
            <tr key={r}>
              {Array.from({ length: cols }).map((_, c) => (
                <td key={c}>
                  <span
                    className="szv2-skeleton-cell"
                    style={{ width: widths[(r + c) % widths.length] }}
                  />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
