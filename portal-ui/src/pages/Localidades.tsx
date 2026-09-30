// Localidades (Áreas de operação) — porte fiel de templates/portal/v2/sections/localidades.php.
// Mostra as regiões (CDs) e as zonas/cidades atendidas. Ligada ao go/portal (GET /portal/localidades).
// Dataset = conjunto global de CDs ativos (configurados pela Senderzz), gated por sessão do portal.
// Visível para todas as roles (produtor | afiliado | operator). Sem busca (descontinuada no WP).
import { useEffect, useState } from 'react'
import type { CSSProperties } from 'react'
import { api } from '../api'
import EmptyState from '../components/EmptyState'
import Drawer from '../components/Drawer'

// Azul da marca FALK — usado nos acentos do drawer de agenda (#17).
const FALK_BLUE = '#1E6FF2'

type Zona = {
  id: number
  nome: string
  ativo: boolean
  cutoff: boolean
  // Dados crus expostos pela API (go/portal/localidades.go) p/ a agenda real (#17):
  //   dias_funcionamento: índices CSV "1,2,3,4,5,6" (0=Dom..6=Sáb); vazio = padrão Seg-Sáb.
  //   cutoff_horarios: ou JSON {"0":"21:00",...} ou um horário único "21:00" (formato legado).
  // Opcionais por compatibilidade: respostas antigas podem não trazê-los.
  dias_funcionamento?: string
  cutoff_horarios?: string
}

type Localidade = {
  id: number
  nome: string
  cidade: string
  uf: string
  ativo: boolean
  num_zonas: number
  zonas: Zona[]
}

type ListResp = { ok: boolean; data: Localidade[]; total: number }

// Contexto da cidade clicada — alimenta o drawer de agenda (#17).
// Carrega a zona (cidade) + o CD (região) ao qual ela pertence, pois a agenda
// de atendimento/fechamento é exibida no contexto cidade ↔ CD.
type AgendaSel = { cd: Localidade; zona: Zona } | null

const SHOWN = 4 // primeiros 4 visíveis, restante oculto

