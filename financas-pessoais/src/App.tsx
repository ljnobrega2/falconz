import { ChangeEvent, FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowDownLeft, ArrowUpRight, BarChart3, CalendarDays, Check,
  ChevronDown, CircleDollarSign, LayoutDashboard, Menu, MoreHorizontal,
  Cloud, Download, LogOut, Package, Pencil, Plus, ReceiptText, Search, ShoppingBag, Trash2, Upload, UserPlus, Users, WalletCards, Wrench, X,
} from 'lucide-react'
import { EXPENSE_CATEGORIES, INCOME_CATEGORIES } from './data'
import { loadData, loadLegacyData, normalizeData, saveData } from './storage'
import { accountIdFromCode, CloudConflictError, forgetAccount, loadCloudData, rememberAccount, saveCloudData, savedAccountId } from './sync'
import type { Entry, EntryStatus, EntryType, FinanceData, Investment, InvestmentKind, PaymentMethod } from './types'

type View = 'dashboard' | 'entries' | 'investments' | 'people'
type Modal = 'entry' | 'person' | 'investment' | 'sale' | null

const money = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' })
const monthLabel = new Intl.DateTimeFormat('pt-BR', { month: 'long', year: 'numeric' })
const shortMonth = new Intl.DateTimeFormat('pt-BR', { month: 'short' })
const dateLabel = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: 'short' })

const methodLabel: Record<PaymentMethod, string> = {
  pix: 'Pix', debit: 'Débito', credit: 'Crédito', cash: 'Dinheiro', boleto: 'Boleto', transfer: 'Transferência',
}

const categoryColors = ['#18664b', '#e08a52', '#6c74c9', '#d9aa36', '#4c91ad', '#bd6174', '#87915a', '#795a91']

