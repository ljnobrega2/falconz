// Checkouts & Links — porte fiel de templates/portal/v2/sections/links.php (Fase 6 — v432).
// Ligada ao go/portal (namespace /wp-json/senderzz/v1):
//   GET    /portal/links                       — lista escopada por role
//   POST   /portal/links/{id}/affiliate-toggle — liga/desliga visibilidade p/ afiliados (só produtor)
//   POST   /portal/links/{id}/commission       — atualiza comissão % (501: coluna não migrada)
//   DELETE /portal/links/{id}                  — exclui o link (só produtor)
//
// ESCOPO (validado contra go/portal/internal/handlers/links_portal.go):
//   - Produtor vê os DELE (producer_id); afiliado vê só os liberados (affiliate_visible)
//     dos produtores a que está vinculado. No admin nunca aparecem (escopo por dono).
//   - A lista do produtor vem do backend já PLANA: o espelho PG não tem link_motoboy_id,
//     então NÃO há agrupamento "principal + espelho Motoboy dentro do card". Cada linha
//     é um card; o badge sai de `tipo` (motoboy → "Motoboy", senão → "Expedição").
//     Como não há cascata de espelho no DELETE, CADA card (inclusive motoboy) tem seu
//     próprio botão excluir — senão links motoboy ficariam sem como remover.
//   - O afiliado só recebe tipo=motoboy (Cash on Delivery) e affiliate_visible=1 (o backend já filtra), e usa
//     affiliate_url (fallback p/ url se vier vazio). Produtor usa url.
//   - components_text vem sempre "" (não migrado) → a linha some, como no WP.
//   - Comissão por-link do produtor é degradada (sempre 0) e o POST /commission devolve 501:
//     mantemos o campo, mas o erro do backend vira toast — NUNCA fingimos sucesso.
//   - can_manage = !is_affiliate. O gate de sub-usuário do WP (!is_sub) NÃO é verificável
//     pelo contexto Go (PortalUser não tem parent_user_id) — divergência intencional, não bug.
//
// Brand só via var(--szv2-brand). Confirm = ConfirmDialog (confirmAsync), nunca window.confirm.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import { confirmAsync } from '../components/ConfirmDialog'
import EmptyState from '../components/EmptyState'
import { brl, num2, dt } from '../utils/format'

// ── Shape (espelha linkCard de links_portal.go) ─────────────────────────────────
type LinkCard = {
  id: number
  name: string
  components_text: string // sempre "" (degradado) — não renderiza
  tipo: string // correio | expedicao | motoboy | misto
  url: string
  affiliate_url: string // só afiliado; produtor = ""
  price_label: string
  display_value: number
  affiliate_visible: boolean
  affiliate_commission_pct: number
  created_at: string // YYYY-MM-DD
  banner_url?: string
}

type ListResp = {
  ok: boolean
  data: LinkCard[]
  total: number
  role: string
  is_affiliate: boolean
}