export default function Localidades() {
  const [cds, setCds] = useState<Localidade[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [selectedId, setSelectedId] = useState<number>(0)
  // Estado de expansão por CD (mantém igual ao WP: cada painel guarda seu próprio toggle).
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  // Drawer lateral de agenda da cidade (#17) — cidade clicada + seu CD.
  const [agenda, setAgenda] = useState<AgendaSel>(null)

  useEffect(() => {
    setLoading(true)
    api<ListResp>('/portal/localidades')
      .then(r => {
        // Resiliência: backend pode devolver 200 com data ausente/null/objeto.
        // Só aceitamos array; qualquer outra coisa cai em [] → empty-state.
        const raw = Array.isArray(r?.data) ? r.data : []
        // Remove a zona sentinela "(Sem zona)" (id 0) — placeholder do sistema p/ cidades
        // sem zona real (admin usa zona_id=0 como fallback). Não é cidade atendida → não
        // entra na cobertura do portal nem na contagem. Filtra no cliente (NÃO apagar a
        // linha: ela é usada pelo admin/pedidos).
        const list = raw.map(cd => {
          const zonas = (Array.isArray(cd?.zonas) ? cd.zonas : []).filter(z => {
            const nm = (z?.nome ?? '').trim()
            return nm !== '' && nm.toLowerCase() !== '(sem zona)'
          })
          return { ...cd, zonas, num_zonas: zonas.length }
        })
        setCds(list)
        setSelectedId(list.length > 0 ? list[0]?.id ?? 0 : 0)
      })
      .catch(e => setErr((e && e.message) || 'Erro ao carregar localidades'))
      .finally(() => setLoading(false))
  }, [])

  function selectCd(id: number) {
    setSelectedId(id)
  }

  function toggleCities(id: number) {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // Abre o drawer de agenda para a cidade (zona) clicada, no contexto do seu CD.
  function openAgenda(cd: Localidade, zona: Zona) {
    setAgenda({ cd, zona })
  }

  return (
    <section id="sec-localidades" className="sz-sec" data-szv2-label="Localidades">
      <style>{LO_CSS}</style>

      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>Áreas de operação</h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>Regiões e cidades onde a FALK entrega. Clique numa cidade para ver dias e horário de corte.</p>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading ? (
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      ) : err ? (
        // Erro já exibido no alerta acima — não duplicar com empty-state.
        null
      ) : cds.length === 0 ? (
        <EmptyState
          icon="📍"
          title="Nenhuma área configurada"
          description="As regiões de entrega aparecem aqui assim que a FALK ativar sua operação."
        />
      ) : (
        // Layout: lista lateral + detalhe
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '300px 1fr',
            gap: 'var(--szv2-space-4)',
            alignItems: 'flex-start',
          }}
        >
          {/* Lista de regiões (CDs) */}
          <div
            id="szv2-lo-list"
            style={{ display: 'flex', flexDirection: 'column', gap: 'var(--szv2-space-2)' }}
          >
            {cds.map((cd, cdIdx) => {
              const nZonas = Array.isArray(cd?.zonas) ? cd.zonas.length : 0
              const isActive = cd?.id === selectedId
              return (
                <button
                  key={cd?.id ?? cdIdx}
                  type="button"
                  className={'szv2-lo-item' + (isActive ? ' szv2-lo-item--active' : '')}
                  data-lo-id={String(cd?.id ?? '')}
                  onClick={() => selectCd(cd?.id ?? 0)}
                >
                  <strong className="szv2-lo-item-name">{cd?.nome ?? ''}</strong>
                  <div style={{ display: 'flex', gap: 12, marginTop: 3 }}>
                    <span className="szv2-lo-item-meta">
                      <svg
                        viewBox="0 0 20 20"
                        style={{
                          width: 11,
                          height: 11,
                          fill: 'currentColor',
                          verticalAlign: 'middle',
                          marginRight: 2,
                        }}
                        aria-hidden="true"
                      >
                        <path d="M3 3h6v6H3zM11 3h6v6h-6zM3 11h6v6H3zM11 11h6v6h-6z" />
                      </svg>
                      1 unidade
                    </span>
                    {nZonas > 0 && (
                      <span className="szv2-lo-item-meta">
                        <svg
                          viewBox="0 0 20 20"
                          style={{
                            width: 11,
                            height: 11,
                            fill: 'currentColor',
                            verticalAlign: 'middle',
                            marginRight: 2,
                          }}
                          aria-hidden="true"
                        >
                          <path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5z" />
                        </svg>
                        {nZonas} {nZonas !== 1 ? 'cidades' : 'cidade'}
                      </span>
                    )}
                  </div>
                </button>
              )
            })}
          </div>

          {/* Painel de detalhe */}
          <div id="szv2-lo-detail">
            {cds.map((cd, cdIdx) => {
              const zonas = Array.isArray(cd?.zonas) ? cd.zonas : []
              const n = zonas.length
              const isOpen = cd?.id === selectedId
              const isExpanded = expanded.has(cd?.id ?? -1)
              return (
                <div
                  key={cd?.id ?? cdIdx}
                  className="szv2-card szv2-lo-panel"
                  id={'szv2-lo-panel-' + (cd?.id ?? '')}
                  style={{ display: isOpen ? undefined : 'none' }}
                >
                  {/* Header da região */}
                  <div
                    style={{
                      marginBottom: 'var(--szv2-space-5)',
                      paddingBottom: 'var(--szv2-space-4)',
                      borderBottom: '1px solid var(--szv2-divider)',
                    }}
                  >
                    <h2
                      style={{
                        fontSize: 'var(--szv2-text-xl, 18px)',
                        margin: '0 0 2px',
                        fontWeight: 700,
                      }}
                    >
                      {cd?.nome ?? ''}
                    </h2>
                    {/* Subtítulo UF só quando difere do nome — evita "SP" duplicado
                        (CD nome='SP' + uf='sp' renderizava SP duas vezes). */}
                    {!!cd?.uf && (cd.uf ?? '').trim().toUpperCase() !== (cd.nome ?? '').trim().toUpperCase() && (
                      <div
                        style={{
                          fontSize: 13,
                          color: 'var(--szv2-text-muted)',
                          fontWeight: 500,
                        }}
                      >
                        {cd?.uf ?? ''}
                      </div>
                    )}
                  </div>

                  {n > 0 && (
                    <>
                      {/* Cobertura de entrega */}
                      <h3
                        style={{
                          fontSize: 14,
                          fontWeight: 700,
                          color: 'var(--szv2-text)',
                          margin: '0 0 var(--szv2-space-3)',
                        }}
                      >
                        Cobertura de entrega ({n})
                      </h3>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: '1fr 1fr',
                          gap: 0,
                          marginBottom: 'var(--szv2-space-5)',
                        }}
                      >
                        {zonas.map((zona, idx) => {
                          const ativo = !!zona?.ativo
                          const hidden = idx >= SHOWN && !isExpanded
                          return (
                            // #17: cidade clicável → abre drawer lateral com a agenda
                            // (dias de atendimento + fechamento). role=button + teclado
                            // (Enter/Espaço) para acessibilidade; reaproveita o markup da
                            // lista existente (mantém ícone de pin e relógio de cutoff).
                            <div
                              key={zona?.id ?? idx}
                              className={'szv2-lo-city szv2-lo-city--clickable' + (hidden ? ' szv2-lo-city--hidden' : '')}
                              role="button"
                              tabIndex={0}
                              aria-label={`Ver agenda de ${zona?.nome ?? 'cidade'}`}
                              onClick={() => openAgenda(cd, zona)}
                              onKeyDown={e => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                  e.preventDefault()
                                  openAgenda(cd, zona)
                                }
                              }}
                              style={{
                                alignItems: 'center',
                                gap: 6,
                                fontSize: 13,
                                padding: '6px 0',
                                borderBottom: '1px solid var(--szv2-divider)',
                                ...(!ativo ? { opacity: 0.5 } : {}),
                              }}
                            >
                              <svg
                                viewBox="0 0 20 20"
                                style={{
                                  width: 12,
                                  height: 12,
                                  fill: 'var(--szv2-text-muted)',
                                  flexShrink: 0,
                                }}
                                aria-hidden="true"
                              >
                                <path d="M10 2a5 5 0 0 0-5 5c0 3.5 5 11 5 11s5-7.5 5-11a5 5 0 0 0-5-5z" />
                              </svg>
                              {zona?.nome ?? ''}
                              {!!zona?.cutoff && (
                                <svg
                                  viewBox="0 0 20 20"
                                  style={{
                                    width: 12,
                                    height: 12,
                                    fill: 'var(--szv2-text-faint)',
                                    marginLeft: 'auto',
                                  }}
                                  aria-label="Horário de corte"
                                >
                                  <title>Horário de corte configurado</title>
                                  <path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z" />
                                </svg>
                              )}
                              {/* Chevron sutil indicando que a linha é clicável (#17). */}
                              <svg
                                viewBox="0 0 20 20"
                                style={{
                                  width: 12,
                                  height: 12,
                                  fill: 'var(--szv2-text-faint)',
                                  marginLeft: zona?.cutoff ? 4 : 'auto',
                                  flexShrink: 0,
                                }}
                                aria-hidden="true"
                              >
                                <path d="M7 4l6 6-6 6V4z" />
                              </svg>
                            </div>
                          )
                        })}
                      </div>
                      {n > SHOWN && (
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary"
                          style={{ marginBottom: 'var(--szv2-space-4)' }}
                          onClick={() => toggleCities(cd?.id ?? 0)}
                        >
                          {isExpanded ? (
                            <svg
                              viewBox="0 0 20 20"
                              style={{
                                width: 14,
                                height: 14,
                                fill: 'currentColor',
                                marginRight: 4,
                              }}
                              aria-hidden="true"
                            >
                              <path d="M10 8l6 6H4z" />
                            </svg>
                          ) : (
                            <svg
                              viewBox="0 0 20 20"
                              style={{
                                width: 14,
                                height: 14,
                                fill: 'currentColor',
                                marginRight: 4,
                              }}
                              aria-hidden="true"
                            >
                              <path d="M10 12L4 6h12z" />
                            </svg>
                          )}
                          {isExpanded ? 'Mostrar menos' : `Ver todas (${n} cidades)`}
                        </button>
                      )}
                    </>
                  )}

                  {/* Endereço da unidade removido (não exibir ao usuário) */}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* #17 — Drawer lateral direito com a agenda da cidade clicada.
          Reaproveita components/Drawer.tsx (sem editá-lo). */}
      <Drawer
        open={!!agenda}
        onClose={() => setAgenda(null)}
        width={420}
        ariaLabel="Agenda de atendimento da cidade"
        title={
          agenda ? (
            <span>
              {agenda.zona?.nome ?? 'Cidade'}
              <span style={{ color: 'var(--szv2-text-muted)', fontWeight: 500 }}>
                {' '}
                · {agenda.cd?.nome ?? ''}
                {agenda.cd?.uf ? ` (${agenda.cd.uf})` : ''}
              </span>
            </span>
          ) : (
            'Agenda'
          )
        }
      >
        {agenda && <AgendaBody cd={agenda.cd} zona={agenda.zona} />}
      </Drawer>
    </section>
  )
}

