import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import FilterButton from '../components/FilterButton'
import FilterTopPanel, {
  FilterField,
  filterInputStyle,
  ActiveFilterChips,
  type ActiveChip,
} from '../components/FilterTopPanel'
import EmptyState from '../components/EmptyState'

type CD = { id: number; nome: string; cidade: string; uf: string; ativo: boolean }
type Zona = {
  id: number
  cd_id: number
  nome: string
  descricao: string | null
  dias_funcionamento: string
  cutoff_horarios: string | null
  ativo: boolean
  ceps?: CepRange[]
}
type CepRange = { id: number; zona_id: number; cep_inicio: string; cep_fim: string }

type CepCheckResult =
  | { found: false }
  | {
      found: true
      zona_id: number
      zona_nome: string
      dias_funcionamento: string
      cutoff_horarios: string | null
    }

const DIAS_ABREV = ['Dom', 'Seg', 'Ter', 'Qua', 'Qui', 'Sex', 'Sáb']

function fmtDias(d: string): string {
  return d
    .split(',')
    .map(n => DIAS_ABREV[+n] ?? n)
    .join(', ')
}

/** Retorna o horário de cutoff único da zona (primeiro dia ativo, ou fallback '21:00'). */
function cutoffLabel(cutoffJson: string | null, diasFuncionamento: string): string {
  if (!cutoffJson) return '21:00'
  try {
    const map: Record<string, string> = JSON.parse(cutoffJson)
    const dias = diasFuncionamento.split(',').map(Number).filter(n => !isNaN(n))
    for (const d of dias) {
      if (map[String(d)]) return map[String(d)]
    }
    const first = Object.values(map)[0]
    return first ?? '21:00'
  } catch {
    return '21:00'
  }
}

/** Classifica os dias de operação no mesmo esquema do WP admin. */
function opKey(dias: string): string {
  const arr = dias.split(',').map(Number)
  if (arr.length === 1 && arr[0] === 5) return 'sexta'
  if (arr.length === 1 && arr[0] === 6) return 'sabado'
  if (arr.includes(0)) return 'domingo'
  return 'seg-sab'
}

