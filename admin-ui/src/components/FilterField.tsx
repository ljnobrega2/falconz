// FilterField — ponto de import dos helpers de campo de filtro.
//
// A definição canônica de FilterField + estilos vive em FilterTopPanel.tsx
// (fonte única). Este módulo re-exporta para que telas de filtro possam
// importar daqui e para concentrar o select tematizado FALK num só lugar.
//
// Regra select-temático: filtros usam <FalkSelect> (dropdown no padrão do
// site, claro/escuro). Selects nativos remanescentes usam `filterSelectStyle`
// (box tematizado + chevron azul FALK). Datas usam `filterDateStyle`.

export {
  FilterField,
  filterInputStyle,
  filterSelectStyle,
  filterDateStyle,
  ActiveFilterChips,
  FalkSelect,
} from './FilterTopPanel'

export type { ActiveChip } from './FilterTopPanel'
export type { FalkSelectOption } from './FalkSelect'

// Re-export default para conveniência: <FilterField> também acessível como
// default deste módulo.
export { FilterField as default } from './FilterTopPanel'
