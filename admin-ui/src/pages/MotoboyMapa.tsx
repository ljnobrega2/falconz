import { useEffect, useRef, useState } from 'react'
import { api } from '../api'

// ── Tipos ────────────────────────────────────────────────────────────────────

type MotoboiLocation = {
  id: number
  nome: string
  zona_nome: string
  cd: string
  pedidos_abertos: number
  entregues_hoje: number
  ultimo_lat: number | null
  ultimo_lng: number | null
  ultimo_ping: string | null
  online: boolean
}

type MapCenter = { lat: number; lng: number }

type LocationsResp = {
  motoboys: MotoboiLocation[]
  center: MapCenter
  updated_at: string
}

// ── Utilitário: "há Xmin atrás" ─────────────────────────────────────────────

function minutosAtras(ping: string | null): string {
  if (!ping) return 'sem sinal'
  const diff = Math.floor((Date.now() - new Date(ping).getTime()) / 60000)
  if (diff < 1) return 'agora'
  if (diff === 1) return 'há 1 min'
  return `há ${diff} min`
}

// ── Componente principal ─────────────────────────────────────────────────────

export default function MotoboyMapa() {
  const [motoboys, setMotoboys] = useState<MotoboiLocation[]>([])
  const [center, setCenter] = useState<MapCenter>({ lat: -23.55, lng: -46.63 })
  const [updatedAt, setUpdatedAt] = useState('')
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [leafletReady, setLeafletReady] = useState(false)
  const [countdown, setCountdown] = useState(30)

  // Refs para o mapa Leaflet — evitam re-render e memory leak.
  const mapRef = useRef<any>(null)
  const markersRef = useRef<Record<number, any>>({})

  // ── Carrega Leaflet via CDN (sem npm install) ─────────────────────────────
  useEffect(() => {
    // CSS
    if (!document.getElementById('leaflet-css')) {
      const link = document.createElement('link')
      link.id = 'leaflet-css'
      link.rel = 'stylesheet'
      link.href = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css'
      document.head.appendChild(link)
    }

    // JS
    if (document.getElementById('leaflet-js')) {
      // Já carregado numa navegação anterior — marca pronto imediatamente.
      if ((window as any).L) setLeafletReady(true)
      return
    }
    const script = document.createElement('script')
    script.id = 'leaflet-js'
    script.src = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.js'
    script.onload = () => setLeafletReady(true)
    document.head.appendChild(script)
  }, [])

  // ── Fetch de dados ────────────────────────────────────────────────────────
  async function fetchLocations() {
    try {
      const data = await api<LocationsResp>('/motoboy-mapa/locations')
      setMotoboys(data.motoboys || [])
      setCenter(data.center)
      setUpdatedAt(data.updated_at)
      setErr('')
    } catch (e: any) {
      setErr(e.message || 'Erro ao carregar posições')
    } finally {
      setLoading(false)
    }
  }

  // Fetch inicial + auto-refresh a cada 30 segundos.
  useEffect(() => {
    fetchLocations()
    const dataTimer = setInterval(() => {
      fetchLocations()
      setCountdown(30)
    }, 30_000)

    // Countdown visual regressivo.
    const cdTimer = setInterval(() => setCountdown(c => (c > 0 ? c - 1 : 30)), 1_000)

    return () => {
      clearInterval(dataTimer)
      clearInterval(cdTimer)
    }
  }, [])

  // ── Inicializa mapa quando Leaflet estiver pronto ─────────────────────────
  useEffect(() => {
    if (!leafletReady) return
    initMap([center.lat, center.lng])
  }, [leafletReady])

  // ── Atualiza marcadores sempre que os dados mudam ─────────────────────────
  useEffect(() => {
    if (!leafletReady || !mapRef.current) return
    updateMarkers(motoboys)
  }, [motoboys, leafletReady])

  // ── Funções de mapa (sem hooks — chamadas diretas) ────────────────────────

  function initMap(coord: [number, number]) {
    if (mapRef.current) return
    const L = (window as any).L
    mapRef.current = L.map('sz-mapa', { center: coord, zoom: 11 })
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '© OpenStreetMap contributors',
    }).addTo(mapRef.current)
  }

  function makeIcon(mb: MotoboiLocation) {
    const L = (window as any).L
    const bg = mb.online ? '#EA580C' : '#9ca3af'
    const initial = mb.nome.charAt(0).toUpperCase()
    return L.divIcon({
      className: '',
      html: `<div style="background:${bg};width:28px;height:28px;border-radius:50%;display:flex;align-items:center;justify-content:center;color:white;font-size:11px;font-weight:700;border:2px solid white;box-shadow:0 2px 6px rgba(0,0,0,.3);opacity:${mb.online ? 1 : 0.4}">${initial}</div>`,
      iconSize: [28, 28],
      iconAnchor: [14, 14],
    })
  }

  function popupHtml(mb: MotoboiLocation): string {
    return `
      <div style="min-width:160px">
        <strong style="font-size:13px">${mb.nome}</strong><br/>
        <span style="color:#6b7280;font-size:11px">${mb.zona_nome || '—'}</span><br/>
        <hr style="margin:4px 0;border-color:#e5e7eb"/>
        Pedidos abertos: <b>${mb.pedidos_abertos}</b><br/>
        Entregues hoje: <b>${mb.entregues_hoje}</b><br/>
        <span style="color:#6b7280;font-size:11px">${minutosAtras(mb.ultimo_ping)}</span>
      </div>`
  }

  function updateMarkers(list: MotoboiLocation[]) {
    const L = (window as any).L
    list.forEach(mb => {
      // Sem GPS: não exibe marcador.
      if (mb.ultimo_lat == null || mb.ultimo_lng == null) return
      const pos: [number, number] = [mb.ultimo_lat, mb.ultimo_lng]
      const icon = makeIcon(mb)

      if (markersRef.current[mb.id]) {
        markersRef.current[mb.id]
          .setLatLng(pos)
          .setIcon(icon)
          .setPopupContent(popupHtml(mb))
      } else {
        markersRef.current[mb.id] = L.marker(pos, { icon })
          .bindPopup(popupHtml(mb))
          .addTo(mapRef.current)
      }
    })

    // Remove marcadores de motoboys que sumiram da lista.
    const idsAtivos = new Set(list.map(m => m.id))
    Object.keys(markersRef.current).forEach(key => {
      const id = Number(key)
      if (!idsAtivos.has(id)) {
        markersRef.current[id].remove()
        delete markersRef.current[id]
      }
    })
  }

  function flyTo(mb: MotoboiLocation) {
    if (!mapRef.current || mb.ultimo_lat == null || mb.ultimo_lng == null) return
    mapRef.current.flyTo([mb.ultimo_lat, mb.ultimo_lng], 14)
    markersRef.current[mb.id]?.openPopup()
  }

  const onlineCount = motoboys.filter(m => m.online).length

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <div>
      {err && (
        <div className="sz-alert-danger" style={{ marginBottom: 16 }}>
          {err}
        </div>
      )}

      {/* Cabeçalho */}
      <div className="szv2-card" style={{ marginBottom: 16 }}>
        <div className="szv2-card-head">
          <div>
            <h2>Motoboys em operação</h2>
            <p className="szv2-card-sub">
              {onlineCount} online de {motoboys.length} ativos
            </p>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {/* Badge de auto-refresh */}
            <span
              style={{
                fontSize: 12,
                color: 'var(--szv2-text-muted)',
                background: 'var(--szv2-bg-alt, #f3f4f6)',
                borderRadius: 999,
                padding: '3px 10px',
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              ↺ {countdown}s
            </span>
            <button
              type="button"
              className="szv2-btn-secondary"
              onClick={() => { fetchLocations(); setCountdown(30) }}
              disabled={loading}
            >
              ↺ Atualizar
            </button>
          </div>
        </div>
      </div>

      {/* Mapa */}
      <div className="szv2-card" style={{ padding: 0, overflow: 'hidden' }}>
        <div style={{ position: 'relative' }}>
          {/* Div do Leaflet */}
          <div
            id="sz-mapa"
            style={{ height: 480, width: '100%', background: '#e5e7eb' }}
          />

          {/* Estado vazio — overlay centralizado */}
          {!loading && leafletReady && onlineCount === 0 && (
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                pointerEvents: 'none',
                zIndex: 500,
              }}
            >
              <div
                style={{
                  background: 'rgba(255,255,255,.9)',
                  borderRadius: 8,
                  padding: '12px 24px',
                  color: 'var(--szv2-text-muted)',
                  fontSize: 14,
                  fontWeight: 500,
                }}
              >
                Nenhum motoboy em operação no momento.
              </div>
            </div>
          )}

          {/* Spinner de carregamento inicial */}
          {loading && (
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                background: 'rgba(255,255,255,.7)',
                zIndex: 600,
              }}
            >
              <span style={{ color: 'var(--szv2-text-muted)', fontSize: 14 }}>
                Carregando…
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Pills de status por motoboy */}
      {motoboys.length > 0 && (
        <div className="szv2-card" style={{ marginTop: 16 }}>
          <div style={{ marginBottom: 10 }}>
            <h3 style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>
              Status dos motoboys
            </h3>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
            {motoboys.map(mb => (
              <button
                key={mb.id}
                type="button"
                onClick={() => flyTo(mb)}
                title={mb.ultimo_lat == null ? 'Sem GPS' : 'Centralizar no mapa'}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  padding: '6px 12px',
                  borderRadius: 999,
                  border: '1px solid',
                  borderColor: mb.online ? '#EA580C' : '#d1d5db',
                  background: mb.online ? 'rgba(234,88,12,.07)' : '#f9fafb',
                  color: mb.online ? '#EA580C' : '#6b7280',
                  fontSize: 12,
                  fontWeight: 500,
                  cursor: mb.ultimo_lat != null ? 'pointer' : 'default',
                  transition: 'opacity .15s',
                  opacity: mb.ultimo_lat == null ? 0.6 : 1,
                }}
              >
                {/* Indicador de status online/offline */}
                <span
                  style={{
                    width: 8,
                    height: 8,
                    borderRadius: '50%',
                    background: mb.online ? '#22c55e' : '#9ca3af',
                    flexShrink: 0,
                  }}
                />
                <span>{mb.nome}</span>
                <span
                  style={{
                    background: mb.online ? '#EA580C' : '#9ca3af',
                    color: 'white',
                    borderRadius: 999,
                    padding: '1px 7px',
                    fontSize: 11,
                    fontWeight: 700,
                  }}
                >
                  {mb.pedidos_abertos}
                </span>
                <span style={{ color: '#9ca3af', fontSize: 11 }}>
                  {minutosAtras(mb.ultimo_ping)}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
