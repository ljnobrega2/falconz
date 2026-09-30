// Rodapé do Encarregado de Dados (DPO) — canal publicado de privacidade
// (LGPD Art. 41 — o controlador deve indicar e divulgar o encarregado).
// Discreto, usado no app (fim do conteúdo) e na tela de login.
//
// `variant`:
//   'app'   → tons claros sobre o fundo do app (texto-muted, link brand).
//   'login' → tons sobre fundo claro do formulário (cinza neutro).
//
// Canal de privacidade do DPO (privacidade@falklog.com.br). O link "Política de
// Privacidade" abre a política real publicada em falklog.com.br em nova aba.

const DPO_EMAIL = 'privacidade@falklog.com.br'
const POLICY_HREF = 'https://falklog.com.br/privacidade.html'

export default function DpoFooter({ variant = 'app' }: { variant?: 'app' | 'login' }) {
  const muted = variant === 'login' ? '#9CA3AF' : 'var(--szv2-text-faint)'
  const link = variant === 'login' ? '#6B7280' : 'var(--szv2-text-muted)'

  return (
    <footer
      style={{
        marginTop: 24,
        paddingTop: 14,
        borderTop: '1px solid var(--szv2-divider, #E6EAF0)',
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        justifyContent: 'center',
        gap: '4px 14px',
        fontSize: 11.5,
        lineHeight: 1.5,
        color: muted,
        textAlign: 'center',
      }}
    >
      <span>
        Encarregado de Dados (DPO):{' '}
        <a href={`mailto:${DPO_EMAIL}`} style={{ color: link, fontWeight: 600, textDecoration: 'none' }}>
          {DPO_EMAIL}
        </a>
      </span>
      <span aria-hidden="true" style={{ opacity: 0.5 }}>·</span>
      <a
        href={POLICY_HREF}
        target="_blank"
        rel="noopener noreferrer"
        style={{ color: link, fontWeight: 600, textDecoration: 'none' }}
      >
        Política de Privacidade
      </a>
    </footer>
  )
}