// Remove o protocolo do texto exibido (espelha preg_replace('#^https?://#','')).
function stripProto(u: string): string {
  return (u || '').replace(/^https?:\/\//, '')
}

// Valor exibido: price_label, ou brl(display_value) se houver valor, senão "".
function priceText(c: LinkCard): string {
  if (c.price_label) return c.price_label
  if (c.display_value > 0) return brl(c.display_value)
  return ''
}

export default function Links() {
  const toast = useToast()
  const [rows, setRows] = useState<LinkCard[]>([])
  const [isAff, setIsAff] = useState(false)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  // Rascunho de comissão por link (input controlado) — chave = link id.
  const [commDraft, setCommDraft] = useState<Record<number, string>>({})
  // Ações em andamento (desabilitam botões) — chaves "toggle-<id>", "comm-<id>", "del-<id>".
  const [busy, setBusy] = useState<Record<string, boolean>>({})

  function setBusyKey(key: string, v: boolean) {
    setBusy(prev => ({ ...prev, [key]: v }))
  }

  function load() {
    setLoading(true)
    setErr('')
    api<ListResp>('/portal/links')
      .then(r => {
        const list = r.data || []
        setRows(list)
        setIsAff(!!r.is_affiliate)
        // Inicializa rascunhos de comissão a partir do valor atual ("10.00").
        const draft: Record<number, string> = {}
        for (const c of list) draft[c.id] = c.affiliate_commission_pct.toFixed(2)
        setCommDraft(draft)
      })
      .catch(e => setErr(e.message || 'Erro ao carregar os links'))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  // ── Copiar link (client-side, espelha navigator.clipboard do WP) ──────────────
  function copy(url: string) {
    if (!url) return
    navigator.clipboard?.writeText(url).then(
      () => toast('ok', 'Link copiado.'),
      () => toast('err', 'Não foi possível copiar o link.'),
    )
  }

  // ── Toggle "Visível para afiliados" (só produtor) ─────────────────────────────
  async function toggleAff(c: LinkCard, enabled: boolean) {
    const key = `toggle-${c.id}`
    setBusyKey(key, true)
    // Otimista: reflete a mudança imediatamente; reverte no erro.
    setRows(prev => prev.map(r => (r.id === c.id ? { ...r, affiliate_visible: enabled } : r)))
    try {
      const resp = await api<{ ok: boolean; message: string; affiliate_visible: boolean }>(
        `/portal/links/${c.id}/affiliate-toggle`,
        { method: 'POST', body: JSON.stringify({ enabled }) },
      )
      toast('ok', resp.message || (enabled ? 'Oferta liberada para afiliados.' : 'Oferta removida dos afiliados.'))
    } catch (e: any) {
      setRows(prev => prev.map(r => (r.id === c.id ? { ...r, affiliate_visible: !enabled } : r)))
      toast('err', e.message || 'Erro ao salvar.')
    } finally {
      setBusyKey(key, false)
    }
  }

  // ── Salvar comissão (só produtor) ─────────────────────────────────────────────
  // Backend devolve 501 ("coluna não migrada"): NÃO fingimos sucesso — o erro vira toast.
  async function saveCommission(c: LinkCard) {
    const key = `comm-${c.id}`
    const raw = commDraft[c.id] ?? ''
    const pct = parseFloat(raw)
    if (!Number.isFinite(pct) || pct < 0 || pct > 100) {
      toast('err', 'A comissão deve ficar entre 0% e 100%.')
      return
    }
    setBusyKey(key, true)
    try {
      await api(`/portal/links/${c.id}/commission`, {
        method: 'POST',
        body: JSON.stringify({ commission_pct: pct }),
      })
      // Caminho de sucesso reservado p/ quando a coluna for migrada (hoje cai no catch/501).
      setRows(prev => prev.map(r => (r.id === c.id ? { ...r, affiliate_commission_pct: pct } : r)))
      toast('ok', 'Comissão atualizada.')
    } catch (e: any) {
      toast('err', e.message || 'Não foi possível atualizar a comissão.')
    } finally {
      setBusyKey(key, false)
    }
  }

  async function changeBanner(c: LinkCard, file: File) {
    const key = `banner-${c.id}`
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) { toast('err', 'Use uma imagem JPG, PNG ou WEBP.'); return }
    if (file.size > 8 * 1024 * 1024) { toast('err', 'O banner deve ter no máximo 8 MB.'); return }
    setBusyKey(key, true)
    try {
      const form = new FormData()
      form.append('image', file)
      const uploaded = await api<{ url: string }>('/portal/settings/brand/upload', { method: 'POST', body: form })
      await api(`/portal/links/${c.id}/banner`, { method: 'POST', body: JSON.stringify({ banner_url: uploaded.url }) })
      setRows(prev => prev.map(r => r.id === c.id ? { ...r, banner_url: uploaded.url } : r))
      toast('ok', 'Banner deste checkout atualizado.')
    } catch (e: any) { toast('err', e.message || 'Erro ao salvar banner.') }
    finally { setBusyKey(key, false) }
  }

  // ── Excluir link (só produtor) ────────────────────────────────────────────────
  async function remove(c: LinkCard) {
    const ok = await confirmAsync({
      title: 'Excluir oferta',
      message: `Excluir o checkout “${c.name || '—'}”? Esta ação não pode ser desfeita.`,
      confirmLabel: 'Excluir',
      danger: true,
    })
    if (!ok) return
    const key = `del-${c.id}`
    setBusyKey(key, true)
    try {
      const resp = await api<{ ok: boolean; message: string }>(`/portal/links/${c.id}`, { method: 'DELETE' })
      setRows(prev => prev.filter(r => r.id !== c.id))
      toast('ok', resp.message || 'Checkout excluído.')
    } catch (e: any) {
      toast('err', e.message || 'Erro ao excluir o checkout.')
    } finally {
      setBusyKey(key, false)
    }
  }

  // can_manage = !afiliado (gate de sub-usuário do WP não é verificável aqui — ver doc no topo).
  const canManage = !isAff

  return (
    <section id="sec-links" className="sz-sec" data-szv2-label="Checkouts & Links">
      <div className="szv2-page-head" style={{ marginBottom: 16 }}>
        <h2 className="szv2-page-title" style={{ margin: 0, fontSize: 18, fontWeight: 700, color: 'var(--szv2-text)' }}>
          Checkouts &amp; Links
        </h2>
        <p style={{ margin: '4px 0 0', fontSize: 13, color: 'var(--szv2-text-muted)' }}>
          Copie e compartilhe os links de pagamento dos seus produtos. Produtores veem Expedição, Cash on Delivery e Link único; afiliados veem apenas Cash on Delivery.
        </p>
      </div>

      {!!err && <div className="sz-alert-danger">{err}</div>}

      {loading ? (
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      ) : err ? (
        // Erro já exibido no alerta acima — não duplicar com empty-state.
        null
      ) : rows.length === 0 ? (
        <EmptyState
          title={isAff ? 'Nenhum link encontrado' : 'Nenhum checkout ainda'}
          description={
            isAff
              ? 'Nenhum produtor liberou checkouts para você ainda. Assim que liberarem, os links de divulgação aparecem aqui.'
              : 'Crie seu primeiro checkout no painel clássico para começar a vender. Os links aparecem aqui.'
          }
        />
      ) : (
        <div className="szv2-links-grid" id="szv2-links-list">
          {rows.map(c => {
            const tipo = (c.tipo || '').toLowerCase()
            const isMotoboy = tipo === 'motoboy'
            const isMisto = tipo === 'misto'
            // URL por perfil: afiliado usa affiliate_url (fallback url); produtor usa url.
            const url = isAff ? c.affiliate_url || c.url : c.url
            const price = priceText(c)
            const commPct = c.affiliate_commission_pct || 0
            // Toggle/comissão só fazem sentido no produtor e em ofertas não-motoboy.
            const showControls = canManage && !isMotoboy

            return (
              <div
                key={c.id}
                className="szv2-link-card"
                id={`szv2-link-card-${c.id}`}
                data-link-id={c.id}
              >
                {/* Cabeçalho: preço + excluir */}
                <div className="szv2-link-card-head">
                  <div className="szv2-link-card-meta">
                    {!!price && <span className="szv2-link-price szv2-num">{price}</span>}
                  </div>
                  {canManage && (
                    <button
                      type="button"
                      className="szv2-link-btn szv2-link-del-btn"
                      title="Excluir oferta"
                      disabled={!!busy[`del-${c.id}`]}
                      onClick={() => remove(c)}
                    >
                      ×
                    </button>
                  )}
                </div>

                <h3 className="szv2-link-name">{c.name || '—'}</h3>
                {/* components_text sempre "" (degradado) → linha oculta */}

                {/* Links de checkout disponíveis */}
                <div className="szv2-link-urls">
                  <div className="szv2-link-url-row">
                    <span
                      className={`sz-badge ${isMotoboy ? 'szv2-badge-warning' : 'szv2-badge-brand'} szv2-link-tipo-badge`}
                    >
                      {isMotoboy ? 'Cash on Delivery' : isMisto ? 'Link único' : 'Expedição'}
                    </span>
                    {url ? (
                      <>
                        <a
                          className="szv2-link-url-text"
                          href={url}
                          target="_blank"
                          rel="noopener noreferrer"
                        >
                          {stripProto(url)}
                        </a>
                        <button
                          type="button"
                          className="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-copy-btn"
                          title={isMotoboy ? 'Copiar link Cash on Delivery' : isMisto ? 'Copiar link único' : 'Copiar link Expedição'}
                          onClick={() => copy(url)}
                        >
                          Copiar
                        </button>
                      </>
                    ) : (
                      <span className="szv2-text-faint-val">Link indisponível</span>
                    )}
                  </div>
                </div>

                {canManage && (
                  <div style={{ marginTop: 12, paddingTop: 10, borderTop: '1px solid var(--szv2-border)' }}>
                    <div className="szv2-label" style={{ marginBottom: 6 }}>Banner deste checkout</div>
                    <label className="szv2-btn szv2-btn-secondary szv2-btn-sm" style={{ display: 'inline-flex', cursor: busy[`banner-${c.id}`] ? 'wait' : 'pointer' }}>
                      {busy[`banner-${c.id}`] ? 'Enviando…' : c.banner_url ? 'Trocar banner' : 'Escolher banner'}
                      <input type="file" accept="image/png,image/jpeg,image/webp" hidden disabled={!!busy[`banner-${c.id}`]} onChange={e => { const f = e.target.files?.[0]; if (f) changeBanner(c, f); e.currentTarget.value = '' }} />
                    </label>
                    {c.banner_url && <span style={{ marginLeft: 8, fontSize: 11, color: 'var(--szv2-text-muted)' }}>Banner personalizado ativo</span>}
                  </div>
                )}

                {/* Rodapé — produtor */}
                {!isAff ? (
                  <div className="szv2-link-card-foot">
                    {showControls ? (
                      <>
                        <label className="szv2-link-toggle-label">
                          <input
                            type="checkbox"
                            className="szv2-link-aff-toggle"
                            checked={!!c.affiliate_visible}
                            disabled={!!busy[`toggle-${c.id}`]}
                            onChange={e => toggleAff(c, e.target.checked)}
                          />
                          <span>Visível para afiliados</span>
                        </label>
                        <div className="szv2-link-comm-row">
                          <label className="szv2-label" htmlFor={`szv2-link-comm-${c.id}`}>
                            Comissão %
                          </label>
                          <input
                            id={`szv2-link-comm-${c.id}`}
                            type="number"
                            className="szv2-input szv2-link-comm-input"
                            value={commDraft[c.id] ?? ''}
                            min={0}
                            max={100}
                            step={0.01}
                            onChange={e => setCommDraft(prev => ({ ...prev, [c.id]: e.target.value }))}
                          />
                          <button
                            type="button"
                            className="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-link-comm-save"
                            disabled={!!busy[`comm-${c.id}`]}
                            onClick={() => saveCommission(c)}
                          >
                            Salvar
                          </button>
                        </div>
                      </>
                    ) : null /* Card motoboy (produtor): sem toggle/comissão — motoboy nunca é exposto a afiliados (sem rótulo de visibilidade, fiel ao WP). */}
                    {!!c.created_at && <span className="szv2-link-date">{dt(c.created_at)}</span>}
                  </div>
                ) : (
                  commPct > 0 && (
                    <div className="szv2-link-card-foot">
                      <span className="szv2-link-info-label">Sua comissão: {num2(commPct)}%</span>
                    </div>
                  )
                )}
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
