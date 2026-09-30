import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import './index.css'

// Captura o token de convite de afiliado ANTES do React/router montarem.
// O link compartilhado pelo produtor é <base>/portal/?convite=<TOKEN> e cai na
// rota "/" → <Navigate to="/dashboard"> (ou /login se deslogado), que descarta
// a query string antes de qualquer useEffect rodar. Guardamos em sessionStorage
// (sobrevive ao round-trip /login → dashboard) e limpamos a URL imediatamente.
// O Layout consome a chave, oferece resgatar e remove em sucesso OU erro.
;(() => {
  try {
    const convite = new URLSearchParams(window.location.search).get('convite')
    if (convite) {
      sessionStorage.setItem('sz_pending_convite', convite)
      window.history.replaceState({}, '', window.location.pathname)
    }
  } catch {
    /* sessionStorage indisponível (modo privado etc.) — ignora silenciosamente. */
  }
})()

// AUDIT-2026-07-31: registra o service worker real do PWA (ver public/sw.js —
// PWA nunca tinha funcionado, dependia de um /app-sw.js que era servido pelo
// WordPress e foi apagado no strangler-fig sem substituto). Best-effort:
// falha de registro (browser sem suporte, contexto não-seguro) não quebra o app.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/portal/sw.js', { scope: '/portal/' }).catch(() => {
      /* PWA é progressive enhancement — app funciona normal sem SW */
    })
  })
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter basename="/portal">
      <App />
    </BrowserRouter>
  </React.StrictMode>
)
