// Hub de Produtos — "Aprovar Produtos" vira submenu (aba) de Produtos (pedido do
// dono: "aprovar produtos pode ser um submenu de produtos"). Compõe Products +
// ProductApproval em abas; cada aba só monta quando ativa (fetch lazy). Rota
// /products abre em Produtos; /products-approval (deep-link) abre na aba Aprovar.
// AUDIT-2026-07-14 — aba "Estoque" REMOVIDA: mergeada dentro de <Products>
// (tabela única produto+estoque, pedido do dono). /stock redireciona p/ /products.
import { useState } from 'react'
import Products from './Products'
import ProductApproval from './ProductApproval'

type Tab = 'produtos' | 'aprovar'

export default function ProdutosHub({ initialTab = 'produtos' }: { initialTab?: Tab }) {
  const [tab, setTab] = useState<Tab>(initialTab)
  return (
    <div>
      <div
        className="szv2-tabs"
        role="tablist"
        style={{ display: 'flex', gap: 4, marginBottom: 16, borderBottom: '1px solid var(--szv2-divider)' }}
      >
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'produtos'} onClick={() => setTab('produtos')}>
          Produtos
        </button>
        <button type="button" role="tab" className="szv2-tab" aria-selected={tab === 'aprovar'} onClick={() => setTab('aprovar')}>
          Aprovar produtos
        </button>
      </div>

      {tab === 'produtos' && <Products />}
      {tab === 'aprovar' && <ProductApproval />}
    </div>
  )
}
