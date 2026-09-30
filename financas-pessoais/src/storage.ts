import { initialData } from './data'
import type { FinanceData } from './types'

const KEY = 'finanz-v1'
const LEGACY_KEY = 'clara-financas-v1'

export function normalizeData(value: unknown): FinanceData | null {
  const parsed = value as Partial<FinanceData> | null
  if (parsed && Array.isArray(parsed.people) && Array.isArray(parsed.entries) && typeof parsed.activePersonId === 'string') {
    return { ...parsed, investments: Array.isArray(parsed.investments) ? parsed.investments : [] } as FinanceData
  }
  return null
}

export function loadSavedData(): FinanceData | null {
  try {
    const saved = localStorage.getItem(KEY) ?? localStorage.getItem(LEGACY_KEY)
    if (saved) return normalizeData(JSON.parse(saved))
  } catch { /* usa dados iniciais */ }
  return null
}

export function loadLegacyData(): FinanceData | null {
  try {
    const saved = localStorage.getItem(LEGACY_KEY)
    return saved ? normalizeData(JSON.parse(saved)) : null
  } catch {
    return null
  }
}

export function loadData(): FinanceData {
  const saved = loadSavedData()
  if (saved) return saved
  return initialData
}

export function saveData(data: FinanceData) {
  localStorage.setItem(KEY, JSON.stringify(data))
}
