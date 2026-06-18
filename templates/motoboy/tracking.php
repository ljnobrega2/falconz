<?php
/**
 * Senderzz — Tracking público do pedido motoboy
 * URL: /rastreio-motoboy/?pedido=ORDER_ID&key=WC_ORDER_KEY
 * O parâmetro ?key é obrigatório para reagendamento (validação de posse do pedido — V-SEC-03).
 */
if ( ! defined( 'ABSPATH' ) ) exit;
$order_id  = absint( $_GET['pedido'] ?? 0 );
$order_key = sanitize_text_field( wp_unslash( $_GET['key'] ?? '' ) );
$api_url   = rest_url( 'sz-motoboy/v1/tracking/' . $order_id );
get_header();
?>
<style>
.sz-track-wrap{max-width:480px;margin:40px auto;padding:0 16px}
.sz-track-card{background:#fff;border-radius:20px;padding:24px;box-shadow:0 4px 20px rgba(0,0,0,.08)}
.sz-track-logo{color:#E8650A;font-weight:700;font-size:var(--sz-text-xl);margin-bottom:20px}
.sz-step{display:flex;gap:14px;margin-bottom:16px;align-items:flex-start}
.sz-step-icon{width:36px;height:36px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:var(--sz-text-lg);flex-shrink:0;background:#f3f4f6}
.sz-step-icon.done{background:#dcfce7}
.sz-step-icon.active{background:#E8650A;color:#fff;box-shadow:0 0 0 4px rgba(232,101,10,.2)}
.sz-step-body{flex:1}
.sz-step-label{font-weight:700;font-size:var(--sz-text-md);color:#111827}
.sz-step-sub{font-size:var(--sz-text-meta);color:#9ca3af;margin-top:2px}
.sz-divider{width:2px;height:20px;background:#e5e7eb;margin-left:17px;margin-bottom:4px}
.sz-divider.done{background:#86efac}
.sz-status-badge{display:inline-block;padding:6px 16px;border-radius:999px;font-weight:700;font-size:var(--sz-text-base);margin-bottom:20px}
.sz-motoboy-location{background:#f0fdf4;border:1px solid #86efac;border-radius:12px;padding:12px;margin-top:16px;font-size:var(--sz-text-base);color:#15803d;font-weight:600}
.sz-reschedule-box{margin-top:18px;padding:14px;border:1px solid #fed7aa;background:#fff7ed;border-radius:14px}
.sz-reschedule-box label{display:block;font-size:var(--sz-text-meta);font-weight:700;color:#9a3412;margin-bottom:8px}
.sz-reschedule-row{display:flex;gap:8px}.sz-reschedule-row input{flex:1;border:1px solid #e5e7eb;border-radius:10px;padding:0 10px;height:40px}.sz-reschedule-row button{border:0;border-radius:10px;background:#E8650A;color:#fff;font-weight:700;padding:0 14px}
</style>

<div class="sz-track-wrap">
  <div class="sz-track-card">
    <div class="sz-track-logo">🏍️ Senderzz</div>
    <?php if ( ! $order_id ) : ?>
      <p>Pedido não informado.</p>
    <?php else : ?>
      <div id="trackContent">Carregando rastreio...</div>
    <?php endif; ?>
  </div>
</div>

<?php if ( $order_id ) : ?>
<script>
const rescheduleApi = '<?php echo esc_js( rest_url( 'sz-motoboy/v1/tracking/' . $order_id . '/reagendar' ) ); ?>';
const szOrderKey = '<?php echo esc_js( $order_key ); ?>';
const steps = [
  { key:'agendado',  icon:'📅', label:'Pedido agendado', sub:'Seu pedido foi recebido' },
  { key:'embalado',  icon:'📦', label:'Em preparação', sub:'Separando e embalando' },
  { key:'em_rota',   icon:'🚚', label:'Saiu para entrega', sub:'Motoboy a caminho' },
  { key:'entregue',  icon:'🎉', label:'Entregue', sub:'Pedido entregue com sucesso' },
];

const statusOrder = ['agendado','embalado','em_rota','entregue'];

async function loadTracking() {
  try {
    const r = await fetch('<?php echo esc_js($api_url); ?>');
    const data = await r.json();
    if (!data.ok) { document.getElementById('trackContent').innerHTML='<p>Pedido não encontrado.</p>'; return; }
    renderTracking(data.tracking);
    if (!['entregue','frustrado','cancelado'].includes(data.tracking.status)) {
      setTimeout(loadTracking, 15000);
    }
  } catch(e) { document.getElementById('trackContent').innerHTML='<p>Erro ao carregar rastreio.</p>'; }
}

function renderTracking(t) {
  const curIdx = statusOrder.indexOf(t.status === 'reagendado' ? 'agendado' : t.status);
  let html = '';

  if (t.status === 'frustrado') {
    html += '<div class="sz-status-badge" style="background:#fee2e2;color:#991b1b">❌ Tentativa de entrega frustrada</div>';
    if (t.reagendado_para) html += `<p style="font-size:var(--sz-text-base);color:#374151">Nova tentativa agendada para: <strong>${t.reagendado_para}</strong></p>`;
    html += rescheduleBox();
    document.getElementById('trackContent').innerHTML = html;
    return;
  }

  steps.forEach((step, i) => {
    const isDone   = i <= curIdx;
    const isActive = i === curIdx;
    html += `
      <div class="sz-step">
        <div class="sz-step-icon ${isActive?'active':isDone?'done':''}">${step.icon}</div>
        <div class="sz-step-body">
          <div class="sz-step-label" style="color:${isDone?'#111827':'#9ca3af'}">${step.label}</div>
          <div class="sz-step-sub">${step.sub}</div>
        </div>
      </div>
      ${i < steps.length-1 ? `<div class="sz-divider ${isDone?'done':''}"></div>` : ''}
    `;
  });

  if (t.status === 'em_rota') {
    html += '<div class="sz-motoboy-location">🏍️ Motoboy chegando! Prepare-se para receber seu pedido.</div>';
  }

  if (['agendado','reagendado','frustrado'].includes(t.status)) {
    html += rescheduleBox();
  }

  document.getElementById('trackContent').innerHTML = html;
}


function minDate() {
  const d = new Date();
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
  return d.toISOString().slice(0,10);
}

function rescheduleBox() {
  const min = minDate();
  return `
    <div class="sz-reschedule-box">
      <label>📅 Reagendar entrega</label>
      <div class="sz-reschedule-row">
        <input id="szClientRescheduleDate" type="date" min="${min}" value="${min}">
        <button type="button" onclick="clientReschedule()">Confirmar</button>
      </div>
      <div id="szClientRescheduleMsg" style="font-size:var(--sz-text-meta);margin-top:8px;color:#9a3412"></div>
    </div>`;
}

async function clientReschedule() {
  const input = document.getElementById('szClientRescheduleDate');
  const msg = document.getElementById('szClientRescheduleMsg');
  if (!input || !input.value) return;
  if (!szOrderKey) { msg.textContent = 'Link inválido. Use o link do e-mail de confirmação.'; return; }
  msg.textContent = 'Salvando...';
  try {
    const r = await fetch(rescheduleApi, {
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body: JSON.stringify({date: input.value, key: szOrderKey})
    });
    const data = await r.json();
    if (!data.ok) { msg.textContent = data.erro || 'Não foi possível reagendar.'; return; }
    msg.textContent = data.message || 'Entrega reagendada.';
    setTimeout(loadTracking, 600);
  } catch(e) {
    msg.textContent = 'Erro ao reagendar. Tente novamente.';
  }
}

loadTracking();
</script>
<?php endif; ?>
<?php get_footer(); ?>
