// AUDIT-2026-06-19 — Busca global / command palette (Cmd-K / Ctrl-K).
//
// Com 5 hubs × até 11 abas + ~30 telas, achar "Conciliação"/"Custódia"/"Split"
// exige memória (Hick). A paleta indexa telas + abas de hub (como path?tab=) num
// array estático e navega via SPA. Mantida estática de propósito — não refatora
// os hubs para exportar TABS (duplicação pequena e barata vs acoplamento).
//
// Atalho: Cmd+K (mac) / Ctrl+K. Esc fecha. ↑/↓ navega, Enter abre.

import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

type Cmd = {
  label: string       // título exibido
  path: string        // destino SPA (pode conter ?tab=)
  group: string       // agrupamento p/ contexto
  keywords?: string   // termos extras p/ matching
}

// Índice estático de telas + abas de hub. Espelha App.tsx / *Hub.tsx.
const COMMANDS: Cmd[] = [
  // Topo
  { label: 'Dashboard',            path: '/',            group: 'Geral' },
  { label: 'Faturamento FALK',     path: '/faturamento', group: 'Financeiro', keywords: 'receita take meta revenue' },

  // Vendas
  { label: 'Usuários',             path: '/usuarios',    group: 'Vendas', keywords: 'afiliados produtores operadores admin' },
  { label: 'Produtos',             path: '/products',    group: 'Vendas' },
  { label: 'Estoque',              path: '/stock',       group: 'Vendas', keywords: 'inventário bipar entrada' },
  { label: 'Relatório de Checkouts', path: '/checkout-links', group: 'Vendas', keywords: 'ofertas links' },

  // Usuários — abas do hub
  { label: 'Afiliados',            path: '/usuarios?tab=afiliados',   group: 'Usuários' },
  { label: 'Produtores',           path: '/usuarios?tab=produtores',  group: 'Usuários' },
  { label: 'Operadores Logísticos',path: '/usuarios?tab=operadores',  group: 'Usuários', keywords: 'ol operator' },
  { label: 'Administradores',      path: '/usuarios?tab=admin',       group: 'Usuários' },

  // Pedidos / Operação
  { label: 'Pedidos',              path: '/orders',         group: 'Operação', keywords: 'motoboy entregas' },
  { label: 'Motoboys do Dia',      path: '/motoboys-dia',   group: 'Operação' },

  // Administração (Motoboy) — abas do hub
  { label: 'Dashboard Motoboy',    path: '/administracao?tab=dashboard',    group: 'Motoboy' },
  { label: 'Motoboys',             path: '/administracao?tab=motoboys',     group: 'Motoboy' },
  { label: 'Etiquetas / QR',       path: '/administracao?tab=etiquetas',    group: 'Motoboy', keywords: 'qrcode label' },
  { label: 'Ações em Lote (etiquetas)', path: '/administracao?tab=lote',    group: 'Motoboy', keywords: 'bulk' },
  { label: 'Comprovantes',         path: '/administracao?tab=comprovantes', group: 'Motoboy' },
  { label: 'Custódia',             path: '/administracao?tab=custodia',     group: 'Motoboy', keywords: 'estoque motoboy' },
  { label: 'Conciliação',          path: '/administracao?tab=conciliacao',  group: 'Motoboy', keywords: 'bancária' },
  { label: 'Fechamento',           path: '/administracao?tab=fechamento',   group: 'Motoboy' },
  { label: 'Mapa Ao Vivo',         path: '/administracao?tab=mapa',         group: 'Motoboy' },
  { label: 'Config COD/Motoboy',   path: '/taxas-config?sec=motoboy',       group: 'Taxas & Config' },

  // Carteiras — tela única com seletor de ator (AUDIT-2026-07-11)
  { label: 'Carteira de Afiliados',  path: '/carteiras?actor=afiliados',  group: 'Carteiras' },
  { label: 'Carteira de Produtores', path: '/carteiras?actor=produtores', group: 'Carteiras', keywords: 'cod' },
  { label: 'Carteira de Motoboys',   path: '/carteiras?actor=motoboys',   group: 'Carteiras' },
  { label: 'Carteira de Clientes',   path: '/carteiras?actor=clientes',   group: 'Carteiras', keywords: 'expedicao tpc' },

  { label: 'Saques COD',           path: '/saques?fila=cod',            group: 'Carteiras', keywords: 'withdraw aprovar pagar produtor afiliado' },
  { label: 'Saques Motoboy',       path: '/saques?fila=motoboy',        group: 'Carteiras' },

  // Carteira Expedição — abas do hub (transações, PIX, REST API)
  { label: 'Transações Expedição', path: '/carteira-expedicao?tab=transacoes',  group: 'Carteira Expedição' },
  { label: 'PIX / Recargas',       path: '/carteira-expedicao?tab=pix',         group: 'Carteira Expedição' },
  { label: 'REST API (Expedição)', path: '/carteira-expedicao?tab=restapi',     group: 'Carteira Expedição' },

  // Taxas & Config — tela única com seletor de seção (AUDIT-2026-07-11)
  { label: 'Regra de Cálculo',     path: '/taxas-config?sec=calculo',   group: 'Taxas & Config', keywords: 'regra calculo taxa produtor repasse motoboy afiliado take' },
  { label: 'Taxas COD',            path: '/taxas-config?sec=cod',       group: 'Taxas & Config' },
  { label: 'Config Expedição',     path: '/taxas-config?sec=expedicao', group: 'Taxas & Config', keywords: 'melhor envio pix jwt' },

  // Logística
  { label: 'Centros de Distribuição', path: '/cds',   group: 'Logística', keywords: 'cd' },
  { label: 'Zonas / CEPs',         path: '/zonas',    group: 'Logística' },

  // Expedição (Melhor Envio)
  { label: 'Etiquetas ME',         path: '/labels',                group: 'Expedição', keywords: 'melhor envio label' },
  { label: 'Markup / Integrações', path: '/expedicao-integracoes', group: 'Expedição' },
  { label: 'Webhooks Expedição',   path: '/expedicao-webhooks',    group: 'Expedição' },
  { label: 'Tracking Brand',       path: '/tracking-brand',        group: 'Expedição', keywords: 'rastreio marca' },

  // Afiliados — unificado na aba Afiliados de Usuários (sub-abas).
  { label: 'Regras de Afiliados',  path: '/usuarios?tab=afiliados&sub=regras',   group: 'Afiliados' },

  // Onboarding
  { label: 'Onboarding',           path: '/onboarding-requests',   group: 'Onboarding', keywords: 'cadastro aprovação solicitação' },
  { label: 'Trocas CPF/CNPJ',      path: '/document-changes',      group: 'Onboarding', keywords: 'documento cpf cnpj razão social troca aprovação' },

  // Sistema — abas do hub. AUDIT-2026-06-23: Auditoria/Alertas/Ferramentas/Log
  // unificadas em 1 aba com sub-abas internas (?sub=) — ver AuditoriaHub.
  { label: 'Auditoria Financeira', path: '/sistema?tab=auditoria&sub=auditoria',   group: 'Sistema', keywords: 'split divergência reconciliação' },
  { label: 'Alertas',              path: '/sistema?tab=auditoria&sub=alertas',     group: 'Sistema', keywords: 'auditoria diagnóstico financeiro' },
  { label: 'Ferramentas',          path: '/sistema?tab=auditoria&sub=ferramentas', group: 'Sistema', keywords: 'tools diagnóstico' },
  { label: 'Log de Auditoria',     path: '/sistema?tab=auditoria&sub=log',         group: 'Sistema' },
  { label: 'Manutenção',           path: '/sistema?tab=manutencao',    group: 'Sistema' },
  { label: 'Crons',                path: '/sistema?tab=crons',         group: 'Sistema' },
  { label: 'PWA Config',           path: '/sistema?tab=pwa',           group: 'Sistema' },
  { label: 'Templates PWA',        path: '/sistema?tab=templates-pwa', group: 'Sistema', keywords: 'notificação push' },
  { label: 'Push Técnico (VAPID)', path: '/sistema?tab=push-tecnico',  group: 'Sistema' },
  { label: 'Capabilities',         path: '/sistema?tab=capabilities',  group: 'Sistema', keywords: 'permissões' },
  { label: 'API Docs',             path: '/sistema?tab=api-docs',      group: 'Sistema' },
  { label: 'Logs do Sistema',      path: '/sistema?tab=logs',          group: 'Sistema' },
]

