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
import TableSkeleton from '../components/TableSkeleton'
import DetailDrawer from '../components/DetailDrawer'
import FalkSelect from '../components/FalkSelect'
import { emitToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'

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

/**
 * Constrói o JSON de cutoff_horarios no formato armazenado no banco:
 * um array de 7 posições (uma por dia, índice 0=Dom … 6=Sáb), todas com
 * o mesmo horário. É o formato que o router PHP do motoboy e o cutoffLabel
 * deste arquivo já esperam (JSON.parse + acesso por índice numérico).
 */
function buildCutoffJson(hora: string): string {
  const h = /^\d{2}:\d{2}$/.test(hora) ? hora : '21:00'
  return JSON.stringify(Array(7).fill(h))
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
  // Sem o flag de loading a tela mostrava EmptyState ("Nenhuma zona cadastrada")
  // durante o fetch inicial — falso vazio. true até o primeiro carregamento concluir.
  const [loading, setLoading] = useState(true)
  const cepRef = useRef<HTMLInputElement>(null)

  // Edição de zona (modal szv2).
  const [editing, setEditing] = useState<Zona | null>(null)
  const [edNome, setEdNome] = useState('')
  const [edCd, setEdCd] = useState<number>(0)
  const [edDescricao, setEdDescricao] = useState('')
  const [edDias, setEdDias] = useState<number[]>([])
  const [edCutoff, setEdCutoff] = useState('21:00')
  const [edAtivo, setEdAtivo] = useState(true)
  const [saving, setSaving] = useState(false)

  // Criação de zona (mesmo shape do modal de edição, drawer separado).
  const [creating, setCreating] = useState(false)
  const [cNome, setCNome] = useState('')
  const [cCd, setCCd] = useState<number>(0)
  const [cDescricao, setCDescricao] = useState('')
  const [cDias, setCDias] = useState<number[]>([0, 1, 2, 3, 4, 5, 6])
  const [cCutoff, setCCutoff] = useState('21:00')
  const [cAtivo, setCAtivo] = useState(true)
  const [cSaving, setCSaving] = useState(false)

  // Faixa de CEP: criação/edição inline por zona (um form ativo por vez).
  const [cepEditingZona, setCepEditingZona] = useState<number | null>(null)
  const [cepEditingId, setCepEditingId] = useState<number | null>(null)
  const [cepIni, setCepIni] = useState('')
  const [cepFim, setCepFim] = useState('')
  const [cepSaving, setCepSaving] = useState(false)

  // Dispara o toast lateral no tema do site (stack .szv2-toasts via ToastHost).
  function showToast(kind: 'ok' | 'err', msg: string) {
    emitToast(kind, msg)
  }

  function recarregarZonas() {
    api<{ items: Zona[] }>('/zonas').then(r => setZonas(r.items ?? [])).catch(e => setErr(e.message))
  }

  function abrirEdicao(z: Zona) {
    setEditing(z)
    setEdNome(z.nome)
    setEdCd(z.cd_id)
    setEdDescricao(z.descricao ?? '')
    setEdDias(z.dias_funcionamento.split(',').map(Number).filter(n => !isNaN(n)))
    setEdCutoff(cutoffLabel(z.cutoff_horarios, z.dias_funcionamento))
    setEdAtivo(z.ativo)
  }

  function toggleDia(d: number) {
    setEdDias(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d])
  }

  async function salvarEdicao() {
    if (!editing) return
    if (!edNome.trim()) { showToast('err', 'Informe o nome da zona.'); return }
    if (edDias.length === 0) { showToast('err', 'Selecione ao menos um dia de operação.'); return }
    setSaving(true)
    try {
      const dias = [...edDias].sort((a, b) => a - b).join(',')
      const updated = await api<Zona>(`/zonas/${editing.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          nome: edNome.trim(),
          cd_id: edCd,
          // String vazia (não null) para permitir LIMPAR a descrição:
          // o handler usa COALESCE($n, descricao), então null manteria o valor antigo.
          descricao: edDescricao.trim(),
          dias_funcionamento: dias,
          cutoff_horarios: buildCutoffJson(edCutoff),
          ativo: edAtivo,
        }),
      })
      // Atualiza a zona localmente e recarrega a lista para refletir o estado real.
      setZonas(prev => prev.map(z => z.id === updated.id ? { ...z, ...updated } : z))
      recarregarZonas()
      setEditing(null)
      showToast('ok', 'Zona atualizada.')
    } catch (e) {
      showToast('err', e instanceof Error ? e.message : 'Falha ao salvar zona.')
    } finally {
      setSaving(false)
    }
  }

  async function handleDeleteZona(z: Zona) {
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir zona',
      message: `Excluir a zona "${z.nome}"? Todas as faixas de CEP vinculadas também serão removidas. Esta ação não pode ser desfeita.`,
      confirmLabel: 'Excluir zona',
    })
    if (!ok) return
    try {
      await api(`/zonas/${z.id}`, { method: 'DELETE' })
      setZonas(prev => prev.filter(x => x.id !== z.id))
      showToast('ok', `Zona "${z.nome}" excluída.`)
    } catch (e) {
      showToast('err', e instanceof Error ? e.message : 'Falha ao excluir zona.')
    }
  }

  function abrirCriacao() {
    setCNome(''); setCCd(cds[0]?.id ?? 0); setCDescricao('')
    setCDias([0, 1, 2, 3, 4, 5, 6]); setCCutoff('21:00'); setCAtivo(true)
    setCreating(true)
  }
  function toggleDiaCriacao(d: number) {
    setCDias(prev => prev.includes(d) ? prev.filter(x => x !== d) : [...prev, d])
  }

  async function salvarCriacao() {
    if (!cNome.trim()) { showToast('err', 'Informe o nome da zona.'); return }
    if (!cCd) { showToast('err', 'Selecione o CD.'); return }
    if (cDias.length === 0) { showToast('err', 'Selecione ao menos um dia de operação.'); return }
    setCSaving(true)
    try {
      const dias = [...cDias].sort((a, b) => a - b).join(',')
      const created = await api<Zona>('/zonas', {
        method: 'POST',
        body: JSON.stringify({
          nome: cNome.trim(),
          cd_id: cCd,
          descricao: cDescricao.trim(),
          dias_funcionamento: dias,
          cutoff_horarios: buildCutoffJson(cCutoff),
          ativo: cAtivo,
        }),
      })
      setZonas(prev => [...prev, created])
      setCreating(false)
      showToast('ok', 'Zona criada.')
    } catch (e) {
      showToast('err', e instanceof Error ? e.message : 'Falha ao criar zona.')
    } finally {
      setCSaving(false)
    }
  }

  function abrirNovaFaixa(zonaId: number) {
    setCepEditingZona(zonaId); setCepEditingId(null); setCepIni(''); setCepFim('')
  }
  function abrirEdicaoFaixa(c: CepRange) {
    setCepEditingZona(c.zona_id); setCepEditingId(c.id); setCepIni(c.cep_inicio); setCepFim(c.cep_fim)
  }
  function cancelarFaixa() {
    setCepEditingZona(null); setCepEditingId(null); setCepIni(''); setCepFim('')
  }

  async function salvarFaixa(zonaId: number) {
    const ini = cepIni.replace(/\D/g, '')
    const fim = cepFim.replace(/\D/g, '')
    if (ini.length !== 8 || fim.length !== 8) {
      showToast('err', 'CEP inicial e final devem ter 8 dígitos.')
      return
    }
    if (ini > fim) {
      showToast('err', 'CEP inicial deve ser menor ou igual ao final.')
      return
    }
    setCepSaving(true)
    try {
      if (cepEditingId) {
        const updated = await api<CepRange>(`/zonas/ceps/${cepEditingId}`, {
          method: 'PUT',
          body: JSON.stringify({ zona_id: zonaId, cep_inicio: ini, cep_fim: fim }),
        })
        setCeps(prev => prev.map(c => c.id === updated.id ? updated : c))
        showToast('ok', 'Faixa de CEP atualizada.')
      } else {
        const created = await api<CepRange>('/zonas/ceps', {
          method: 'POST',
          body: JSON.stringify({ zona_id: zonaId, cep_inicio: ini, cep_fim: fim }),
        })
        setCeps(prev => [...prev, created])
        showToast('ok', 'Faixa de CEP adicionada.')
      }
      cancelarFaixa()
    } catch (e) {
      showToast('err', e instanceof Error ? e.message : 'Falha ao salvar faixa de CEP.')
    } finally {
      setCepSaving(false)
    }
  }

  async function excluirFaixa(c: CepRange) {
    const ok = await confirmAsync({
      variant: 'danger',
      title: 'Excluir faixa de CEP',
      message: `Excluir a faixa ${c.cep_inicio}–${c.cep_fim}? Esta ação não pode ser desfeita.`,
      confirmLabel: 'Excluir faixa',
    })
    if (!ok) return
    try {
      await api(`/zonas/ceps/${c.id}`, { method: 'DELETE' })
      setCeps(prev => prev.filter(x => x.id !== c.id))
      showToast('ok', 'Faixa de CEP excluída.')
    } catch (e) {
      showToast('err', e instanceof Error ? e.message : 'Falha ao excluir faixa de CEP.')
    }
  }

  useEffect(() => {
    let alive = true
    setLoading(true); setErr('')
    Promise.all([
      api<{ items: CD[] }>('/cds').then(r => r.items ?? []),
      api<{ items: Zona[] }>('/zonas').then(r => r.items ?? []),
      api<{ items: CepRange[] }>('/zonas/ceps').then(r => r.items ?? []),
    ])
      .then(([cdsR, zonasR, cepsR]) => {
        if (!alive) return
        setCds(cdsR); setZonas(zonasR); setCeps(cepsR)
      })
      .catch(e => { if (alive) setErr(e?.message || 'Falha ao carregar zonas.') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
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
          <p>{loading ? 'Carregando…' : `${zonas.length} zonas · ${ceps.length} faixas de CEP`}</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="button" className="szv2-btn szv2-btn-brand" onClick={abrirCriacao}>
            + Cadastrar zona
          </button>
          <FilterButton active={activeCount > 0} count={activeCount} onClick={openPanel} />
        </div>
      </div>

      <ActiveFilterChips chips={chips} onClearAll={clearFilters} />

      {err && <div className="sz-alert-danger">{err}</div>}

      {/* Lista de zonas */}
      {loading ? (
        <TableSkeleton rows={6} cols={1} />
      ) : (
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
              <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  onClick={() => abrirEdicao(z)}
                >
                  Editar
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-danger szv2-btn-sm"
                  onClick={() => handleDeleteZona(z)}
                >
                  Excluir
                </button>
              </div>
            </div>
            {z.descricao && (
              <p style={{ fontSize: '12px', color: 'var(--szv2-text-muted)', margin: '0 0 8px' }}>
                {z.descricao}
              </p>
            )}
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', alignItems: 'center' }}>
              {zonaCeps(z.id).map(c => (
                <span
                  key={c.id}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    fontFamily: 'var(--szv2-font-mono)',
                    fontSize: '11px',
                    background: 'var(--szv2-brand-light)',
                    color: 'var(--szv2-brand)',
                    padding: '2px 4px 2px 8px',
                    borderRadius: '6px',
                  }}
                >
                  {c.cep_inicio}–{c.cep_fim}
                  <button
                    type="button"
                    onClick={() => abrirEdicaoFaixa(c)}
                    title="Editar faixa"
                    style={{ background: 'none', border: 0, cursor: 'pointer', color: 'inherit', padding: 2, lineHeight: 1, fontSize: 11 }}
                  >
                    ✎
                  </button>
                  <button
                    type="button"
                    onClick={() => excluirFaixa(c)}
                    title="Excluir faixa"
                    style={{ background: 'none', border: 0, cursor: 'pointer', color: 'var(--szv2-danger)', padding: 2, lineHeight: 1, fontSize: 11 }}
                  >
                    ✕
                  </button>
                </span>
              ))}
              {zonaCeps(z.id).length === 0 && (
                <span style={{ fontSize: '12px', color: 'var(--szv2-text-faint)' }}>Sem CEPs cadastrados</span>
              )}
              {cepEditingZona !== z.id && (
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  onClick={() => abrirNovaFaixa(z.id)}
                >
                  + Faixa de CEP
                </button>
              )}
            </div>
            {cepEditingZona === z.id && (
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', marginTop: 8 }}>
                <input
                  className="szv2-input"
                  style={{ maxWidth: 140, fontSize: 12 }}
                  type="text"
                  placeholder="CEP inicial"
                  maxLength={9}
                  value={cepIni}
                  onChange={e => setCepIni(e.target.value)}
                  disabled={cepSaving}
                />
                <span style={{ color: 'var(--szv2-text-muted)' }}>–</span>
                <input
                  className="szv2-input"
                  style={{ maxWidth: 140, fontSize: 12 }}
                  type="text"
                  placeholder="CEP final"
                  maxLength={9}
                  value={cepFim}
                  onChange={e => setCepFim(e.target.value)}
                  disabled={cepSaving}
                />
                <button
                  type="button"
                  className="szv2-btn szv2-btn-brand szv2-btn-sm"
                  onClick={() => salvarFaixa(z.id)}
                  disabled={cepSaving}
                >
                  {cepSaving ? 'Salvando…' : cepEditingId ? 'Salvar' : 'Adicionar'}
                </button>
                <button
                  type="button"
                  className="szv2-btn szv2-btn-secondary szv2-btn-sm"
                  onClick={cancelarFaixa}
                  disabled={cepSaving}
                >
                  Cancelar
                </button>
              </div>
            )}
          </div>
        ))}
        {filteredZonas.length === 0 && (
          <EmptyState
            icon="🗺️"
            title={zonas.length === 0 ? 'Nenhuma zona cadastrada.' : 'Nenhuma zona com esses filtros.'}
            description={zonas.length === 0
              ? 'Crie a primeira zona vinculada a um CD para configurar entregas.'
              : 'Ajuste os filtros aplicados para ver as zonas existentes.'}
            action={zonas.length === 0 ? {
              label: 'Criar zona',
              onClick: abrirCriacao,
            } : undefined}
          />
        )}
      </div>
      )}

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
            {!loading && cds.length === 0 && (
              <tr>
                <td colSpan={3} style={{ padding: '16px 8px', textAlign: 'center', color: 'var(--szv2-text-faint)', fontSize: '13px' }}>
                  Nenhum CD cadastrado
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Modal de edição de zona — 2026-06-19 migrado para DetailDrawer (lateral direito, marca registrada) */}
      {editing && (
        <DetailDrawer
          open
          onClose={() => { if (!saving) setEditing(null) }}
          title={`Editar zona — ${editing.nome}`}
          footer={
            <>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setEditing(null)}
                disabled={saving}
              >
                Cancelar
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={salvarEdicao}
                disabled={saving}
              >
                {saving ? 'Salvando…' : 'Salvar alterações'}
              </button>
            </>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <div className="szv2-field">
              <label className="szv2-label">Nome da zona</label>
              <input
                className="szv2-input"
                type="text"
                value={edNome}
                onChange={e => setEdNome(e.target.value)}
                disabled={saving}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">CD (centro de distribuição)</label>
              <FalkSelect
                aria-label="CD (centro de distribuição)"
                value={String(edCd)}
                onChange={v => setEdCd(Number(v))}
                disabled={saving}
                options={[
                  ...(!cds.some(c => c.id === edCd) && edCd > 0
                    ? [{ value: String(edCd), label: `CD #${edCd}` }]
                    : []),
                  ...cds.map(c => ({
                    value: String(c.id),
                    label: `${c.nome} — ${c.cidade}/${c.uf.toUpperCase()}`,
                  })),
                ]}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Descrição (opcional)</label>
              <textarea
                className="szv2-input"
                rows={2}
                style={{ height: 'auto', padding: '8px 12px', resize: 'vertical' }}
                value={edDescricao}
                onChange={e => setEdDescricao(e.target.value)}
                disabled={saving}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Dias de operação</label>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {DIAS_ABREV.map((dia, i) => {
                  const ativo = edDias.includes(i)
                  return (
                    <button
                      key={i}
                      type="button"
                      disabled={saving}
                      onClick={() => toggleDia(i)}
                      className={`szv2-btn szv2-btn-sm ${ativo ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                      style={{ minWidth: 52 }}
                    >
                      {dia}
                    </button>
                  )
                })}
              </div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Horário de cutoff (dia anterior)</label>
              <input
                className="szv2-input"
                type="time"
                style={{ maxWidth: 140 }}
                value={edCutoff}
                onChange={e => setEdCutoff(e.target.value)}
                disabled={saving}
              />
              <span className="szv2-text-xs szv2-text-muted">
                Aplicado a todos os dias de operação da zona.
              </span>
            </div>
            <div className="szv2-field">
              <label className="szv2-label" style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={edAtivo}
                  onChange={e => setEdAtivo(e.target.checked)}
                  disabled={saving}
                />
                Zona ativa
              </label>
            </div>
          </div>
        </DetailDrawer>
      )}

      {/* Modal de criação de zona — mesmo shape do modal de edição. */}
      {creating && (
        <DetailDrawer
          open
          onClose={() => { if (!cSaving) setCreating(false) }}
          title="Cadastrar zona"
          footer={
            <>
              <button
                type="button"
                className="szv2-btn szv2-btn-secondary"
                onClick={() => setCreating(false)}
                disabled={cSaving}
              >
                Cancelar
              </button>
              <button
                type="button"
                className="szv2-btn szv2-btn-brand"
                onClick={salvarCriacao}
                disabled={cSaving}
              >
                {cSaving ? 'Criando…' : 'Criar zona'}
              </button>
            </>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <div className="szv2-field">
              <label className="szv2-label">Nome da zona</label>
              <input
                className="szv2-input"
                type="text"
                value={cNome}
                onChange={e => setCNome(e.target.value)}
                disabled={cSaving}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">CD (centro de distribuição)</label>
              <FalkSelect
                aria-label="CD (centro de distribuição)"
                value={String(cCd)}
                onChange={v => setCCd(Number(v))}
                disabled={cSaving}
                options={cds.map(c => ({
                  value: String(c.id),
                  label: `${c.nome} — ${c.cidade}/${c.uf.toUpperCase()}`,
                }))}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Descrição (opcional)</label>
              <textarea
                className="szv2-input"
                rows={2}
                style={{ height: 'auto', padding: '8px 12px', resize: 'vertical' }}
                value={cDescricao}
                onChange={e => setCDescricao(e.target.value)}
                disabled={cSaving}
              />
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Dias de operação</label>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {DIAS_ABREV.map((dia, i) => {
                  const ativo = cDias.includes(i)
                  return (
                    <button
                      key={i}
                      type="button"
                      disabled={cSaving}
                      onClick={() => toggleDiaCriacao(i)}
                      className={`szv2-btn szv2-btn-sm ${ativo ? 'szv2-btn-brand' : 'szv2-btn-secondary'}`}
                      style={{ minWidth: 52 }}
                    >
                      {dia}
                    </button>
                  )
                })}
              </div>
            </div>
            <div className="szv2-field">
              <label className="szv2-label">Horário de cutoff (dia anterior)</label>
              <input
                className="szv2-input"
                type="time"
                style={{ maxWidth: 140 }}
                value={cCutoff}
                onChange={e => setCCutoff(e.target.value)}
                disabled={cSaving}
              />
              <span className="szv2-text-xs szv2-text-muted">
                Aplicado a todos os dias de operação da zona.
              </span>
            </div>
            <div className="szv2-field">
              <label className="szv2-label" style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={cAtivo}
                  onChange={e => setCAtivo(e.target.checked)}
                  disabled={cSaving}
                />
                Zona ativa
              </label>
            </div>
          </div>
        </DetailDrawer>
      )}

      <FilterTopPanel
        open={filterOpen}
        onClose={() => setFilterOpen(false)}
        onApply={applyFilters}
        onClear={clearFilters}
        title="Filtros"
      >
        <FilterField label="CD">
          <FalkSelect
            value={String(draftCd)}
            onChange={v => setDraftCd(v === '' ? '' : Number(v))}
            options={[
              { value: '', label: 'Todos CDs' },
              ...cds.map(c => ({
                value: String(c.id),
                label: `${c.nome} — ${c.cidade}/${c.uf.toUpperCase()}`,
              })),
            ]}
          />
        </FilterField>
        <FilterField label="Operação">
          <FalkSelect
            value={draftOp}
            onChange={v => setDraftOp(v)}
            options={[
              { value: '', label: 'Todas as operações' },
              { value: 'seg-sab', label: 'Segunda a sábado' },
              { value: 'sexta', label: 'Apenas sexta-feira' },
              { value: 'sabado', label: 'Apenas sábado' },
              { value: 'domingo', label: 'Inclui domingo' },
            ]}
          />
        </FilterField>
        <FilterField label="Zona">
          <FalkSelect
            value={draftZona}
            onChange={v => setDraftZona(v)}
            options={[
              { value: '', label: 'Todas as zonas' },
              ...zonas.map(z => ({
                value: z.nome.toLowerCase(),
                label: z.nome,
              })),
            ]}
          />
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