export default function Zonas() {
  const [cds, setCds] = useState<CD[]>([])
  const [zonas, setZonas] = useState<Zona[]>([])
  const [ceps, setCeps] = useState<CepRange[]>([])

  // Filtros aplicados.
  const [cdFilter, setCdFilter] = useState<number | ''>('')
  const [textFilter, setTextFilter] = useState('')
  const [opFilter, setOpFilter] = useState('')
  const [zonaFilter, setZonaFilter] = useState('')

  // Drafts no painel.
  const [draftCd, setDraftCd] = useState<number | ''>('')
  const [draftText, setDraftText] = useState('')
  const [draftOp, setDraftOp] = useState('')
  const [draftZona, setDraftZona] = useState('')
  const [filterOpen, setFilterOpen] = useState(false)

  const [cepInput, setCepInput] = useState('')
  const [cepResult, setCepResult] = useState<CepCheckResult | null>(null)
  const [cepLoading, setCepLoading] = useState(false)
  const [err, setErr] = useState('')
  const cepRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api<{ items: CD[] }>('/cds').then(r => setCds(r.items ?? [])).catch(e => setErr(e.message))
    api<{ items: Zona[] }>('/zonas').then(r => setZonas(r.items ?? [])).catch(e => setErr(e.message))
    api<{ items: CepRange[] }>('/zonas/ceps').then(r => setCeps(r.items ?? [])).catch(e => setErr(e.message))
  }, [])

  const cdName = (id: number) => cds.find(c => c.id === id)?.nome ?? `CD #${id}`
  const zonaCeps = (zonaId: number) => ceps.filter(c => c.zona_id === zonaId)

  // Filtragem em cascata: CD → texto → dia de operação → zona específica
  const filteredZonas = zonas.filter(z => {
    if (cdFilter !== '' && z.cd_id !== cdFilter) return false
    if (textFilter) {
      const q = textFilter.toLowerCase()
      const cdNome = cdName(z.cd_id).toLowerCase()
      const cepsTexto = zonaCeps(z.id)
        .map(c => `${c.cep_inicio} ${c.cep_fim}`)
        .join(' ')
      if (
        !z.nome.toLowerCase().includes(q) &&
        !cdNome.includes(q) &&
        !cepsTexto.includes(q)
      )
        return false
    }
    if (opFilter && opKey(z.dias_funcionamento) !== opFilter) return false
    if (zonaFilter && z.nome.toLowerCase() !== zonaFilter) return false
    return true
  })

  // Verificar CEP via endpoint Go
  async function verificarCep() {
    const cep = cepInput.replace(/\D/g, '')
    if (cep.length !== 8) {
      setCepResult(null)
      return
    }
    setCepLoading(true)
    try {
      const res = await api<CepCheckResult>(`/zonas/cep-check?cep=${cep}`)
      setCepResult(res)
    } catch {
      setCepResult({ found: false })
    } finally {
      setCepLoading(false)
    }
  }

  function openPanel() {
    setDraftCd(cdFilter); setDraftText(textFilter); setDraftOp(opFilter); setDraftZona(zonaFilter)
    setFilterOpen(true)
  }
  function applyFilters() {
    setCdFilter(draftCd); setTextFilter(draftText); setOpFilter(draftOp); setZonaFilter(draftZona)
    setFilterOpen(false)
  }
  function clearFilters() {
    setCdFilter(''); setTextFilter(''); setOpFilter(''); setZonaFilter('')
    setDraftCd(''); setDraftText(''); setDraftOp(''); setDraftZona('')
    setFilterOpen(false)
  }

  // Chips ativos.
  const chips: ActiveChip[] = []
  if (cdFilter !== '') {
    const cd = cds.find(c => c.id === cdFilter)
    chips.push({ key: 'cd', label: `CD: ${cd?.nome || `#${cdFilter}`}`, onRemove: () => setCdFilter('') })
  }
  if (textFilter) chips.push({ key: 'q', label: `Busca: ${textFilter}`, onRemove: () => setTextFilter('') })
  if (opFilter) chips.push({ key: 'op', label: `Operação: ${opFilter}`, onRemove: () => setOpFilter('') })
  if (zonaFilter) chips.push({ key: 'zona', label: `Zona: ${zonaFilter}`, onRemove: () => setZonaFilter('') })
  const activeCount = chips.length

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Zonas de Entrega</h1>
          <p>{zonas.length} zonas · {ceps.length} faixas de CEP</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* Lista de zonas */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {filteredZonas.map(z => (
          <div key={z.id} className="szv2-card" style={{ padding: '16px 20px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '4px' }}>
              <span style={{ fontSize: '14px', fontWeight: 700, color: 'var(--szv2-text)' }}>{z.nome}</span>
              <span style={{ fontSize: '11px', background: 'var(--szv2-neutral-bg)', padding: '2px 8px', borderRadius: '6px', color: 'var(--szv2-text-muted)' }}>
                {cdName(z.cd_id)}
              </span>
              <span style={{ fontSize: '11px', color: 'var(--szv2-text-muted)' }}>{fmtDias(z.dias_funcionamento)}</span>
              <span style={{ fontSize: '11px', color: 'var(--szv2-text-muted)' }}>
                ⏰ até {cutoffLabel(z.cutoff_horarios, z.dias_funcionamento)} do dia anterior
              </span>
              <span className={`sz-badge ${z.ativo ? 'szv2-badge-success' : 'szv2-badge-neutral'}`}>
                {z.ativo ? 'Ativa' : 'Inativa'}
              </span>
            </div>
            {z.descricao && (
              <p style={{ fontSize: '12px', color: 'var(--szv2-text-muted)', margin: '0 0 8px' }}>
                {z.descricao}
              </p>
            )}
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
              {zonaCeps(z.id).map(c => (
                <span
                  key={c.id}
                  style={{
                    fontFamily: 'var(--szv2-font-mono)',
                    fontSize: '11px',
                    background: 'var(--szv2-brand-light)',
                    color: 'var(--szv2-brand)',
                    padding: '2px 8px',
                    borderRadius: '6px',
                  }}
                >
                  {c.cep_inicio}–{c.cep_fim}
                </span>
              ))}
              {zonaCeps(z.id).length === 0 && (
                <span style={{ fontSize: '12px', color: 'var(--szv2-text-faint)' }}>Sem CEPs cadastrados</span>
              )}
            </div>
          </div>
        ))}
        {filteredZonas.length === 0 && (
          <EmptyState
            icon="🗺️"
            title={zonas.length === 0 ? 'Nenhuma zona cadastrada.' : 'Nenhuma zona com esses filtros.'}
            description={zonas.length === 0
              ? 'Crie a primeira zona vinculada a um CD para configurar entregas.'
              : 'Ajuste os filtros aplicados para ver as zonas existentes.'}
            // TODO: substituir alert por modal de criação de zona quando o endpoint existir.
            action={zonas.length === 0 ? {
              label: 'Criar zona',
              onClick: () => alert('Em breve: criação de zona pelo painel. Por enquanto, configure pelo módulo CDs / banco.'),
            } : undefined}
          />
        )}
      </div>

      {/* Preview de cobertura por CEP */}
      <div className="szv2-card" style={{ marginTop: '24px', padding: '16px 20px' }}>
        <h3 style={{ margin: '0 0 12px', fontSize: '14px', fontWeight: 700 }}>
          Preview de cobertura por CEP
        </h3>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
          <input
            ref={cepRef}
            className="szv2-input"
            style={{ maxWidth: '200px' }}
            type="text"
            placeholder="Digite um CEP para testar"
            maxLength={9}
            value={cepInput}
            onChange={e => {
              setCepInput(e.target.value)
              setCepResult(null)
            }}
            onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); verificarCep() } }}
          />
          <button
            className="szv2-btn-secondary"
            onClick={verificarCep}
            disabled={cepLoading}
          >
            {cepLoading ? 'Verificando...' : 'Verificar'}
          </button>
        </div>
        {cepResult === null && (
          <p style={{ fontSize: '12px', color: 'var(--szv2-text-faint)', marginTop: '8px' }}>
            Digite um CEP para ver zona, operação e cobertura.
          </p>
        )}
        {cepResult !== null && !cepResult.found && (
          <p style={{ fontSize: '13px', color: 'var(--szv2-danger)', marginTop: '8px' }}>
            ❌ CEP fora das faixas cadastradas.
          </p>
        )}
        {cepResult !== null && cepResult.found && (
          <div style={{ marginTop: '8px', fontSize: '13px' }}>
            <span style={{ color: 'var(--szv2-success)' }}>✅</span>{' '}
            <strong>{cepInput.replace(/\D/g, '')}</strong> está em{' '}
            <strong>{cepResult.zona_nome}</strong>
            <br />
            <span style={{ color: 'var(--szv2-text-muted)' }}>
              🗓️ {fmtDias(cepResult.dias_funcionamento)} · ⏰ até{' '}
              {cutoffLabel(cepResult.cutoff_horarios, cepResult.dias_funcionamento)} do dia anterior
            </span>
          </div>
        )}
      </div>

      {/* Tabela de CDs com contagem de zonas */}
      <div className="szv2-card" style={{ marginTop: '16px', padding: '16px 20px' }}>
        <h3 style={{ margin: '0 0 12px', fontSize: '14px', fontWeight: 700 }}>CDs Cadastrados</h3>
        <table className="szv2-table" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left', padding: '6px 8px', fontSize: '12px', color: 'var(--szv2-text-muted)' }}>Nome</th>
              <th style={{ textAlign: 'left', padding: '6px 8px', fontSize: '12px', color: 'var(--szv2-text-muted)' }}>Cidade/UF</th>
              <th style={{ textAlign: 'left', padding: '6px 8px', fontSize: '12px', color: 'var(--szv2-text-muted)' }}>Zonas</th>
            </tr>
          </thead>
          <tbody>
            {cds.map(c => {
              const qtd = zonas.filter(z => z.cd_id === c.id).length
              return (
                <tr key={c.id}>
                  <td style={{ padding: '6px 8px', fontSize: '13px', fontWeight: 600 }}>{c.nome}</td>
                  <td style={{ padding: '6px 8px', fontSize: '13px', color: 'var(--szv2-text-muted)' }}>
                    {c.cidade}/{c.uf.toUpperCase()}
                  </td>
                  <td style={{ padding: '6px 8px', fontSize: '13px' }}>
                    <span
                      style={{
                        background: qtd > 0 ? 'var(--szv2-brand-light)' : 'var(--szv2-neutral-bg)',
                        color: qtd > 0 ? 'var(--szv2-brand)' : 'var(--szv2-text-faint)',
                        padding: '2px 8px',
                        borderRadius: '6px',
                        fontSize: '11px',
                        fontWeight: 600,
                      }}
                    >
                      {qtd} zona{qtd !== 1 ? 's' : ''}
                    </span>
                  </td>
                </tr>
              )
            })}
            {cds.length === 0 && (
              <tr>
                <td colSpan={3} style={{ padding: '16px 8px', textAlign: 'center', color: 'var(--szv2-text-faint)', fontSize: '13px' }}>
                  Nenhum CD cadastrado
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="CD">
          <select
            style={filterInputStyle}
            value={draftCd}
            onChange={e => setDraftCd(e.target.value === '' ? '' : +e.target.value)}
          >
            <option value="">Todos CDs</option>
            {cds.map(c => (
              <option key={c.id} value={c.id}>
                {c.nome} — {c.cidade}/{c.uf.toUpperCase()}
              </option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Operação">
          <select
            style={filterInputStyle}
            value={draftOp}
            onChange={e => setDraftOp(e.target.value)}
          >
            <option value="">Todas as operações</option>
            <option value="seg-sab">Segunda a sábado</option>
            <option value="sexta">Apenas sexta-feira</option>
            <option value="sabado">Apenas sábado</option>
            <option value="domingo">Inclui domingo</option>
          </select>
        </FilterField>
        <FilterField label="Zona">
          <select
            style={filterInputStyle}
            value={draftZona}
            onChange={e => setDraftZona(e.target.value)}
          >
            <option value="">Todas as zonas</option>
            {zonas.map(z => (
              <option key={z.id} value={z.nome.toLowerCase()}>
                {z.nome}
              </option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Busca (zona / CD / CEP)">
          <input
            type="search"
            style={filterInputStyle}
            placeholder="ex.: Centro, 01310"
            value={draftText}
            onChange={e => setDraftText(e.target.value)}
          />
        </FilterField>
      </FilterTopPanel>
    </div>
  )
}
