export type EntryType = 'income' | 'expense'
export type EntryStatus = 'paid' | 'planned'
export type PaymentMethod = 'pix' | 'debit' | 'credit' | 'cash' | 'boleto' | 'transfer'
export type InvestmentKind = 'physical' | 'service'

export type Person = {
  id: string
  name: string
  color: string
}

export type Entry = {
  id: string
  personId: string
  description: string
  amount: number
  type: EntryType
  category: string
  method: PaymentMethod
  status: EntryStatus
  date: string
  createdAt: string
  quantity?: number
  unitValue?: number
  investmentId?: string
}

export type Investment = {
  id: string
  personId: string
  name: string
  kind: InvestmentKind
  investedAmount: number
  totalUnits?: number
  createdAt: string
}

export type FinanceData = {
  people: Person[]
  entries: Entry[]
  investments: Investment[]
  activePersonId: string
}