// ── Parsers da agenda (#17) — espelham includes/motoboy/router.php ─────────────
// A API GET /portal/localidades hoje serializa, por zona, `dias_funcionamento`
// (CSV "1,2,3,4,5,6", 0=Dom..6=Sáb) e `cutoff_horarios` (JSON {"0":"21:00",...}
// OU horário único "21:00" no formato legado). Decodificamos de forma tolerante,
// igual ao PHP (sz_motoboy_zone_days_array / sz_motoboy_zone_single_cutoff_time):
// valores inválidos/ausentes nunca quebram o drawer.

// Índices de dias ativos (0=Dom..6=Sáb). Vazio/ausente → padrão Seg-Sáb (1..6),
// mesmo default do banco (database.php). Espelha sz_motoboy_zone_days_array.
function parseDiasAtivos(dias?: string): Set<number> {
  const raw = (dias ?? '').trim()
  if (!raw) return new Set([1, 2, 3, 4, 5, 6])
  const out = new Set<number>()
  for (const part of raw.split(',')) {
    const n = parseInt(part.trim(), 10)
    if (Number.isInteger(n) && n >= 0 && n <= 6) out.add(n)
  }
  return out.size > 0 ? out : new Set([1, 2, 3, 4, 5, 6])
}

// Valida e normaliza um horário "HH:MM"; retorna '' se inválido.
function normHora(v: unknown): string {
  const m = /^(\d{1,2}):(\d{2})$/.exec(String(v ?? '').trim())
  if (!m) return ''
  const h = Math.max(0, Math.min(23, parseInt(m[1], 10)))
  const i = Math.max(0, Math.min(59, parseInt(m[2], 10)))
  return `${String(h).padStart(2, '0')}:${String(i).padStart(2, '0')}`
}

