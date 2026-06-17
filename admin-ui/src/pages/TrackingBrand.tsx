import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

type BrandItem = {
  class_id: number
  class_name: string
  logo: string
  cor: string
  cor_texto: string
  nome: string
  rodape: string
}

type ShippingClass = {
  id: number
  name: string
}

function PreviewCard({ item }: { item: BrandItem }) {
  const bg = item.cor || '#E8650A'
  const fg = item.cor_texto || '#FFFFFF'
  return (
    <div
      style={{
        borderRadius: 10,
        overflow: 'hidden',
        border: '1px solid var(--szv2-border)',
        minWidth: 220,
      }}
    >
      {/* Cabeçalho da marca */}
      <div
        style={{
          background: bg,
          color: fg,
          padding: '10px 14px',
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        {item.logo && (
          <img
            src={item.logo}
            alt="logo"
            style={{ height: 28, objectFit: 'contain', borderRadius: 4 }}
            onError={e => { (e.currentTarget as HTMLImageElement).style.display = 'none' }}
          />
        )}
        <span style={{ fontWeight: 700, fontSize: 14 }}>
          {item.nome || 'Minha Marca'}
        </span>
      </div>
      {/* Corpo simulado */}
      <div style={{ padding: '10px 14px', background: 'var(--szv2-bg-card)' }}>
        <div style={{ fontSize: 12, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>
          Status: <strong style={{ color: bg }}>Em trânsito</strong>
        </div>
        <div
          style={{
            height: 6,
            borderRadius: 3,
            background: 'var(--szv2-border)',
            overflow: 'hidden',
          }}
        >
          <div style={{ width: '60%', height: '100%', background: bg }} />
        </div>
      </div>
      {/* Rodapé */}
      {item.rodape && (
        <div
          style={{
            padding: '6px 14px',
            fontSize: 11,
            color: 'var(--szv2-text-muted)',
            borderTop: '1px solid var(--szv2-border)',
            background: 'var(--szv2-bg)',
          }}
        >
          {item.rodape}
        </div>
      )}
    </div>
  )
}

export default function TrackingBrand() {
  const [items, setItems] = useState<BrandItem[]>([])
  const [shippingClasses, setShippingClasses] = useState<ShippingClass[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [toast, setToast] = useState<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  const [addBusy, setAddBusy] = useState(false)
  // classe selecionada no dropdown para adicionar (classes ainda não configuradas)
  const [selectedNewClass, setSelectedNewClass] = useState<string>('')
  const localRef = useRef<BrandItem[]>([])

  function showToast(kind: 'ok' | 'err', msg: string) {
    setToast({ kind, msg })
    setTimeout(() => setToast(null), 5000)
  }

  // Mescla as classes de envio existentes com os dados de marca configurados.
  // Garante que toda classe de envio aparece como linha editável, mesmo sem configuração salva.
  function mergeClassesWithBrands(classes: ShippingClass[], brands: BrandItem[]): BrandItem[] {
    const brandMap = new Map<number, BrandItem>()
    for (const b of brands) {
      brandMap.set(b.class_id, b)
    }

    const merged: BrandItem[] = []

    // Primeiro as classes de envio (ordem: id=0 "Padrão" no topo, demais por nome).
    for (const sc of classes) {
      const existing = brandMap.get(sc.id)
      if (existing) {
        // Já configurada — usa dados salvos mas garante class_name do catálogo.
        merged.push({ ...existing, class_name: sc.name })
      } else {
        // Não configurada — linha em branco com defaults do WP (#E8650A / #ffffff).
        merged.push({
          class_id: sc.id,
          class_name: sc.name,
          logo: '',
          cor: '#E8650A',
          cor_texto: '#ffffff',
          nome: '',
          rodape: '',
        })
      }
      brandMap.delete(sc.id)
    }

    // Classes que estão no banco mas não mais no catálogo (classes removidas do WC).
    for (const orphan of brandMap.values()) {
      merged.push(orphan)
    }

    return merged
  }

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const [brandsRes, classesRes] = await Promise.all([
        api<{ items: BrandItem[] }>('/tracking-brand'),
        api<{ items: ShippingClass[] }>('/shipping-classes'),
      ])
      const classes = classesRes.items || []
      const brands = brandsRes.items || []
      setShippingClasses(classes)
      const merged = mergeClassesWithBrands(classes, brands)
      setItems(merged)
      localRef.current = merged
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function updateItem(classID: number, field: keyof BrandItem, value: string) {
    setItems(prev =>
      prev.map(it => it.class_id === classID ? { ...it, [field]: value } : it)
    )
  }

  async function handleSave() {
    setSaving(true)
    try {
      const res = await api<{ items: BrandItem[] }>('/tracking-brand', {
        method: 'POST',
        body: JSON.stringify({ items }),
      })
      const brands = res.items || []
      const merged = mergeClassesWithBrands(shippingClasses, brands)
      setItems(merged)
      showToast('ok', 'Marcas salvas com sucesso.')
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao salvar')
    } finally {
      setSaving(false)
    }
  }

  // Classes ainda não exibidas (não estão em items com origem do catálogo).
  // Usado no dropdown de adição manual para classes-órfãs ou quando o catálogo falha.
  const configuredIds = new Set(items.map(it => it.class_id))
  const availableToAdd = shippingClasses.filter(sc => !configuredIds.has(sc.id))

  async function handleAddClass() {
    const id = selectedNewClass !== '' ? parseInt(selectedNewClass, 10) : NaN
    if (isNaN(id) || id < 0) {
      showToast('err', 'Selecione uma classe válida.')
      return
    }
    setAddBusy(true)
    try {
      const res = await api<{ items: BrandItem[] }>('/tracking-brand/add-class', {
        method: 'POST',
        body: JSON.stringify({ class_id: id }),
      })
      const brands = res.items || []
      const merged = mergeClassesWithBrands(shippingClasses, brands)
      setItems(merged)
      setSelectedNewClass('')
      const cls = shippingClasses.find(sc => sc.id === id)
      showToast('ok', `Classe "${cls?.name ?? `#${id}`}" adicionada.`)
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao adicionar classe')
    } finally {
      setAddBusy(false)
    }
  }

  async function handleDelete(classID: number, className: string) {
    if (!window.confirm(`Remover marca da classe "${className}"?`)) return
    try {
      const res = await api<{ items: BrandItem[] }>(`/tracking-brand/${classID}`, {
        method: 'DELETE',
      })
      const brands = res.items || []
      // Após remover, re-mesclar para que a linha volte como "não configurada"
      // (mantém a classe de envio visível, apenas reseta os campos).
      const merged = mergeClassesWithBrands(shippingClasses, brands)
      setItems(merged)
      showToast('ok', `Classe "${className}" removida.`)
    } catch (e: any) {
      showToast('err', e.message || 'Erro ao remover')
    }
  }

  return (
    <div>
      {err && <div className="sz-alert-danger" style={{ marginBottom: 16 }}>{err}</div>}
      {toast && (
        <div
          className={toast.kind === 'ok' ? 'sz-alert-success' : 'sz-alert-danger'}
          style={{ marginBottom: 16 }}
        >
          {toast.msg}
        </div>
      )}

      {/* Barra superior — cabeçalho + adicionar classe extra (apenas quando não está no catálogo) */}
      <div className="szv2-card" style={{ marginBottom: 24 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Marca por Classe de Envio</h2>
            <p className="szv2-card-sub">
              Personaliza logo, cores e rodapé exibidos na página de rastreio por classe de envio.
              Todas as classes de envio cadastradas no WooCommerce aparecem abaixo automaticamente.
            </p>
          </div>
          {/* Só mostra o seletor de adição manual quando há classes fora do catálogo carregado */}
          {availableToAdd.length > 0 && (
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <select
                value={selectedNewClass}
                onChange={e => setSelectedNewClass(e.target.value)}
                className="szv2-input"
                style={{ minWidth: 200 }}
              >
                <option value="">Selecione uma classe…</option>
                {availableToAdd.map(sc => (
                  <option key={sc.id} value={sc.id}>{sc.name}</option>
                ))}
              </select>
              <button
                type="button"
                className="szv2-btn-brand"
                onClick={handleAddClass}
                disabled={addBusy || selectedNewClass === ''}
              >
                {addBusy ? 'Adicionando…' : '+ Adicionar'}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* Grid de cards */}
      {loading ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Carregando…
        </div>
      ) : items.length === 0 ? (
        <div style={{ padding: 48, textAlign: 'center', color: 'var(--szv2-text-muted)' }}>
          Nenhuma classe de envio cadastrada no WooCommerce.
        </div>
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(2, minmax(0,1fr))',
            gap: 20,
            marginBottom: 24,
          }}
        >
          {items.map(item => (
            <div key={item.class_id} className="szv2-card">
              {/* Cabeçalho do card */}
              <div className="szv2-card-head" style={{ marginBottom: 16 }}>
                <div>
                  <h3 style={{ margin: 0 }}>{item.class_name || `Classe #${item.class_id}`}</h3>
                  <span className="szv2-card-sub">
                    {item.class_id === 0 ? 'Padrão — sem classe (class_id: 0)' : `class_id: ${item.class_id}`}
                  </span>
                </div>
                <button
                  type="button"
                  className="szv2-btn-danger"
                  onClick={() => handleDelete(item.class_id, item.class_name)}
                  style={{ fontSize: 12, padding: '4px 10px' }}
                >
                  Remover
                </button>
              </div>

              {/* Layout: formulário + preview lado a lado */}
              <div style={{ display: 'flex', gap: 20, alignItems: 'flex-start' }}>
                {/* Formulário */}
                <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <label style={{ fontSize: 13 }}>
                    <span style={{ display: 'block', marginBottom: 4, color: 'var(--szv2-text-muted)' }}>
                      Nome da marca
                    </span>
                    <input
                      type="text"
                      value={item.nome}
                      onChange={e => updateItem(item.class_id, 'nome', e.target.value)}
                      placeholder="Ex: Senderzz Express"
                      className="szv2-input"
                    />
                  </label>

                  <label style={{ fontSize: 13 }}>
                    <span style={{ display: 'block', marginBottom: 4, color: 'var(--szv2-text-muted)' }}>
                      URL do logo
                    </span>
                    <input
                      type="text"
                      value={item.logo}
                      onChange={e => updateItem(item.class_id, 'logo', e.target.value)}
                      placeholder="https://..."
                      className="szv2-input"
                    />
                  </label>

                  <div style={{ display: 'flex', gap: 12 }}>
                    <label style={{ fontSize: 13, flex: 1 }}>
                      <span style={{ display: 'block', marginBottom: 4, color: 'var(--szv2-text-muted)' }}>
                        Cor de fundo
                      </span>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <input
                          type="color"
                          value={item.cor || '#E8650A'}
                          onChange={e => updateItem(item.class_id, 'cor', e.target.value)}
                          style={{ width: 36, height: 36, border: 'none', cursor: 'pointer', borderRadius: 6 }}
                        />
                        <input
                          type="text"
                          value={item.cor}
                          onChange={e => updateItem(item.class_id, 'cor', e.target.value)}
                          placeholder="#E8650A"
                          className="szv2-input"
                          style={{ flex: 1 }}
                          maxLength={7}
                        />
                      </div>
                    </label>

                    <label style={{ fontSize: 13, flex: 1 }}>
                      <span style={{ display: 'block', marginBottom: 4, color: 'var(--szv2-text-muted)' }}>
                        Cor do texto
                      </span>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <input
                          type="color"
                          value={item.cor_texto || '#ffffff'}
                          onChange={e => updateItem(item.class_id, 'cor_texto', e.target.value)}
                          style={{ width: 36, height: 36, border: 'none', cursor: 'pointer', borderRadius: 6 }}
                        />
                        <input
                          type="text"
                          value={item.cor_texto}
                          onChange={e => updateItem(item.class_id, 'cor_texto', e.target.value)}
                          placeholder="#ffffff"
                          className="szv2-input"
                          style={{ flex: 1 }}
                          maxLength={7}
                        />
                      </div>
                    </label>
                  </div>

                  <label style={{ fontSize: 13 }}>
                    <span style={{ display: 'block', marginBottom: 4, color: 'var(--szv2-text-muted)' }}>
                      Texto do rodapé
                    </span>
                    <input
                      type="text"
                      value={item.rodape}
                      onChange={e => updateItem(item.class_id, 'rodape', e.target.value)}
                      placeholder="Ex: Dúvidas? falecom@empresa.com.br"
                      className="szv2-input"
                    />
                  </label>
                </div>

                {/* Preview ao vivo */}
                <div style={{ flexShrink: 0 }}>
                  <p style={{ fontSize: 11, color: 'var(--szv2-text-muted)', marginBottom: 6 }}>
                    Preview
                  </p>
                  <PreviewCard item={item} />
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Botão salvar global */}
      {items.length > 0 && (
        <div className="szv2-card">
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button
              type="button"
              className="szv2-btn-brand"
              onClick={handleSave}
              disabled={saving || loading}
            >
              {saving ? 'Salvando…' : 'Salvar todas as marcas'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
