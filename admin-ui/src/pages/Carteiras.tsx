// Tela ÚNICA "Carteiras" — consolida os pontos de carteira antes espalhados
// (Usuários > Afiliados > Carteira, Carteira COD > aba Carteira [Produtor/
// Afiliado], Carteira Expedição > aba Carteira). Cada ator é um domínio
// financeiro DIFERENTE — "disponível" não significa a mesma coisa em cada um
// (retenção 30d de comissão vs. saldo COD conciliado vs. saldo PIX de
// cliente) — então NÃO fundimos colunas/cálculos numa tabela única. Só
// trocamos "4+ menus" por "1 menu com seletor de ator" — cada componente é
// montado intacto, com suas próprias colunas, ações e drawers de detalhe.
// AUDIT-2026-07-14 — ator "Motoboys" MOVIDO p/ Administração (grupo
// Financeiro, junto de Saques Motoboy) — é operação motoboy, não carteira
// de cliente/produtor/afiliado.
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import AffiliateWallet from './AffiliateWallet'
import CodWalletProducer from './CodWalletProducer'
import TpcClientes from './TpcClientes'

type Actor = 'afiliados' | 'produtores' | 'clientes'

const ACTORS: { key: Actor; label: string }[] = [
  { key: 'afiliados',  label: 'Afiliados' },
  { key: 'produtores', label: 'Produtores COD' },
  { key: 'clientes',   label: 'Clientes' },
]

export default function Carteiras() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('actor') as Actor) || 'afiliados'
  const [actor, setActor] = useState<Actor>(
    ACTORS.some(a => a.key === initial) ? initial : 'afiliados',
  )

  function selectActor(key: Actor) {
    setActor(key)
    const next = new URLSearchParams(params)
    next.set('actor', key)
    setParams(next, { replace: true })
  }

  // Sem <h1> próprio aqui — cada componente montado abaixo já traz seu
  // próprio cabeçalho (evita header duplicado, "aquela merda em vários menus").
  return (
    <div>
      <div className="szv2-tabs" style={{ marginBottom: 16 }}>
        {ACTORS.map(a => (
          <button
            key={a.key}
            className="szv2-tab"
            aria-selected={actor === a.key}
            onClick={() => selectActor(a.key)}
          >
            {a.label}
          </button>
        ))}
      </div>

      {actor === 'afiliados'  && <AffiliateWallet />}
      {actor === 'produtores' && <CodWalletProducer />}
      {actor === 'clientes'   && <TpcClientes />}
    </div>
  )
}
