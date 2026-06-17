import { useEffect, useState } from 'react'
import { api } from '../api'

// Tela TPC · Configurações — paridade com tpc_tab_configuracoes() (PHP).
// Lê/grava em senderzz_options. Token/secret podem ser env-managed (read-only).
// Mapeamento de classe→carteira usa dropdown de classes (GET /shipping-classes)
// e seletor de usuário (GET /users/search). Saldo ME buscado ao vivo via
// GET /tpc-config/me-balance (espelha tpc_consultar_saldo_me() do PHP).

type ConfigME = {
  token_masked: string
  token_from_env: boolean
  webhook_secret_masked: string
  webhook_secret_from_env: boolean
  jwt_secret_from_env: boolean
  webhook_url: string
  saldo_atual_me: number
  saldo_at: string
}

type ConfigRegras = {
  saldo_minimo: number
}

type ConfigMotor = {
  pix_valid_minutes: number
  pix_auto_cancel_expired: boolean
  enforce_wallet_on_label: boolean
  block_duplicate_label: boolean
  checkout_template_id: number
}

type ConfigResp = {
  me: ConfigME
  regras: ConfigRegras
  motor: ConfigMotor
}

type WalletOwnerItem = {
  class_id: number
  class_name: string
  user_id: number
  user_nome: string
  user_email: string
}

type ShippingClass = {
  id: number
  name: string
}

type UserSearchItem = {
  id: number
  wp_user_id: number | null
  nome: string
  email: string
}

type MEBalanceResp = {
  balance: number
  fetched_at: string
  erro?: string
}

// fmtBRL formata um número como moeda BR.
const fmtBRL = (v: number) =>
  v.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' })

