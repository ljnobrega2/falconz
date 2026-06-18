/* ── TP Carteira Painel v2 ── */
(function(){
'use strict';

var API    = tpcPainel.apiBase;
var token  = sessionStorage.getItem('tpc_jwt')||'';
var user   = null;
var role   = 'client';
var pix    = {orderId:null,poll:null,timer:null};
var cotacaoSel = null;

/* ── Utils ── */
var fmt = function(v){ return 'R$ '+parseFloat(v||0).toLocaleString('pt-BR',{minimumFractionDigits:2}); };
var fmtDate = function(d){ return d ? new Date(d).toLocaleString('pt-BR',{day:'2-digit',month:'2-digit',year:'numeric',hour:'2-digit',minute:'2-digit'}) : '—'; };
var esc = function(s){ return String(s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); };
var el  = function(id){ return document.getElementById(id); };
var show = function(id){ var e=el(id); if(e) e.style.display=''; };
var hide = function(id){ var e=el(id); if(e) e.style.display='none'; };
var setText = function(id,v){ var e=el(id); if(e) e.textContent=v; };
var setHTML = function(id,v){ var e=el(id); if(e) e.innerHTML=v; };

/* ── API ── */
function api(path, opts){
    opts = opts||{};
    var h = {'Content-Type':'application/json'};
    if(token) h['Authorization'] = 'Bearer '+token;
    else h['X-WP-Nonce'] = tpcPainel.nonce;
    return fetch(API+path, Object.assign({headers:h},opts))
        .then(function(r){ if(r.status===401){logout();return Promise.reject('unauth');} return r.json(); });
}

/* ── Auth ── */
function login(email,pass){
    var btn=el('btn-login');
    btn.textContent='Entrando...'; btn.disabled=true;
    hide('login-err');
    api('/auth/token',{method:'POST',body:JSON.stringify({username:email,password:pass})})
    .then(function(d){
        if(d.error) throw new Error(d.error);
        token = d.token;
        user  = {id:d.user_id,nome:d.nome,email:d.email};
        sessionStorage.setItem('tpc_jwt',token);
        return api('/me/role');
    }).then(function(r){
        role = r.role||'client';
        entrarApp();
    }).catch(function(e){
        el('login-err').textContent = typeof e==='string'?e:(e.message||'Credenciais inválidas.');
        show('login-err');
    }).finally(function(){ btn.textContent='Entrar'; btn.disabled=false; });
}

function logout(){
    token=''; user=null; role='client';
    try{ sessionStorage.removeItem('tpc_jwt'); localStorage.removeItem('tpc_jwt'); }catch(e){}
    try{ document.cookie='tpc_jwt=; Max-Age=0; path=/'; }catch(e){}
    clearPix();
    hide('tpc-app'); show('tpc-login');
    if(window.history && window.history.replaceState){
        window.history.replaceState(null,'', window.location.pathname + '?logout=1&_=' + Date.now());
    }
}
function entrarApp(){
    hide('tpc-login'); show('tpc-app');
    setText('sb-nome', tpcPainel.nomeLoja);
    setText('sb-name', user.nome);
    setText('sb-email', user.email);
    el('sb-av').textContent = user.nome.charAt(0).toUpperCase();

    if(role==='admin'){
        hide('nav-client'); show('nav-admin');
        // Mostra badge admin
        var nameEl = el('sb-name');
        if(nameEl && !nameEl.querySelector('.admin-badge')){
            nameEl.insertAdjacentHTML('afterend','<div class="admin-badge">ADMIN</div>');
        }
        tpcNav('adm-dashboard');
    } else {
        show('nav-client'); hide('nav-admin');
        tpcNav('dashboard');
    }
}

/* ── Navegação ── */
window.tpcNav = function(s){
    document.querySelectorAll('.nav-btn').forEach(function(b){ b.classList.toggle('active', b.dataset.s===s); });
    document.querySelectorAll('.sec').forEach(function(x){ x.style.display = x.id==='s-'+s ? '' : 'none'; });

    if(s==='dashboard')       loadDashboard();
    if(s==='extrato')         loadExt(1);
    if(s==='fretes')          loadFrt(1);
    if(s==='suporte')         loadTickets();
    if(s==='webhooks')       loadWebhooks();
    if(s==='rastreio')       loadRastreio();
    if(s==='adm-dashboard')   loadAdmDash();
    if(s==='adm-clientes')    loadAdmClientes();
    if(s==='adm-transacoes')  loadAdmTx(1);
    if(s==='adm-graficos')    loadGraficos();
    if(s==='adm-alertas')     loadAlertas();
};

/* ══════════════════════════════════════
   CLIENTE
══════════════════════════════════════ */

function loadDashboard(){
    api('/saldo').then(function(d){ setText('c-saldo', d.saldo_fmt||fmt(d.saldo)); });

    var hoje = new Date(), ini = hoje.getFullYear()+'-'+String(hoje.getMonth()+1).padStart(2,'0')+'-01', fim = hoje.toISOString().slice(0,10);
    api('/extrato?data_ini='+ini+'&data_fim='+fim+'&per_page=100').then(function(d){
        var deb = d.data.filter(function(t){return t.tipo==='debito';});
        var gasto = deb.reduce(function(s,t){return s+parseFloat(t.valor);},0);
        setText('c-fretes-mes', deb.length);
        setText('c-gasto', fmt(gasto));
        var html = d.data.slice(0,5).map(txRow).join('');
        setHTML('dash-tx', html||'<div class="empty">Nenhuma transação.</div>');
        var fhtml = deb.slice(0,5).map(function(t){
            return '<div class="item"><div class="item-info"><div class="item-desc">'+esc(t.descricao)+'</div><div class="item-date">'+fmtDate(t.created_at)+'</div></div><span class="item-val debito">− '+t.valor_fmt+'</span></div>';
        }).join('');
        setHTML('dash-fr', fhtml||'<div class="empty">Nenhum frete.</div>');
    });
    api('/extrato?tipo=credito&per_page=100').then(function(d){
        var tot = d.data.reduce(function(s,t){return s+parseFloat(t.valor);},0);
        setText('c-recarga-total', fmt(tot));
    });
}

function txRow(t){
    return '<div class="item"><div class="item-info"><div class="item-desc">'+esc(t.descricao)+'</div><div class="item-date">'+fmtDate(t.created_at)+'</div></div><span class="badge badge-'+t.tipo+'">'+t.tipo+'</span><span class="item-val '+t.tipo+'">'+(t.tipo==='credito'?'+':'−')+' '+t.valor_fmt+'</span></div>';
}

/* Extrato */
window.loadExt = function(p){
    setHTML('ext-body','<div class="loading">Carregando...</div>');
    var params = new URLSearchParams({per_page:15,page:p,tipo:el('ext-tipo').value,data_ini:el('ext-ini').value,data_fim:el('ext-fim').value});
    api('/extrato?'+params).then(function(d){
        if(!d.data||!d.data.length){ setHTML('ext-body','<div class="empty">Nenhuma transação.</div>'); setHTML('ext-pag',''); return; }
        setHTML('ext-body', d.data.map(txRow).join(''));
        paginate('ext-pag',p,d.total_pages,'loadExt');
    });
};
window.clearExt = function(){ el('ext-tipo').value=''; el('ext-ini').value=''; el('ext-fim').value=''; loadExt(1); };

/* Fretes */
window.loadFrt = function(p){
    setHTML('frt-body','<div class="loading">Carregando...</div>');
    var params = new URLSearchParams({per_page:15,page:p,tipo:'debito',data_ini:el('frt-ini').value,data_fim:el('frt-fim').value});
    api('/extrato?'+params).then(function(d){
        if(!d.data||!d.data.length){ setHTML('frt-body','<div class="empty">Nenhum frete.</div>'); setHTML('frt-pag',''); return; }
        setHTML('frt-body', d.data.map(function(t){
            return '<div class="item"><div class="item-info"><div class="item-desc">'+esc(t.descricao)+'</div><div class="item-date">'+fmtDate(t.created_at)+'</div></div><span class="item-val debito">− '+t.valor_fmt+'</span></div>';
        }).join(''));
        paginate('frt-pag',p,d.total_pages,'loadFrt');
    });
};
window.clearFrt = function(){ el('frt-ini').value=''; el('frt-fim').value=''; loadFrt(1); };

function normalizePixImg(qr, copia){
  qr=String(qr||'').trim(); copia=String(copia||'').trim();
  if(qr.indexOf('data:image')===0) return qr;
  if(/^https?:\/\//i.test(qr)) return qr;
  if(/^[A-Za-z0-9+\\/=\\r\\n]+$/.test(qr) && qr.length>80) return 'data:image/png;base64,'+qr.replace(/\\s+/g,'');
  if(copia) return 'https://api.qrserver.com/v1/create-qr-code/?size=220x220&data='+encodeURIComponent(copia);
  return '';
}

/* Recarga PIX */
function gerarPix(){
    var v = parseFloat(el('rec-val').value);
    if(!v||v<10){ el('rec-err').textContent='Valor mínimo R$10,00'; show('rec-err'); return; }
    hide('rec-err');
    var btn=el('btn-pix'); btn.textContent='Gerando...'; btn.disabled=true;
    api('/recarregar',{method:'POST',body:JSON.stringify({valor:v})})
    .then(function(d){
        if(d.error) throw new Error(d.error);
        pix.orderId = d.recarga_id || d.order_id;
        exibirPix(d.pix); iniciarPoll(); iniciarTimer(d.pix.expira_em);
        hide('pix-form-wrap'); show('pix-wrap');
    }).catch(function(e){ el('rec-err').textContent=typeof e==='string'?e:(e.message||'Erro.'); show('rec-err'); })
    .finally(function(){ btn.textContent='Gerar PIX'; btn.disabled=false; });
}

function exibirPix(p){
    var src = p.qr_src || normalizePixImg(p.qr_code || '', p.copia_cola || '');
    if(src) { el('pix-img').src=src; el('pix-img').style.display='block'; }
    else { el('pix-img').style.display='none'; }
    el('pix-cc').value = p.copia_cola || p.link || '';
    hide('pix-ok'); show('pix-wait');
}

function iniciarPoll(){
    clearInterval(pix.poll);
    pix.poll = setInterval(function(){
        if(!pix.orderId) return;
        api('/recarga/'+pix.orderId+'/pix').then(function(d){
            if(d.pago){
                clearInterval(pix.poll); clearInterval(pix.timer);
                hide('pix-wait'); setText('pix-novo-saldo',fmt(d.saldo)); show('pix-ok');
                setText('c-saldo',fmt(d.saldo));
                loadDashboard();
            }
        });
    },5000);
}

function iniciarTimer(exp){
    clearInterval(pix.timer);
    var expiry = new Date(exp).getTime();
    pix.timer = setInterval(function(){
        var rem = Math.max(0,expiry-Date.now());
        var m=Math.floor(rem/60000), s=Math.floor((rem%60000)/1000);
        setText('pix-timer',String(m).padStart(2,'0')+':'+String(s).padStart(2,'0'));
        if(rem===0){ clearInterval(pix.timer); clearInterval(pix.poll); setText('pix-wait','PIX expirado.'); }
    },1000);
}

function clearPix(){ clearInterval(pix.poll); clearInterval(pix.timer); pix={orderId:null,poll:null,timer:null}; }

window.tpcCancelPix = function(){
    clearPix();
    el('rec-val').value='';
    document.querySelectorAll('.chip').forEach(function(c){c.classList.remove('on');});
    hide('pix-wrap'); show('pix-form-wrap');
};

/* Etiqueta */
window.calcularFrete = function(){
    var cep = el('et-cep').value.replace(/\D/g,'');
    if(cep.length<8){ alert('CEP inválido'); return; }
    setHTML('et-cotacoes','<div class="loading">Calculando...</div>');
    api('/calcular-frete',{method:'POST',body:JSON.stringify({
        cep_destino:cep, peso:el('et-peso').value,
        altura:el('et-altura').value, largura:el('et-largura').value, comprimento:el('et-comp').value
    })}).then(function(d){
        var arr = Array.isArray(d)?d:[];
        var validos = arr.filter(function(s){return s.price&&!s.error;});
        if(!validos.length){ setHTML('et-cotacoes','<div class="alert alert-warn">Nenhum serviço disponível para este CEP.</div>'); return; }
        validos.sort(function(a,b){return parseFloat(a.price)-parseFloat(b.price);});
        var html = validos.map(function(s){
            return '<div class="cotacao-item" onclick="selCotacao('+JSON.stringify(s).replace(/"/g,"'")+',this)" data-id="'+s.id+'">'
                +'<div><div class="cotacao-nome">'+esc(s.name)+'</div><div class="cotacao-prazo">'+s.delivery_range.min+' a '+s.delivery_range.max+' dias úteis</div></div>'
                +'<div class="cotacao-preco">'+fmt(s.price)+'</div></div>';
        }).join('');
        setHTML('et-cotacoes',html);
        // Preenche select
        var sel = el('et-service');
        sel.innerHTML = validos.map(function(s){ return '<option value="'+s.id+'">'+s.name+' — '+fmt(s.price)+'</option>'; }).join('');
    }).catch(function(){ setHTML('et-cotacoes','<div class="alert alert-err">Erro ao calcular.</div>'); });
};

window.selCotacao = function(s,el_){
    cotacaoSel = s;
    document.querySelectorAll('.cotacao-item').forEach(function(c){c.classList.remove('sel');});
    el_.classList.add('sel');
    document.querySelector('#et-service option[value="'+s.id+'"]') && (el('et-service').value=s.id);
};

window.buscarCEP = function(v){
    v = v.replace(/\D/g,'');
    if(v.length!==8) return;
    fetch('https://viacep.com.br/ws/'+v+'/json/').then(function(r){return r.json();}).then(function(d){
        if(d.erro) return;
        el('et-end').value   = d.logradouro||'';
        el('et-bairro').value= d.bairro||'';
        el('et-cidade').value= d.localidade||'';
        el('et-estado').value= d.uf||'';
    });
};

function emitirEtiqueta(){
    hide('et-err'); hide('et-ok');
    var sid = el('et-service').value;
    var preco = cotacaoSel && cotacaoSel.id==sid ? parseFloat(cotacaoSel.price) : 0;
    if(!sid){ el('et-err').textContent='Selecione uma transportadora.'; show('et-err'); return; }
    if(!el('et-nome').value||!el('et-cep').value){ el('et-err').textContent='Preencha todos os campos obrigatórios.'; show('et-err'); return; }

    var btn=el('btn-emitir'); btn.textContent='Emitindo...'; btn.disabled=true;
    api('/emitir-etiqueta',{method:'POST',body:JSON.stringify({
        service_id:sid, preco:preco,
        nome:el('et-nome').value, documento:el('et-doc').value, email:el('et-email').value, telefone:el('et-tel').value,
        cep:el('et-cep').value, endereco:el('et-end').value, bairro:el('et-bairro').value,
        cidade:el('et-cidade').value, estado:el('et-estado').value,
        peso:el('et-peso').value, altura:el('et-altura').value, largura:el('et-largura').value, comprimento:el('et-comp').value,
    })}).then(function(d){
        if(d.error){ el('et-err').textContent=d.error; show('et-err'); return; }
        setHTML('et-resultado-body',
            '<p style="margin-bottom:10px">✅ Etiqueta gerada com sucesso!</p>'+
            '<p style="margin-bottom:6px"><strong>Serviço:</strong> '+esc(d.servico)+'</p>'+
            '<p style="margin-bottom:6px"><strong>Rastreio:</strong> '+esc(d.tracking||'—')+'</p>'+
            '<p style="margin-bottom:14px"><strong>Valor debitado:</strong> '+fmt(d.preco)+'</p>'+
            (d.etiqueta_url?'<a href="'+d.etiqueta_url+'" target="_blank" class="btn btn-primary">🖨 Imprimir etiqueta</a>':'<div class="alert alert-warn">URL de impressão não disponível ainda.</div>')
        );
        show('et-resultado');
        loadDashboard();
    }).catch(function(){ el('et-err').textContent='Erro ao emitir etiqueta.'; show('et-err'); })
    .finally(function(){ btn.textContent='Emitir etiqueta'; btn.disabled=false; });
}

/* Tickets */
function loadTickets(){
    setHTML('tickets-list','<div class="loading">Carregando...</div>');
    api('/tickets').then(function(d){
        if(!d.length){ setHTML('tickets-list','<div class="empty">Nenhum chamado.</div>'); return; }
        setHTML('tickets-list',d.map(function(t){
            return '<div class="item"><div class="item-info"><div class="item-desc">'+esc(t.assunto)+'</div>'
                +(t.resposta?'<div style="font-size:var(--sz-text-meta);color:#555;margin-top:4px;padding:5px 8px;background:#f9f9f7;border-radius:5px">💬 '+esc(t.resposta)+'</div>':'')
                +'<div class="item-date">'+fmtDate(t.criado_em)+'</div></div>'
                +'<span class="badge badge-'+(t.status==='resolvido'?'resolvido':'aberto')+'">'+t.status+'</span></div>';
        }).join(''));
    });
}

function enviarTicket(){
    hide('sup-err'); hide('sup-ok');
    var msg = el('sup-msg').value.trim();
    if(msg.length<10){ el('sup-err').textContent='Mensagem muito curta.'; show('sup-err'); return; }
    var btn=el('btn-ticket'); btn.textContent='Enviando...'; btn.disabled=true;
    api('/tickets',{method:'POST',body:JSON.stringify({assunto:el('sup-ass').value,mensagem:msg})})
    .then(function(d){
        if(d.error) throw new Error(d.error);
        el('sup-msg').value=''; el('sup-ok').textContent='Chamado enviado!'; show('sup-ok');
        loadTickets();
    }).catch(function(e){ el('sup-err').textContent=typeof e==='string'?e:(e.message||'Erro.'); show('sup-err'); })
    .finally(function(){ btn.textContent='Enviar chamado'; btn.disabled=false; });
}

/* ══════════════════════════════════════
   ADMIN
══════════════════════════════════════ */

function loadAdmDash(){
    api('/admin/resumo').then(function(d){
        setText('adm-saldo-total', fmt(d.saldo_total));
        setText('adm-clientes', d.n_clientes);
        setText('adm-fretes-mes', d.fretes_mes);
        setText('adm-receita', fmt(d.receita_mes));

        var baixo = d.saldo_baixo||[];
        setHTML('adm-baixo', baixo.length
            ? '<table class="tbl"><tr><th>Cliente</th><th>Email</th><th>Saldo</th><th>Ação</th></tr>'
              + baixo.map(function(c){
                  return '<tr><td>'+esc(c.display_name)+'</td><td>'+esc(c.user_email)+'</td>'
                      +'<td style="color:#c0392b;font-weight:600">'+fmt(c.saldo)+'</td>'
                      +'<td><button class="btn btn-sm btn-secondary" onclick="admCreditarRapido(\''+esc(c.user_email)+'\')">Creditar</button></td></tr>';
                }).join('')+'</table>'
            : '<div class="empty">Nenhum cliente com saldo baixo.</div>');

        var ul = d.ultimas||[];
        setHTML('adm-ultimas', ul.length
            ? ul.map(function(t){
                return '<div class="item"><div class="item-info"><div class="item-desc">'+esc(t.display_name)+' — '+esc(t.descricao)+'</div><div class="item-date">'+fmtDate(t.created_at)+'</div></div>'
                    +'<span class="badge badge-'+t.tipo+'">'+t.tipo+'</span>'
                    +'<span class="item-val '+t.tipo+'">'+(t.tipo==='credito'?'+':'−')+' '+fmt(t.valor)+'</span></div>';
              }).join('')
            : '<div class="empty">Nenhuma transação.</div>');
    });
}

window.loadAdmClientes = function(p){
    p = p||1;
    setHTML('adm-clientes-body','<div class="loading">Carregando...</div>');
    var busca = el('adm-busca').value;
    api('/admin/clientes?page='+p+(busca?'&busca='+encodeURIComponent(busca):'')).then(function(d){
        var rows = d.data||[];
        if(!rows.length){ setHTML('adm-clientes-body','<div class="empty">Nenhum cliente.</div>'); setHTML('adm-cli-pag',''); return; }
        setHTML('adm-clientes-body',
            '<table class="tbl"><tr><th>Cliente</th><th>Email</th><th>Saldo</th><th>Recargas</th><th>Gasto</th><th>Transações</th><th>Ações</th></tr>'
            + rows.map(function(c){
                return '<tr><td><strong>'+esc(c.display_name)+'</strong></td><td>'+esc(c.user_email)+'</td>'
                    +'<td style="font-weight:600;color:'+(parseFloat(c.saldo)>0?'#1d6f42':'#c0392b')+'">'+fmt(c.saldo)+'</td>'
                    +'<td>'+fmt(c.total_recarga||0)+'</td><td>'+fmt(c.total_gasto||0)+'</td><td>'+c.total_tx+'</td>'
                    +'<td><button class="btn btn-sm btn-secondary" onclick="admVerExtrato('+c.user_id+')">Extrato</button> '
                    +'<button class="btn btn-sm btn-secondary" onclick="admCreditarRapido(\''+esc(c.user_email)+'\')">Creditar</button></td></tr>';
              }).join('')+'</table>'
        );
        paginate('adm-cli-pag',p,d.pages,'loadAdmClientes');
    });
};

window.admCreditarRapido = function(email){
    el('adm-cr-email').value = email;
    el('adm-cr-email').scrollIntoView({behavior:'smooth',block:'center'});
    el('adm-cr-valor').focus();
};

window.admVerExtrato = function(uid){
    el('adm-tx-user').value = uid;
    tpcNav('adm-transacoes');
};

window.admCreditar = function(){
    var email = el('adm-cr-email').value;
    var valor = parseFloat(el('adm-cr-valor').value);
    var motivo = el('adm-cr-motivo').value;
    var msg = el('adm-cr-msg');
    msg.textContent=''; msg.style.color='';

    if(!email||!valor){ msg.textContent='Preencha email e valor.'; msg.style.color='#c0392b'; return; }

    api('/admin/creditar',{method:'POST',body:JSON.stringify({email:email,valor:valor,motivo:motivo})})
    .then(function(d){
        if(d.error){ msg.textContent='❌ '+d.error; msg.style.color='#c0392b'; return; }
        msg.textContent='✅ Crédito de '+fmt(valor)+' adicionado para '+d.nome+'. Novo saldo: '+d.novo_saldo_fmt;
        msg.style.color='#1d6f42';
        el('adm-cr-email').value=''; el('adm-cr-valor').value='';
        loadAdmClientes();
    }).catch(function(){ msg.textContent='Erro.'; msg.style.color='#c0392b'; });
};

window.loadAdmTx = function(p){
    setHTML('adm-tx-body','<div class="loading">Carregando...</div>');
    var params = new URLSearchParams({page:p,per_page:20,tipo:el('adm-tx-tipo').value,data_ini:el('adm-tx-ini').value,data_fim:el('adm-tx-fim').value,user_id:el('adm-tx-user').value});
    api('/admin/transacoes?'+params).then(function(d){
        if(!d.data||!d.data.length){ setHTML('adm-tx-body','<div class="empty">Nenhuma transação.</div>'); setHTML('adm-tx-pag',''); return; }
        setHTML('adm-tx-body',
            '<table class="tbl"><tr><th>Cliente</th><th>Tipo</th><th>Valor</th><th>Saldo após</th><th>Descrição</th><th>Data</th></tr>'
            + d.data.map(function(t){
                return '<tr><td>'+esc(t.display_name)+'<br><small style="color:#bbb">'+esc(t.user_email)+'</small></td>'
                    +'<td><span class="badge badge-'+t.tipo+'">'+t.tipo+'</span></td>'
                    +'<td style="font-weight:600">'+(t.tipo==='credito'?'+':'−')+' '+t.valor_fmt+'</td>'
                    +'<td>'+t.saldo_apos_fmt+'</td><td>'+esc(t.descricao)+'</td><td>'+fmtDate(t.created_at)+'</td></tr>';
              }).join('')+'</table>'
        );
        paginate('adm-tx-pag',p,d.pages,'loadAdmTx');
    });
};

var chartObjs = {};
function loadGraficos(){
    api('/admin/graficos').then(function(d){
        drawChart('chart-recargas', d.labels, d.recargas, '#E8650A', 'Recargas (R$)');
        drawChart('chart-fretes',   d.labels, d.fretes,   '#1d6f42', 'Fretes (R$)');
        if(d.top&&d.top.length) drawBarChart('chart-top', d.top.map(function(t){return t.nome;}), d.top.map(function(t){return parseFloat(t.total);}));
    });
}

function drawChart(id, labels, data, color, label){
    if(typeof Chart==='undefined') return;
    if(chartObjs[id]) chartObjs[id].destroy();
    var ctx = el(id).getContext('2d');
    chartObjs[id] = new Chart(ctx,{
        type:'line',
        data:{labels:labels,datasets:[{label:label,data:data,borderColor:color,backgroundColor:color+'22',fill:true,tension:0.3,pointRadius:3}]},
        options:{plugins:{legend:{display:false}},scales:{y:{ticks:{callback:function(v){return 'R$ '+v.toLocaleString('pt-BR');}}}}}
    });
}

function drawBarChart(id, labels, data){
    if(typeof Chart==='undefined') return;
    if(chartObjs[id]) chartObjs[id].destroy();
    var ctx = el(id).getContext('2d');
    chartObjs[id] = new Chart(ctx,{
        type:'bar',
        data:{labels:labels,datasets:[{label:'Volume (R$)',data:data,backgroundColor:'#E8650A99',borderColor:'#E8650A',borderWidth:1}]},
        options:{indexAxis:'y',plugins:{legend:{display:false}},scales:{x:{ticks:{callback:function(v){return 'R$ '+v.toLocaleString('pt-BR');}}}}}
    });
}

window.loadAlertas = function(){
    var lim = parseFloat(el('adm-limite').value)||50;
    setHTML('adm-alertas-body','<div class="loading">Carregando...</div>');
    api('/admin/clientes?per_page=100').then(function(d){
        var baixo = (d.data||[]).filter(function(c){return parseFloat(c.saldo)<=lim;});
        if(!baixo.length){ setHTML('adm-alertas-body','<div class="empty">Nenhum cliente abaixo do limite.</div>'); return; }
        setHTML('adm-alertas-body',
            '<table class="tbl"><tr><th>Cliente</th><th>Email</th><th>Saldo</th><th>Ação</th></tr>'
            + baixo.map(function(c){
                return '<tr><td>'+esc(c.display_name)+'</td><td>'+esc(c.user_email)+'</td>'
                    +'<td style="color:#c0392b;font-weight:600">'+fmt(c.saldo)+'</td>'
                    +'<td><button class="btn btn-sm btn-primary" onclick="admCreditarRapido(\''+esc(c.user_email)+'\');tpcNav(\'adm-clientes\')">Creditar agora</button></td></tr>';
              }).join('')+'</table>'
        );
    });
};


/* Webhooks por classe */
function loadWebhooks(){
    setHTML('wh-list','<div class="loading">Carregando...</div>');
    api('/webhooks').then(function(d){
        var classes = d.classes || [];
        var opts = classes.map(function(c){ return '<option value="'+c.id+'">'+esc(c.name)+'</option>'; }).join('');
        if(el('wh-class')) el('wh-class').innerHTML = opts;
        if(el('wh-payload')) el('wh-payload').textContent = JSON.stringify(d.payload_exemplo || {}, null, 2);
        var rows = d.data || [];
        if(!rows.length){ setHTML('wh-list','<div class="empty">Nenhum webhook cadastrado.</div>'); return; }
        setHTML('wh-list','<table class="tbl"><tr><th>Classe</th><th>URL</th><th>Status</th><th>Último disparo</th><th>Ações</th></tr>'+ 
            rows.map(function(w){
                var hasUrl = !!String(w.url||'').trim();
                return '<tr><td>'+esc(w.classe_nome)+'</td><td>'+(hasUrl?('<code>'+esc(w.url)+'</code>'):'<em>Aguardando URL de destino</em>')+'</td>'+ 
                    '<td>'+(parseInt(w.active)?'Ativo':'Inativo')+'<br><small>'+esc(w.last_status||'—')+'</small></td>'+ 
                    '<td>'+esc(w.last_fired_at||'—')+'</td>'+ 
                    '<td>'+(hasUrl?'<button class="btn btn-sm btn-secondary" onclick="testWebhook('+w.id+')">Testar</button> ':'')+
                    '<button class="btn btn-sm btn-ghost" onclick="editWebhook('+w.id+')">Editar</button></td></tr>';
            }).join('')+'</table>');
        window._senderzzWebhooks = rows;
    }).catch(function(){ setHTML('wh-list','<div class="alert alert-err">Erro ao carregar webhooks.</div>'); });
}

function whMsg(msg, ok){
    var e=el('wh-msg'); if(!e) return;
    e.className='alert '+(ok?'alert-ok':'alert-err'); e.textContent=msg; e.style.display='';
}

function saveWebhook(){
    var payload = {
        shipping_class_id: parseInt(el('wh-class').value||0),
        url: el('wh-url').value,
        secret: el('wh-secret').value,
        active: el('wh-active').checked ? 1 : 0
    };
    api('/webhooks',{method:'POST',body:JSON.stringify(payload)}).then(function(d){
        if(d.error) throw new Error(d.error);
        whMsg('Webhook salvo.', true);
        el('wh-url').value=''; el('wh-secret').value=''; el('wh-active').checked=true;
        loadWebhooks();
    }).catch(function(e){ whMsg(e.message||'Erro ao salvar webhook.', false); });
}

window.editWebhook = function(id){
    var rows = window._senderzzWebhooks || [];
    var w = rows.find(function(x){ return parseInt(x.id)===parseInt(id); });
    if(!w) return;
    el('wh-class').value = w.shipping_class_id;
    el('wh-url').value = w.url;
    el('wh-secret').value = w.secret || '';
    el('wh-active').checked = !!parseInt(w.active);
    window.scrollTo({top:0,behavior:'smooth'});
};

window.deleteWebhook = function(id){
    if(!confirm('Excluir este webhook?')) return;
    api('/webhooks/'+id,{method:'DELETE'}).then(function(){ loadWebhooks(); });
};

window.testWebhook = function(id){
    api('/webhooks/'+id+'/test',{method:'POST',body:'{}'}).then(function(d){
        alert(d.ok ? 'Teste enviado com sucesso.' : ('Falha no teste: '+(d.error||d.body||d.code)));
        loadWebhooks();
    });
};

/* ── Export CSV ── */
window.exportCSV = function(tipo){
    var rows = [], headers = [];
    if(tipo==='extrato'){
        var params = new URLSearchParams({per_page:1000,page:1,tipo:el('ext-tipo').value,data_ini:el('ext-ini').value,data_fim:el('ext-fim').value});
        api('/extrato?'+params).then(function(d){
            headers=['Data','Tipo','Valor','Saldo após','Descrição'];
            rows = d.data.map(function(t){return [fmtDate(t.created_at),t.tipo,t.valor_fmt,t.saldo_apos_fmt,t.descricao];});
            downloadCSV(headers,rows,'extrato');
        });
    } else if(tipo==='admin-transacoes'){
        var params2 = new URLSearchParams({per_page:1000,page:1,tipo:el('adm-tx-tipo').value,data_ini:el('adm-tx-ini').value,data_fim:el('adm-tx-fim').value});
        api('/admin/transacoes?'+params2).then(function(d){
            headers=['Cliente','Email','Tipo','Valor','Saldo após','Descrição','Data'];
            rows = d.data.map(function(t){return [t.display_name,t.user_email,t.tipo,t.valor_fmt,t.saldo_apos_fmt,t.descricao,fmtDate(t.created_at)];});
            downloadCSV(headers,rows,'transacoes-admin');
        });
    } else if(tipo==='admin-clientes'){
        api('/admin/clientes?per_page=1000').then(function(d){
            headers=['Cliente','Email','Saldo','Total recargas','Total gasto','Transações'];
            rows = (d.data||[]).map(function(c){return [c.display_name,c.user_email,fmt(c.saldo),fmt(c.total_recarga),fmt(c.total_gasto),c.total_tx];});
            downloadCSV(headers,rows,'clientes');
        });
    }
};

function downloadCSV(headers,rows,name){
    var csv = [headers.join(',')].concat(rows.map(function(r){return r.map(function(v){return '"'+String(v).replace(/"/g,'""')+'"';}).join(',');})).join('\n');
    var blob = new Blob(['\uFEFF'+csv],{type:'text/csv;charset=utf-8;'});
    var url  = URL.createObjectURL(blob);
    var a    = document.createElement('a'); a.href=url; a.download=name+'-'+new Date().toISOString().slice(0,10)+'.csv'; a.click();
    URL.revokeObjectURL(url);
}

/* ── Paginação ── */
function paginate(elId, cur, total, fn){
    if(total<=1){ setHTML(elId,''); return; }
    var h='<button '+(cur<=1?'disabled':'')+' onclick="'+fn+'('+(cur-1)+')">‹</button>';
    for(var i=1;i<=Math.min(total,7);i++) h+='<button class="'+(i===cur?'on':'')+'" onclick="'+fn+'('+i+')">'+i+'</button>';
    h+='<button '+(cur>=total?'disabled':'')+' onclick="'+fn+'('+(cur+1)+')">›</button>';
    setHTML(elId,h);
}

/* ── Chart.js lazy load ── */
function loadChartJS(cb){
    if(typeof Chart!=='undefined'){ cb(); return; }
    var s=document.createElement('script');
    s.src='https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.1/chart.umd.min.js';
    s.onload=cb; document.head.appendChild(s);
}

/* ── Bindings ── */
document.addEventListener('DOMContentLoaded',function(){
    ['l-email','l-pass'].forEach(function(id){ el(id)&&el(id).addEventListener('keydown',function(e){ if(e.key==='Enter') el('btn-login').click(); }); });
    el('btn-login')&&el('btn-login').addEventListener('click',function(){ login(el('l-email').value,el('l-pass').value); });
    el('btn-logout')&&el('btn-logout').addEventListener('click',logout);

    document.querySelectorAll('.nav-btn').forEach(function(b){ b.addEventListener('click',function(){ tpcNav(this.dataset.s); }); });
    document.querySelectorAll('.chip').forEach(function(c){ c.addEventListener('click',function(){ document.querySelectorAll('.chip').forEach(function(x){x.classList.remove('on');}); this.classList.add('on'); el('rec-val').value=this.dataset.v; }); });

    el('btn-pix')&&el('btn-pix').addEventListener('click',gerarPix);
    el('btn-copy')&&el('btn-copy').addEventListener('click',function(){ el('pix-cc').select(); document.execCommand('copy'); this.textContent='Copiado!'; var s=this; setTimeout(function(){s.textContent='Copiar';},2000); });
    el('btn-emitir')&&el('btn-emitir').addEventListener('click',emitirEtiqueta);
    el('btn-ticket')&&el('btn-ticket').addEventListener('click',enviarTicket);
    el('btn-wh-save')&&el('btn-wh-save').addEventListener('click',saveWebhook);

    // Lazy load Chart.js ao abrir gráficos
    document.querySelectorAll('[data-s="adm-graficos"]').forEach(function(b){ b.addEventListener('click',function(){ loadChartJS(function(){ setTimeout(loadGraficos,100); }); }); });

    // Auto-login
    if(token){
        api('/me').then(function(d){
            if(d.error||d.code){ logout(); return; }
            user={id:d.id,nome:d.nome,email:d.email};
            return api('/me/role');
        }).then(function(r){ if(r){ role=r.role||'client'; entrarApp(); } }).catch(logout);
    }
});

})();

/* ══ RASTREIO ══════════════════════════════════════════════════════════ */
function loadRastreio(){
    // URL de exemplo
    var urlEx=el('rast-url-example');
    if(urlEx) urlEx.textContent=window.location.origin+'/rastreio/BR123456789';

    // Carrega configs salvas
    api('/rastreio-brand').then(function(d){
        if(!d||!d.brand) return;
        var b=d.brand;
        if(b.logo)      el('rast-logo').value=b.logo;
        if(b.cor)       { el('rast-cor').value=b.cor; el('rast-cor-picker').value=b.cor; }
        if(b.cor_texto) { el('rast-cor-texto').value=b.cor_texto; el('rast-cor-texto-picker').value=b.cor_texto; }
        if(b.nome)      el('rast-nome').value=b.nome;
        if(b.rodape)    el('rast-rodape').value=b.rodape;
        updatePreview();
    }).catch(function(){});

    // Sync color pickers
    ['rast-cor','rast-cor-texto'].forEach(function(id){
        var input=el(id), picker=el(id+'-picker');
        if(!input||!picker) return;
        input.addEventListener('input',function(){ picker.value=this.value; updatePreview(); });
        picker.addEventListener('input',function(){ input.value=this.value; updatePreview(); });
    });
    ['rast-logo','rast-nome'].forEach(function(id){
        var inp=el(id); if(inp) inp.addEventListener('input',updatePreview);
    });

    // Salvar
    var btn=el('btn-rast-save');
    if(btn) btn.onclick=function(){
        var payload={
            logo:    (el('rast-logo')||{}).value||'',
            cor:     (el('rast-cor')||{}).value||'#E8650A',
            cor_texto:(el('rast-cor-texto')||{}).value||'#ffffff',
            nome:    (el('rast-nome')||{}).value||'',
            rodape:  (el('rast-rodape')||{}).value||'',
        };
        L('btn-rast-save',true);
        api('/rastreio-brand','POST',payload).then(function(d){
            L('btn-rast-save',false);
            var msg=el('rast-msg');
            if(msg){ msg.style.display='block'; msg.style.color=d.ok?'#16a34a':'#dc2626'; msg.textContent=d.ok?'✓ Salvo com sucesso!':'Erro ao salvar.'; setTimeout(function(){ msg.style.display='none'; },3000); }
        }).catch(function(){ L('btn-rast-save',false); });
    };
}

function updatePreview(){
    var bar=el('rast-preview-bar'); if(!bar) return;
    var cor=(el('rast-cor')||{}).value||'#E8650A';
    var corTxt=(el('rast-cor-texto')||{}).value||'#ffffff';
    var nome=(el('rast-nome')||{}).value||'Senderzz';
    var logo=(el('rast-logo')||{}).value||'';
    bar.style.background=cor;
    var inner=logo
        ? '<img src="'+logo+'" style="height:32px;max-width:140px;object-fit:contain" onerror="this.style.display=\'none\'">'
        : '<span style="color:'+corTxt+';font-weight:700;font-size:var(--sz-text-lg)">'+nome+'</span>';
    bar.innerHTML=inner;
}
