import { useOutletContext } from 'react-router-dom'
import Webhooks from './Webhooks'
import Integrations from './Integrations'
import Freight from './Freight'
import Localidades from './Localidades'
import Users from './Users'
import Settings from './Settings'
import type { PortalMe } from '../components/Layout'

export type PlataformaHubTab = 'webhooks' | 'integracoes' | 'freight' | 'localidades' | 'users' | 'settings'

type Props = {
  initialTab?: PlataformaHubTab
}

// Integrações/Frete são telas de EXPEDIÇÃO (Melhor Envio) — mesma regra do menu
// lateral (Layout.tsx buildNavGroups): só produtor com expedição ATIVA
// (settings.expedicao_ativa, ligado pelo admin em Produtores) enxerga qualquer
// coisa relacionada ao ME. Cliente/afiliado/operador NUNCA veem essas abas,
// mesmo entrando direto pela URL (/integrations, /freight).
const ME_TABS: PlataformaHubTab[] = ['integracoes', 'freight']

// Sem barra de abas / header próprio aqui — a sidebar já navega entre Webhooks,
// Localidades, Usuários e Perfil; duplicar isso em cima da página é ruído (pedido
// do dono). Cada rota (/webhooks, /localidades, /users, /settings) renderiza SÓ
// a página correspondente; só resta o gate de permissão pras telas de ME.
export default function PlataformaHub({ initialTab = 'webhooks' }: Props) {
  const { me } = useOutletContext<{ me: PortalMe | null }>() ?? { me: null }
  const role = (me?.role || 'cliente').toLowerCase()
  const isProducer = role === 'produtor' || role === 'producer'
  // Fonte única = settings.expedicao_ativa (toggle do admin em Produtores). Sem
  // fallback de shipping_class_id (legado WP) — FALK não depende mais dele.
  // Duplicava (e divergia d)o cálculo de Layout.tsx buildNavGroups, causando o
  // bug: menu mostrava Integrações/Frete mas a rota caía sempre em Webhooks.
  const expFlag = me?.settings?.expedicao_ativa
  const expedicaoAtiva = isProducer && (expFlag === true || expFlag === 'true')

  const tab = ME_TABS.includes(initialTab) && !expedicaoAtiva ? 'webhooks' : initialTab

  return (
    <>
      {tab === 'webhooks' && <Webhooks />}
      {tab === 'integracoes' && <Integrations />}
      {tab === 'freight' && <Freight />}
      {tab === 'localidades' && <Localidades />}
      {tab === 'users' && <Users />}
      {tab === 'settings' && <Settings />}
    </>
  )
}
