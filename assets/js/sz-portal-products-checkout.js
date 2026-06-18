/**
 * Senderzz — Painel de produtos: gerador inline de checkouts.
 *
 * Externalizado do nowdoc SZ_PRODUCT_INLINE_ASSETS em Portal_Page::render_products_panel()
 * — REFACTOR-v459. Renderizado via <script src> na mesma posição de saída.
 * Não contém interpolação PHP.
 */

// Shims V2: mapeiam nomes V1 (de portal.js, removido) para equivalentes V2.
// P(body)       → _szAjaxPost(url, body) exposto por senderzz-v2-products.js
// szCopyText()  → szV2CopyUrl() exposto por senderzz-v2-products.js
// szCLAlert()   → szToast() / szV2Toast() (fallback inline)
window.P = window.P || function(data) {
  var url = window._szGetAjax ? window._szGetAjax() : '/wp-admin/admin-ajax.php';
  return window._szAjaxPost ? window._szAjaxPost(url, Object.assign({}, data)) :
    fetch(url, { method:'POST', credentials:'same-origin', body: new URLSearchParams(data) }).then(function(r){return r.json();});
};
window.szCopyText = window.szCopyText || function(btn, url) {
  if (window.szV2CopyUrl) { window.szV2CopyUrl(btn, url); return; }
  navigator.clipboard && navigator.clipboard.writeText(url).catch(function(){});
};
window.szCLAlert = window.szCLAlert || function(el, msg, type) {
  if (!el) return;
  el.textContent = msg;
  el.style.display = msg ? 'block' : 'none';
  el.style.color = type === 'err' ? '#dc2626' : type === 'ok' ? '#16a34a' : '#d97706';
};
function szProductCheckoutFormToggle(pid,open){
  var card=document.getElementById('sz-prod-create-'+pid); if(!card)return;
  card.classList.toggle('is-collapsed',open===false); card.classList.toggle('is-open',open!==false);
  if(open!==false){ setTimeout(function(){ card.scrollIntoView({behavior:'smooth',block:'center'}); var i=card.querySelector('.sz-prod-cl-name'); if(i)i.focus(); },60); }
}
function szProductCLAddItem(pid){
  var card=document.getElementById('sz-prod-create-'+pid), box=card&&card.querySelector('.sz-prod-inline-components'); if(!box)return;
  var first=box.querySelector('.sz-prod-inline-component'); if(!first)return;
  var clone=first.cloneNode(true); clone.querySelectorAll('select').forEach(function(s){s.selectedIndex=0}); clone.querySelectorAll('input[type=number]').forEach(function(i){i.value=1});
  var v=clone.querySelector('.sz-cl-variation'); if(v){v.style.display='none';v.innerHTML='<option value="">Variação</option>';}
  box.appendChild(clone);
}
function szProductCLRemoveItem(btn){var box=btn.closest('.sz-prod-inline-components'); if(box&&box.querySelectorAll('.sz-prod-inline-component').length>1)btn.closest('.sz-prod-inline-component').remove();}
async function szProductCLGerar(pid,n){
  var card=document.getElementById('sz-prod-create-'+pid); if(!card)return;
  var btn=card.querySelector('.sz-prod-inline-generate'), msg=card.querySelector('.sz-prod-inline-msg');
  var name=(card.querySelector('.sz-prod-cl-name')?.value||'').trim(); var valor=card.querySelector('.sz-prod-cl-valor')?.value||''; var checkoutCommission=card.querySelector('.sz-prod-cl-commission')?.value||'';
  if(!name){ if(window.szCLAlert)szCLAlert(msg,'Informe um nome para o link.','err'); return; }
  if(checkoutCommission!=='' && checkoutCommission!=null){
    var pct=parseFloat(String(checkoutCommission).replace(',','.'));
    if(isNaN(pct) || pct<0 || pct>100){ if(window.szCLAlert)szCLAlert(msg,'A comissão do checkout deve ficar entre 0% e 100%.','err'); return; }
  }
  var components=[], has=false;
  card.querySelectorAll('.sz-prod-inline-component').forEach(function(item){var pid=item.querySelector('.sz-cl-product')?.value||''; if(!pid){pid=card.querySelector('.sz-prod-cl-current-product')?.value||card.getAttribute('data-product-id')||'';} var v=item.querySelector('.sz-cl-variation'); var vid=(v&&v.style.display!=='none')?v.value:''; var qty=parseInt(item.querySelector('.sz-cl-qty')?.value||'1')||1; if(pid){has=true; components.push({product_id:pid,variation_id:vid,qty:qty});}});
  if(!has){ if(window.szCLAlert)szCLAlert(msg,'Adicione pelo menos um produto.','err'); return; }
  if(window.szCLAlert)szCLAlert(msg,'Criando checkout...','warn'); if(btn){btn.disabled=true;btn.textContent='Aguarde…';}
  n = window.SZ_NONCE || window.SZ_PORTAL_NONCE || n || '';
  try{ var r=await P({action:'senderzz_portal',szaction:'checkout_link_generate',cl_name:name,cl_valor:valor,cl_components:JSON.stringify(components),cl_commission_pct:checkoutCommission,cl_affiliate_visible:(card.querySelector('.sz-prod-cl-affiliate-visible')?.checked?1:0),_ajax_nonce:n});
    if(r&&r.success){
      if(window.szToast)szToast('Checkout criado.','success',2200); else if(window.szCLAlert)szCLAlert(msg,'Checkout criado.','ok');
      if(window.szProductAppendCheckoutRow) window.szProductAppendCheckoutRow(card,r.data||{});
      card.querySelector('.sz-prod-cl-name').value=''; card.querySelector('.sz-prod-cl-valor').value=''; var ci=card.querySelector('.sz-prod-cl-commission'); if(ci)ci.value='';
      szProductCheckoutFormToggle(pid,false);
    }
    else { if(window.szCLAlert)szCLAlert(msg,(r&&r.data&&r.data.message)||r.message||'Erro ao criar checkout. Verifique o retorno do banco/template base.','err'); }
  }catch(e){ console.error('Erro checkout_link_generate:', e); if(window.szCLAlert)szCLAlert(msg,'Erro de conexão/AJAX ao criar checkout. Veja o Response em Network > admin-ajax.php.','err'); }
  if(btn){btn.disabled=false;btn.textContent='Gerar link';}
}
function szEscHtml(v){return String(v==null?'':v).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#039;'}[c];});}
function szProductAppendCheckoutRow(card,d){
  var table=card&&card.closest('.sz-prod-card-v135')&&card.closest('.sz-prod-card-v135').querySelector('.sz-prod-table-v135'); if(!table)return;
  table.querySelectorAll('.sz-prod-empty-v135').forEach(function(e){e.remove();});
  var exp=d.url||d.exp_url||'', mb=d.url_motoboy||d.motoboy_url||'', id=d.link_id||0;
  var row=document.createElement('div'); row.className='sz-prod-row-v135'; row.setAttribute('data-link-id',id);
  row.innerHTML='<span><strong>'+szEscHtml(d.name||'Checkout')+'</strong><small>'+szEscHtml(d.token||'')+'</small></span>'+
    '<span>'+szEscHtml(d.components||'')+'</span><span>'+szEscHtml(d.price_label||'')+'</span>'+
    '<span class="sz-prod-commission-cell"><div class="sz-prod-commission-editor"><input class="sz-prod-commission-input" type="number" min="0" max="100" step="0.01" value="'+szEscHtml(d.affiliate_commission_pct||0)+'"><span class="sz-prod-commission-suffix">%</span></div><button type="button" class="sz-prod-commission-save">Salvar</button></span>'+
    '<span class="sz-prod-links-cell">'+
    (exp?'<button type="button" class="sz-prod-btn-copy sz-prod-btn-solid">📦 Expedição</button>':'')+
    (mb?'<button type="button" class="sz-prod-btn-copy sz-prod-btn-solid">🏍️ Motoboy</button>':'')+
    '</span><span class="actions sz-prod-checkout-actions">'+
    '<label class="sz-prod-aff-toggle" title="Liberar para afiliados"><input type="checkbox" '+(d.affiliate_visible?'checked':'')+'><span></span><em>Afiliados</em></label><button type="button" class="sz-prod-btn-delete">Excluir</button></span>';
  var btns=row.querySelectorAll('.sz-prod-btn-copy'); if(btns[0])btns[0].onclick=function(){szCopyText(this,exp)}; if(btns[1])btns[1].onclick=function(){szCopyText(this,mb)};
  var save=row.querySelector('.sz-prod-commission-save'); if(save)save.onclick=function(){szCLSaveCommission(this,id,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'')};
  var aff=row.querySelector('.sz-prod-aff-toggle input'); if(aff)aff.onchange=function(){szCLToggleAffiliate(id,this.checked,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'')};
  var del=row.querySelector('.sz-prod-btn-delete'); if(del)del.onclick=function(){szCLDelete(id,window.SZ_NONCE||window.SZ_PORTAL_NONCE||'',this)};
  table.appendChild(row);
}
async function szCLSaveCommission(btn,id,n){
  var row=btn&&btn.closest('.sz-prod-row-v135'), input=row&&row.querySelector('.sz-prod-commission-input'); if(!input)return;
  var val=String(input.value||'').replace(',','.'); var pct=parseFloat(val);
  if(isNaN(pct)||pct<0||pct>100){alert('A comissão deve ficar entre 0% e 100%.');return;}
  var old=btn.textContent; btn.disabled=true; btn.textContent='Salvando…';
  try{var r=await P({action:'senderzz_portal',szaction:'checkout_link_commission_update',link_id:id,commission_pct:pct,_ajax_nonce:(window.SZ_NONCE||window.SZ_PORTAL_NONCE||n||'')});
    if(!(r&&r.success)) alert((r&&r.data&&r.data.message)||r.message||'Erro ao salvar comissão.'); else btn.textContent='Salvo';
  }catch(e){alert('Erro de conexão ao salvar comissão.');}
  setTimeout(function(){btn.disabled=false;btn.textContent=old||'Salvar';},900);
}
