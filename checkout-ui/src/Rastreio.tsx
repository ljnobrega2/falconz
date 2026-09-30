// =============================================================================
// RASTREIO — pagina publica de acompanhamento do pedido (cliente final, SEM auth).
//
// Rota: /rastreio/:code  (e alias /pedido/:code) — basename "/checkout".
// Consome GET /checkout-api/order/{code} (api.ts -> fetchTracking).
//
// Layout (mobile-first, vira 2 colunas no desktop):
//   - Titulo "Detalhes do pedido #<code>"
//   - Card "Resumo do pedido": banner de status + STEPPER horizontal
//     (Agendado -> Em Separacao -> Separado -> Em Rota -> A Caminho -> Completo)
//   - Card "Itens do pedido"
//   - Coluna direita: "Dados do cliente" + "Suporte"
//
// MARCA FALKZ: accent AZUL (--falkz-brand). Verde so no que ja existe.
//
// REGRA DO STEPPER (critica — ver nota em api.ts/tracking.go): a etapa atingida
// vem do `status` de topo, NAO de `at != null`. `em_separacao` e sempre null por
// design; `a_caminho` pode ser null mesmo entregue. Mostramos a data sob a etapa
// quando `at` existir, senao "Sem previsao".
// =============================================================================

import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  fetchTracking,
  CheckoutApiError,
  type TrackingResponse,
  type TrackingTimelineStep,
  type OfferBrand,
} from './api'

// AUDIT-2026-07-30 #5 (dono: "essa página de rastreio precisa ser de expedição
// - não mexer com coisa de COD... você tá confundindo COD com PAD"): motoboy/COD
// e expedição (ME) são fluxos DIFERENTES com vocabulário DIFERENTE — cada um
// usa o SEU ladder, escolhido por `entrega.tipo` (ver tracking.go, mesma
// separação motoboyLadder/expedicaoLadder).

// Ladder motoboy/COD.
const MOTOBOY_LADDER_KEYS = [
  'agendado',
  'em_separacao',
  'separado',
  'em_rota',
  'a_caminho',
  'completo',
] as const
const MOTOBOY_LADDER_LABELS: Record<string, string> = {
  agendado: 'Agendado',
  em_separacao: 'Em Separação',
  separado: 'Separado',
  em_rota: 'Em Rota',
  a_caminho: 'A Caminho',
  completo: 'Completo',
}

// Ladder expedição (ME) — MESMO vocabulário do admin (ExpedicaoOrders.tsx
// FILTER_GROUPS): pendente → em andamento → aprovado → separado → enviado →
// entregue. Nenhuma etapa de motoboy (agendado/em rota/a caminho) aqui.
const EXPEDICAO_LADDER_KEYS = [
  'pendente',
  'em_andamento',
  'aprovado',
  'separado',
  'enviado',
  'a_caminho',
  'entregue',
] as const
const EXPEDICAO_LADDER_LABELS: Record<string, string> = {
  pendente: 'Pendente',
  em_andamento: 'Em Andamento',
  aprovado: 'Aprovado',
  separado: 'Separado',
  enviado: 'Enviado',
  a_caminho: 'A Caminho',
  entregue: 'Entregue',
}

// Descrições por etapa — mesmo tom do painel de referência ("A etiqueta de
// envio já foi criada e o pacote está pronto para a coleta ou postagem"),
// nunca nomeando a plataforma intermediária.
const EVENTO_DESCRICOES: Record<string, string> = {
  pendente: 'Seu pedido foi recebido e confirmado.',
  em_andamento: 'Seu pedido está sendo processado.',
  aprovado: 'Pedido aprovado — estamos preparando pra envio.',
  separado: 'A etiqueta de envio já foi criada e o pacote está pronto para a coleta ou postagem.',
  enviado: 'O pacote foi postado e está a caminho do destino.',
  a_caminho: 'O pacote saiu para entrega e está a caminho do endereço.',
  entregue: 'Entrega concluída com sucesso.',
  agendado: 'Entrega agendada.',
  em_separacao: 'Pedido em separação.',
  completo: 'Entrega concluída com sucesso.',
}

