// Tela ÚNICA "Taxas & Config" — consolida ConfigTaxas + CodTaxasEntrega +
// TpcConfiguracoes numa tela só com seletor no topo. 3 domínios diferentes
// (regra de cálculo global / taxas COD por produtor-motoboy-afiliado /
// config ME+TPC) — cada um mantém seu form/validação/endpoint intacto,
// mount-intacto (mesmo padrão de /carteiras). AUDIT-2026-07-11.
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import ConfigTaxas from './ConfigTaxas'
import CodTaxasEntrega from './CodTaxasEntrega'
import TpcConfiguracoes from './TpcConfiguracoes'
import MotoboyConfig from './MotoboyConfig'
import AffiliateRules from './AffiliateRules'
import CheckoutLinks from './CheckoutLinks'
import CodSaques from './CodSaques'
import CarteiraExpedicaoHub from './CarteiraExpedicaoHub'

// AUDIT-2026-07-14 — "Regras Afiliados" (antes sub-aba de Usuários→Afiliados),
// "Relatório Checkouts" (antes sub-aba de Usuários→Produtores), "Regras
// Globais de Saque + Repasse Produtor COD" (antes aba dentro de Saques) e
// "PIX Expedição" (antes item de menu próprio) migrados p/ cá — são
// config/relatório, não fila operacional nem gestão de pessoas.
type Secao = 'calculo' | 'cod' | 'expedicao' | 'motoboy' | 'regras-afiliados' | 'checkouts' | 'regras-saque' | 'pix-expedicao'

const SECOES: { key: Secao; label: string }[] = [
  { key: 'calculo',   label: 'Regra de Cálculo' },
  { key: 'cod',       label: 'Taxas COD' },
  { key: 'motoboy',   label: 'Config COD/Motoboy' },
  { key: 'expedicao', label: 'Config Expedição' },
  { key: 'regras-afiliados', label: 'Regras Afiliados' },
  { key: 'checkouts',        label: 'Relatório Checkouts' },
  { key: 'regras-saque',     label: 'Regras de Saque / Repasse Produtor' },
  { key: 'pix-expedicao',    label: 'PIX Expedição' },
]

export default function TaxasConfigPage() {
  const [params, setParams] = useSearchParams()
  const initial = (params.get('sec') as Secao) || 'calculo'
  const [sec, setSec] = useState<Secao>(
    SECOES.some(s => s.key === initial) ? initial : 'calculo',
  )

  function selectSec(key: Secao) {
    setSec(key)
    const next = new URLSearchParams(params)
    next.set('sec', key)
    setParams(next, { replace: true })
  }

  // Sem <h1> próprio — cada seção já traz o seu (ConfigTaxas/CodTaxasEntrega/
  // TpcConfiguracoes). Evita header duplicado.
  return (
    <div>
      <div className="szv2-tabs" style={{ marginBottom: 16 }}>
        {SECOES.map(s => (
          <button
            key={s.key}
            className="szv2-tab"
            aria-selected={sec === s.key}
            onClick={() => selectSec(s.key)}
          >
            {s.label}
          </button>
        ))}
      </div>

      {sec === 'calculo'   && <ConfigTaxas />}
      {sec === 'cod'       && <CodTaxasEntrega />}
      {sec === 'motoboy'   && <MotoboyConfig />}
      {sec === 'expedicao' && <TpcConfiguracoes />}
      {sec === 'regras-afiliados' && <AffiliateRules />}
      {sec === 'checkouts'        && <CheckoutLinks />}
      {sec === 'regras-saque'     && <CodSaques onlyRules />}
      {sec === 'pix-expedicao'    && <CarteiraExpedicaoHub />}
    </div>
  )
}
