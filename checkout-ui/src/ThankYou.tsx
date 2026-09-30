import { Link } from 'react-router-dom'
import { formatBRL, type ScheduleType } from './api'
import type { OrderResult } from './App'
import { resolveBrand } from './brand'

export default function ThankYou({ result }: { result: OrderResult }) {
  const { order, offer, customerName, selectedDate } = result
  const isMotoboy = order.delivery_mode === 'motoboy'
  const priceLabel = offer.display_value || formatBRL(offer.value)
  const orderNumber = order.order_number || `#${order.order_id}`
  // Code ASSINADO para a URL de rastreio ("<order_number>-<sig>"). E opaco: nao
  // dividir nem exibir — so alimenta o link de acompanhamento. O numero humano
  // exibido continua sendo `orderNumber`.
  const trackCode = order.tracking_code
  const trackingBrand = resolveBrand(offer)
  const trackingQuery = new URLSearchParams({ cor: trackingBrand.accent })
  if (trackingBrand.hasCustomName) trackingQuery.set('brand', trackingBrand.name)
  if (trackingBrand.logoUrl) trackingQuery.set('logo', trackingBrand.logoUrl)
  const trackingHref = `/rastreio/${encodeURIComponent(trackCode)}?${trackingQuery.toString()}`

  // Tipo confirmado: o servidor manda a verdade; caimos para a escolha do cliente.
  const tipo: ScheduleType | undefined =
    order.sz_delivery_type ||
    (order.status === 'pre_agendado'
      ? 'pre_agendado'
      : order.status === 'agendado'
        ? 'agendamento'
        : selectedDate?.tipo)
  const isPre = tipo === 'pre_agendado'

  // Rotulo da data: servidor (delivery_text) > data escolhida no checkout.
  const dateLabel = order.delivery_text || selectedDate?.label || ''

  const tipoLabel = isPre ? 'Pré-agendamento' : tipo === 'agendamento' ? 'Agendamento' : ''

  return (
    <div>
      <div className="fk-center" style={{ minHeight: 'auto', paddingBottom: 12 }}>
        <div className="fk-ty-check">✓</div>
        <h1 className="fk-h1">Pedido confirmado!</h1>
        <p className="fk-sub">Recebemos seu pedido. Anote o número abaixo.</p>
        <div className="fk-ty-order">{orderNumber}</div>
      </div>

      <div className="fk-card">
        <h3 className="fk-card-title">Resumo</h3>
        <div className="fk-ty-line">
          <span className="k">Oferta</span>
          <span className="v">{offer.name}</span>
        </div>
        <div className="fk-ty-line">
          {/* Pagamento na entrega (COD) só existe no fluxo motoboy. Checkout misto
              finalizado como expedição é prepago — nunca mostrar "na entrega". */}
          <span className="k">{isMotoboy ? 'Valor a pagar na entrega' : 'Valor do pedido'}</span>
          <span className="v">{priceLabel}</span>
        </div>
        <div className="fk-ty-line">
          <span className="k">Cliente</span>
          <span className="v">{customerName}</span>
        </div>
        <div className="fk-ty-line">
          <span className="k">Entrega</span>
          <span className="v">
            {isMotoboy ? '🏍️ Motoboy' : '🚚 Expedição'}
            {!isMotoboy && order.delivery_text ? ` · ${order.delivery_text}` : ''}
          </span>
        </div>

        {/* Agendamento (somente motoboy com data) */}
        {isMotoboy && dateLabel && (
          <>
            <div className="fk-ty-line">
              <span className="k">Data de entrega</span>
              <span className="v">{dateLabel}</span>
            </div>
            {tipoLabel && (
              <div className="fk-ty-line">
                <span className="k">Tipo</span>
                <span className="v">
                  <span
                    className={`fk-date-tag ${isPre ? 'fk-date-tag-pre' : 'fk-date-tag-ag'}`}
                  >
                    {tipoLabel}
                  </span>
                </span>
              </div>
            )}
          </>
        )}
      </div>

      {/* Aviso de confirmacao quando pre-agendado */}
      {isMotoboy && isPre && (
        <div className="fk-card fk-card-warn">
          <span className="fk-badge fk-badge-pre">⏳ Pré-agendamento</span>
          <p className="fk-sub" style={{ marginTop: 10 }}>
            Confirme o pedido até <strong>3 dias de entrega antes</strong>
            {dateLabel ? (
              <>
                {' '}de <strong>{dateLabel}</strong>
              </>
            ) : null}
            , senão ele será <strong>cancelado automaticamente</strong>.
          </p>
        </div>
      )}

      {/* COD (pagamento na entrega) só existe no fluxo motoboy — expedição é sempre
          prepago, então este card nunca deve aparecer pra pedido de expedição. */}
      {isMotoboy && (
        <div className="fk-card">
          <span className="fk-badge">💵 Pague na entrega</span>
          <p className="fk-sub" style={{ marginTop: 10 }}>
            Você não paga nada agora. O pagamento de <strong>{priceLabel}</strong> é
            feito no momento da entrega.
          </p>
        </div>
      )}

      {/* CTA para a pagina de rastreio do pedido recem-criado. */}
      {trackCode && (
        <Link
          className="fk-btn fk-btn-track"
          to={trackingHref}
        >
          📦 Acompanhar meu pedido
        </Link>
      )}

      <p className="fk-note">
        {isMotoboy
          ? 'Guarde esta página. Em breve você receberá atualizações sobre a entrega.'
          : 'Você receberá atualizações sobre o envio do seu pedido.'}
      </p>
    </div>
  )
}
