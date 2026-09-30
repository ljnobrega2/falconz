// Página placeholder padrão das seções do portal ainda não portadas.
// Conteúdo real virá via go/portal nas próximas ondas.
import EmptyState from './EmptyState'

export default function Placeholder({ title, icon }: { title: string; icon?: string }) {
  return (
    <section>
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          {title}
        </h2>
      </div>
      <EmptyState
        icon={icon || '🚧'}
        title="Em construção"
        description="Dados via go/portal (próxima onda)."
      />
    </section>
  )
}