/** RFC3339 -> {dia:"29 jul", hora:"10:22"} pro card de evento. */
function formatEventoData(at: string): { dia: string; hora: string } {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return { dia: '', hora: '' }
  const MESES = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez']
  const dia = `${d.getDate()} ${MESES[d.getMonth()]}`
  const hora = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return { dia, hora }
}

function limparMensagemRastreio(texto: string): string {
  return texto
    .replace(/\s+e já está com\s+(?:a |o )?(?:jadlog|melhor envio|correios|loggi)\.?$/i, '.')
    .replace(/\bjadlog\b/gi, '')
    .replace(/\bmelhor envio\b/gi, '')
    .replace(/\bcorreios\b/gi, '')
    .replace(/\bloggi\b/gi, '')
    .replace(/\s{2,}/g, ' ')
    .replace(/\s+([.,;:])/g, '$1')
    .trim()
}

// Status terminais fora do ladder (so aparecem no banner).
const TERMINAIS: Record<string, { titulo: string; tom: 'err' }> = {
  frustrado: { titulo: 'Entrega frustrada', tom: 'err' },
  cancelado: { titulo: 'Pedido cancelado', tom: 'err' },
  alerta: { titulo: 'Pedido com alerta', tom: 'err' }, // expedição: cancelado/frustrado/reembolsado
}

const DIAS = [
  'Domingo',
  'Segunda-feira',
  'Terça-feira',
  'Quarta-feira',
  'Quinta-feira',
  'Sexta-feira',
  'Sábado',
]

// ── Helpers de data (civil YYYY-MM-DD -> local, sem cair no dia anterior) ──────

/** Parseia "YYYY-MM-DD" como data LOCAL (T00:00:00) — evita shift de fuso. */
function parseCivilDate(ymd: string | null): Date | null {
  if (!ymd) return null
  const head = ymd.slice(0, 10)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(head)) return null
  const d = new Date(`${head}T00:00:00`)
  return Number.isNaN(d.getTime()) ? null : d
}

/** "sexta-feira, 20/06" a partir de YYYY-MM-DD. */
function formatDiaData(ymd: string | null): string {
  const d = parseCivilDate(ymd)
  if (!d) return ''
  const dia = DIAS[d.getDay()]
  const dd = String(d.getDate()).padStart(2, '0')
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  return `${dia}, ${dd}/${mm}`
}

/** RFC3339 -> "20/06 às 14:32" (timeline ja vem em horario de SP). */
function formatTimelineAt(at: string | null): string {
  if (!at) return ''
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return ''
  const dd = String(d.getDate()).padStart(2, '0')
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mi = String(d.getMinutes()).padStart(2, '0')
  return `${dd}/${mm} às ${hh}:${mi}`
}

// Fallback para compatibilidade durante rollout: mantém a previsão estável por
// pedido e distribui por minuto entre 15:01 e 17:59. Em produção, o payload
// `previsao_horario` do backend é a fonte de verdade.
function previsaoChegadaMotoboy(code: string): string {
  let hash = 0
  for (let i = 0; i < code.length; i++) hash = (hash * 31 + code.charCodeAt(i)) >>> 0
  const minute = 15 * 60 + 1 + (hash % 179)
  return `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`
}

/** Nome amigável no detalhe público do pedido. */
function nomeItemExibicao(nome: string): string {
  return /^egipzya\s+espuma$/i.test(nome.trim()) ? 'Espuma' : nome.trim()
}

function nomeComboEmLinhas(nome: string): string {
  return nome.includes(' + ') ? nome.replace(/\s+\+\s+/g, '\n') : nome
}