// Normaliza p/ matching insensível a acento/caixa.
function norm(s: string): string {
  return s.toLowerCase().normalize('NFD').replace(/[\u0300-\u036f]/g, '')
}

export default function CommandPalette() {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // Atalho global Cmd/Ctrl+K — abre/fecha. Esc fecha.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen(o => !o)
      } else if (e.key === 'Escape' && open) {
        setOpen(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  // Foco + reset ao abrir.
  useEffect(() => {
    if (open) {
      setQuery('')
      setActive(0)
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  const results = useMemo(() => {
    const q = norm(query.trim())
    if (!q) return COMMANDS
    const terms = q.split(/\s+/)
    return COMMANDS.filter(c => {
      const hay = norm(`${c.label} ${c.group} ${c.keywords ?? ''}`)
      return terms.every(t => hay.includes(t))
    })
  }, [query])

  // Mantém o índice ativo dentro dos limites quando os resultados mudam.
  useEffect(() => { setActive(0) }, [query])

  function go(path: string) {
    setOpen(false)
    navigate(path)
  }

  function onInputKey(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive(a => Math.min(a + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive(a => Math.max(a - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const r = results[active]
      if (r) go(r.path)
    }
  }

  // Mantém o item ativo visível no scroll.
  useEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [active, open])

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Busca global"
      onClick={e => { if (e.target === e.currentTarget) setOpen(false) }}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,.45)',
        zIndex: 99998,
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'center',
        paddingTop: '12vh',
      }}
    >
      <div
        style={{
          width: 'min(560px, 92vw)',
          maxHeight: '70vh',
          display: 'flex',
          flexDirection: 'column',
          background: 'var(--szv2-surface, #fff)',
          border: '1px solid var(--szv2-divider)',
          borderRadius: 14,
          boxShadow: '0 24px 64px rgba(0,0,0,.30)',
          overflow: 'hidden',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '14px 16px', borderBottom: '1px solid var(--szv2-divider)' }}>
          <span aria-hidden="true" style={{ color: 'var(--szv2-text-muted)', fontSize: 16 }}>🔎</span>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={onInputKey}
            placeholder="Buscar telas, abas, ações… (Cmd/Ctrl+K)"
            aria-label="Buscar"
            style={{
              flex: 1,
              border: 0,
              outline: 'none',
              background: 'transparent',
              font: 'inherit',
              fontSize: 15,
              color: 'var(--szv2-text)',
            }}
          />
          <kbd style={{ fontSize: 11, color: 'var(--szv2-text-muted)', border: '1px solid var(--szv2-border)', borderRadius: 6, padding: '2px 6px' }}>esc</kbd>
        </div>

        <div ref={listRef} style={{ overflowY: 'auto', padding: 6 }}>
          {results.length === 0 ? (
            <div style={{ padding: '24px 16px', textAlign: 'center', color: 'var(--szv2-text-muted)', fontSize: 14 }}>
              Nada encontrado para “{query}”.
            </div>
          ) : (
            results.map((r, i) => (
              <button
                key={r.path}
                type="button"
                data-idx={i}
                onMouseEnter={() => setActive(i)}
                onClick={() => go(r.path)}
                style={{
                  width: '100%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  padding: '10px 12px',
                  borderRadius: 8,
                  border: 0,
                  cursor: 'pointer',
                  textAlign: 'left',
                  font: 'inherit',
                  background: i === active ? 'var(--szv2-brand-light, rgba(30,111,242,.10))' : 'transparent',
                  color: 'var(--szv2-text)',
                }}
              >
                <span style={{ fontWeight: 600, fontSize: 14 }}>{r.label}</span>
                <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>{r.group}</span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
