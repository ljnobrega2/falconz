// Tela ÚNICA "Saques" — consolida CodSaques (Produtor/Afiliado/Regras, já
// com abas internas) numa tela só com seletor no topo.
// Mount-intacto: cada fila mantém seu próprio status enum, colunas e fluxo
// de aprovação (DetailDrawer) — dinheiro real muda de mão aqui, então NÃO
// fundimos tabela nem endpoint (mesmo padrão de /carteiras). AUDIT-2026-07-11.
// AUDIT-2026-07-14 — fila "Motoboy" MOVIDA p/ Administração (grupo
// Financeiro) — é operação motoboy, junto do resto do módulo.
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import CodSaques from './CodSaques'
import WalletFreightTransfers from './WalletFreightTransfers'

type Fila = 'cod' | 'frete'

const FILAS: { key: Fila; label: string }[] = [
  { key: 'cod',     label: 'COD (Produtor / Afiliado)' },
  { key: 'frete',   label: 'Transferência p/ Frete' },
]

export default function Saques() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('fila') as Fila) || 'cod'
  const [fila, setFila] = useState<Fila>(
    FILAS.some(f => f.key === initial) ? initial : 'cod',
  )

  function selectFila(key: Fila) {
    setFila(key)
    const next = new URLSearchParams(params)
    next.set('fila', key)
    setParams(next, { replace: true })
  }

  // Sem <h1> próprio — MotoboySaques já traz o seu; CodSaques usa h2 interno
  // por fila (Produtor/Afiliado). Evita header duplicado.
  return (
    <div>
      <div className="szv2-tabs" style={{ marginBottom: 16 }}>
        {FILAS.map(f => (
          <button
            key={f.key}
            className="szv2-tab"
            aria-selected={fila === f.key}
            onClick={() => selectFila(f.key)}
          >
            {f.label}
          </button>
        ))}
      </div>

      {fila === 'cod'     && <CodSaques />}
      {fila === 'frete'   && <WalletFreightTransfers />}
    </div>
  )
}
