import { useEffect, useState } from 'react'
import { api } from '../api'

type ApiEndpoint = {
  method: string
  path: string
  description: string
  auth_required: boolean
}

type ApiNamespace = {
  namespace: string
  label: string
  base_url: string
  auth: string
  endpoints: ApiEndpoint[]
}

// Cores por método HTTP
const METHOD_COLORS: Record<string, { bg: string; color: string }> = {
  GET:    { bg: '#1d4ed8', color: '#fff' },
  POST:   { bg: '#15803d', color: '#fff' },
  PUT:    { bg: '#b45309', color: '#fff' },
  PATCH:  { bg: '#7c3aed', color: '#fff' },
  DELETE: { bg: '#dc2626', color: '#fff' },
}

function MethodBadge({ method }: { method: string }) {
  const style = METHOD_COLORS[method] || { bg: '#6b7280', color: '#fff' }
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        fontSize: 11,
        fontWeight: 700,
        fontFamily: 'monospace',
        background: style.bg,
        color: style.color,
        minWidth: 54,
        textAlign: 'center',
      }}
    >
      {method}
    </span>
  )
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }
  return (
    <button
      type="button"
      className="szv2-btn-secondary"
      onClick={copy}
      style={{ fontSize: 12, padding: '3px 10px' }}
    >
      {copied ? 'Copiado!' : 'Copiar'}
    </button>
  )
}

// Gera snippet curl para um endpoint
function buildCurl(baseURL: string, ep: ApiEndpoint, token = '<seu_token>'): string {
  const url = `https://seusite.com.br${baseURL}${ep.path}`
  const authHeader = ep.auth_required ? ` \\\n  -H "Authorization: Bearer ${token}"` : ''
  const body = ep.method === 'POST' ? ` \\\n  -H "Content-Type: application/json" \\\n  -d '{}'` : ''
  return `curl -X ${ep.method} "${url}"${authHeader}${body}`
}

// Exemplo JS fetch completo: login → extrato → recarregar → consultar PIX QR
const JS_FETCH_EXAMPLE = `const BASE = 'https://seusite.com.br/wp-json/tp-carteira/v1';

// 1. Login → JWT
const loginRes = await fetch(BASE + '/auth/token', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: 'usuario@exemplo.com', password: 'senha' }),
});
const { token } = await loginRes.json();
const headers = { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' };

// 2. Extrato paginado
const extratoRes = await fetch(BASE + '/extrato?per_page=20&page=1', { headers });
const extrato = await extratoRes.json();
console.log('Extrato:', extrato);

// 3. Criar recarga PIX (mínimo R$ 10,00)
const recargaRes = await fetch(BASE + '/recarregar', {
  method: 'POST',
  headers,
  body: JSON.stringify({ valor: 50 }),
});
const { recarga_id } = await recargaRes.json();

// 4. Consultar QR Code / status da recarga
const pixRes = await fetch(BASE + '/recarga/' + recarga_id + '/pix', { headers });
const pix = await pixRes.json();
console.log('PIX QR:', pix.qr_code, 'Status:', pix.status);`