// Horário-limite único exibido para a zona, igual a sz_motoboy_zone_single_cutoff_time:
// usa o primeiro dia ativo como referência; aceita JSON-array, JSON-map OU string
// única (legado). Retorna '' quando nada válido — chamador decide o fallback.
// try/catch p/ não derrubar o drawer com JSON malformado (regra 6).
//
// #81: o banco grava cutoff_horarios como ARRAY indexado por dia (0=Dom..6=Sáb),
// ex.: ["21:00",...,"12:00",...] (ver sz_motoboy_sanitize_zone_cutoffs no PHP).
// Antes, arrays eram rejeitados → cutoffHora ficava '' e o drawer só mostrava
// "Horário de corte configurado" sem o horário real. Agora normalizamos o array
// em um map "0".."6" e reaproveitamos a seleção por primeiro-dia-ativo, idêntica
// ao sz_motoboy_zone_single_cutoff_time (que indexa $arr[(string)$d]).
function parseCutoffHora(cutoffs: string | undefined, diasAtivos: Set<number>): string {
  const raw = (cutoffs ?? '').trim()
  if (!raw) return ''
  let map: Record<string, unknown> | null = null
  try {
    const decoded = JSON.parse(raw)
    if (Array.isArray(decoded)) {
      // Array indexado por dia (0=Dom..6=Sáb) → map "0".."6" p/ a seleção abaixo.
      const m: Record<string, unknown> = {}
      decoded.forEach((v, i) => {
        m[String(i)] = v
      })
      map = m
    } else if (decoded && typeof decoded === 'object') {
      map = decoded as Record<string, unknown>
    }
  } catch {
    map = null // não é JSON → trata como horário único abaixo
  }
  if (map) {
    for (const d of diasAtivos) {
      const hit = normHora(map[String(d)])
      if (hit) return hit
    }
    for (const v of Object.values(map)) {
      const hit = normHora(v)
      if (hit) return hit
    }
    return ''
  }
  // String simples (formato legado "21:00").
  return normHora(raw)
}

