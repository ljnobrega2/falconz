// Banner de consentimento de cookies/dados (LGPD — Art. 8º, base no consentimento).
// Aparece no rodapé na PRIMEIRA visita (flag em localStorage keyed por versão do
// documento). Não-intrusivo e dismissível: "Aceitar" grava o aceite localmente e,
// se houver sessão (token), faz best-effort POST /portal/account/consent
// {doc_type:'cookies', doc_version}. O POST é silencioso — banner some mesmo se o
// backend ainda não tiver o endpoint (paridade com o resto do fluxo LGPD).
//
// Renderizado em App.tsx (fora do Layout) → aparece tanto na tela de login quanto
// no app autenticado. Marca azul FALK (--szv2-brand). Sem cor verde.
import { useEffect, useState } from 'react'
import { api, getToken } from '../api'

// Versão vigente do aviso de cookies. Ao subir esta string, o banner reaparece
// (a flag em localStorage é keyed por versão).
const COOKIE_DOC_VERSION = '2026-06-18'
const COOKIE_DOC_TYPE = 'cookies'
const STORAGE_KEY = `sz_consent_${COOKIE_DOC_TYPE}_${COOKIE_DOC_VERSION}`

// Alvo do link "Política de Privacidade". Aponta para a política real publicada
// em falklog.com.br. Abre em nova aba.
const POLICY_HREF = 'https://falklog.com.br/privacidade.html'

export default function CookieConsent() {
  // Mostra só se o aceite vigente ainda não foi dado. Lê localStorage no mount —
  // se indisponível (modo privado), assume "ainda não aceito" e mostra o banner.
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    try {
      if (localStorage.getItem(STORAGE_KEY) !== '1') setVisible(true)
    } catch {
      setVisible(true)
    }
  }, [])

  function accept() {
    try {
      localStorage.setItem(STORAGE_KEY, '1')
    } catch {
      /* localStorage indisponível — segue sem persistir o flag local. */
    }
    setVisible(false)
    // Best-effort: registra o aceite no backend SE houver sessão. Falha é
    // silenciosa (endpoint pode não existir ainda / usuário deslogado).
    if (getToken()) {
      api('/portal/account/consent', {
        method: 'POST',
        body: JSON.stringify({
          doc_type: COOKIE_DOC_TYPE,
          doc_version: COOKIE_DOC_VERSION,
        }),
      }).catch(() => {
        /* noop — aceite local já basta para dispensar o banner. */
      })
    }
  }

  if (!visible) return null

  return (
    <div role="dialog" aria-label="Aviso de cookies e privacidade" style={S.bar}>
      <div style={S.inner}>
        <p style={S.text}>
          Usamos cookies e dados conforme a{' '}
          <a href={POLICY_HREF} target="_blank" rel="noopener noreferrer" style={S.link}>
            Política de Privacidade
          </a>
          {' '}para operar a plataforma e melhorar sua experiência.
        </p>
        <div style={S.actions}>
          <a href={POLICY_HREF} target="_blank" rel="noopener noreferrer" style={S.linkBtn}>
            Saiba mais
          </a>
          <button type="button" onClick={accept} style={S.acceptBtn}>
            Aceitar
          </button>
          <button
            type="button"
            onClick={() => setVisible(false)}
            aria-label="Dispensar aviso"
            title="Dispensar"
            style={S.dismiss}
          >
            &times;
          </button>
        </div>
      </div>
    </div>
  )
}

const BLUE = '#1E6FF2'

const S: Record<string, React.CSSProperties> = {
  bar: {
    position: 'fixed',
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 99998,
    background: '#0F172A',
    color: '#fff',
    boxShadow: '0 -6px 24px rgba(15,23,42,.22)',
  },
  inner: {
    maxWidth: 1140,
    margin: '0 auto',
    padding: '14px 20px',
    display: 'flex',
    alignItems: 'center',
    gap: 16,
    flexWrap: 'wrap',
    justifyContent: 'space-between',
  },
  text: {
    fontSize: 13,
    lineHeight: 1.5,
    color: 'rgba(255,255,255,.82)',
    margin: 0,
    flex: '1 1 280px',
    minWidth: 240,
  },
  link: { color: '#5B97F9', fontWeight: 600, textDecoration: 'underline' },
  actions: { display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 },
  linkBtn: {
    fontSize: 13,
    fontWeight: 600,
    color: 'rgba(255,255,255,.72)',
    textDecoration: 'none',
    padding: '8px 6px',
  },
  acceptBtn: {
    height: 38,
    padding: '0 20px',
    border: 'none',
    borderRadius: 9,
    background: BLUE,
    color: '#fff',
    fontSize: 13.5,
    fontWeight: 700,
    cursor: 'pointer',
    boxShadow: '0 6px 16px rgba(30,111,242,.30)',
  },
  dismiss: {
    width: 32,
    height: 32,
    border: 'none',
    background: 'transparent',
    color: 'rgba(255,255,255,.6)',
    fontSize: 22,
    lineHeight: 1,
    cursor: 'pointer',
    borderRadius: 8,
  },
}