export default function ApiDocs() {
  const [namespaces, setNamespaces] = useState<ApiNamespace[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  // Endpoint selecionado para snippet curl (namespace_idx:endpoint_idx)
  const [selectedNs, setSelectedNs] = useState(0)
  const [selectedEp, setSelectedEp] = useState(0)

  useEffect(() => {
    api<{ namespaces: ApiNamespace[] }>('/api-docs')
      .then(res => setNamespaces(res.namespaces || []))
      .catch(e => setErr(e.message || 'Erro ao carregar'))
      .finally(() => setLoading(false))
  }, [])

  const curNs = namespaces[selectedNs]
  const curEp = curNs?.endpoints?.[selectedEp]
  const curlSnippet = curNs && curEp ? buildCurl(curNs.base_url, curEp) : ''

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}

      {/* Badge API Ativa */}
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Documentação de API</h2>
            <p className="szv2-card-sub">
              Endpoints REST do Senderzz disponíveis para integração externa.
            </p>
          </div>
          <span
            className="sz-badge"
            style={{ background: '#dcfce7', color: '#15803d', fontWeight: 700, fontSize: 13, padding: '4px 14px' }}
          >
            API Ativa
          </span>
        </div>
      </div>

      {/* Webhook Melhor Envio — bloco de configuração */}
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div className="szv2-card-head" style={{ marginBottom: 12 }}>
          <div>
            <h2>Webhook Melhor Envio</h2>
            <p className="szv2-card-sub">
              Configure este URL no painel da Melhor Envio para receber eventos de rastreio e pagamento.
            </p>
          </div>
        </div>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            marginBottom: 14,
            padding: '8px 12px',
            background: 'var(--szv2-bg)',
            borderRadius: 6,
            border: '1px solid var(--szv2-border)',
          }}
        >
          <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)', whiteSpace: 'nowrap' }}>URL do Webhook:</span>
          <code style={{ flex: 1, fontSize: 12 }}>
            https://seusite.com.br/wp-json/senderzz/v1/webhook/me
          </code>
          <CopyButton text="https://seusite.com.br/wp-json/senderzz/v1/webhook/me" />
        </div>
        <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0 }}>
          No painel da Melhor Envio: <strong>Integrações → Área Dev → Webhooks</strong> → adicione o URL acima
          e selecione os eventos desejados (rastreamento, pagamento, etc.).
        </p>
      </div>

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : (
        <>
          {/* Cards por namespace */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16, marginBottom: 24 }}>
            {namespaces.map((ns, nsIdx) => (
              <details key={ns.namespace} className="szv2-card" style={{ padding: 0 }}>
                <summary
                  style={{
                    padding: '14px 20px',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    listStyle: 'none',
                    userSelect: 'none',
                  }}
                >
                  <div style={{ flex: 1 }}>
                    <span style={{ fontWeight: 700, fontSize: 15 }}>{ns.label}</span>
                    <span
                      style={{
                        marginLeft: 10,
                        fontSize: 12,
                        fontFamily: 'monospace',
                        color: 'var(--szv2-text-muted)',
                      }}
                    >
                      {ns.namespace}
                    </span>
                  </div>
                  <span
                    className="sz-badge"
                    style={{ fontSize: 11, background: 'rgba(234,88,12,.1)', color: 'var(--szv2-brand)' }}
                  >
                    {ns.auth}
                  </span>
                  <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12 }}>
                    {ns.endpoints.length} endpoint{ns.endpoints.length !== 1 ? 's' : ''}
                  </span>
                </summary>

                <div style={{ padding: '0 20px 20px' }}>
                  {/* Base URL + botão copiar */}
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 10,
                      marginBottom: 14,
                      padding: '8px 12px',
                      background: 'var(--szv2-bg)',
                      borderRadius: 6,
                      border: '1px solid var(--szv2-border)',
                    }}
                  >
                    <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)' }}>Base URL:</span>
                    <code style={{ flex: 1, fontSize: 12 }}>{ns.base_url}</code>
                    <CopyButton text={ns.base_url} />
                  </div>

                  {/* Exemplo de autenticação JWT */}
                  <pre
                    style={{
                      background: 'var(--szv2-bg)',
                      borderRadius: 6,
                      padding: '10px 14px',
                      fontSize: 12,
                      color: 'var(--szv2-text)',
                      overflowX: 'auto',
                      marginBottom: 14,
                      border: '1px solid var(--szv2-border)',
                    }}
                  >
{`# Obter token (POST ${ns.base_url}/auth/token ou /wp-json/tp-carteira/v1/auth/token)
curl -X POST "https://seusite.com.br${ns.base_url}/auth/token" \\
  -H "Content-Type: application/json" \\
  -d '{"email":"usuario@exemplo.com","password":"senha"}'

# Usar token nas rotas autenticadas:
# Authorization: Bearer <token_retornado>`}
                  </pre>

                  {/* Tabela de endpoints */}
                  <div style={{ overflowX: 'auto' }}>
                    <table className="szv2-table">
                      <thead>
                        <tr>
                          <th style={{ width: 74 }}>Método</th>
                          <th>Path</th>
                          <th>Descrição</th>
                          <th style={{ width: 60, textAlign: 'center' }}>Auth</th>
                          <th style={{ width: 80 }}>Curl</th>
                        </tr>
                      </thead>
                      <tbody>
                        {ns.endpoints.map((ep, epIdx) => (
                          <tr key={`${ep.method}-${ep.path}`}>
                            <td><MethodBadge method={ep.method} /></td>
                            <td>
                              <code style={{ fontSize: 12 }}>{ep.path}</code>
                            </td>
                            <td style={{ fontSize: 13 }}>{ep.description}</td>
                            <td style={{ textAlign: 'center', fontSize: 16 }}>
                              {ep.auth_required ? '🔒' : '🔓'}
                            </td>
                            <td>
                              <button
                                type="button"
                                className="szv2-btn-secondary"
                                style={{ fontSize: 11, padding: '3px 8px' }}
                                onClick={() => { setSelectedNs(nsIdx); setSelectedEp(epIdx) }}
                              >
                                Ver curl
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </details>
            ))}
          </div>

          {/* Snippet curl copiável */}
          <div className="szv2-card" style={{ marginBottom: 24 }}>
            <div className="szv2-card-head" style={{ marginBottom: 12 }}>
              <div>
                <h2>Exemplo curl</h2>
                <p className="szv2-card-sub">
                  Selecione um endpoint na tabela acima para gerar o snippet.
                </p>
              </div>
              {curNs && curEp && (
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  {/* Select namespace */}
                  <select
                    value={selectedNs}
                    onChange={e => { setSelectedNs(Number(e.target.value)); setSelectedEp(0) }}
                    className="szv2-select"
                  >
                    {namespaces.map((ns, i) => (
                      <option key={ns.namespace} value={i}>{ns.label}</option>
                    ))}
                  </select>
                  {/* Select endpoint */}
                  <select
                    value={selectedEp}
                    onChange={e => setSelectedEp(Number(e.target.value))}
                    className="szv2-select"
                  >
                    {curNs.endpoints.map((ep, i) => (
                      <option key={`${ep.method}-${ep.path}`} value={i}>
                        {ep.method} {ep.path}
                      </option>
                    ))}
                  </select>
                  <CopyButton text={curlSnippet} />
                </div>
              )}
            </div>

            <pre
              style={{
                background: 'var(--szv2-bg)',
                borderRadius: 6,
                padding: '14px 16px',
                fontSize: 12,
                color: 'var(--szv2-text)',
                overflowX: 'auto',
                border: '1px solid var(--szv2-border)',
                minHeight: 72,
              }}
            >
              {curlSnippet || '# Selecione namespace + endpoint acima'}
            </pre>
          </div>

          {/* Exemplo JS fetch completo (tp-carteira/v1) */}
          <div className="szv2-card">
            <div className="szv2-card-head" style={{ marginBottom: 12 }}>
              <div>
                <h2>Exemplo JavaScript (fetch)</h2>
                <p className="szv2-card-sub">
                  Fluxo completo: login → extrato → recarregar → consultar PIX QR Code.
                </p>
              </div>
              <CopyButton text={JS_FETCH_EXAMPLE} />
            </div>
            <pre
              style={{
                background: 'var(--szv2-bg)',
                borderRadius: 6,
                padding: '14px 16px',
                fontSize: 12,
                color: 'var(--szv2-text)',
                overflowX: 'auto',
                border: '1px solid var(--szv2-border)',
              }}
            >
              {JS_FETCH_EXAMPLE}
            </pre>
          </div>
        </>
      )}
    </div>
  )
}
