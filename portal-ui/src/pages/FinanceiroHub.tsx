import { useOutletContext, useNavigate } from 'react-router-dom'
import Wallet from './Wallet'
import WalletExpedition from './WalletExpedition'
import LabelCredits from './LabelCredits'
import type { PortalMe } from '../components/Layout'

export type FinanceiroHubTab = 'carteira' | 'expedicao' | 'creditos'

type Props = {
  initialTab?: FinanceiroHubTab
}

// "Recarga de frete" e "Créditos de Etiquetas" são carteiras da conta ME (Melhor
// Envio) — mesma regra de PlataformaHub: só produtor com expedição ATIVA vê.
const ME_TABS: FinanceiroHubTab[] = ['expedicao', 'creditos']

const TAB_ROUTE: Record<FinanceiroHubTab, string> = {
  carteira: '/wallet',
  expedicao: '/wallet-expedition',
  creditos: '/label-credits',
}
const TAB_LABEL: Record<FinanceiroHubTab, string> = {
  carteira: 'Carteira',
  expedicao: 'Recarga de frete',
  creditos: 'Créditos de Etiquetas',
}

// O menu lateral só tem UMA entrada ("Carteira") apontando pra /wallet — as
// abas de expedição/créditos (rotas /wallet-expedition, /label-credits) não têm
// link nenhum no nav (bug: ficavam só alcançáveis digitando a URL na mão).
// A barra de abas AQUI DENTRO é o único jeito de chegar nelas — só aparece pro
// produtor com expedição ATIVA (mesmo gate do menu lateral, feito de novo aqui
// porque /portal/me não expõe shipping_class_id como sinal confiável isolado).
export default function FinanceiroHub({ initialTab = 'carteira' }: Props) {
  const { me } = useOutletContext<{ me: PortalMe | null }>() ?? { me: null }
  const navigate = useNavigate()
  const role = (me?.role || 'cliente').toLowerCase()
  const isProducer = role === 'produtor' || role === 'producer'
  const expFlag = me?.settings?.expedicao_ativa
  const expedicaoAtiva = isProducer && (expFlag === true || expFlag === 'true')

  const tab = ME_TABS.includes(initialTab) && !expedicaoAtiva ? 'carteira' : initialTab
  // Créditos de Etiquetas oculto (pedido do dono) — rota /label-credits some do
  // tab bar, mas o deep-link continua registrado em App.tsx (não quebra nada
  // que aponte pra lá, só não é mais alcançável navegando pela UI).
  const tabs: FinanceiroHubTab[] = expedicaoAtiva ? ['carteira', 'expedicao'] : ['carteira']

  return (
    <>
      {tabs.length > 1 && (
        <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
          {tabs.map(t => (
            <button
              key={t}
              type="button"
              className={`szv2-btn ${tab === t ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
              onClick={() => navigate(TAB_ROUTE[t])}
            >
              {TAB_LABEL[t]}
            </button>
          ))}
        </div>
      )}
      {tab === 'carteira' && <Wallet />}
      {tab === 'expedicao' && <WalletExpedition />}
      {tab === 'creditos' && <LabelCredits />}
    </>
  )
}