// Índice 0..6 → rótulo curto (espelha sz_motoboy_zone_days_label).
const DIA_NOMES = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb']

// ── Corpo do drawer de agenda (#17) ───────────────────────────────────────────
// Exibe dados REAIS vindos da API:
//   • Status da zona (booleano `ativo`).
//   • Dias de atendimento: chips destacados conforme `dias_funcionamento`.
//   • Fechamento de agenda: horário-limite único derivado de `cutoff_horarios`.
function AgendaBody({ cd, zona }: { cd: Localidade; zona: Zona }) {
  const temFechamento = !!zona?.cutoff
  const zonaAtiva = !!zona?.ativo
  const diasAtivos = parseDiasAtivos(zona?.dias_funcionamento)
  const cutoffHora = temFechamento ? parseCutoffHora(zona?.cutoff_horarios, diasAtivos) : ''

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      {/* Status da zona (dado real) */}
      <div
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 8,
          alignSelf: 'flex-start',
          padding: '4px 10px',
          borderRadius: 999,
          fontSize: 12,
          fontWeight: 600,
          background: zonaAtiva ? `${FALK_BLUE}1A` : 'var(--szv2-surface-alt)',
          color: zonaAtiva ? FALK_BLUE : 'var(--szv2-text-muted)',
          border: `1px solid ${zonaAtiva ? FALK_BLUE : 'var(--szv2-border)'}`,
        }}
      >
        <span
          style={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            background: zonaAtiva ? FALK_BLUE : 'var(--szv2-text-faint)',
          }}
        />
        {zonaAtiva ? 'Cidade atendida' : 'Cidade inativa'}
      </div>

      {/* Dias de atendimento — dado real (dias_funcionamento). */}
      <section>
        <h4 style={agendaH4}>Dias de atendimento</h4>
        <DiasChips ativos={diasAtivos} />
        <p style={agendaNote}>
          Dias destacados em azul indicam atendimento nesta cidade.
        </p>
      </section>

      {/* Fechamento de agenda (horário de corte) */}
      <section>
        <h4 style={agendaH4}>Fechamento de agenda</h4>
        {temFechamento ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              fontSize: 13,
              color: 'var(--szv2-text)',
            }}
          >
            <svg viewBox="0 0 20 20" style={{ width: 16, height: 16, fill: FALK_BLUE }} aria-hidden="true">
              <path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z" />
            </svg>
            <span style={{ fontWeight: 600 }}>
              {cutoffHora ? `Limite até ${cutoffHora} do dia anterior` : 'Horário de corte configurado'}
            </span>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--szv2-text-muted)' }}>
            <svg viewBox="0 0 20 20" style={{ width: 16, height: 16, fill: 'var(--szv2-text-faint)' }} aria-hidden="true">
              <path d="M10 2a8 8 0 1 0 0 16A8 8 0 0 0 10 2zm0 3v5l3 2-1 1.7L9 11V5h1z" />
            </svg>
            <span>Sem horário de corte configurado</span>
          </div>
        )}
        <p style={agendaNote}>
          {temFechamento
            ? cutoffHora
              ? `Pedidos para um dia de entrega devem ser feitos até ${cutoffHora} do dia anterior.`
              : 'Esta cidade possui um horário-limite para pedidos do dia.'
            : 'Nenhum horário-limite cadastrado para esta cidade.'}
        </p>
      </section>

      {/* Rodapé de contexto */}
      <div
        style={{
          marginTop: 4,
          paddingTop: 12,
          borderTop: '1px solid var(--szv2-divider)',
          fontSize: 12,
          color: 'var(--szv2-text-muted)',
        }}
      >
        Região: <strong style={{ color: 'var(--szv2-text)' }}>{cd?.nome ?? '—'}</strong>
        {cd?.uf ? ` · ${cd.uf}` : ''}
      </div>
    </div>
  )
}

