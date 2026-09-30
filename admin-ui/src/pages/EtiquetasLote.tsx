// Tela ÚNICA de Etiquetas — unifica "Etiquetas / QR Code" (MotoboyEtiquetas) +
// "Ações em Lote (Etiquetas)" (BulkActions) em abas (pedido do dono: "etiquetas e
// ações em lote tb deve ser tela única"). Compõe os componentes existentes; cada
// aba só monta quando ativa (BulkActions é pesada — fetch lazy evita custo à toa).
// Rotas legadas /motoboy-etiquetas e /bulk-actions continuam abrindo a aba certa.
import { useState } from 'react'
import MotoboyEtiquetas from './MotoboyEtiquetas'
import BulkActions from './BulkActions'

type Tab = 'etiquetas' | 'lote'

export default function EtiquetasLote({ initialTab = 'etiquetas' }: { initialTab?: Tab }) {
  const [tab, setTab] = useState<Tab>(initialTab)
  return (
    <div>
      <div
        className="szv2-tabs"
        role="tablist"
        style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--szv2-divider)' }}
      >
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'etiquetas'} onClick={() => setTab('etiquetas')}>
          Etiquetas / QR
        </button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'lote'} onClick={() => setTab('lote')}>
          Ações em lote
        </button>
      </div>

      {tab === 'etiquetas' ? <MotoboyEtiquetas /> : <BulkActions />}
    </div>
  )
}
