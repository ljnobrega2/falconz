import type { Entry, FinanceData } from './types'

const iso = (monthOffset: number, day: number) => {
  const now = new Date()
  return new Date(now.getFullYear(), now.getMonth() + monthOffset, day, 12).toISOString().slice(0, 10)
}

const entry = (id: string, description: string, amount: number, type: Entry['type'], category: string, method: Entry['method'], status: Entry['status'], date: string): Entry => ({
  id, personId: 'lucas', description, amount, type, category, method, status, date, createdAt: new Date().toISOString(),
})

export const initialData: FinanceData = {
  activePersonId: 'lucas',
  investments: [],
  people: [
    { id: 'lucas', name: 'Lucas', color: '#18664b' },
    { id: 'casa', name: 'Casa', color: '#cf6b3e' },
  ],
  entries: [
    entry('1', 'Salário', 7200, 'income', 'Salário', 'transfer', 'paid', iso(0, 5)),
    entry('2', 'Freelance', 1800, 'income', 'Renda extra', 'pix', 'planned', iso(0, 24)),
    entry('3', 'Aluguel', 2100, 'expense', 'Moradia', 'pix', 'paid', iso(0, 8)),
    entry('4', 'Supermercado', 846.5, 'expense', 'Alimentação', 'credit', 'paid', iso(0, 11)),
    entry('5', 'Plano de saúde', 489.9, 'expense', 'Saúde', 'boleto', 'planned', iso(0, 21)),
    entry('6', 'Academia', 129.9, 'expense', 'Saúde', 'credit', 'paid', iso(0, 10)),
    entry('7', 'Internet', 119.9, 'expense', 'Casa', 'debit', 'planned', iso(0, 18)),
    entry('8', 'Restaurantes', 328.4, 'expense', 'Lazer', 'credit', 'paid', iso(0, 13)),
    entry('9', 'Salário', 7200, 'income', 'Salário', 'transfer', 'planned', iso(1, 5)),
    entry('10', 'Aluguel', 2100, 'expense', 'Moradia', 'pix', 'planned', iso(1, 8)),
    entry('11', 'Curso online', 299, 'expense', 'Educação', 'credit', 'planned', iso(1, 15)),
  ],
}

export const EXPENSE_CATEGORIES = ['Moradia', 'Alimentação', 'Transporte', 'Saúde', 'Lazer', 'Educação', 'Casa', 'Assinaturas', 'Investimento', 'Outros']
export const INCOME_CATEGORIES = ['Salário', 'Renda extra', 'Retorno de investimento', 'Investimentos', 'Reembolso', 'Outros']