/** Consolida linhas repetidas do mesmo combo e preserva o valor cheio. */
function itensParaExibicao(itens: TrackingResponse['itens']): TrackingResponse['itens'] {
  const agrupados = new Map<string, TrackingResponse['itens'][number]>()
  for (const item of itens) {
    const nome = nomeItemExibicao(item.nome)
    // Um combo chega do backend em uma linha por produto/variação, com o mesmo
    // nome-base e sufixos como "— Sérum"/"— Espuma". No detalhe público ele é
    // uma única oferta: exibir o nome-base uma vez e somar o valor cheio.
    const comboBase = nome.includes(' — ') && nome.includes(' + ')
      ? nome.split(' — ')[0].trim()
      : nome
    const chave = comboBase.toLocaleLowerCase()
    const anterior = agrupados.get(chave)
    if (!anterior) {
      agrupados.set(chave, { ...item, nome: comboBase })
      continue
    }
    anterior.qtd += item.qtd
    anterior.total = (Number(anterior.total) + Number(item.total)).toFixed(2)
    if (!anterior.image_url && item.image_url) anterior.image_url = item.image_url
  }
  return Array.from(agrupados.values())
}

// NOTA: telefone/e-mail do cliente vem JA MASCARADOS do backend (rastreio publico,
// sem auth). Por isso NAO ha mais helper de formatar telefone nem de montar link
// wa.me/mailto para o cliente — um valor mascarado nao e acionavel e nao deve ser
// "desmascarado" no front.
//
// AUDIT-2026-07-30 (dono): "tira o suporte dali" — removido o card de Suporte
// (contato fixo da marca, sem fonte real no contrato do rastreio).

// ── Status -> banner ──────────────────────────────────────────────────────────

function bannerInfo(t: TrackingResponse): {
  titulo: string
  detalhe: string
  tom: 'brand' | 'ok' | 'err'
} {
  const term = TERMINAIS[t.status]
  if (term) {
    return { titulo: term.titulo, detalhe: '', tom: term.tom }
  }

  const isMotoboy = t.entrega.tipo === 'motoboy'
  const diaData = formatDiaData(t.entrega.data)
  const bairro = t.entrega.destino_bairro || t.entrega.cidade || ''

  if (!isMotoboy) {
    // ── Expedição (ME): vocabulário próprio, NUNCA usa em_rota/a_caminho ──────
    if (t.status === 'entregue') {
      const dest = bairro ? ` em ${bairro}` : ''
      return { titulo: 'Pedido entregue', detalhe: `Entrega concluída${dest}.`, tom: 'ok' }
    }
    if (t.status === 'enviado') {
      const dest = bairro ? ` com destino a ${bairro}` : ''
      return {
        titulo: 'Pedido enviado',
        detalhe: diaData ? `previsão para ${diaData}${dest}` : `a caminho${dest}`,
        tom: 'brand',
      }
    }
    if (t.status === 'a_caminho') {
      return { titulo: 'Pedido a caminho', detalhe: 'o pacote saiu para entrega', tom: 'brand' }
    }
    if (t.status === 'separado') {
      return {
        titulo: 'Pedido separado',
        detalhe: diaData ? `entrega prevista para ${diaData}` : 'pedido pronto pra envio',
        tom: 'brand',
      }
    }
    if (t.status === 'aprovado') {
      return { titulo: 'Pedido aprovado', detalhe: 'estamos preparando seu pedido', tom: 'brand' }
    }
    if (t.status === 'em_andamento') {
      return { titulo: 'Pedido em andamento', detalhe: 'processando seu pedido', tom: 'brand' }
    }
    // pendente / default
    return { titulo: 'Pedido confirmado', detalhe: 'seu pedido foi recebido', tom: 'brand' }
  }

  // ── Motoboy/COD ──────────────────────────────────────────────────────────
  if (t.status === 'completo') {
    const dest = bairro ? ` em ${bairro}` : ''
    return { titulo: 'Pedido entregue', detalhe: `Entrega concluída${dest}.`, tom: 'ok' }
  }
  if (t.status === 'em_rota' || t.status === 'a_caminho') {
    const dest = bairro ? ` com destino a ${bairro}` : ''
    return {
      titulo: 'Pedido a caminho',
      detalhe: t.status === 'em_rota' && diaData ? `previsão para ${diaData}${dest}` : `saiu para entrega${dest}`,
      tom: 'brand',
    }
  }
  if (t.status === 'separado' || t.status === 'em_separacao') {
    return {
      titulo: 'Pedido em preparação',
      detalhe: diaData ? `entrega prevista para ${diaData}` : 'estamos preparando seu pedido',
      tom: 'brand',
    }
  }
  // agendado / default
  const dest = bairro ? ` com destino a ${bairro}` : ''
  let det: string
  if (diaData) {
    det = `agendada para ${diaData}${dest}`
  } else if (bairro) {
    det = `com destino a ${bairro}`
  } else {
    det = 'seu pedido foi recebido'
  }
  return { titulo: 'Entrega Agendada', detalhe: det, tom: 'brand' }
}