function monthKey(date: Date | string) {
  const d = typeof date === 'string' ? new Date(`${date}T12:00:00`) : date
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function uid() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function parseMoney(value: string) {
  const normalized = value.trim().replace(/\s/g, '').replace(/^R\$/, '')
  const decimal = normalized.includes(',') ? normalized.replace(/\./g, '').replace(',', '.') : normalized
  return Number(decimal)
}

function investmentStats(investment: Investment, entries: Entry[]) {
  const sales = entries.filter(item => item.investmentId === investment.id && item.type === 'income' && item.status === 'paid')
  const reservedUnits = entries.filter(item => item.investmentId === investment.id && item.type === 'income').reduce((sum, item) => sum + (item.quantity ?? 1), 0)
  const soldUnits = sales.reduce((sum, item) => sum + (item.quantity ?? 1), 0)
  const revenue = sales.reduce((sum, item) => sum + item.amount, 0)
  const totalUnits = investment.totalUnits ?? 0
  const allocatedCost = investment.kind === 'physical' && totalUnits > 0
    ? investment.investedAmount / totalUnits * Math.min(soldUnits, totalUnits)
    : investment.investedAmount
  const profit = revenue - allocatedCost
  return {
    soldUnits,
    revenue,
    allocatedCost,
    profit,
    margin: revenue > 0 ? profit / revenue * 100 : 0,
    cashResult: revenue - investment.investedAmount,
    remainingUnits: investment.kind === 'physical' ? Math.max(totalUnits - reservedUnits, 0) : undefined,
  }
}

export default function App() {
  const [data, setData] = useState<FinanceData>(loadData)
  const [accountId, setAccountId] = useState<string | null>(savedAccountId)
  const [cloudReady, setCloudReady] = useState(false)
  const [syncState, setSyncState] = useState<'loading' | 'saved' | 'saving' | 'error'>('loading')
  const saveQueue = useRef(Promise.resolve())
  const cloudVersion = useRef(0)
  const lastSavedJson = useRef('')
  const legacyData = useRef(loadLegacyData())
  const [view, setView] = useState<View>('dashboard')
  const [modal, setModal] = useState<Modal>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [startMonth, setStartMonth] = useState(monthKey(new Date()))
  const [endMonth, setEndMonth] = useState(monthKey(new Date()))
  const [query, setQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState<'all' | EntryType>('all')
  const [saleInvestmentId, setSaleInvestmentId] = useState<string | null>(null)

  useEffect(() => saveData(data), [data])
  useEffect(() => {
    if (!accountId) { setCloudReady(false); return }
    let cancelled = false
    setSyncState('loading')
    loadCloudData(accountId).then(async remote => {
      if (cancelled) return
      if (remote) {
        const legacy = legacyData.current
        if (legacy && legacy.entries.length > remote.data.entries.length) {
          const saved = await saveCloudData(accountId, legacy, remote.version)
          cloudVersion.current = saved.version
          lastSavedJson.current = JSON.stringify(legacy)
          setData(legacy)
          legacyData.current = null
        } else {
          cloudVersion.current = remote.version
          lastSavedJson.current = JSON.stringify(remote.data)
          setData(remote.data)
        }
      } else {
        const local = loadData()
        const saved = await saveCloudData(accountId, local, 0)
        cloudVersion.current = saved.version
        lastSavedJson.current = JSON.stringify(local)
        setData(local)
      }
      if (!cancelled) { setCloudReady(true); setSyncState('saved') }
    }).catch(() => { if (!cancelled) setSyncState('error') })
    return () => { cancelled = true }
  }, [accountId])
  useEffect(() => {
    if (!accountId || !cloudReady) return
    const serialized = JSON.stringify(data)
    if (serialized === lastSavedJson.current) return
    setSyncState('saving')
    saveQueue.current = saveQueue.current
      .then(async () => {
        const saved = await saveCloudData(accountId, data, cloudVersion.current)
        cloudVersion.current = saved.version
        lastSavedJson.current = serialized
        setSyncState('saved')
      })
      .catch(error => {
        if (error instanceof CloudConflictError && error.latest) {
          cloudVersion.current = error.latest.version
          lastSavedJson.current = JSON.stringify(error.latest.data)
          setData(error.latest.data)
          setSyncState('saved')
          window.alert('Outra aba tinha dados mais recentes. A versão mais nova foi carregada e nenhuma revisão anterior foi apagada.')
          return
        }
        setSyncState('error')
      })
  }, [accountId, cloudReady, data])

  const activePerson = data.people.find(person => person.id === data.activePersonId) ?? data.people[0]
  const personEntries = useMemo(() => data.entries.filter(item => item.personId === activePerson?.id), [data.entries, activePerson?.id])
  const personInvestments = useMemo(() => data.investments.filter(item => item.personId === activePerson?.id), [data.investments, activePerson?.id])
  const periodEntries = useMemo(() => personEntries.filter(item => {
    const itemMonth = monthKey(item.date)
    return itemMonth >= startMonth && itemMonth <= endMonth
  }), [personEntries, startMonth, endMonth])
  const income = periodEntries.filter(item => item.type === 'income').reduce((sum, item) => sum + item.amount, 0)
  const expenses = periodEntries.filter(item => item.type === 'expense').reduce((sum, item) => sum + item.amount, 0)
  const paidIncome = periodEntries.filter(item => item.type === 'income' && item.status === 'paid').reduce((sum, item) => sum + item.amount, 0)
  const paidExpenses = periodEntries.filter(item => item.type === 'expense' && item.status === 'paid').reduce((sum, item) => sum + item.amount, 0)
  const balance = paidIncome - paidExpenses
  const projected = income - expenses

  const categoryTotals = useMemo(() => {
    const grouped = new Map<string, number>()
    periodEntries.filter(item => item.type === 'expense').forEach(item => grouped.set(item.category, (grouped.get(item.category) ?? 0) + item.amount))
    return [...grouped.entries()].map(([name, value]) => ({ name, value })).sort((a, b) => b.value - a.value)
  }, [periodEntries])

  const forecast = useMemo(() => Array.from({ length: 6 }, (_, index) => {
    const base = new Date()
    base.setDate(1)
    base.setMonth(base.getMonth() + index)
    const key = monthKey(base)
    const entries = personEntries.filter(item => monthKey(item.date) === key)
    return {
      key,
      label: shortMonth.format(base).replace('.', ''),
      income: entries.filter(item => item.type === 'income').reduce((sum, item) => sum + item.amount, 0),
      expense: entries.filter(item => item.type === 'expense').reduce((sum, item) => sum + item.amount, 0),
    }
  }), [personEntries])

  const filteredEntries = useMemo(() => personEntries
    .filter(item => typeFilter === 'all' || item.type === typeFilter)
    .filter(item => `${item.description} ${item.category}`.toLowerCase().includes(query.toLowerCase()))
    .sort((a, b) => b.date.localeCompare(a.date)), [personEntries, query, typeFilter])

  const setActivePerson = (id: string) => setData(current => ({ ...current, activePersonId: id }))
  const addEntry = (entry: Entry) => setData(current => ({ ...current, entries: [entry, ...current.entries] }))
  const removeEntry = (id: string) => setData(current => ({ ...current, entries: current.entries.filter(item => item.id !== id) }))
  const toggleStatus = (id: string) => setData(current => ({ ...current, entries: current.entries.map(item => item.id === id ? { ...item, status: item.status === 'paid' ? 'planned' : 'paid' } : item) }))
  const moveEntry = (id: string, personId: string) => setData(current => ({ ...current, entries: current.entries.map(item => item.id === id ? { ...item, personId } : item) }))
  const renamePerson = (id: string, name: string) => setData(current => ({ ...current, people: current.people.map(person => person.id === id ? { ...person, name } : person) }))
  const addInvestment = (investment: Investment, date: string, method: PaymentMethod, status: EntryStatus) => setData(current => ({
    ...current,
    investments: [investment, ...current.investments],
    entries: [{
      id: uid(), personId: investment.personId, description: `Investimento · ${investment.name}`,
      amount: investment.investedAmount, type: 'expense', category: 'Investimento', method, status, date,
      createdAt: new Date().toISOString(), investmentId: investment.id,
    }, ...current.entries],
  }))
  const addInvestmentSale = (investment: Investment, sale: { quantity: number; revenue: number; date: string; method: PaymentMethod; status: EntryStatus }) => setData(current => ({
    ...current,
    entries: [{
      id: uid(), personId: investment.personId, description: `Venda · ${investment.name}`,
      amount: sale.revenue, type: 'income', category: investment.kind === 'physical' ? 'Venda de produto' : 'Serviço',
      method: sale.method, status: sale.status, date: sale.date, createdAt: new Date().toISOString(),
      quantity: sale.quantity, unitValue: sale.revenue / sale.quantity, investmentId: investment.id,
    }, ...current.entries],
  }))
  const removeInvestment = (id: string) => setData(current => ({
    ...current,
    investments: current.investments.filter(item => item.id !== id),
    entries: current.entries.filter(item => item.investmentId !== id),
  }))

  const exportData = () => {
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `finanz-backup-${new Date().toISOString().slice(0, 10)}.json`
    link.click()
    URL.revokeObjectURL(url)
  }
  const importData = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    try {
      const imported = normalizeData(JSON.parse(await file.text()))
      if (!imported) throw new Error('invalid')
      if (window.confirm(`Importar ${imported.entries.length} lançamentos e substituir a visualização atual? Uma revisão dos dados atuais será preservada.`)) setData(imported)
    } catch {
      window.alert('Esse arquivo não é um backup válido do Finanz/Clara.')
    }
  }

  if (!accountId) return <AccessGate onAccess={async code => {
    const id = await accountIdFromCode(code)
    rememberAccount(id)
    setAccountId(id)
  }} />
  if (!cloudReady) return <LoadingCloud state={syncState} onExit={() => { forgetAccount(); setAccountId(null) }} />

  if (!activePerson) return <EmptyStart onCreate={() => setModal('person')} modal={modal} onClose={() => setModal(null)} onSave={person => setData({ people: [person], entries: [], investments: [], activePersonId: person.id })} />

  return (
    <div className="app-shell">
      <aside className={`sidebar ${sidebarOpen ? 'open' : ''}`}>
        <div className="brand"><span className="brand-mark"><CircleDollarSign /></span><span>finanz</span><button className="close-mobile" onClick={() => setSidebarOpen(false)}><X /></button></div>
        <div className="person-picker-wrap">
          <label>Visualizando</label>
          <div className="person-picker">
            <span className="avatar" style={{ background: activePerson.color }}>{activePerson.name.slice(0, 1).toUpperCase()}</span>
            <select value={activePerson.id} onChange={event => setActivePerson(event.target.value)} aria-label="Selecionar pessoa">
              {data.people.map(person => <option key={person.id} value={person.id}>{person.name}</option>)}
            </select>
            <ChevronDown size={16} />
          </div>
        </div>
        <nav>
          <NavButton active={view === 'dashboard'} icon={<LayoutDashboard />} label="Visão geral" onClick={() => { setView('dashboard'); setSidebarOpen(false) }} />
          <NavButton active={view === 'entries'} icon={<ReceiptText />} label="Lançamentos" onClick={() => { setView('entries'); setSidebarOpen(false) }} />
          <NavButton active={view === 'investments'} icon={<ShoppingBag />} label="Investimentos" onClick={() => { setView('investments'); setSidebarOpen(false) }} />
          <NavButton active={view === 'people'} icon={<Users />} label="Pessoas" onClick={() => { setView('people'); setSidebarOpen(false) }} />
        </nav>
        <div className="sidebar-tip">
          <span><WalletCards /></span>
          <strong>Tudo no seu ritmo</strong>
          <p>Seus dados ficam somente neste navegador.</p>
        </div>
      </aside>
      {sidebarOpen && <button className="backdrop" onClick={() => setSidebarOpen(false)} aria-label="Fechar menu" />}

      <main>
        <header className="topbar">
          <button className="menu-button" onClick={() => setSidebarOpen(true)}><Menu /></button>
          <div>
            <p>{view === 'dashboard' ? 'Sua vida financeira' : view === 'entries' ? 'Histórico financeiro' : view === 'investments' ? 'Produtos e serviços' : 'Perfis financeiros'}</p>
            <h1>{view === 'dashboard' ? `Olá, ${activePerson.name.split(' ')[0]}!` : view === 'entries' ? 'Lançamentos' : view === 'investments' ? 'Investimentos' : 'Pessoas'}</h1>
          </div>
          <button className="primary-button" onClick={() => setModal(view === 'investments' ? 'investment' : 'entry')}><Plus /> <span>{view === 'investments' ? 'Novo investimento' : 'Novo lançamento'}</span></button>
        </header>
        <div className={`sync-indicator ${syncState}`}><Cloud /> {syncState === 'saved' ? 'Salvo na nuvem' : syncState === 'saving' ? 'Salvando…' : syncState === 'error' ? 'Falha ao salvar' : 'Carregando…'}</div>

        {view === 'dashboard' && <Dashboard
          startMonth={startMonth} endMonth={endMonth}
          setStartMonth={value => { setStartMonth(value); if (value > endMonth) setEndMonth(value) }}
          setEndMonth={value => { setEndMonth(value); if (value < startMonth) setStartMonth(value) }}
          balance={balance} projected={projected}
          income={income} expenses={expenses} entries={periodEntries} categoryTotals={categoryTotals} forecast={forecast}
          onShowEntries={() => setView('entries')} onToggleStatus={toggleStatus}
        />}
        {view === 'entries' && <EntriesView entries={filteredEntries} people={data.people} query={query} setQuery={setQuery} typeFilter={typeFilter} setTypeFilter={setTypeFilter} onToggleStatus={toggleStatus} onMove={moveEntry} onRemove={removeEntry} />}
        {view === 'investments' && <InvestmentsView investments={personInvestments} entries={personEntries} onAdd={() => setModal('investment')} onSale={id => { setSaleInvestmentId(id); setModal('sale') }} onRemove={removeInvestment} />}
        {view === 'people' && <PeopleView data={data} activePersonId={activePerson.id} onActive={setActivePerson} onAdd={() => setModal('person')} onRename={renamePerson} onExport={exportData} onImport={importData} onExit={() => { forgetAccount(); setAccountId(null) }} onRemove={id => setData(current => {
          const people = current.people.filter(person => person.id !== id)
          return { people, entries: current.entries.filter(item => item.personId !== id), investments: current.investments.filter(item => item.personId !== id), activePersonId: current.activePersonId === id ? people[0]?.id ?? '' : current.activePersonId }
        })} />}
      </main>

      {modal === 'entry' && <EntryModal personId={activePerson.id} onClose={() => setModal(null)} onSave={entry => { addEntry(entry); setModal(null) }} />}
      {modal === 'person' && <PersonModal onClose={() => setModal(null)} onSave={person => { setData(current => ({ ...current, people: [...current.people, person], activePersonId: person.id })); setModal(null) }} />}
      {modal === 'investment' && <InvestmentModal personId={activePerson.id} onClose={() => setModal(null)} onSave={(investment, date, method, status) => { addInvestment(investment, date, method, status); setModal(null) }} />}
      {modal === 'sale' && saleInvestmentId && (() => {
        const investment = data.investments.find(item => item.id === saleInvestmentId)
        if (!investment) return null
        const sold = data.entries.filter(item => item.investmentId === investment.id && item.type === 'income').reduce((sum, item) => sum + (item.quantity ?? 1), 0)
        return <SaleModal investment={investment} soldUnits={sold} onClose={() => { setModal(null); setSaleInvestmentId(null) }} onSave={sale => { addInvestmentSale(investment, sale); setModal(null); setSaleInvestmentId(null) }} />
      })()}
    </div>
  )
}

function NavButton({ active, icon, label, onClick }: { active: boolean; icon: JSX.Element; label: string; onClick: () => void }) {
  return <button className={active ? 'active' : ''} onClick={onClick}>{icon}<span>{label}</span></button>
}

type DashboardProps = {
  startMonth: string; endMonth: string; setStartMonth: (value: string) => void; setEndMonth: (value: string) => void; balance: number; projected: number; income: number; expenses: number
  entries: Entry[]; categoryTotals: { name: string; value: number }[]; forecast: { key: string; label: string; income: number; expense: number }[]
  onShowEntries: () => void; onToggleStatus: (id: string) => void
}

function Dashboard({ startMonth, endMonth, setStartMonth, setEndMonth, balance, projected, income, expenses, entries, categoryTotals, forecast, onShowEntries, onToggleStatus }: DashboardProps) {
  const maxForecast = Math.max(...forecast.flatMap(item => [item.income, item.expense]), 1)
  const totalCategories = categoryTotals.reduce((sum, item) => sum + item.value, 0)
  let cursor = 0
  const gradient = categoryTotals.length ? `conic-gradient(${categoryTotals.map((item, index) => {
    const start = cursor
    cursor += item.value / totalCategories * 100
    return `${categoryColors[index % categoryColors.length]} ${start}% ${cursor}%`
  }).join(',')})` : '#edf0eb'
  const upcoming = entries.filter(item => item.status === 'planned').sort((a, b) => a.date.localeCompare(b.date)).slice(0, 5)

  return <div className="content dashboard-content">
    <div className="section-heading">
      <div><h2>Resumo do período</h2><p>Acompanhe o realizado e o que ainda está por vir.</p></div>
      <div className="period-filter">
        <CalendarDays />
        <label><span>De</span><input type="month" value={startMonth} onChange={event => setStartMonth(event.target.value)} /></label>
        <i>até</i>
        <label><span>Até</span><input type="month" value={endMonth} onChange={event => setEndMonth(event.target.value)} /></label>
      </div>
    </div>
    <section className="metric-grid">
      <Metric label="Saldo atual" value={balance} detail="Recebido menos o que já saiu" tone="dark" icon={<WalletCards />} />
      <Metric label="Previsão do período" value={projected} detail={projected >= 0 ? 'O período fecha no positivo' : 'Atenção ao fechamento do período'} tone={projected >= 0 ? 'green' : 'orange'} icon={<BarChart3 />} />
      <Metric label="Recebimentos" value={income} detail={`${entries.filter(item => item.type === 'income').length} lançamentos no período`} tone="plain" icon={<ArrowDownLeft />} />
      <Metric label="Despesas" value={expenses} detail={`${entries.filter(item => item.type === 'expense').length} lançamentos no período`} tone="plain" icon={<ArrowUpRight />} />
    </section>
    <section className="dashboard-grid">
      <article className="card forecast-card">
        <div className="card-title"><div><h3>Previsão dos próximos meses</h3><p>Receitas e despesas já cadastradas</p></div><div className="legend"><span className="income-dot" />Entradas <span className="expense-dot" />Saídas</div></div>
        <div className="bar-chart">
          {forecast.map(item => <div className="bar-group" key={item.key}>
            <div className="bars"><span className="income-bar" style={{ height: `${Math.max(item.income / maxForecast * 100, item.income ? 5 : 0)}%` }} title={money.format(item.income)} /><span className="expense-bar" style={{ height: `${Math.max(item.expense / maxForecast * 100, item.expense ? 5 : 0)}%` }} title={money.format(item.expense)} /></div>
            <small>{item.label}</small>
          </div>)}
        </div>
      </article>
      <article className="card categories-card">
        <div className="card-title"><div><h3>O que mais pesa</h3><p>Despesas por categoria</p></div><MoreHorizontal /></div>
        {categoryTotals.length ? <div className="category-body">
          <div className="donut" style={{ background: gradient }}><span><small>Total</small>{money.format(totalCategories)}</span></div>
          <div className="category-list">{categoryTotals.slice(0, 5).map((item, index) => <div key={item.name}><span className="category-dot" style={{ background: categoryColors[index % categoryColors.length] }} /><span>{item.name}</span><strong>{Math.round(item.value / totalCategories * 100)}%</strong></div>)}</div>
        </div> : <EmptyCard text="Cadastre despesas neste mês para ver a divisão por categoria." />}
      </article>
      <article className="card upcoming-card">
        <div className="card-title"><div><h3>Próximos lançamentos</h3><p>O que ainda está previsto no período</p></div><button className="text-button" onClick={onShowEntries}>Ver todos</button></div>
        {upcoming.length ? <div className="transaction-list">{upcoming.map(item => <TransactionRow key={item.id} entry={item} onToggleStatus={onToggleStatus} />)}</div> : <EmptyCard text="Tudo certo por aqui. Não há lançamentos pendentes neste mês." />}
      </article>
    </section>
  </div>
}

function Metric({ label, value, detail, tone, icon }: { label: string; value: number; detail: string; tone: string; icon: JSX.Element }) {
  return <article className={`metric ${tone}`}><div className="metric-top"><span>{label}</span><i>{icon}</i></div><strong>{money.format(value)}</strong><small>{detail}</small></article>
}

function TransactionRow({ entry, onToggleStatus, onRemove, people, onMove }: { entry: Entry; onToggleStatus: (id: string) => void; onRemove?: (id: string) => void; people?: FinanceData['people']; onMove?: (id: string, personId: string) => void }) {
  return <div className="transaction-row">
    <span className={`transaction-icon ${entry.type}`}>{entry.type === 'income' ? <ArrowDownLeft /> : <ArrowUpRight />}</span>
    <div className="transaction-name"><strong>{entry.description}</strong><small>{entry.category} · {methodLabel[entry.method]}</small></div>
    <time>{dateLabel.format(new Date(`${entry.date}T12:00:00`))}</time>
    <button className={`status ${entry.status}`} onClick={() => onToggleStatus(entry.id)}>{entry.status === 'paid' ? <><Check /> Realizado</> : 'Previsto'}</button>
    <strong className={entry.type}>{entry.type === 'expense' ? '−' : '+'}{money.format(entry.amount)}</strong>
    {people && onMove && !entry.investmentId && <select className="person-move" value={entry.personId} onChange={event => onMove(entry.id, event.target.value)} title="Mover lançamento para outra pessoa" aria-label={`Pessoa responsável por ${entry.description}`}>{people.map(person => <option key={person.id} value={person.id}>{person.name}</option>)}</select>}
    {onRemove && (!entry.investmentId || entry.type === 'income') && <button className="icon-button danger" onClick={() => { if (window.confirm('Excluir este lançamento?')) onRemove(entry.id) }} aria-label="Excluir"><Trash2 /></button>}
  </div>
}

function EntriesView({ entries, people, query, setQuery, typeFilter, setTypeFilter, onToggleStatus, onMove, onRemove }: { entries: Entry[]; people: FinanceData['people']; query: string; setQuery: (v: string) => void; typeFilter: 'all' | EntryType; setTypeFilter: (v: 'all' | EntryType) => void; onToggleStatus: (id: string) => void; onMove: (id: string, personId: string) => void; onRemove: (id: string) => void }) {
  return <div className="content">
    <div className="section-heading"><div><h2>Todos os lançamentos</h2><p>{entries.length} itens encontrados</p></div></div>
    <article className="card entries-card">
      <div className="filters"><label className="search"><Search /><input value={query} onChange={event => setQuery(event.target.value)} placeholder="Buscar por nome ou categoria" /></label><div className="segmented">{([['all', 'Todos'], ['income', 'Receitas'], ['expense', 'Despesas']] as const).map(([value, label]) => <button className={typeFilter === value ? 'active' : ''} onClick={() => setTypeFilter(value)} key={value}>{label}</button>)}</div></div>
      {entries.length ? <div className="transaction-list full">{entries.map(item => <TransactionRow key={item.id} entry={item} onToggleStatus={onToggleStatus} people={people} onMove={onMove} onRemove={onRemove} />)}</div> : <EmptyCard text="Nenhum lançamento encontrado com esses filtros." />}
    </article>
  </div>
}

function InvestmentsView({ investments, entries, onAdd, onSale, onRemove }: { investments: Investment[]; entries: Entry[]; onAdd: () => void; onSale: (id: string) => void; onRemove: (id: string) => void }) {
  const rows = investments.map(investment => ({ investment, stats: investmentStats(investment, entries) }))
  const invested = rows.reduce((sum, row) => sum + row.investment.investedAmount, 0)
  const revenue = rows.reduce((sum, row) => sum + row.stats.revenue, 0)
  const profit = rows.reduce((sum, row) => sum + row.stats.profit, 0)
  const allocatedCost = rows.reduce((sum, row) => sum + row.stats.allocatedCost, 0)
  const margin = revenue > 0 ? profit / revenue * 100 : 0

  return <div className="content investments-content">
    <div className="section-heading">
      <div><h2>Rentabilidade</h2><p>Acompanhe o capital aplicado e as vendas realizadas.</p></div>
      <button className="secondary-button" onClick={onAdd}><Plus /> Adicionar investimento</button>
    </div>
    <section className="metric-grid investment-metrics">
      <Metric label="Capital investido" value={invested} detail={`${investments.length} ${investments.length === 1 ? 'investimento' : 'investimentos'}`} tone="dark" icon={<WalletCards />} />
      <Metric label="Faturamento realizado" value={revenue} detail="Somente vendas marcadas como realizadas" tone="plain" icon={<ArrowDownLeft />} />
      <Metric label="Lucro bruto" value={profit} detail={`Custo apropriado: ${money.format(allocatedCost)}`} tone={profit >= 0 ? 'green' : 'orange'} icon={<BarChart3 />} />
      <article className={`metric ${margin >= 0 ? 'green' : 'orange'}`}><div className="metric-top"><span>Margem bruta</span><i><CircleDollarSign /></i></div><strong>{margin.toFixed(1).replace('.', ',')}%</strong><small>Lucro bruto dividido pelo faturamento</small></article>
    </section>
    {rows.length ? <section className="investment-grid">{rows.map(({ investment, stats }) => <article className="card investment-card" key={investment.id}>
      <div className="investment-card-head">
        <span className={`investment-kind ${investment.kind}`}>{investment.kind === 'physical' ? <Package /> : <Wrench />}</span>
        <div><small>{investment.kind === 'physical' ? 'Produto físico' : 'Serviço'}</small><h3>{investment.name}</h3></div>
        <button className="icon-button danger" onClick={() => { if (window.confirm(`Excluir ${investment.name} e todos os lançamentos vinculados?`)) onRemove(investment.id) }} aria-label={`Excluir ${investment.name}`}><Trash2 /></button>
      </div>
      <div className="investment-numbers">
        <div><span>Investido</span><strong>{money.format(investment.investedAmount)}</strong></div>
        <div><span>{investment.kind === 'physical' ? 'Unidades vendidas' : 'Vendas realizadas'}</span><strong>{stats.soldUnits}</strong></div>
        {investment.kind === 'physical' && <div><span>Disponível após reservas</span><strong>{stats.remainingUnits}</strong></div>}
        <div><span>Faturamento</span><strong>{money.format(stats.revenue)}</strong></div>
      </div>
      <div className="investment-result">
        <div><span>Lucro bruto</span><strong className={stats.profit >= 0 ? 'positive' : 'negative'}>{money.format(stats.profit)}</strong></div>
        <div><span>Margem</span><strong className={stats.margin >= 0 ? 'positive' : 'negative'}>{stats.margin.toFixed(1).replace('.', ',')}%</strong></div>
        <div><span>Resultado de caixa</span><strong className={stats.cashResult >= 0 ? 'positive' : 'negative'}>{money.format(stats.cashResult)}</strong></div>
      </div>
      <button className="primary-button investment-sale-button" disabled={investment.kind === 'physical' && stats.remainingUnits === 0} onClick={() => onSale(investment.id)}><Plus /> Registrar venda</button>
    </article>)}</section> : <article className="card"><EmptyCard text="Cadastre um produto físico ou serviço para começar a acompanhar vendas, lucro e margem." /></article>}
  </div>
}

function PeopleView({ data, activePersonId, onActive, onAdd, onRename, onRemove, onExport, onImport, onExit }: { data: FinanceData; activePersonId: string; onActive: (id: string) => void; onAdd: () => void; onRename: (id: string, name: string) => void; onRemove: (id: string) => void; onExport: () => void; onImport: (event: ChangeEvent<HTMLInputElement>) => void; onExit: () => void }) {
  return <div className="content">
    <div className="section-heading"><div><h2>Perfis financeiros</h2><p>Separe suas finanças por pessoa, casa ou projeto.</p></div><button className="secondary-button" onClick={onAdd}><UserPlus /> Adicionar pessoa</button></div>
    <section className="people-grid">{data.people.map(person => {
      const count = data.entries.filter(item => item.personId === person.id).length
      return <article className={`person-card ${person.id === activePersonId ? 'selected' : ''}`} key={person.id}>
        <span className="large-avatar" style={{ background: person.color }}>{person.name.slice(0, 1).toUpperCase()}</span>
        <div><h3>{person.name}</h3><p>{count} {count === 1 ? 'lançamento' : 'lançamentos'}</p></div>
        {person.id === activePersonId ? <span className="active-pill"><Check /> Em uso</span> : <button className="text-button" onClick={() => onActive(person.id)}>Visualizar</button>}
        <div className="person-actions"><button className="icon-button" title="Renomear pessoa" onClick={() => { const name = window.prompt('Novo nome:', person.name)?.trim(); if (name) onRename(person.id, name) }}><Pencil /></button>{data.people.length > 1 && <button className="icon-button danger" title="Excluir pessoa" onClick={() => { if (window.confirm(`Excluir ${person.name} e todos os seus lançamentos?`)) onRemove(person.id) }}><Trash2 /></button>}</div>
      </article>
    })}</section>
    <section className="card data-tools"><div><h3>Dados e acesso</h3><p>Faça um backup manual ou importe os lançamentos da versão anterior.</p></div><div className="data-tool-actions"><button className="secondary-button" onClick={onExport}><Download /> Exportar</button><label className="secondary-button"><Upload /> Importar<input type="file" accept="application/json,.json" onChange={onImport} /></label><button className="secondary-button" onClick={onExit}><LogOut /> Trocar acesso</button></div></section>
  </div>
}

function ModalShell({ title, subtitle, onClose, children }: { title: string; subtitle: string; onClose: () => void; children: React.ReactNode }) {
  return <div className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}><div className="modal" role="dialog" aria-modal="true"><div className="modal-header"><div><h2>{title}</h2><p>{subtitle}</p></div><button className="icon-button" onClick={onClose}><X /></button></div>{children}</div></div>
}

