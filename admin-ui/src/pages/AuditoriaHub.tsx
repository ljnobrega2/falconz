// Hub Auditoria & Diagnóstico — UNIFICA 3 (4) telas que antes eram abas soltas
// no SistemaHub: "Alertas" + "Auditoria" (financeira) + "Ferramentas" + "Log
// Auditoria". Regra do dono (#73): "unifique [Ferramentas] ao menu Auditoria e
// o Alertas também — 3 submenus em um só, de forma prática, menus clicáveis sem
// estar tudo exposto". Resultado: 1 aba só em Sistema → ao abrir mostra sub-abas
// internas (szv2-tabs) ao invés de 3 abas no topo.
//
// Sub-navegação sincronizada com ?sub= (mesma convenção do UsuariosHub). Default
// = 'auditoria' (AuditEngine), preservando o deep-link /audit → /sistema?tab=
// auditoria que historicamente mostrava o motor de auditoria financeira.
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import AlertasAuditoria from './AlertasAuditoria'
import AuditEngine from './AuditEngine'
import Tools from './Tools'
import AuditLogViewer from './AuditLogViewer'

type Sub = 'alertas' | 'auditoria' | 'ferramentas' | 'log'

const SUBS: { key: Sub; label: string }[] = [
  { key: 'alertas',     label: 'Alertas' },
  { key: 'auditoria',   label: 'Auditoria' },
  { key: 'ferramentas', label: 'Ferramentas' },
  { key: 'log',         label: 'Log Auditoria' },
]

const subTabsStyle = {
  display: 'flex',
  gap: 4,
  marginBottom: 16,
  borderBottom: '1px solid var(--szv2-divider)',
  overflowX: 'auto' as const,
}

export default function AuditoriaHub() {
  const [params, setParams] = useSearchParams()
  const initialSub = (params.get('sub') as Sub) || 'auditoria'
  const [sub, setSub] = useState<Sub>(
    SUBS.some(s => s.key === initialSub) ? initialSub : 'auditoria',
  )

  // Sincroniza com ?sub= em navegação externa (Cmd-K / deep-link). selectSub
  // re-dispara setParams → setSub(mesmo) = no-op, sem loop.
  useEffect(() => {
    const s = params.get('sub')
    if (s && SUBS.some(x => x.key === s)) setSub(s as Sub)
  }, [params])

  function selectSub(key: Sub) {
    setSub(key)
    const next = new URLSearchParams(params)
    next.set('tab', 'auditoria')
    next.set('sub', key)
    setParams(next, { replace: true })
  }

  return (
    <div>
      <div className="szv2-tabs" role="tablist" style={subTabsStyle}>
        {SUBS.map(s => (
          <button
            key={s.key}
            type="button"
            role="tab"
            className="szv2-tab"
            aria-selected={sub === s.key}
            onClick={() => selectSub(s.key)}
          >
            {s.label}
          </button>
        ))}
      </div>

      {sub === 'alertas'     && <AlertasAuditoria />}
      {sub === 'auditoria'   && <AuditEngine />}
      {sub === 'ferramentas' && <Tools />}
      {sub === 'log'         && <AuditLogViewer />}
    </div>
  )
}
