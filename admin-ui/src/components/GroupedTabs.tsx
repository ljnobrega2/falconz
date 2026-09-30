// AUDIT-2026-06-19 — Tab-strip horizontal sub-agrupada.
//
// Hubs com muitas abas (AdministracaoHub 11, SistemaHub 11) ficam ilegíveis num
// strip plano. Este componente renderiza as abas em blocos visuais por `group`,
// com um rótulo discreto + separador entre grupos, mantendo o mesmo visual
// `szv2-tab` (aria-selected) já usado no resto do admin. Scroll horizontal no
// overflow (telas estreitas).

type GroupedTab<K extends string> = { key: K; label: string; group: string }

type GroupedTabsProps<K extends string> = {
  tabs: GroupedTab<K>[]
  active: K
  onSelect: (key: K) => void
}

export default function GroupedTabs<K extends string>({ tabs, active, onSelect }: GroupedTabsProps<K>) {
  // Preserva a ordem de aparição dos grupos.
  const groups: string[] = []
  for (const t of tabs) if (!groups.includes(t.group)) groups.push(t.group)

  return (
    <div
      className="szv2-tabs"
      style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4, overflowX: 'auto' }}
      role="tablist"
    >
      {groups.map((g, gi) => (
        <div key={g} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          {gi > 0 && (
            <span
              aria-hidden="true"
              style={{ width: 1, alignSelf: 'stretch', margin: '4px 6px', background: 'var(--szv2-divider)' }}
            />
          )}
          <span
            style={{
              fontSize: 10,
              fontWeight: 700,
              textTransform: 'uppercase',
              letterSpacing: 0.4,
              color: 'var(--szv2-text-faint, var(--szv2-text-muted))',
              padding: '0 4px',
              whiteSpace: 'nowrap',
            }}
          >
            {g}
          </span>
          {tabs.filter(t => t.group === g).map(t => (
            <button
              key={t.key}
              type="button"
              role="tab"
              className="szv2-tab"
              aria-selected={active === t.key}
              onClick={() => onSelect(t.key)}
            >
              {t.label}
            </button>
          ))}
        </div>
      ))}
    </div>
  )
}
