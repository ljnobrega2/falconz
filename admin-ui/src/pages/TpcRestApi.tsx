import { useEffect, useState } from 'react'
import { api } from '../api'

// Tela TPC · REST API (estática) — documentação dos endpoints tp-carteira/v1.
// Extraída de Wallet.tsx::TabRestApi para o hub "Carteira Expedição", junto das
// demais sub-abas TPC (Clientes / Transações / PIX / Config).

// Apenas a URL do webhook é lida ao vivo de /tpc-config.
type ConfigResp = { me: { webhook_url: string } }

// Fallback exibido se o GET /tpc-config falhar (401/500) — a página é
// majoritariamente documentação estática e nunca deve ficar travada em "Carregando…".
const WEBHOOK_URL_FALLBACK = 'https://app.falklog.com.br/wp-json/tp-carteira/v1/webhook/pix'

export default function TpcRestApi() {
  const [webhookUrl, setWebhookUrl] = useState('')
  const [loadingUrl, setLoadingUrl] = useState(true)

  useEffect(() => {
    let alive = true
    api<ConfigResp>('/tpc-config')
      .then(c => { if (alive) setWebhookUrl(c?.me?.webhook_url || WEBHOOK_URL_FALLBACK) })
      .catch(() => { if (alive) setWebhookUrl(WEBHOOK_URL_FALLBACK) })
      .finally(() => { if (alive) setLoadingUrl(false) })
    return () => { alive = false }
  }, [])

  const endpoints = [
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/carteira',         desc: 'Retorna saldo e saldo_reservado do usuário autenticado.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/recarga',          desc: 'Inicia recarga PIX (gera QR code e copia-cola).' },
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/recargas',         desc: 'Lista recargas do usuário (paginado).' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/webhook/pix',      desc: 'Webhook PIX entrante (HMAC validado). Confirma recarga.' },
    { method: 'GET',  path: '/wp-json/tp-carteira/v1/transacoes',       desc: 'Extrato de transações do usuário.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/reservar',         desc: 'Reserva saldo para emissão de etiqueta (BEGIN reserva).' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/debitar-reserva',  desc: 'Debita reserva após etiqueta emitida com sucesso.' },
    { method: 'POST', path: '/wp-json/tp-carteira/v1/estornar-reserva', desc: 'Libera reserva em caso de falha na emissão.' },
  ]

  const jsFetch = `const url = '${webhookUrl || 'https://app.falklog.com.br/wp-json/tp-carteira/v1/webhook/pix'}';

// Exemplo: notificação PIX confirmado
const body = {
  type: 'PAYMENT',
  pix_id: 'stub-abc123',
  status: 'paid',
  amount: 100.00,
};

// Assinar com HMAC-SHA256 usando o webhook_secret
// No lado do servidor, o header é: X-Webhook-Signature
const sig = hmacSHA256(JSON.stringify(body), webhookSecret);

fetch(url, {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'X-Webhook-Signature': sig,
  },
  body: JSON.stringify(body),
});`

  return (
    <div>
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>Endpoints tp-carteira/v1</h2><p className="szv2-card-sub">8 endpoints do módulo carteira/PIX registrados no WordPress.</p></div></div>
        <div className="szv2-table-wrap">
          <table className="szv2-table">
            <thead><tr><th style={{ width: 80 }}>Método</th><th>Endpoint</th><th>Descrição</th></tr></thead>
            <tbody>
              {endpoints.map(e => (
                <tr key={e.path}>
                  <td>
                    <span className={`sz-badge ${e.method === 'GET' ? 'szv2-badge-brand' : 'szv2-badge-success'}`} style={{ fontFamily: 'monospace', fontSize: 11 }}>
                      {e.method}
                    </span>
                  </td>
                  <td><code style={{ fontSize: 12 }}>{e.path}</code></td>
                  <td style={{ fontSize: 13, color: 'var(--szv2-text-soft)' }}>{e.desc}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head"><div><h2>URL do webhook PIX</h2><p className="szv2-card-sub">Configure no painel Melhor Envio para receber notificações.</p></div></div>
        <div className="szv2-field">
          <input className="szv2-input" type="text" readOnly
            value={webhookUrl || (loadingUrl ? 'Carregando…' : WEBHOOK_URL_FALLBACK)}
            style={{ fontFamily: 'monospace', fontSize: 13, color: 'var(--szv2-text-muted)', cursor: 'default' }} />
        </div>
      </div>

      <div className="szv2-card">
        <div className="szv2-card-head"><div><h2>Exemplo de integração JS</h2><p className="szv2-card-sub">Como assinar e enviar notificação ao webhook PIX.</p></div></div>
        <pre style={{
          background: 'var(--szv2-surface-alt, #f5f5f5)', borderRadius: 8, padding: 16,
          fontSize: 12, fontFamily: 'monospace', overflowX: 'auto', lineHeight: 1.6,
          color: 'var(--szv2-text)', border: '1px solid var(--szv2-border)',
        }}>
          {jsFetch}
        </pre>
      </div>
    </div>
  )
}
