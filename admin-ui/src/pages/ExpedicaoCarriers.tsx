// Catálogo global de transportadoras (Melhor Envio) — expedicao.
// Admin escolhe quais companies/services aparecem para TODOS os produtores.
// A seleção salva aqui é o teto do mecanismo de preferida/bloqueada por
// produtor (Portal → Frete): fora daqui, o produtor não vê a opção.
import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from '../hooks/useToast'
import TableSkeleton from '../components/TableSkeleton'
import EmptyState from '../components/EmptyState'

type Service = { id: number; name: string; type?: string; enabled: boolean }
type Company = { id: number; name: string; picture?: string; enabled: boolean; services: Service[] }

export default function ExpedicaoCarriers() {
  const showToast = useToast()
  const [companies, setCompanies] = useState<Company[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const r = await api<{ companies: Company[] }>('/expedicao/carriers/live')
      setCompanies(r.companies ?? [])
    } catch (e: any) {
      setErr(e.message || 'Falha ao consultar catálogo na Melhor Envio')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  function toggleCompany(companyId: number, enabled: boolean) {
    setCompanies(cs => cs.map(c => c.id === companyId
      ? { ...c, enabled, services: c.services.map(s => ({ ...s, enabled: enabled ? s.enabled : false })) }
      : c))
  }

  function toggleService(companyId: number, serviceId: number, enabled: boolean) {
    setCompanies(cs => cs.map(c => {
      if (c.id !== companyId) return c
      const services = c.services.map(s => s.id === serviceId ? { ...s, enabled } : s)
      const anyEnabled = services.some(s => s.enabled)
      return { ...c, services, enabled: c.enabled || anyEnabled }
    }))
  }

  async function save() {
    setSaving(true)
    try {
      await api('/expedicao/carriers/catalog', { method: 'PUT', body: JSON.stringify({ companies }) })
      showToast('ok', 'Catálogo de transportadoras salvo. Já vale para todos os produtores.')
    } catch (e: any) {
      showToast('err', e.message || 'Falha ao salvar catálogo')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <div className="szv2-section-head">
        <div>
          <h1>Transportadoras (Melhor Envio)</h1>
          <p>Selecione companies e serviços visíveis para todos os produtores. Fora daqui, ninguém enxerga.</p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="szv2-btn szv2-btn-secondary" onClick={load} disabled={loading}>Recarregar da ME</button>
          <button className="szv2-btn szv2-btn-primary" onClick={save} disabled={saving || loading}>
            {saving ? 'Salvando…' : 'Salvar catálogo'}
          </button>
        </div>
      </div>

      {err && <div className="sz-alert-danger">{err}</div>}

      {loading ? (
        <TableSkeleton rows={5} cols={2} />
      ) : companies.length === 0 ? (
        <EmptyState
          icon="🚚"
          title="Nenhuma transportadora retornada pela Melhor Envio."
          description="Verifique se ME_TOKEN está configurado no labels-service."
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {companies.map(c => (
            <div key={c.id} className="szv2-card" style={{ padding: 16 }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontWeight: 600, marginBottom: 8 }}>
                <input type="checkbox" checked={c.enabled} onChange={e => toggleCompany(c.id, e.target.checked)} />
                {c.picture && <img src={c.picture} alt="" style={{ width: 20, height: 20, objectFit: 'contain' }} />}
                {c.name}
              </label>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 16, paddingLeft: 30 }}>
                {c.services.map(s => (
                  <label key={s.id} style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--szv2-text-soft)' }}>
                    <input
                      type="checkbox"
                      checked={s.enabled}
                      onChange={e => toggleService(c.id, s.id, e.target.checked)}
                    />
                    {s.name}
                  </label>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