// Chips dos 7 dias da semana — dias atendidos (índice em `ativos`) destacados em
// azul FALK; demais ficam esmaecidos. Dado real vindo de `dias_funcionamento`.
function DiasChips({ ativos }: { ativos: Set<number> }) {
  return (
    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
      {DIA_NOMES.map((d, i) => {
        const on = ativos.has(i)
        return (
          <span
            key={d}
            title={on ? `Atendido (${d})` : `Sem atendimento (${d})`}
            style={{
              minWidth: 38,
              textAlign: 'center',
              padding: '5px 8px',
              borderRadius: 8,
              fontSize: 12,
              fontWeight: 600,
              color: on ? FALK_BLUE : 'var(--szv2-text-faint)',
              background: on ? `${FALK_BLUE}1A` : 'var(--szv2-surface-alt)',
              border: `1px ${on ? 'solid' : 'dashed'} ${on ? FALK_BLUE : 'var(--szv2-border)'}`,
            }}
          >
            {d}
          </span>
        )
      })}
    </div>
  )
}

const agendaH4: CSSProperties = {
  margin: '0 0 8px',
  fontSize: 13,
  fontWeight: 700,
  color: 'var(--szv2-text)',
}

const agendaNote: CSSProperties = {
  margin: '8px 0 0',
  fontSize: 11.5,
  lineHeight: 1.5,
  color: 'var(--szv2-text-faint)',
}

// CSS específico de Localidades — copiado verbatim do <style> de localidades.php.
// Não existe em portal-ui/src/styles; injetado aqui (só este arquivo é meu).
const LO_CSS = `
.sz-dashboard-v2 .szv2-lo-item {
    display: block; width: 100%; text-align: left;
    padding: 12px 14px;
    border: 1px solid var(--szv2-border);
    border-radius: var(--szv2-radius-md);
    background: var(--szv2-surface);
    cursor: pointer;
    transition: border-color 0.1s, background 0.1s;
}
.sz-dashboard-v2 .szv2-lo-item:hover { background: var(--szv2-surface-alt); border-color: var(--szv2-brand); }
.sz-dashboard-v2 .szv2-lo-item--active {
    border: 2px solid var(--szv2-brand);
    background: var(--szv2-surface);
}
.sz-dashboard-v2 .szv2-lo-item-name { font-size: 13px; font-weight: 600; color: var(--szv2-text); }
.sz-dashboard-v2 .szv2-lo-item--active .szv2-lo-item-name { color: var(--szv2-brand); }
.sz-dashboard-v2 .szv2-lo-item-meta { font-size: 12px; color: var(--szv2-text-muted); }
.sz-dashboard-v2 .szv2-lo-city { display: flex; }
.sz-dashboard-v2 .szv2-lo-city--hidden { display: none; }
/* #17 — cidade clicável abre o drawer de agenda. Hover azul FALK (#1E6FF2). */
.sz-dashboard-v2 .szv2-lo-city--clickable {
    cursor: pointer;
    margin: 0 -8px;
    padding-left: 8px !important;
    padding-right: 8px !important;
    border-radius: 8px;
    transition: background 0.1s, color 0.1s;
}
.sz-dashboard-v2 .szv2-lo-city--clickable:hover {
    background: rgba(30, 111, 242, 0.08);
    color: #1E6FF2;
}
.sz-dashboard-v2 .szv2-lo-city--clickable:focus-visible {
    outline: 2px solid #1E6FF2;
    outline-offset: -2px;
}
`