// ── Stepper ───────────────────────────────────────────────────────────────────

/** Indice da etapa atual no ladder informado (-1 = status terminal ou desconhecido). */
function currentLadderIndex(status: string, ladderKeys: readonly string[]): number {
  return ladderKeys.indexOf(status)
}

/** Mapa key->at do timeline para mostrar a data sob cada etapa. */
function timelineByKey(steps: TrackingTimelineStep[]): Record<string, string | null> {
  const m: Record<string, string | null> = {}
  for (const s of steps) m[s.key] = s.at
  return m
}

/** Última etapa com timestamp real conhecido (varre o ladder de trás pra frente). */
function ultimaOcorrencia(
  atMap: Record<string, string | null>,
  ladderKeys: readonly string[]
): { key: string; at: string } | null {
  for (let i = ladderKeys.length - 1; i >= 0; i--) {
    const key = ladderKeys[i]
    const at = atMap[key]
    if (at) return { key, at }
  }
  return null
}

function Stepper({ t }: { t: TrackingResponse }) {
  // AUDIT-2026-07-30 #5 (dono): motoboy/COD e expedição usam ladders diferentes.
  const isMotoboy = t.entrega.tipo === 'motoboy'
  const ladderKeys: readonly string[] = isMotoboy ? MOTOBOY_LADDER_KEYS : EXPEDICAO_LADDER_KEYS
  const ladderLabels = isMotoboy ? MOTOBOY_LADDER_LABELS : EXPEDICAO_LADDER_LABELS

  const curr = currentLadderIndex(t.status, ladderKeys)
  const atMap = timelineByKey(t.status_timeline)
  const isTerminal = curr === -1 && !!TERMINAIS[t.status]
  const ultima = ultimaOcorrencia(atMap, ladderKeys)
  const currentKey = curr >= 0 ? ladderKeys[curr] : t.status

  // AUDIT-2026-07-30 #7 (dono): "quando tiver etapa sem horário de disparo -
  // tira ela do fluxo" — etapa sem timestamp confirmado NÃO aparece mais no
  // stepper (nem em branco/pontilhado). Só mostra o que realmente aconteceu.
  const visibleKeys = ladderKeys.filter((key) => !!atMap[key])
  const historicoKeys = isTerminal
    ? [{ key: t.status, at: ultima?.at || '' }]
    : visibleKeys.map(key => ({ key, at: atMap[key] || '' }))

  return (
    <>
      <div className="fk-trk-stepper" role="list" aria-label="Progresso do pedido">
        {visibleKeys.map((key) => {
          const isCurrent = key === currentKey
          const at = atMap[key] || null
          const atLabel = at ? formatTimelineAt(at) : ''
          return (
            <div
              key={key}
              role="listitem"
              className={`fk-trk-step is-reached${isCurrent ? ' is-current' : ''}${
                isTerminal ? ' is-terminal' : ''
              }`}
            >
              <div className="fk-trk-step-top">
                <span className="fk-trk-line fk-trk-line-l" aria-hidden="true" />
                <span className="fk-trk-dot" aria-hidden="true">
                  ✓
                </span>
                <span className="fk-trk-line fk-trk-line-r" aria-hidden="true" />
              </div>
              <div className="fk-trk-step-label">{ladderLabels[key] || key}</div>
              <div className="fk-trk-step-at">{atLabel}</div>
            </div>
          )
        })}
      </div>
      {/* Quando há eventos reais da transportadora, eles são o histórico
          principal. O cartão interno "Enviado" seria duplicação e ficaria
          fora de ordem em relação às movimentações posteriores. */}
      {historicoKeys.length > 0 && t.carrier_events.length === 0 && (
        <div aria-label="Histórico do pedido">
          {historicoKeys.map((evento, index) => {
            const { dia, hora } = evento.at ? formatEventoData(evento.at) : { dia: '', hora: '' }
            const previsao = evento.key === 'em_rota' ? formatDiaData(t.entrega.data) : ''
            const descricao = TERMINAIS[evento.key]?.titulo || EVENTO_DESCRICOES[evento.key] || ''
            return (
          <div className="fk-trk-evento" key={`${evento.key}-${evento.at || index}`}>
            <div className="fk-trk-evento-data"><div>{dia}</div><div>{hora}</div></div>
            <div className="fk-trk-evento-corpo">
              <div className="fk-trk-evento-titulo">{ladderLabels[evento.key] || TERMINAIS[evento.key]?.titulo || evento.key}</div>
              <div className="fk-trk-evento-desc">{descricao}</div>
              {previsao && <div className="fk-trk-evento-previsao">Previsão de chegada: {previsao}</div>}
            </div>
          </div>
            )
          })}
        </div>
      )}
      {/* AUDIT-2026-07-30 #7 (dono): "falta mostrar a informação... que vem do
          rastreio" — código real da transportadora (Jadlog/Loggi/Correios),
          nunca a plataforma intermediária. */}
      {t.carrier_events.length > 0 && (
        <div aria-label="Histórico de rastreio">
          {t.carrier_events.slice(0, 8).map((ev, index) => {
            const { dia, hora } = formatEventoData(ev.at)
            return (
              <div className="fk-trk-evento" key={`${ev.at}-${index}`}>
                <div className="fk-trk-evento-data"><div>{dia}</div><div>{hora}</div></div>
                <div className="fk-trk-evento-corpo">
                  <div className="fk-trk-evento-titulo">{limparMensagemRastreio(ev.description)}</div>
                  {ev.location && <div className="fk-trk-evento-desc">{ev.location}</div>}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}

// ── Linha chave/valor da coluna direita ───────────────────────────────────────

function InfoLine({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  if (children == null || children === '') return null
  return (
    <div className="fk-trk-info">
      <span className="fk-trk-info-k">{label}</span>
      <span className="fk-trk-info-v">{children}</span>
    </div>
  )
}

// ── Pagina ────────────────────────────────────────────────────────────────────

export default function Rastreio({ onBrand, hideGenericCopy = false }: { onBrand?: (brand: OfferBrand) => void; hideGenericCopy?: boolean }) {
  const { code: rawCode } = useParams()
  const code = (rawCode || '').trim()

  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [data, setData] = useState<TrackingResponse | null>(null)

  useEffect(() => {
    let alive = true
    if (!code) {
      setLoading(false)
      setError('Código do pedido ausente. Confira o link de acompanhamento.')
      return
    }
    setLoading(true)
    setError(null)
    setNotFound(false)
    fetchTracking(code)
      .then((r) => {
        if (!alive) return
        setData(r)
        onBrand?.(r.brand)
      })
      .catch((e: unknown) => {
        if (!alive) return
        if (e instanceof CheckoutApiError && e.status === 404) {
          setNotFound(true)
          return
        }
        setError(
          e instanceof CheckoutApiError
            ? e.message
            : 'Não foi possível carregar o rastreio. Tente novamente.'
        )
      })
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [code, onBrand])

  if (loading) {
    return (
      <div className="fk-center">
        <div className="fk-spinner" />
        <p className="fk-sub">Carregando seu pedido…</p>
      </div>
    )
  }

  if (notFound) {
    return (
      <div className="fk-center">
        <div className="fk-emoji">🔍</div>
        <h1 className="fk-h1">Pedido não encontrado</h1>
        <p className="fk-sub">
          Não localizamos um pedido com o código <strong>{code}</strong>. Confira o
          número informado.
        </p>
      </div>
    )
  }

  if (error || !data) {
    return (
      <div className="fk-center">
        <div className="fk-emoji">⚠️</div>
        <h1 className="fk-h1">Não foi possível abrir</h1>
        <p className="fk-sub">{error || 'Tente novamente em instantes.'}</p>
      </div>
    )
  }

  const banner = bannerInfo(data)
  const c = data.cliente
  const horarioEntrega = data.entrega.tipo === 'motoboy' ? (data.entrega.horario_entrega || '') : ''
  const previsaoMotoboy = data.entrega.tipo === 'motoboy' && data.status === 'em_rota' && !horarioEntrega
    ? (data.entrega.previsao_horario || previsaoChegadaMotoboy(data.code || code))
    : ''
  const itensExibicao = itensParaExibicao(data.itens)

  return (
    <div className="fk-trk">
      <h1 className="fk-trk-title">Detalhes do pedido #{data.code}</h1>
      <p className="fk-sub">Acompanhe o andamento do seu pedido.</p>

      <div className="fk-trk-grid">
        {/* Coluna principal -------------------------------------------------- */}
        <div className="fk-trk-main">
          {/* Resumo do pedido + banner + stepper */}
          <div className="fk-card">
            <h3 className="fk-card-title">Resumo do pedido</h3>

            {!TERMINAIS[data.status] && <div className={`fk-trk-banner fk-trk-banner-${banner.tom}`}>
              <div className="fk-trk-banner-titulo">{banner.titulo}</div>
              {banner.detalhe && (
                <div className="fk-trk-banner-detalhe">{banner.detalhe}</div>
              )}
            </div>}

            {horarioEntrega && (
              <div className="fk-trk-previsao" role="status">
                <strong>Entrega concluída às {horarioEntrega}</strong>
              </div>
            )}

            {previsaoMotoboy && (
              <div className="fk-trk-previsao" role="note">
                <strong>Previsão de chegada: às {previsaoMotoboy}</strong>
                <span>
                  O horário é apenas uma previsão e pode ser alterado. Entraremos em contato
                  com a cliente e a entrega será concluída dentro do horário comercial, até as 20h.
                </span>
              </div>
            )}

            <Stepper t={data} />
          </div>

          {/* Itens do pedido */}
          <div className="fk-card">
            <h3 className="fk-card-title">Itens do pedido</h3>
            {itensExibicao.length === 0 ? (
              <p className="fk-sub">Ainda não há itens neste pedido. Se isso parecer errado, fale com o suporte.</p>
            ) : (
              <ul className="fk-trk-items">
                {itensExibicao.map((it, i) => (
                  <li key={i} className="fk-trk-item">
                    <div className="fk-trk-item-thumb">
                      {it.image_url ? (
                        <img src={it.image_url} alt="" loading="lazy" />
                      ) : (
                        <span className="fk-trk-item-thumb-ph" aria-hidden="true">
                          📦
                        </span>
                      )}
                    </div>
                    <div className="fk-trk-item-body">
                      <div className="fk-trk-item-nome">{nomeComboEmLinhas(it.nome)}</div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>

        {/* Coluna lateral ---------------------------------------------------- */}
        <aside className="fk-trk-aside">
          {/* Dados do cliente — campos vêm JA MASCARADOS do backend (rastreio é
              público, sem auth). Exibimos verbatim: NAO reformatar, NAO criar
              link wa.me/mailto (um valor mascarado nao e acionavel). */}
          <div className="fk-card">
            <h3 className="fk-card-title">Dados do cliente</h3>
            <InfoLine label="Nome">{c.nome || '—'}</InfoLine>
            {c.telefone && <InfoLine label="Telefone">{c.telefone}</InfoLine>}
            {c.email && <InfoLine label="E-mail">{c.email}</InfoLine>}
            {c.cpf && <InfoLine label="CPF">{c.cpf}</InfoLine>}
            {c.endereco && <InfoLine label="Endereço">{c.endereco}</InfoLine>}
          </div>
        </aside>
      </div>

      {!hideGenericCopy && <p className="fk-note">
        {/* Usa o `code` da ROTA (o code assinado com que o cliente chegou), nao
            `data.code` — este ultimo pode vir como order_number cru no rastreio e
            quebraria o link quando a assinatura for exigida. */}
        <Link className="fk-trk-link" to={`/rastreio/${encodeURIComponent(code)}`}>
          Atualizar status
        </Link>
        {' · '}Guarde este link para acompanhar seu pedido.
      </p>}
    </div>
  )
}