export default function TpcConfiguracoes() {
  // Estado principal — campos editáveis isolados para evitar overwrite acidental.
  const [cfg, setCfg] = useState<ConfigResp | null>(null)
  const [meTokenInput, setMeTokenInput] = useState<string>('') // vazio = não altera
  const [whSecretInput, setWhSecretInput] = useState<string>('')
  const [showMeToken, setShowMeToken] = useState(false)
  const [showWhSecret, setShowWhSecret] = useState(false)

  const [saldoMinimo, setSaldoMinimo] = useState<number>(0)
  const [pixValidMinutes, setPixValidMinutes] = useState<number>(30)
  const [pixAutoCancel, setPixAutoCancel] = useState<boolean>(true)
  const [enforceWallet, setEnforceWallet] = useState<boolean>(true)
  const [blockDuplicate, setBlockDuplicate] = useState<boolean>(true)
  const [checkoutTemplateID, setCheckoutTemplateID] = useState<number>(140)

  const [owners, setOwners] = useState<WalletOwnerItem[]>([])
  const [ownersOpen, setOwnersOpen] = useState<boolean>(false)
  const [showAddOwner, setShowAddOwner] = useState(false)
  const [addClassID, setAddClassID] = useState<string>('')
  const [addUserID, setAddUserID] = useState<string>('')

  // Catálogos para dropdowns de wallet-owners.
  const [shippingClasses, setShippingClasses] = useState<ShippingClass[]>([])
  const [users, setUsers] = useState<UserSearchItem[]>([])
  const [userSearch, setUserSearch] = useState<string>('')

  // Saldo ME ao vivo (overlay sobre o valor estático do GET /tpc-config).
  const [liveBalance, setLiveBalance] = useState<MEBalanceResp | null>(null)
  const [liveBalanceLoading, setLiveBalanceLoading] = useState<boolean>(false)

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // Carrega dados iniciais (config + wallet-owners + catálogos em paralelo).
  async function loadAll() {
    setLoading(true)
    setErr('')
    try {
      const [c, ow, cls, usrs] = await Promise.all([
        api<ConfigResp>('/tpc-config'),
        api<{ items: WalletOwnerItem[] }>('/tpc-config/wallet-owners'),
        api<{ items: ShippingClass[] }>('/shipping-classes'),
        api<{ items: UserSearchItem[] }>('/users/search'),
      ])
      setCfg(c)
      // Hidrata inputs com valores atuais.
      setSaldoMinimo(c.regras.saldo_minimo)
      setPixValidMinutes(c.motor.pix_valid_minutes)
      setPixAutoCancel(c.motor.pix_auto_cancel_expired)
      setEnforceWallet(c.motor.enforce_wallet_on_label)
      setBlockDuplicate(c.motor.block_duplicate_label)
      setCheckoutTemplateID(c.motor.checkout_template_id)
      setMeTokenInput('')
      setWhSecretInput('')
      setOwners(ow.items || [])
      setShippingClasses(cls.items || [])
      setUsers(usrs.items || [])
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar configurações')
    } finally {
      setLoading(false)
    }
  }

  // Busca saldo ME ao vivo (endpoint separado — paridade com PHP que chama API a cada load).
  async function loadLiveBalance() {
    setLiveBalanceLoading(true)
    try {
      const b = await api<MEBalanceResp>('/tpc-config/me-balance')
      setLiveBalance(b)
    } catch {
      // Degradação silenciosa — exibe KPI estático se falhar.
    } finally {
      setLiveBalanceLoading(false)
    }
  }

  useEffect(() => {
    loadAll()
  }, [])

  // Carrega saldo ao vivo após config principal estar disponível.
  useEffect(() => {
    loadLiveBalance()
  }, [])

  // Busca de usuários com debounce simples no campo de pesquisa.
  useEffect(() => {
    const t = setTimeout(async () => {
      try {
        const usrs = await api<{ items: UserSearchItem[] }>(
          `/users/search?q=${encodeURIComponent(userSearch)}`,
        )
        setUsers(usrs.items || [])
      } catch {
        // silencioso
      }
    }, 300)
    return () => clearTimeout(t)
  }, [userSearch])

  // Salva configuração geral. Token/secret só vão se o input foi preenchido E
  // o campo não está sob controle de env.
  async function handleSave() {
    if (!cfg) return
    if (pixValidMinutes < 5 || pixValidMinutes > 1440) {
      showToast('err', 'PIX válido deve estar entre 5 e 1440 minutos.')
      return
    }
    setSaving(true)
    try {
      const body: any = {
        saldo_minimo: saldoMinimo,
        pix_valid_minutes: pixValidMinutes,
        pix_auto_cancel_expired: pixAutoCancel,
        enforce_wallet_on_label: enforceWallet,
        block_duplicate_label: blockDuplicate,
        checkout_template_id: checkoutTemplateID,
      }
      if (meTokenInput.trim() !== '' && !cfg.me.token_from_env) {
        body.me_token = meTokenInput.trim()
      }
      if (whSecretInput.trim() !== '' && !cfg.me.webhook_secret_from_env) {
        body.webhook_secret = whSecretInput.trim()
      }
      await api('/tpc-config', { method: 'POST', body: JSON.stringify(body) })
      showToast('ok', 'Configurações salvas.')
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar')
    } finally {
      setSaving(false)
    }
  }

  // Regenera webhook secret. Confirm nativo (paridade com AuditEngine.tsx).
  async function handleRegenerate(which: 'webhook' | 'jwt') {
    if (!cfg) return
    const label = which === 'webhook' ? 'Webhook' : 'JWT'
    const fromEnv =
      which === 'webhook' ? cfg.me.webhook_secret_from_env : cfg.me.jwt_secret_from_env
    if (fromEnv) {
      showToast('err', `Segredo ${label} está gerenciado por variável de ambiente.`)
      return
    }
    if (
      !window.confirm(
        `Gerar novo segredo ${label}?\n\nO valor antigo será invalidado imediatamente.\nServiços que dependem dele precisarão ser reconfigurados.\n\nContinuar?`,
      )
    )
      return
    try {
      const r = await api<{ ok: boolean; masked: string }>('/tpc-config/regenerate-secret', {
        method: 'POST',
        body: JSON.stringify({ which }),
      })
      showToast('ok', `Segredo ${label} regenerado: ${r.masked}`)
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao regenerar')
    }
  }

  // Copia para área de transferência (mostra feedback via toast).
  async function copyToClipboard(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      showToast('ok', 'Copiado para a área de transferência.')
    } catch {
      // Fallback silencioso — alguns browsers exigem HTTPS para clipboard.
      showToast('err', 'Não foi possível copiar (verifique permissões do navegador).')
    }
  }

  // ----- Wallet owners ---------------------------------------------------

  function handleAddOwner() {
    const cid = parseInt(addClassID.trim(), 10)
    const uid = parseInt(addUserID.trim(), 10)
    if (!isFinite(cid) || cid < 0) {
      showToast('err', 'Selecione uma classe de entrega.')
      return
    }
    if (!isFinite(uid) || uid <= 0) {
      showToast('err', 'Selecione um usuário.')
      return
    }
    // Resolve class_name do catálogo (fallback para #N).
    const cls = shippingClasses.find(c => c.id === cid)
    const className = cls ? cls.name : `Classe #${cid}`
    // Resolve dados do usuário do catálogo (fallback vazio).
    const usr = users.find(u => u.id === uid)
    // Substitui mapping existente se houver (mesmo class_id).
    setOwners(prev => {
      const filtered = prev.filter(p => p.class_id !== cid)
      return [
        ...filtered,
        {
          class_id: cid,
          class_name: className,
          user_id: uid,
          user_nome: usr?.nome || '',
          user_email: usr?.email || '',
        },
      ]
    })
    setAddClassID('')
    setAddUserID('')
    setShowAddOwner(false)
  }

  function handleRemoveOwner(class_id: number) {
    if (!window.confirm(`Remover mapeamento da classe #${class_id}?`)) return
    setOwners(prev => prev.filter(p => p.class_id !== class_id))
  }

  function handleOwnerUserIDChange(class_id: number, v: string) {
    const uid = parseInt(v, 10)
    const usr = isFinite(uid) && uid > 0 ? users.find(u => u.id === uid) : undefined
    setOwners(prev =>
      prev.map(p =>
        p.class_id === class_id
          ? {
              ...p,
              user_id: isFinite(uid) && uid > 0 ? uid : 0,
              user_nome: usr?.nome || p.user_nome,
              user_email: usr?.email || p.user_email,
            }
          : p,
      ),
    )
  }

  async function handleSaveOwners() {
    setSaving(true)
    try {
      await api('/tpc-config/wallet-owners', {
        method: 'POST',
        body: JSON.stringify({
          items: owners.map(o => ({ class_id: o.class_id, user_id: o.user_id })),
        }),
      })
      showToast('ok', 'Mapeamentos salvos.')
      await loadAll()
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar mapeamentos')
    } finally {
      setSaving(false)
    }
  }

  // ----- Render ----------------------------------------------------------

  if (loading) {
    return (
      <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
        Carregando configurações…
      </div>
    )
  }

  if (!cfg) {
    return (
      <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
        {err || 'Não foi possível carregar.'}
      </div>
    )
  }

  // Helper visual: banner env-managed.
  const envBanner = (
    <div
      style={{
        background: 'rgba(234,88,12,.08)',
        border: '1px solid rgba(234,88,12,.25)',
        color: 'var(--szv2-brand)',
        padding: '8px 12px',
        borderRadius: 8,
        fontSize: 13,
        marginBottom: 12,
      }}
    >
      Esta configuração está sendo gerenciada por variável de ambiente.
    </div>
  )

  // Saldo ME: usa saldo ao vivo se disponível, senão cai para valor estático do DB.
  const saldoME = liveBalance && !liveBalance.erro ? liveBalance.balance : cfg.me.saldo_atual_me
  const saldoAt = liveBalance?.fetched_at || cfg.me.saldo_at

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>TPC · Configurações</h1>
          <p>
            Tokens, segredos, regras da carteira e parâmetros do Motor Senderzz
          </p>
        </div>
      </div>

      {err && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
          {err}
        </div>
      )}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* ===== Card 1 — Melhor Envio ===== */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Melhor Envio</h2>
            <p className="szv2-card-sub">
              Token OAuth, segredo de webhook e saldo atual na conta ME.
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {/* Token */}
          <div className="szv2-field">
            <label className="szv2-label">Token de acesso ME</label>
            {cfg.me.token_from_env && envBanner}
            {/* Valor atual (read-only display). Nunca dentro do input editável
                para não contaminar o que o usuário digita ao rotacionar. */}
            {cfg.me.token_masked && (
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--szv2-text-muted)',
                  marginBottom: 6,
                }}
              >
                Atual: <code>{cfg.me.token_masked}</code>
              </div>
            )}
            {cfg.me.token_from_env ? (
              <input
                className="szv2-input"
                type="text"
                value={cfg.me.token_masked}
                readOnly
                style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }}
              />
            ) : (
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input
                  className="szv2-input"
                  type={showMeToken ? 'text' : 'password'}
                  value={meTokenInput}
                  onChange={e => setMeTokenInput(e.target.value)}
                  placeholder={
                    cfg.me.token_masked
                      ? 'Digite o novo token para substituir…'
                      : 'Cole o Bearer aqui'
                  }
                  autoComplete="off"
                  style={{ flex: 1 }}
                />
                <button
                  type="button"
                  className="szv2-btn-secondary"
                  onClick={() => setShowMeToken(s => !s)}
                >
                  {showMeToken ? 'Esconder' : 'Mostrar'}
                </button>
              </div>
            )}
            <p className="szv2-card-sub" style={{ marginTop: 4 }}>
              Token OAuth do Melhor Envio.{' '}
              <a
                href="https://melhorenvio.com.br/painel/gerenciar/tokens"
                target="_blank"
                rel="noreferrer"
              >
                Gerar token
              </a>
              .
            </p>
          </div>

          {/* Webhook secret */}
          <div className="szv2-field">
            <label className="szv2-label">Webhook secret</label>
            {cfg.me.webhook_secret_from_env && envBanner}
            {cfg.me.webhook_secret_masked && (
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--szv2-text-muted)',
                  marginBottom: 6,
                }}
              >
                Atual: <code>{cfg.me.webhook_secret_masked}</code>
              </div>
            )}
            {cfg.me.webhook_secret_from_env ? (
              <input
                className="szv2-input"
                type="text"
                value={cfg.me.webhook_secret_masked}
                readOnly
                style={{ color: 'var(--szv2-text-muted)', cursor: 'default' }}
              />
            ) : (
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input
                  className="szv2-input"
                  type={showWhSecret ? 'text' : 'password'}
                  value={whSecretInput}
                  onChange={e => setWhSecretInput(e.target.value)}
                  placeholder={
                    cfg.me.webhook_secret_masked
                      ? 'Digite o novo segredo para substituir…'
                      : 'Gere ou cole aqui'
                  }
                  autoComplete="off"
                  style={{ flex: 1 }}
                />
                <button
                  type="button"
                  className="szv2-btn-secondary"
                  onClick={() => setShowWhSecret(s => !s)}
                >
                  {showWhSecret ? 'Esconder' : 'Mostrar'}
                </button>
                <button
                  type="button"
                  className="szv2-btn-secondary"
                  onClick={() => handleRegenerate('webhook')}
                  title="Gerar novo segredo de 48 caracteres"
                >
                  Regenerar
                </button>
              </div>
            )}
            <p className="szv2-card-sub" style={{ marginTop: 4 }}>
              Chave para validar a assinatura dos webhooks PIX.
            </p>
          </div>

          {/* JWT secret — apenas botão regenerar (token não é exposto pela UI) */}
          <div className="szv2-field">
            <label className="szv2-label">JWT secret</label>
            {cfg.me.jwt_secret_from_env && envBanner}
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <button
                type="button"
                className="szv2-btn-secondary"
                onClick={() => handleRegenerate('jwt')}
                disabled={cfg.me.jwt_secret_from_env}
              >
                Regenerar JWT
              </button>
              <span className="szv2-card-sub">
                O segredo JWT só é exibido no momento da regeneração e nunca persistido
                em texto claro na UI.
              </span>
            </div>
          </div>

          {/* Webhook URL */}
          <div className="szv2-field">
            <label className="szv2-label">URL do webhook PIX</label>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input
                className="szv2-input"
                type="text"
                value={cfg.me.webhook_url}
                readOnly
                style={{ flex: 1, color: 'var(--szv2-text-muted)', cursor: 'default' }}
              />
              <button
                type="button"
                className="szv2-btn-secondary"
                onClick={() => copyToClipboard(cfg.me.webhook_url)}
              >
                Copiar
              </button>
            </div>
            <p className="szv2-card-sub" style={{ marginTop: 4 }}>
              Configure esta URL no painel do Melhor Envio para receber notificações de
              PIX pago.
            </p>
          </div>

          {/* Saldo atual ME — busca ao vivo (paridade com PHP que chama API a cada load) */}
          <div className="szv2-kpi">
            <span className="szv2-kpi-label">Saldo atual na conta ME</span>
            <span
              className="szv2-kpi-value"
              style={{ color: 'var(--szv2-brand)', fontSize: 32 }}
            >
              {liveBalanceLoading ? '…' : fmtBRL(saldoME)}
            </span>
            {liveBalance?.erro && (
              <span style={{ fontSize: 12, color: 'var(--szv2-text-muted)', display: 'block' }}>
                Aviso: {liveBalance.erro}
              </span>
            )}
            {saldoAt && !liveBalanceLoading && (
              <span className="szv2-kpi-meta">
                Atualizado em {saldoAt}
              </span>
            )}
            <button
              type="button"
              className="szv2-btn-secondary"
              style={{ marginTop: 8, fontSize: 12 }}
              onClick={loadLiveBalance}
              disabled={liveBalanceLoading}
            >
              {liveBalanceLoading ? 'Consultando…' : 'Atualizar saldo'}
            </button>
          </div>
        </div>
      </div>

      {/* ===== Card 2 — Regras da Carteira ===== */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Regras da carteira</h2>
            <p className="szv2-card-sub">Limites e alertas do saldo do cliente.</p>
          </div>
        </div>

        <div className="szv2-field">
          <label className="szv2-label">Saldo mínimo para alerta (R$)</label>
          <input
            className="szv2-input"
            type="number"
            min="0"
            step="0.01"
            value={isFinite(saldoMinimo) ? saldoMinimo : 0}
            onChange={e => setSaldoMinimo(parseFloat(e.target.value) || 0)}
            style={{ maxWidth: 180 }}
          />
          <p className="szv2-card-sub" style={{ marginTop: 4 }}>
            Cliente verá alerta de saldo baixo quando saldo ≤ esse valor.
          </p>
        </div>
      </div>

      {/* ===== Card 3 — Motor Senderzz ===== */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Motor Senderzz</h2>
            <p className="szv2-card-sub">
              Controles do fluxo de PIX, emissão de etiquetas e template de checkout.
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
          {/* PIX válido (min) */}
          <div className="szv2-field">
            <label className="szv2-label">
              Validade visual do PIX (minutos) — atual: {pixValidMinutes}
            </label>
            <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
              <input
                type="range"
                min={5}
                max={1440}
                step={1}
                value={pixValidMinutes}
                onChange={e => setPixValidMinutes(parseInt(e.target.value, 10) || 30)}
                style={{ flex: 1 }}
              />
              <input
                className="szv2-input"
                type="number"
                min={5}
                max={1440}
                step={1}
                value={pixValidMinutes}
                onChange={e => {
                  const v = parseInt(e.target.value, 10)
                  if (isFinite(v)) setPixValidMinutes(v)
                }}
                style={{ width: 100 }}
              />
            </div>
            <p className="szv2-card-sub" style={{ marginTop: 4 }}>
              Usado quando a API não retorna expiração clara. Range permitido: 5–1440.
            </p>
          </div>

          {/* Toggle: PIX auto-cancel */}
          <label
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 10,
              cursor: 'pointer',
            }}
          >
            <input
              type="checkbox"
              checked={pixAutoCancel}
              onChange={e => setPixAutoCancel(e.target.checked)}
              style={{ marginTop: 3 }}
            />
            <div>
              <div style={{ fontWeight: 600 }}>Cancelar PIX expirado automaticamente</div>
              <div className="szv2-card-sub">
                Cancela localmente recargas PIX vencidas se ainda estiverem pendentes.
              </div>
            </div>
          </label>

          {/* Toggle: enforce wallet on label */}
          <label
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 10,
              cursor: 'pointer',
            }}
          >
            <input
              type="checkbox"
              checked={enforceWallet}
              onChange={e => setEnforceWallet(e.target.checked)}
              style={{ marginTop: 3 }}
            />
            <div>
              <div style={{ fontWeight: 600 }}>Validar saldo ao emitir etiqueta</div>
              <div className="szv2-card-sub">
                Bloqueia emissão se o cliente não tiver saldo disponível. Não bloqueia o
                checkout.
              </div>
            </div>
          </label>

          {/* Toggle: block duplicate label */}
          <label
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 10,
              cursor: 'pointer',
            }}
          >
            <input
              type="checkbox"
              checked={blockDuplicate}
              onChange={e => setBlockDuplicate(e.target.checked)}
              style={{ marginTop: 3 }}
            />
            <div>
              <div style={{ fontWeight: 600 }}>Bloquear etiqueta duplicada</div>
              <div className="szv2-card-sub">
                Impede gerar nova etiqueta se o pedido já tiver protocolo/rastreio.
              </div>
            </div>
          </label>

          {/* Checkout template */}
          <div className="szv2-field">
            <label className="szv2-label">Template de Checkout FunnelKit</label>
            <input
              className="szv2-input"
              type="number"
              min={1}
              step={1}
              value={isFinite(checkoutTemplateID) ? checkoutTemplateID : 140}
              onChange={e => setCheckoutTemplateID(parseInt(e.target.value, 10) || 140)}
              style={{ maxWidth: 180 }}
            />
            <p className="szv2-card-sub" style={{ marginTop: 4 }}>
              ID do post <code>wfacp_checkout</code> usado como template (padrão 140).
            </p>
          </div>
        </div>
      </div>

      {/* ===== Card 4 — Dono Financeiro por Classe (collapsible) ===== */}
      <div className="szv2-card" style={{ marginBottom: 20 }}>
        <div
          className="szv2-card-head"
          style={{ cursor: 'pointer' }}
          onClick={() => setOwnersOpen(o => !o)}
        >
          <div>
            <h2>Dono financeiro por classe de entrega</h2>
            <p className="szv2-card-sub">
              {ownersOpen ? 'Clique para recolher' : 'Clique para expandir'} ·{' '}
              {owners.length} mapeamento(s)
            </p>
          </div>
          <span
            style={{
              fontSize: 18,
              color: 'var(--szv2-text-muted)',
              transform: ownersOpen ? 'rotate(180deg)' : 'rotate(0deg)',
              transition: 'transform .15s',
            }}
          >
            ▼
          </span>
        </div>

        {ownersOpen && (
          <div>
            <p className="szv2-card-sub" style={{ marginBottom: 12 }}>
              Use a classe de entrega do WooCommerce para definir qual carteira paga o frete.
              A linha "Produtos sem classe" corresponde a term_id=0 (produtos sem classe atribuída).
            </p>

            {owners.length === 0 ? (
              <div
                style={{
                  padding: 24,
                  textAlign: 'center',
                  color: 'var(--szv2-text-muted)',
                  border: '1px dashed var(--szv2-border)',
                  borderRadius: 8,
                  marginBottom: 12,
                }}
              >
                Nenhum mapeamento configurado.
              </div>
            ) : (
              <div style={{ overflowX: 'auto', marginBottom: 12 }}>
                <table className="szv2-table">
                  <thead>
                    <tr>
                      <th>Classe de entrega</th>
                      <th>Usuário</th>
                      <th style={{ width: 100 }}>Ação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {owners.map(o => (
                      <tr key={o.class_id}>
                        <td>
                          <span style={{ fontWeight: 500 }}>{o.class_name}</span>
                          <span style={{ color: 'var(--szv2-text-muted)', marginLeft: 6, fontSize: 12 }}>
                            (ID: {o.class_id})
                          </span>
                        </td>
                        <td>
                          {/* Dropdown de usuário para o row existente */}
                          <select
                            className="szv2-input"
                            value={o.user_id || ''}
                            onChange={e => handleOwnerUserIDChange(o.class_id, e.target.value)}
                            style={{ minWidth: 200 }}
                          >
                            <option value="">— selecionar usuário —</option>
                            {users.map(u => (
                              <option key={u.id} value={u.id}>
                                {u.nome || u.email} {u.email && u.nome ? `(${u.email})` : ''}
                              </option>
                            ))}
                          </select>
                          {o.user_id > 0 && !users.find(u => u.id === o.user_id) && (
                            <span style={{ color: 'var(--szv2-text-muted)', fontSize: 12, marginLeft: 6 }}>
                              ID {o.user_id} (não encontrado no catálogo)
                            </span>
                          )}
                        </td>
                        <td>
                          <button
                            type="button"
                            className="szv2-btn-secondary"
                            onClick={() => handleRemoveOwner(o.class_id)}
                          >
                            Remover
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <button
                type="button"
                className="szv2-btn-secondary"
                onClick={() => setShowAddOwner(true)}
              >
                Adicionar mapeamento
              </button>
              <button
                type="button"
                className="szv2-btn-brand"
                onClick={handleSaveOwners}
                disabled={saving}
              >
                {saving ? 'Salvando…' : 'Salvar mapeamentos'}
              </button>
            </div>

            {/* Mini-modal inline para adicionar — usa dropdowns de classe e usuário */}
            {showAddOwner && (
              <div
                style={{
                  marginTop: 12,
                  padding: 12,
                  border: '1px solid var(--szv2-border)',
                  borderRadius: 8,
                  background: 'rgba(234,88,12,.04)',
                }}
              >
                <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end', flexWrap: 'wrap' }}>
                  {/* Dropdown de classe de entrega */}
                  <div style={{ flex: 1, minWidth: 200 }}>
                    <label className="szv2-label">Classe de entrega</label>
                    <select
                      className="szv2-input"
                      value={addClassID}
                      onChange={e => setAddClassID(e.target.value)}
                    >
                      <option value="">— selecionar classe —</option>
                      {shippingClasses.map(c => (
                        <option key={c.id} value={c.id}>
                          {c.name} (ID: {c.id})
                        </option>
                      ))}
                    </select>
                  </div>

                  {/* Campo de busca + dropdown de usuário */}
                  <div style={{ flex: 1, minWidth: 220 }}>
                    <label className="szv2-label">Usuário</label>
                    <input
                      className="szv2-input"
                      type="text"
                      placeholder="Pesquisar por nome ou e-mail…"
                      value={userSearch}
                      onChange={e => setUserSearch(e.target.value)}
                      style={{ marginBottom: 4 }}
                    />
                    <select
                      className="szv2-input"
                      value={addUserID}
                      onChange={e => setAddUserID(e.target.value)}
                    >
                      <option value="">— selecionar usuário —</option>
                      {users.map(u => (
                        <option key={u.id} value={u.id}>
                          {u.nome || u.email} {u.email && u.nome ? `(${u.email})` : ''}
                        </option>
                      ))}
                    </select>
                  </div>

                  <button
                    type="button"
                    className="szv2-btn-brand"
                    onClick={handleAddOwner}
                  >
                    Adicionar
                  </button>
                  <button
                    type="button"
                    className="szv2-btn-secondary"
                    onClick={() => {
                      setShowAddOwner(false)
                      setAddClassID('')
                      setAddUserID('')
                      setUserSearch('')
                    }}
                  >
                    Cancelar
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* ===== Footer: Salvar Configurações ===== */}
      <div
        style={{
          marginTop: 24,
          display: 'flex',
          gap: 12,
          alignItems: 'center',
          justifyContent: 'flex-end',
          borderTop: '1px solid var(--szv2-border)',
          paddingTop: 16,
        }}
      >
        <button
          type="button"
          className="szv2-btn-secondary"
          onClick={loadAll}
          disabled={saving}
        >
          Recarregar
        </button>
        <button
          type="button"
          className="szv2-btn-brand"
          onClick={handleSave}
          disabled={saving}
          style={{ minWidth: 200 }}
        >
          {saving ? 'Salvando…' : 'Salvar configurações'}
        </button>
      </div>
    </div>
  )
}
