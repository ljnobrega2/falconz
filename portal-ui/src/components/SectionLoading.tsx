// Loading padronizado de SEÇÃO (top-level de página). Acessível:
//   - role="status" + aria-live="polite" → leitores de tela anunciam "Carregando".
//   - spinner aria-hidden (respeita prefers-reduced-motion).
// Substitui o <p>Carregando…</p> ad-hoc espalhado pelas páginas.
//
// IMPORTANTE: este componente NÃO renderiza a <section className="sz-sec"> — quem
// chama mantém o wrapper .sz-sec (+ page-head) para preservar o fix de CSS
// (.sz-dashboard-v2 .sz-sec { display:flex }). Use DENTRO do branch de loading.
//
//   if (loading) return (
//     <section id="sec-x" className="sz-sec" aria-busy="true">
//       <div className="szv2-page-head">…título…</div>
//       <SectionLoading label="Carregando pedidos…" />
//     </section>
//   )

const SPINNER_STYLE = `
@keyframes szSpin { to { transform: rotate(360deg) } }
.sz-spinner {
  width: 22px; height: 22px; border-radius: 50%;
  border: 2.5px solid var(--szv2-divider);
  border-top-color: var(--szv2-brand);
  animation: szSpin .7s linear infinite;
}
@media (prefers-reduced-motion: reduce) { .sz-spinner { animation: none } }
`

export default function SectionLoading({ label = 'Carregando…' }: { label?: string }) {
  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
        padding: '48px 20px',
        color: 'var(--szv2-text-muted)',
      }}
    >
      <style>{SPINNER_STYLE}</style>
      <span className="sz-spinner" aria-hidden="true" />
      <span style={{ fontSize: 13 }}>{label}</span>
    </div>
  )
}