function EntryModal({ personId, onClose, onSave }: { personId: string; onClose: () => void; onSave: (entry: Entry) => void }) {
  const [type, setType] = useState<EntryType>('expense')
  const [description, setDescription] = useState('')
  const [amount, setAmount] = useState('')
  const [category, setCategory] = useState(EXPENSE_CATEGORIES[0])
  const [method, setMethod] = useState<PaymentMethod>('pix')
  const [status, setStatus] = useState<EntryStatus>('planned')
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10))
  const categories = type === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
  const changeType = (value: EntryType) => { setType(value); setCategory(value === 'income' ? INCOME_CATEGORIES[0] : EXPENSE_CATEGORIES[0]) }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const numericAmount = parseMoney(amount)
    if (!description.trim() || !numericAmount || numericAmount <= 0) return
    onSave({ id: uid(), personId, description: description.trim(), amount: numericAmount, type, category, method, status, date, createdAt: new Date().toISOString() })
  }
  return <ModalShell title="Novo lançamento" subtitle="Registre uma receita ou despesa." onClose={onClose}><form onSubmit={submit}>
    <div className="type-switch"><button type="button" className={type === 'expense' ? 'expense active' : ''} onClick={() => changeType('expense')}><ArrowUpRight /> Despesa</button><button type="button" className={type === 'income' ? 'income active' : ''} onClick={() => changeType('income')}><ArrowDownLeft /> Receita</button></div>
    <label className="field"><span>Descrição</span><input autoFocus value={description} onChange={event => setDescription(event.target.value)} placeholder="Ex.: Supermercado" required /></label>
    <div className="form-grid"><label className="field"><span>Valor</span><div className="money-input"><b>R$</b><input inputMode="decimal" value={amount} onChange={event => setAmount(event.target.value)} placeholder="0,00" required /></div></label><label className="field"><span>Data</span><input type="date" value={date} onChange={event => setDate(event.target.value)} required /></label></div>
    <div className="form-grid"><label className="field"><span>Categoria</span><select value={category} onChange={event => setCategory(event.target.value)}>{categories.map(item => <option key={item}>{item}</option>)}</select></label><label className="field"><span>Forma de pagamento</span><select value={method} onChange={event => setMethod(event.target.value as PaymentMethod)}>{Object.entries(methodLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></div>
    <label className="field"><span>Situação</span><div className="status-choice"><button type="button" className={status === 'paid' ? 'active' : ''} onClick={() => setStatus('paid')}><Check /> Já aconteceu</button><button type="button" className={status === 'planned' ? 'active' : ''} onClick={() => setStatus('planned')}><CalendarDays /> Está previsto</button></div></label>
    <div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancelar</button><button className="primary-button" type="submit">Salvar lançamento</button></div>
  </form></ModalShell>
}

function InvestmentModal({ personId, onClose, onSave }: { personId: string; onClose: () => void; onSave: (investment: Investment, date: string, method: PaymentMethod, status: EntryStatus) => void }) {
  const [kind, setKind] = useState<InvestmentKind>('physical')
  const [name, setName] = useState('')
  const [investedAmount, setInvestedAmount] = useState('')
  const [totalUnits, setTotalUnits] = useState('')
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10))
  const [method, setMethod] = useState<PaymentMethod>('pix')
  const [status, setStatus] = useState<EntryStatus>('paid')
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const amount = parseMoney(investedAmount)
    const units = Number(totalUnits)
    if (!name.trim() || !Number.isFinite(amount) || amount <= 0 || (kind === 'physical' && (!Number.isInteger(units) || units <= 0))) return
    onSave({ id: uid(), personId, name: name.trim(), kind, investedAmount: amount, totalUnits: kind === 'physical' ? units : undefined, createdAt: new Date().toISOString() }, date, method, status)
  }
  return <ModalShell title="Novo investimento" subtitle="Cadastre o capital aplicado em um produto ou serviço." onClose={onClose}><form onSubmit={submit}>
    <div className="type-switch"><button type="button" className={kind === 'physical' ? 'income active' : ''} onClick={() => setKind('physical')}><Package /> Produto físico</button><button type="button" className={kind === 'service' ? 'income active' : ''} onClick={() => setKind('service')}><Wrench /> Serviço</button></div>
    <label className="field"><span>Nome</span><input autoFocus value={name} onChange={event => setName(event.target.value)} placeholder={kind === 'physical' ? 'Ex.: Camisetas' : 'Ex.: Consultoria'} required /></label>
    <div className="form-grid"><label className="field"><span>Capital investido</span><div className="money-input"><b>R$</b><input inputMode="decimal" value={investedAmount} onChange={event => setInvestedAmount(event.target.value)} placeholder="0,00" required /></div></label>{kind === 'physical' ? <label className="field"><span>Unidades compradas/produzidas</span><input type="number" min="1" step="1" value={totalUnits} onChange={event => setTotalUnits(event.target.value)} placeholder="0" required /></label> : <label className="field"><span>Tipo de custo</span><input value="Estrutura inicial do serviço" disabled /></label>}</div>
    <div className="form-grid"><label className="field"><span>Data do investimento</span><input type="date" value={date} onChange={event => setDate(event.target.value)} required /></label><label className="field"><span>Forma de pagamento</span><select value={method} onChange={event => setMethod(event.target.value as PaymentMethod)}>{Object.entries(methodLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></div>
    <label className="field"><span>Situação do investimento</span><div className="status-choice"><button type="button" className={status === 'paid' ? 'active' : ''} onClick={() => setStatus('paid')}><Check /> Já foi pago</button><button type="button" className={status === 'planned' ? 'active' : ''} onClick={() => setStatus('planned')}><CalendarDays /> Está previsto</button></div></label>
    <div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancelar</button><button className="primary-button">Salvar investimento</button></div>
  </form></ModalShell>
}

function SaleModal({ investment, soldUnits, onClose, onSave }: { investment: Investment; soldUnits: number; onClose: () => void; onSave: (sale: { quantity: number; revenue: number; date: string; method: PaymentMethod; status: EntryStatus }) => void }) {
  const remaining = investment.kind === 'physical' ? Math.max((investment.totalUnits ?? 0) - soldUnits, 0) : undefined
  const [quantity, setQuantity] = useState('1')
  const [revenue, setRevenue] = useState('')
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10))
  const [method, setMethod] = useState<PaymentMethod>('pix')
  const [status, setStatus] = useState<EntryStatus>('paid')
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const units = Number(quantity)
    const amount = parseMoney(revenue)
    if (!Number.isInteger(units) || units <= 0 || !Number.isFinite(amount) || amount <= 0 || (remaining !== undefined && units > remaining)) return
    onSave({ quantity: units, revenue: amount, date, method, status })
  }
  return <ModalShell title={`Venda · ${investment.name}`} subtitle={investment.kind === 'physical' ? `${remaining} unidades disponíveis` : 'Registre o faturamento realizado com o serviço.'} onClose={onClose}><form onSubmit={submit}>
    <div className="form-grid"><label className="field"><span>{investment.kind === 'physical' ? 'Quantidade vendida' : 'Quantidade de vendas'}</span><input type="number" min="1" max={remaining} step="1" value={quantity} onChange={event => setQuantity(event.target.value)} required /></label><label className="field"><span>Faturamento total da venda</span><div className="money-input"><b>R$</b><input autoFocus inputMode="decimal" value={revenue} onChange={event => setRevenue(event.target.value)} placeholder="0,00" required /></div></label></div>
    <div className="form-grid"><label className="field"><span>Data</span><input type="date" value={date} onChange={event => setDate(event.target.value)} required /></label><label className="field"><span>Recebimento</span><select value={method} onChange={event => setMethod(event.target.value as PaymentMethod)}>{Object.entries(methodLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label></div>
    <label className="field"><span>Situação da venda</span><div className="status-choice"><button type="button" className={status === 'paid' ? 'active' : ''} onClick={() => setStatus('paid')}><Check /> Realizada</button><button type="button" className={status === 'planned' ? 'active' : ''} onClick={() => setStatus('planned')}><CalendarDays /> Prevista</button></div></label>
    <div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancelar</button><button className="primary-button">Registrar venda</button></div>
  </form></ModalShell>
}

function PersonModal({ onClose, onSave }: { onClose: () => void; onSave: (person: FinanceData['people'][number]) => void }) {
  const [name, setName] = useState('')
  const colors = ['#18664b', '#cf6b3e', '#6c74c9', '#bd6174', '#4c91ad', '#87915a']
  const [color, setColor] = useState(colors[0])
  return <ModalShell title="Adicionar pessoa" subtitle="Não precisa de e-mail ou senha." onClose={onClose}><form onSubmit={event => { event.preventDefault(); if (name.trim()) onSave({ id: uid(), name: name.trim(), color }) }}>
    <label className="field"><span>Nome ou apelido</span><input autoFocus value={name} onChange={event => setName(event.target.value)} placeholder="Ex.: Ana, Casa, Viagem" required /></label>
    <label className="field"><span>Cor do perfil</span><div className="color-picker">{colors.map(item => <button type="button" key={item} className={color === item ? 'active' : ''} style={{ background: item }} onClick={() => setColor(item)}>{color === item && <Check />}</button>)}</div></label>
    <div className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>Cancelar</button><button className="primary-button">Adicionar</button></div>
  </form></ModalShell>
}

function AccessGate({ onAccess }: { onAccess: (code: string) => Promise<void> }) {
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  return <main className="access-gate"><div className="access-card">
    <span className="brand-mark"><CircleDollarSign /></span>
    <h1>finanz</h1>
    <p>Use sempre o mesmo código para acessar seus dados em qualquer dispositivo.</p>
    <form onSubmit={async event => {
      event.preventDefault()
      if (code.trim().length < 12) { setError('Use pelo menos 12 caracteres.'); return }
      setBusy(true); setError('')
      try { await onAccess(code) } catch { setError('Não foi possível acessar agora.'); setBusy(false) }
    }}>
      <label className="field"><span>Código de acesso</span><input type="password" autoFocus autoComplete="current-password" value={code} onChange={event => setCode(event.target.value)} placeholder="Mínimo de 12 caracteres" /></label>
      {error && <p className="access-error">{error}</p>}
      <button className="primary-button" disabled={busy}>{busy ? 'Acessando…' : 'Entrar'}</button>
    </form>
    <small>Guarde esse código. Ele não é enviado em texto puro e não pode ser recuperado.</small>
  </div></main>
}

function LoadingCloud({ state, onExit }: { state: 'loading' | 'saved' | 'saving' | 'error'; onExit: () => void }) {
  return <main className="empty-start"><Cloud /><h1>{state === 'error' ? 'Não foi possível carregar' : 'Carregando seus dados'}</h1><p>{state === 'error' ? 'Confira sua conexão ou tente entrar novamente.' : 'Buscando a versão mais recente no banco seguro.'}</p>{state === 'error' && <button className="secondary-button" onClick={onExit}>Voltar</button>}</main>
}

function EmptyCard({ text }: { text: string }) { return <div className="empty-card"><ReceiptText /><p>{text}</p></div> }

function EmptyStart({ onCreate, modal, onClose, onSave }: { onCreate: () => void; modal: Modal; onClose: () => void; onSave: (person: FinanceData['people'][number]) => void }) {
  return <main className="empty-start"><CircleDollarSign /><h1>Comece suas finanças</h1><p>Crie uma pessoa para registrar os primeiros lançamentos.</p><button className="primary-button" onClick={onCreate}><Plus /> Criar pessoa</button>{modal === 'person' && <PersonModal onClose={onClose} onSave={onSave} />}</main>
}
