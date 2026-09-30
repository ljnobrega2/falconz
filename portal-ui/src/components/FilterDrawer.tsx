// FilterDrawer — painel lateral direito (#28) que concentra TODOS os filtros de uma
// tela com tabela, removendo-os do layout inline. Marca FALK (azul #1E6FF2, ZERO laranja).
//
// Padrão único reutilizável (Orders, Motoboy e futuras telas):
//   - recebe a DEFINIÇÃO declarativa dos campos (label + tipo: select/text/date/daterange)
//   - mantém um RASCUNHO interno (draft) — o usuário edita à vontade SEM aplicar;
//   - "Filtrar" aplica o rascunho de uma só vez (onApply); "Limpar" zera tudo.
//   - reabre sempre semeando o rascunho a partir dos valores APLICADOS (props.values),
//     pra não "perder" o que já estava em vigor.
//
// Reusa components/Drawer.tsx como casca (slide da direita, ESC, clique-fora, foco).
// NÃO edita Drawer.tsx. As cores vêm de var(--szv2-*) (cascateiam do tema .sz-dashboard-v2).
import { useEffect, useState } from 'react'
import Drawer from './Drawer'
import FalkSelect from './FalkSelect'
import FalkDatePicker from './FalkDatePicker'

// ── Modelo de valores ────────────────────────────────────────────────────────────
// Todos os filtros são strings (inclusive datas YYYY-MM-DD) — mantém o modelo trivial
// e cada campo vira um chip removível por chave. Range de data = DUAS chaves planas.
export type FilterValues = Record<string, string>

// Opção de um <select> — value (cru, ex.: slug de status) + label (exibição legível).
export interface FilterOption {
  value: string
  label: string
}

// Definição declarativa de UM campo de filtro.
export type FilterFieldDef =
  | { key: string; label: string; type: 'select'; options: FilterOption[]; placeholder?: string }
  | { key: string; label: string; type: 'text'; placeholder?: string }
  | { key: string; label: string; type: 'date'; placeholder?: string }
  // daterange ocupa DUAS chaves planas (fromKey/toKey) — cada bound é seu próprio chip.
  | { key: string; label: string; type: 'daterange'; fromKey: string; toKey: string }

export interface FilterDrawerProps {
  open: boolean
  onClose: () => void
  /** Definição dos campos exibidos no painel. */
  fields: FilterFieldDef[]
  /** Valores APLICADOS (em vigor). Semeiam o rascunho a cada abertura. */
  values: FilterValues
  /** Aplica o rascunho de uma só vez (botão "Filtrar"). */
  onApply: (values: FilterValues) => void
  /** Limpa todos os filtros (botão "Limpar"). */
  onClear: () => void
  /** Título do painel. Default "Filtros". */
  title?: string
  /** Largura do painel (px). Default 420. */
  width?: number
}

export default function FilterDrawer({
  open,
  onClose,
  fields,
  values,
  onApply,
  onClear,
  title = 'Filtros',
  width = 420,
}: FilterDrawerProps) {
  // Rascunho interno: o usuário edita aqui livremente; só vira "aplicado" no "Filtrar".
  const [draft, setDraft] = useState<FilterValues>(values)

  // A cada abertura, semeia o rascunho com os valores APLICADOS atuais.
  useEffect(() => {
    if (open) setDraft(values)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  function setField(key: string, value: string) {
    setDraft(d => ({ ...d, [key]: value }))
  }

  function handleApply() {
    onApply(draft)
    onClose()
  }

  function handleClear() {
    // Zera o rascunho localmente (todas as chaves conhecidas → '') e propaga.
    const cleared: FilterValues = {}
    fields.forEach(f => {
      if (f.type === 'daterange') {
        cleared[f.fromKey] = ''
        cleared[f.toKey] = ''
      } else {
        cleared[f.key] = ''
      }
    })
    setDraft(cleared)
    onClear()
  }

  return (
    <Drawer open={open} onClose={onClose} title={title} width={width} ariaLabel="Filtros">
      <form
        onSubmit={e => {
          e.preventDefault()
          handleApply()
        }}
        style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14, flex: 1 }}>
          {fields.map(f => (
            <div key={f.key} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <label className="szv2-label" htmlFor={`szv2-fd-${f.key}`}>
                {f.label}
              </label>

              {f.type === 'select' && (
                <FalkSelect
                  id={`szv2-fd-${f.key}`}
                  value={draft[f.key] || ''}
                  onChange={v => setField(f.key, v)}
                  placeholder={f.placeholder || 'Todos'}
                  options={[
                    { value: '', label: f.placeholder || 'Todos' },
                    ...f.options.map(o => ({ value: o.value, label: o.label })),
                  ]}
                />
              )}

              {f.type === 'text' && (
                <input
                  id={`szv2-fd-${f.key}`}
                  type="search"
                  className="szv2-input"
                  value={draft[f.key] || ''}
                  onChange={e => setField(f.key, e.target.value)}
                  placeholder={f.placeholder || ''}
                  autoComplete="new-password"
                  style={{ width: '100%' }}
                />
              )}

              {f.type === 'date' && (
                <FalkDatePicker
                  id={`szv2-fd-${f.key}`}
                  value={draft[f.key] || ''}
                  onChange={v => setField(f.key, v)}
                  placeholder={f.placeholder || 'dd/mm/aaaa'}
                />
              )}

              {f.type === 'daterange' && (
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <FalkDatePicker
                    id={`szv2-fd-${f.key}`}
                    value={draft[f.fromKey] || ''}
                    onChange={v => setField(f.fromKey, v)}
                    aria-label={`${f.label} — de`}
                    style={{ flex: 1, minWidth: 0 }}
                  />
                  <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>até</span>
                  <FalkDatePicker
                    value={draft[f.toKey] || ''}
                    onChange={v => setField(f.toKey, v)}
                    aria-label={`${f.label} — até`}
                    style={{ flex: 1, minWidth: 0 }}
                  />
                </div>
              )}
            </div>
          ))}
        </div>

        {/* Rodapé fixo — "Filtrar" (CTA azul brand) + "Limpar" (secundário). */}
        <div
          style={{
            display: 'flex',
            gap: 8,
            paddingTop: 14,
            borderTop: '1px solid var(--szv2-divider)',
            flexShrink: 0,
          }}
        >
          <button type="submit" className="szv2-btn szv2-btn-brand" style={{ flex: 1 }}>
            Filtrar
          </button>
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={handleClear}>
            Limpar
          </button>
        </div>
      </form>
    </Drawer>
  )
}
