/**
 * Senderzz Dashboard V2 — Controller unificado (v455)
 * Cobre: Produtos, Motoboy, Webhooks+Integrações, Suporte, Usuários, Configurações.
 * Todos os AJAX reusam handlers existentes. Sem endpoint novo. Sem MutationObserver.
 */
(function () {
  "use strict";

  /* ── Helper AJAX ─────────────────────────────────────────────────────── */
  function ajaxPost(url, body) {
    body.action = "senderzz_portal";
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8" },
      body: Object.keys(body).map(function (k) {
        return encodeURIComponent(k) + "=" + encodeURIComponent(body[k]);
      }).join("&"),
    }).then(function (r) { return r.json(); });
  }
  window._szAjaxPost = ajaxPost; // expose for functions defined outside IIFE
  function getAjax(el) {
    if (el && el.getAttribute && el.getAttribute("data-ajax")) return el.getAttribute("data-ajax");
    var s = document.querySelector("[data-ajax]");
    return s ? s.getAttribute("data-ajax") : "/wp-admin/admin-ajax.php";
  }
  window._szGetAjax = getAjax; // expose for functions defined outside IIFE
  function getNonce(el) {
    if (el && el.getAttribute && el.getAttribute("data-nonce")) return el.getAttribute("data-nonce");
    var s = document.querySelector("[data-nonce]");
    return s ? s.getAttribute("data-nonce") : "";
  }
  function toast(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    console.log("[szV2]", msg);
  }

  /* ── Produtos: navegação de abas ─────────────────────────────────────── */
  window.szV2ProdSelect = function (pid) {
    var root = document.getElementById("sec-products");
    if (!root) return;
    pid = String(pid);
    root.querySelectorAll(".szv2-prod-tab").forEach(function (t) {
      var on = t.getAttribute("data-prod") === pid;
      t.classList.toggle("szv2-prod-tab--active", on);
      t.setAttribute("aria-selected", on ? "true" : "false");
    });
    root.querySelectorAll(".szv2-prod-panel").forEach(function (p) {
      p.classList.toggle("szv2-prod-panel--hidden", p.getAttribute("data-prod-panel") !== pid);
    });
  };
  window.szV2ProdSub = function (pid, sub, btn) {
    var root = document.getElementById("sec-products");
    if (!root) return;
    var panel = root.querySelector('.szv2-prod-panel[data-prod-panel="' + pid + '"]');
    if (!panel) return;
    panel.querySelectorAll(".szv2-prod-subtab").forEach(function (b) { b.classList.remove("szv2-prod-subtab--active"); });
    if (btn) btn.classList.add("szv2-prod-subtab--active");
    panel.querySelectorAll(".szv2-prod-sub").forEach(function (s) {
      s.classList.toggle("szv2-prod-sub--hidden", s.getAttribute("data-sub-panel") !== sub);
    });
  };

  /* ── Tabs genérico (Webhooks+Frete reutilizam) ───────────────────────── */
  window.szV2ConnTab = function (id, btn) {
    var sec = (btn && btn.closest("section")) || document;
    sec.querySelectorAll(".szv2-conn-panel").forEach(function (p) {
      p.classList.toggle("szv2-prod-sub--hidden", p.id !== "szv2-panel-" + id);
    });
    var tabs = sec.querySelectorAll(".szv2-prod-subtab");
    tabs.forEach(function (t) { t.classList.remove("szv2-prod-subtab--active"); });
    if (btn) btn.classList.add("szv2-prod-subtab--active");
    if (id === "webhooks-history") szV2WbLoadHistory(0);
    if (id === "integracoes") szV2IntLoad();
  };

  /* ── Copiar URL ──────────────────────────────────────────────────────── */
  window.szV2CopyUrl = function (btn, url) {
    navigator.clipboard.writeText(url || "").then(function () {
      var t = btn.textContent; btn.textContent = "Copiado!";
      setTimeout(function () { btn.textContent = t; }, 1800);
    }).catch(function () { toast("Não foi possível copiar.", "error"); });
  };

  /* ── Produtos: ações de checkout ─────────────────────────────────────── */
  window.szV2ToggleAffVisible = function (cb) {
    ajaxPost(getAjax(cb), { szaction: "checkout_link_affiliate_toggle", link_id: cb.getAttribute("data-link-id"), visible: cb.checked ? 1 : 0, _ajax_nonce: cb.getAttribute("data-nonce") })
      .then(function (r) { if (!r.success) { cb.checked = !cb.checked; toast((r.data && r.data.message) || "Erro.", "error"); } });
  };
  window.szV2SaveComm = function (btn) {
    var row = btn.closest(".szv2-lk-comm-row");
    var inp = row && row.querySelector(".szv2-lk-comm-input");
    if (!inp) return;
    btn.disabled = true;
    ajaxPost(getAjax(btn), { szaction: "checkout_link_commission_update", link_id: inp.getAttribute("data-link-id"), commission_pct: parseFloat(inp.value) || 0, _ajax_nonce: inp.getAttribute("data-nonce") })
      .then(function (r) { btn.disabled = false; toast(r.success ? "Comissao salva." : ((r.data && r.data.message) || "Erro."), r.success ? "success" : "error"); });
  };
  window.szV2DeleteLink = function (btn) {
    var name = btn.getAttribute("data-link-name") || "este checkout";
    window.szV2Confirm({ title: "Excluir checkout", message: "\"" + name + "\" será excluído permanentemente.", btn: "Excluir", danger: true }, function () {
      ajaxPost(getAjax(btn), { szaction: "checkout_link_delete", link_id: btn.getAttribute("data-link-id"), _ajax_nonce: btn.getAttribute("data-nonce") })
        .then(function (r) { if (r.success) { var c = btn.closest(".szv2-lk-card"); if (c) c.remove(); toast("Checkout excluido.", "success"); } else { toast((r.data && r.data.message) || "Erro.", "error"); } });
    });
  };

  var _clPid = "", _clName = "";
  window.szV2ShowNewCheckout = function (pid, pname) {
    _clPid = pid ? String(pid) : ""; _clName = pname ? String(pname) : "";
    var mo = document.getElementById("szv2-new-checkout-modal");
    if (mo) mo.style.display = "flex";
    // Pre-check the checkbox for the current product
    document.querySelectorAll('.szv2-cl-prod-cb').forEach(function(cb) {
      cb.checked = (cb.value === String(_clPid));
    });
    var err = document.getElementById("szv2-modal-err");
    if (err) err.style.display = "none";
  };
  window.szV2CloseNewCheckout = function () {
    var mo = document.getElementById("szv2-new-checkout-modal");
    if (mo) mo.style.display = "none";
  };

  /* ── Motoboy ─────────────────────────────────────────────────────────── */
  var _szMbActiveFilter = "";
  window.szV2MbChipFilter = function (btn) {
    _szMbActiveFilter = btn.getAttribute("data-mb-filter") || "";
    document.querySelectorAll(".szv2-filter-chip[data-mb-filter]").forEach(function (c) {
      c.classList.toggle("szv2-chip-active", c === btn);
    });
    window.szV2MbFilter();
  };
  window.szV2MbPeriod = function (btn) {
    var days = parseInt(btn.getAttribute("data-days"), 10);
    document.querySelectorAll(".szv2-mb-period-btn").forEach(function (b) {
      b.classList.toggle("szv2-chip-active", b === btn);
    });
    var fromEl = document.getElementById("szv2-mb-date-from");
    var toEl   = document.getElementById("szv2-mb-date-to");
    if (!fromEl || !toEl) return;
    if (days < 0) { fromEl.value = ""; toEl.value = ""; }
    else {
      var today = new Date();
      var from  = new Date(today);
      from.setDate(today.getDate() - days);
      fromEl.value = from.toISOString().slice(0, 10);
      toEl.value   = today.toISOString().slice(0, 10);
    }
    window.szV2MbFilter();
  };
  window.szV2MbFilter = function () {
    var q         = ((document.getElementById("szv2-mb-search")       || {}).value || "").toLowerCase();
    var fBaseProd = ((document.getElementById("szv2-mb-f-base-prod")  || {}).value || "");
    var fOffer    = ((document.getElementById("szv2-mb-f-offer")      || {}).value || "");
    var fAff      = ((document.getElementById("szv2-mb-f-aff")        || {}).value || "");
    var fFrom     = (document.getElementById("szv2-mb-date-from")     || {}).value || "";
    var fTo       = (document.getElementById("szv2-mb-date-to")       || {}).value || "";
    var rows      = document.querySelectorAll("#szv2-mb-table tbody tr");
    var vis = 0;
    rows.forEach(function (r) {
      var qm  = !q         || (r.getAttribute("data-search")        || "").indexOf(q) !== -1;
      var sm  = !_szMbActiveFilter || (r.getAttribute("data-status") || "") === _szMbActiveFilter;
      var bpm = !fBaseProd || (r.getAttribute("data-base-product")   || "") === fBaseProd;
      var om  = !fOffer    || (r.getAttribute("data-offer")          || "") === fOffer;
      var am  = !fAff      || (r.getAttribute("data-aff")            || "") === fAff;
      var dt  = (r.getAttribute("data-date") || "").slice(0, 10);
      var dfm = !fFrom || !dt || dt >= fFrom;
      var dto = !fTo   || !dt || dt <= fTo;
      var show = qm && sm && bpm && om && am && dfm && dto;
      r.style.display = show ? "" : "none";
      if (show) vis++;
    });
    var cnt  = document.getElementById("szv2-mb-count");
    var foot = document.getElementById("szv2-mb-foot-count");
    if (cnt)  cnt.textContent  = vis + " pedido(s)";
    if (foot) foot.textContent = "Mostrando " + vis + " pedidos Motoboy";
  };
  var _szDetailReschedOrderId = "", _szDetailReschedRestBase = "", _szDetailReschedNonce = "", _szDetailReschedDate = "";
  function szMbSet(id, val) { var el = document.getElementById(id); if (el) el.textContent = val || "—"; }
  window.szV2MbDetail = function (btn) {
    szMbSet("szv2-detail-num", btn.getAttribute("data-order-num"));
    // Datas
    szMbSet("szv2-detail-order-date", btn.getAttribute("data-order-date") || "—");
    szMbSet("szv2-detail-delivery", btn.getAttribute("data-delivery-date") || "—");
    // Afiliado
    var seller = btn.getAttribute("data-seller") || "";
    var sellerWrap = document.getElementById("szv2-detail-seller-wrap");
    if (sellerWrap) sellerWrap.style.display = seller ? "" : "none";
    szMbSet("szv2-detail-seller", seller);
    // Info extra (não duplica tabela)
    var cpf = btn.getAttribute("data-cpf") || "";
    szMbSet("szv2-detail-cpf", cpf || "—");
    szMbSet("szv2-detail-addr", btn.getAttribute("data-addr"));
    // Complemento
    var comp    = btn.getAttribute("data-complement") || "";
    var compRow = document.getElementById("szv2-detail-complement-row");
    if (compRow) compRow.style.display = comp ? "" : "none";
    szMbSet("szv2-detail-complement", comp);
    // Financeiro
    szMbSet("szv2-detail-bruto", btn.getAttribute("data-bruto"));
    szMbSet("szv2-detail-taxa",  btn.getAttribute("data-taxa"));
    szMbSet("szv2-detail-comm",  btn.getAttribute("data-comm"));
    szMbSet("szv2-detail-liq",   btn.getAttribute("data-liq"));
    // Cartão com taxa
    var ccEl    = document.getElementById("szv2-detail-total-cc");
    if (ccEl) {
      var brutoRaw = parseFloat((btn.getAttribute("data-bruto") || "0").replace(/[^\d,]/g, "").replace(",", ".")) || 0;
      var ccLbl    = document.querySelector("[id='szv2-detail-total-cc']")
                     ? document.querySelector("#szv2-mb-detail-modal [data-ccfee]") : null;
      // fallback: look for % in adjacent text
      var ccFee = 0;
      var ccCtx = document.querySelector("#szv2-mb-detail-modal [style*='taxa']");
      if (ccCtx) { var mx = ccCtx.textContent.match(/([\d.]+)%/); if (mx) ccFee = parseFloat(mx[1]); }
      if (ccFee > 0 && brutoRaw > 0) {
        ccEl.textContent = "R$ " + (brutoRaw * (1 + ccFee/100)).toFixed(2).replace(".", ",");
      } else { ccEl.textContent = "—"; }
    }

    // Seleciona motoboy atual no dropdown
    var mbSel = document.getElementById("szv2-detail-motoboy-sel");
    var mbPedidoId = btn.getAttribute("data-mb-pedido-id") || "";
    var mbMotoboyId = btn.getAttribute("data-mb-motoboy-id") || "";
    if (mbSel) {
      mbSel.value = mbMotoboyId || "";
      mbSel.dataset.pedidoId = mbPedidoId;
      mbSel.dataset.restNonce = btn.getAttribute("data-rest-nonce") || "";
    }
    var mbMsg = document.getElementById("szv2-detail-motoboy-msg");
    if (mbMsg) mbMsg.textContent = "";

    // Armazena para aba histórico
    _szMbHistPedidoId = parseInt(btn.getAttribute("data-mb-pedido-id") || "0");
    _szMbHistNonce = btn.getAttribute("data-rest-nonce") || "";
    // Reset para aba info
    window.szV2MbDetailTab("info");
    // UTM
    var utmData = {};
    try { utmData = JSON.parse(btn.getAttribute("data-utm") || "{}"); } catch(e) {}
    var utmWrap = document.getElementById("szv2-detail-utm-wrap");
    var utmContent = document.getElementById("szv2-detail-utm-content");
    var utmLabels = {utm_source:"Origem",utm_medium:"Mídia",utm_campaign:"Campanha",utm_content:"Conteúdo",utm_term:"Termo"};
    var utmKeys = Object.keys(utmData);
    if (utmWrap && utmContent) {
      if (utmKeys.length) {
        var utmHtml = "";
        utmKeys.forEach(function(k) { utmHtml += '<div style="display:flex;gap:8px;margin-bottom:4px;font-size:12px"><span style="color:var(--szv2-text-muted);min-width:80px">'+(utmLabels[k]||k)+'</span><span style="font-weight:600;color:var(--szv2-text)">'+String(utmData[k]).replace(/</g,'&lt;')+'</span></div>'; });
        utmContent.innerHTML = utmHtml;
      } else {
        utmContent.innerHTML = '<span style="font-size:12px;color:var(--szv2-text-faint)">Sem parâmetros UTM neste pedido.</span>';
      }
      utmWrap.style.display = "";
    }
    // Fiscal: CPF + produto + total
    var fiscalWrap = document.getElementById("szv2-detail-fiscal-wrap");
    var fiscalContent = document.getElementById("szv2-detail-fiscal-content");
    if (fiscalWrap && fiscalContent) {
      var fiscalCpf = btn.getAttribute("data-cpf") || "";
      var fiscalProduct = btn.getAttribute("data-product") || "";
      var fiscalTotal = btn.getAttribute("data-bruto") || "";
      var fiscalHtml = "";
      if (fiscalCpf) fiscalHtml += '<div style="display:flex;gap:8px;margin-bottom:4px"><span style="color:var(--szv2-text-muted);min-width:90px">CPF/CNPJ</span><span style="font-weight:600;color:var(--szv2-text)">'+fiscalCpf+'</span></div>';
      if (fiscalProduct) fiscalHtml += '<div style="display:flex;gap:8px;margin-bottom:4px"><span style="color:var(--szv2-text-muted);min-width:90px">Produto</span><span style="font-weight:600;color:var(--szv2-text)">'+fiscalProduct+'</span></div>';
      if (fiscalTotal) fiscalHtml += '<div style="display:flex;gap:8px"><span style="color:var(--szv2-text-muted);min-width:90px">Valor total</span><span style="font-weight:600;color:var(--szv2-text)">'+fiscalTotal+'</span></div>';
      if (fiscalHtml) {
        fiscalContent.innerHTML = fiscalHtml;
        fiscalWrap.style.display = "";
      } else {
        fiscalWrap.style.display = "none";
      }
    }
    // Reagendamento no modal
    var canResched = btn.getAttribute("data-can-resched") === "1";
    var reschedWrap = document.getElementById("szv2-detail-resched-wrap");
    var reschedConfirm = document.getElementById("szv2-detail-resched-confirm-btn");
    _szDetailReschedOrderId = btn.getAttribute("data-order-id-resched") || "";
    _szDetailReschedRestBase = btn.getAttribute("data-rest-base") || "";
    _szDetailReschedNonce = btn.getAttribute("data-rest-nonce") || "";
    _szDetailReschedDate = "";
    if (reschedWrap) reschedWrap.style.display = canResched ? "" : "none";
    if (reschedConfirm) { reschedConfirm.style.display = canResched ? "" : "none"; reschedConfirm.disabled = true; }
    document.querySelectorAll(".szv2-resched-day-btn").forEach(function (b) { b.classList.remove("szv2-resched-day-btn--selected"); });
    var reschedErr = document.getElementById("szv2-detail-resched-err"); if (reschedErr) reschedErr.style.display = "none";

    document.getElementById("szv2-mb-detail-modal").style.display = "flex";
  };
  window.szV2MbDetailSelectDay = function (btn) {
    _szDetailReschedDate = btn.getAttribute("data-date") || "";
    document.querySelectorAll(".szv2-resched-day-btn").forEach(function (b) { b.classList.remove("szv2-resched-day-btn--selected"); });
    btn.classList.add("szv2-resched-day-btn--selected");
    var cfm = document.getElementById("szv2-detail-resched-confirm-btn"); if (cfm) cfm.disabled = false;
    var err = document.getElementById("szv2-detail-resched-err"); if (err) err.style.display = "none";
  };
  window.szV2MbDetailTab = function (tab) {
    var infoBody = document.getElementById("szv2-detail-body-info");
    var histBody = document.getElementById("szv2-detail-body-hist");
    var tabInfo  = document.getElementById("szv2-detail-tab-info");
    var tabHist  = document.getElementById("szv2-detail-tab-hist");
    var isHist = tab === "hist";
    if (infoBody) infoBody.style.display = isHist ? "none" : "";
    if (histBody) histBody.style.display = isHist ? "" : "none";
    if (tabInfo)  { tabInfo.style.color = isHist ? "var(--szv2-text-muted)" : "var(--szv2-brand)"; tabInfo.style.borderBottomColor = isHist ? "transparent" : "var(--szv2-brand)"; tabInfo.style.fontWeight = isHist ? "600" : "700"; }
    if (tabHist)  { tabHist.style.color = isHist ? "var(--szv2-brand)" : "var(--szv2-text-muted)"; tabHist.style.borderBottomColor = isHist ? "var(--szv2-brand)" : "transparent"; tabHist.style.fontWeight = isHist ? "700" : "600"; }
    if (isHist) _szLoadMbHistory();
  };
  var _szMbHistPedidoId = 0, _szMbHistNonce = "";
  function _szLoadMbHistory() {
    var wrap = document.getElementById("szv2-detail-hist-content");
    if (!wrap || !_szMbHistPedidoId) return;
    wrap.innerHTML = '<div style="text-align:center;padding:20px;color:#9ca3af">Carregando…</div>';
    fetch("/wp-json/sz-motoboy/v1/ol/pedido-historico?pedido_id=" + _szMbHistPedidoId, {
      credentials: "same-origin",
      headers: { "X-WP-Nonce": _szMbHistNonce }
    }).then(function(r){ return r.json(); })
      .then(function(d) {
        if (!d.ok || !d.history || !d.history.length) {
          wrap.innerHTML = '<div style="text-align:center;padding:20px;color:#9ca3af;font-size:13px">Nenhum registro de histórico.</div>';
          return;
        }
        var statusColors = { "Em rota":"#ea580c", "Entregue":"#16a34a", "Frustrado":"#dc2626", "Embalado":"#2563eb", "A caminho":"#7c3aed", "Cancelado":"#6b7280" };
        var html = '<div style="position:relative;padding-left:20px">';
        d.history.forEach(function(h, i) {
          var color = statusColors[h.para] || "#94a3b8";
          var label = h.para ? (h.de ? h.de + " → " + h.para : h.para) : h.acao;
          var actor = h.motoboy_nome ? "Motoboy: " + h.motoboy_nome : { sistema:"Sistema", alan:"Expedição", motoboy:"Motoboy", admin:"Admin", ol:"OL" }[h.actor] || h.actor;
          html += '<div style="position:relative;padding:10px 0 10px 16px;border-left:2px solid ' + (i < d.history.length-1 ? "#e5e7eb" : "transparent") + '">' +
            '<div style="position:absolute;left:-5px;top:14px;width:8px;height:8px;border-radius:50%;background:' + color + '"></div>' +
            '<div style="font-size:13px;font-weight:700;color:#111">' + label + '</div>' +
            '<div style="font-size:11px;color:#9ca3af;margin-top:2px">' + actor + ' · ' + h.ts + '</div>' +
          '</div>';
        });
        html += '</div>';
        wrap.innerHTML = html;
      }).catch(function() {
        wrap.innerHTML = '<div style="text-align:center;padding:20px;color:#dc2626;font-size:13px">Erro ao carregar histórico.</div>';
      });
  }
  window.szV2MbDetailConfirmResched = function () {
    if (!_szDetailReschedDate) return;
    var cfm = document.getElementById("szv2-detail-resched-confirm-btn");
    var err = document.getElementById("szv2-detail-resched-err");
    if (cfm) cfm.disabled = true;
    fetch(_szDetailReschedRestBase + "/motoboy/reagendar", {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-WP-Nonce": _szDetailReschedNonce },
      body: JSON.stringify({ order_id: parseInt(_szDetailReschedOrderId), date: _szDetailReschedDate }),
    }).then(function (r) { return r.json(); })
      .then(function (d) {
        if (d.ok || d.success) {
          document.getElementById("szv2-mb-detail-modal").style.display = "none";
          if (typeof toast === "function") toast("Reagendado para " + _szDetailReschedDate.split("-").reverse().join("/") + ".", "success");
          setTimeout(function () { location.reload(); }, 1200);
        } else {
          if (cfm) cfm.disabled = false;
          if (err) { err.textContent = d.erro || d.message || "Erro ao reagendar."; err.style.display = ""; }
        }
      }).catch(function () {
        if (cfm) cfm.disabled = false;
        if (err) { err.textContent = "Erro de conexão."; err.style.display = ""; }
      });
  };

  window.szV2MbOlStatus = function (btn, status) {
    var pedidoId = btn.getAttribute("data-mb-pedido-id");
    var nonce = btn.getAttribute("data-rest-nonce");
    if (!pedidoId) { toast("Pedido não encontrado.", "error"); return; }
    var label = { em_rota: "em rota", entregue: "entregue", frustrado: "frustrado" }[status] || status;
    window.szV2Confirm({ title: "Confirmar ação", message: "Marcar pedido como " + label + "?", btn: "Confirmar", danger: status === "frustrado" }, function () {
      btn.disabled = true;
      fetch("/wp-json/sz-motoboy/v1/ol/mudar-status", {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-WP-Nonce": nonce },
        body: JSON.stringify({ pedido_id: parseInt(pedidoId), status: status }),
      }).then(function (r) { return r.json(); })
        .then(function (d) {
          if (d.ok) {
            toast("Status atualizado para " + label + ".", "success");
            setTimeout(function () { location.reload(); }, 1200);
          } else {
            btn.disabled = false;
            toast(d.erro || "Erro ao atualizar.", "error");
          }
        }).catch(function () { btn.disabled = false; toast("Erro de conexão.", "error"); });
    });
  };

  window.szV2MbTrocarMotoboy = function (btn) {
    var sel = document.getElementById("szv2-detail-motoboy-sel");
    var msg = document.getElementById("szv2-detail-motoboy-msg");
    if (!sel) return;
    var pedidoId = sel.dataset.pedidoId;
    var motoboyId = sel.value;
    var nonce = sel.dataset.restNonce || btn.getAttribute("data-rest-nonce") || "";
    if (!pedidoId) { if (msg) { msg.style.color = "#dc2626"; msg.textContent = "Pedido não identificado."; } return; }
    btn.disabled = true;
    if (msg) { msg.style.color = "#9ca3af"; msg.textContent = "Salvando…"; }
    fetch("/wp-json/sz-motoboy/v1/ol/trocar-motoboy", {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-WP-Nonce": nonce },
      body: JSON.stringify({ pedido_id: parseInt(pedidoId), motoboy_id: motoboyId ? parseInt(motoboyId) : 0 }),
    }).then(function (r) { return r.json(); })
      .then(function (d) {
        btn.disabled = false;
        if (d.ok) {
          if (msg) { msg.style.color = "#16a34a"; msg.textContent = "✓ Motoboy atualizado."; }
        } else {
          if (msg) { msg.style.color = "#dc2626"; msg.textContent = "Erro ao trocar motoboy."; }
        }
      }).catch(function () { btn.disabled = false; if (msg) { msg.style.color = "#dc2626"; msg.textContent = "Erro de conexão."; } });
  };
  var _reschedOrderId = "", _reschedRestBase = "", _reschedNonce = "", _reschedDate = "";
  window.szV2MbOpenResched = function (btn) {
    _reschedOrderId = btn.getAttribute("data-order-id");
    _reschedRestBase = btn.getAttribute("data-rest-base");
    _reschedNonce = btn.getAttribute("data-nonce");
    _reschedDate = "";
    document.getElementById("szv2-resched-num").textContent = btn.getAttribute("data-order-num");
    document.querySelectorAll(".szv2-resched-day-btn").forEach(function (b) { b.classList.remove("szv2-resched-day-btn--selected"); });
    var err = document.getElementById("szv2-resched-err"); if (err) err.style.display = "none";
    var cfm = document.getElementById("szv2-resched-confirm-btn"); if (cfm) cfm.disabled = true;
    document.getElementById("szv2-mb-resched-modal").style.display = "flex";
  };
  window.szV2MbSelectDay = function (btn) {
    document.querySelectorAll(".szv2-resched-day-btn").forEach(function (b) { b.classList.remove("szv2-resched-day-btn--selected"); });
    btn.classList.add("szv2-resched-day-btn--selected");
    _reschedDate = btn.getAttribute("data-date");
    var cfm = document.getElementById("szv2-resched-confirm-btn"); if (cfm) cfm.disabled = false;
  };
  window.szV2MbConfirmResched = function () {
    if (!_reschedDate || !_reschedOrderId) { toast("Selecione uma data.", "error"); return; }
    var btn = document.getElementById("szv2-resched-confirm-btn"); if (btn) btn.disabled = true;
    fetch(_reschedRestBase + "/motoboy/reschedule", {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-WP-Nonce": _reschedNonce },
      body: JSON.stringify({ order_id: parseInt(_reschedOrderId), date: _reschedDate }),
    }).then(function (r) { return r.json(); }).then(function (r) {
      if (r.success || r.status === "ok") {
        document.getElementById("szv2-mb-resched-modal").style.display = "none";
        toast("Reagendamento confirmado para " + _reschedDate + ".", "success");
      } else {
        var err = document.getElementById("szv2-resched-err");
        if (err) { err.textContent = r.message || "Erro ao reagendar."; err.style.display = "block"; }
        if (btn) btn.disabled = false;
      }
    }).catch(function () { if (btn) btn.disabled = false; toast("Erro de conexao.", "error"); });
  };
  window.szV2MbCancel = function (btn) {
    window.szV2Confirm({ title: "Cancelar pedido", message: "O pedido #" + btn.getAttribute("data-order-num") + " será cancelado.", btn: "Cancelar pedido", danger: true }, function () {
      btn.disabled = true;
      fetch(btn.getAttribute("data-rest-base") + "/motoboy/bulk-cancel", {
        method: "POST", credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-WP-Nonce": btn.getAttribute("data-nonce") },
        body: JSON.stringify({ order_ids: [parseInt(btn.getAttribute("data-order-id"))] }),
      }).then(function (r) { return r.json(); }).then(function (r) {
        btn.disabled = false;
        if (r.success) { var row = btn.closest("tr"); if (row) row.remove(); toast("Pedido cancelado.", "success"); }
        else toast(r.message || "Erro ao cancelar.", "error");
      }).catch(function () { btn.disabled = false; toast("Erro de conexao.", "error"); });
    });
  };

  /* ── Webhooks ────────────────────────────────────────────────────────── */
  window.szV2WbSave = function () {
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    var cls = (document.getElementById("szv2-wh-class") || {}).value || "";
    var url = (document.getElementById("szv2-wh-url") || {}).value || "";
    var active = (document.getElementById("szv2-wh-active") || {}).checked ? 1 : 0;
    // DT-CODE-02: coletar eventos selecionados
    var evCbs = document.querySelectorAll(".szv2-wh-event-cb:checked");
    var eventTypes = evCbs.length > 0
      ? JSON.stringify(Array.from(evCbs).map(function(cb){ return cb.value; }))
      : '';
    var err = document.getElementById("szv2-wh-save-err");
    if (!cls) { if (err) { err.textContent = "Selecione uma classe de entrega."; err.style.display = "block"; } return; }
    var btn = document.getElementById("szv2-wh-save-btn"); if (btn) btn.disabled = true;
    ajaxPost(ajax, { szaction: "webhooks_save", shipping_class_id: cls, url: url, active: active, event_types: eventTypes, _ajax_nonce: nonce })
      .then(function (r) { if (btn) btn.disabled = false; if (r.success) { if (err) err.style.display = "none"; toast("Webhook salvo! Recarregue para ver na lista.", "success"); } else { if (err) { err.textContent = (r.data && r.data.message) || "Erro ao salvar."; err.style.display = "block"; } } });
  };
  window.szV2WbTest = function (btn) {
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    btn.disabled = true;
    ajaxPost(ajax, { szaction: "webhooks_test", webhook_id: btn.getAttribute("data-webhook-id"), _ajax_nonce: nonce })
      .then(function (r) { btn.disabled = false; toast(r.success ? "Webhook testado com sucesso!" : ((r.data && r.data.message) || "Erro no teste."), r.success ? "success" : "error"); });
  };
  window.szV2WbDelete = function (btn) {
    window.szV2Confirm({
      title: 'Excluir webhook',
      message: 'Esta ação não pode ser desfeita. O endpoint deixará de receber eventos dos pedidos.',
      btn: 'Excluir',
      danger: true
    }, function () {
      var sec = document.getElementById("sec-webhooks");
      var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
      var nonce = sec ? sec.getAttribute("data-nonce") : "";
      var webhookId = btn.getAttribute("data-webhook-id") || "";
      ajaxPost(ajax, { szaction: "webhooks_delete", webhook_id: webhookId, _ajax_nonce: nonce })
        .then(function (r) {
          if (r.success) {
            var row = btn.closest("tr");
            if (row) row.remove();
            toast("Webhook removido.", "success");
          } else {
            toast((r.data && r.data.message) || "Erro.", "error");
          }
        });
    });
  };
  window.szV2WbLoadHistory = function (offset) {
    var box = document.getElementById("szv2-wh-history-body");
    if (!box) return;
    if (window.szLoadWebhookHistory) { window.szLoadWebhookHistory(20, offset || 0); return; }
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    box.innerHTML = '<p style="padding:20px;color:var(--szv2-text-muted)">Carregando...</p>';
    ajaxPost(ajax, { szaction: "webhooks_log", offset: offset || 0, limit: 20, _ajax_nonce: nonce })
      .then(function (r) {
        if (!r.success || !r.data || !r.data.logs || !r.data.logs.length) { box.innerHTML = '<p style="padding:20px;color:var(--szv2-text-muted)">Nenhum disparo no historico.</p>'; return; }
        var html = '<div class="szv2-table-wrap"><table class="szv2-table"><thead><tr><th>Data/Hora</th><th>Evento</th><th>Pedido</th><th style="text-align:right">Ações</th></tr></thead><tbody>';
        // F11: adicionar coluna evento + filtro
        var events = {};
        r.data.logs.forEach(function (l) {
          var ev = l.event_label || l.status || '';
          if (ev) events[ev] = true;
        });
        var evKeys = Object.keys(events);
        if (evKeys.length > 1) {
          var fid = 'szv2-wh-ev-filter';
          var fHtml = '<div style="margin-bottom:8px;display:flex;align-items:center;gap:8px"><label style="font-size:12px;font-weight:600;color:var(--szv2-text-muted)">Evento:</label><select id="' + fid + '" class="szv2-input szv2-input-sm" style="max-width:200px" onchange="szV2WbFilterRows(this.value)"><option value="">Todos</option>' + evKeys.map(function(e){ return '<option value="'+e+'">'+e+'</option>'; }).join('') + '</select></div>';
          box.innerHTML = fHtml;
        } else { box.innerHTML = ''; }
        r.data.logs.forEach(function (l) {
          var ev = l.event_label || l.status || '—';
          html += '<tr data-ev="' + ev + '"><td class="szv2-td-sub">' + (l.created_at_fmt || l.created_at || "—") + '</td><td><span class="sz-badge szv2-badge-neutral">' + ev + '</span></td><td>#' + (l.order_id || "—") + '</td><td style="text-align:right"><div style="display:flex;gap:4px;justify-content:flex-end"><button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary" onclick="szV2WbShowPayload(' + l.id + ')">Payload</button></div></td></tr>';
        });
        html += '</tbody></table></div>';
        // append: filtro já está em box.innerHTML
        var tbl = document.createElement('div');
        tbl.innerHTML = html;
        box.appendChild(tbl);
      }).catch(function () { box.innerHTML = '<p style="padding:20px;color:var(--szv2-danger)">Erro ao carregar historico.</p>'; });
  };

  // F11: filtro de eventos no histórico de webhooks
  window.szV2WbFilterRows = function(ev) {
    var box = document.getElementById('szv2-wh-history-body');
    if (!box) return;
    box.querySelectorAll('tr[data-ev]').forEach(function(r) {
      r.style.display = (!ev || r.getAttribute('data-ev') === ev) ? '' : 'none';
    });
  };
  // D3: payload viewer via szV2Confirm em vez de alert()
  window.szV2WbShowPayload = function(id) {
    var p = window.SZ_WH_LOG_PAYLOADS && window.SZ_WH_LOG_PAYLOADS[id];
    var msg = p ? JSON.stringify(p, null, 2) : '{}';
    if (typeof window.szV2Confirm === 'function') {
      window.szV2Confirm({ title: 'Payload #' + id, message: msg, btn: 'Fechar', danger: false }, function(){});
    } else {
      alert(msg);
    }
  };

  /* ── Integracoes ─────────────────────────────────────────────────────── */
  window.szV2IntLoad = function () {
    var loading = document.getElementById("szv2-int-loading");
    var content = document.getElementById("szv2-int-content");
    if (!loading || !content) return;
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    ajaxPost(ajax, { szaction: "integrations_get", _ajax_nonce: nonce })
      .then(function (r) {
        loading.style.display = "none";
        if (!r.success || !r.integration) { content.innerHTML = '<p style="padding:20px;color:var(--szv2-danger)">Integracao indisponivel.</p>'; content.style.display = "block"; return; }
        var int = r.integration;
        var urlEl = document.getElementById("szv2-int-url"); if (urlEl) urlEl.value = int.url || "";
        var cb = function (id, val) { var el = document.getElementById(id); if (el) el.checked = !!val; };
        cb("szv2-int-active", int.active); cb("szv2-int-cheapest", int.auto_cheapest);
        cb("szv2-int-paid", int.require_paid); cb("szv2-int-dup", int.ignore_duplicates);
        var logs = r.logs || [];
        var logsEl = document.getElementById("szv2-int-logs");
        if (logsEl) {
          if (!logs.length) { logsEl.innerHTML = '<p class="szv2-td-sub">Nenhum recebimento ainda.</p>'; }
          else {
            var lhtml = '<div class="szv2-table-wrap"><table class="szv2-table"><thead><tr><th>Data</th><th>Status</th><th>Pedido ext.</th><th style="text-align:right">Acoes</th></tr></thead><tbody>';
            logs.forEach(function (l) {
              lhtml += '<tr><td class="szv2-td-sub">' + (l.created_at_fmt || "") + '</td><td><span class="sz-badge szv2-badge-neutral">' + (l.status || "—") + '</span></td><td class="szv2-num">' + (l.external_order_id || "—") + '</td><td style="text-align:right"><button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary" onclick="szV2IntReprocess(' + l.id + ')">Reprocessar</button></td></tr>';
            });
            lhtml += '</tbody></table></div>';
            logsEl.innerHTML = lhtml;
          }
        }
        content.style.display = "block";
      }).catch(function () { loading.style.display = "none"; content.innerHTML = '<p style="padding:20px;color:var(--szv2-danger)">Erro ao carregar.</p>'; content.style.display = "block"; });
  };
  window.szV2IntSave = function () {
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    var gc = function (id) { var el = document.getElementById(id); return el ? (el.checked ? 1 : 0) : 0; };
    ajaxPost(ajax, { szaction: "integrations_save", active: gc("szv2-int-active"), auto_cheapest: gc("szv2-int-cheapest"), require_paid: gc("szv2-int-paid"), ignore_duplicates: gc("szv2-int-dup"), _ajax_nonce: nonce })
      .then(function (r) { toast(r.success ? "Integracao salva!" : ((r.data && r.data.message) || "Erro."), r.success ? "success" : "error"); });
  };
  window.szV2IntReprocess = function (logId) {
    var sec = document.getElementById("sec-webhooks");
    var ajax = sec ? sec.getAttribute("data-ajax") : getAjax();
    var nonce = sec ? sec.getAttribute("data-nonce") : getNonce();
    ajaxPost(ajax, { szaction: "integrations_reprocess", log_id: logId || 0, _ajax_nonce: nonce })
      .then(function (r) { toast(r.success ? "Reprocessado com sucesso!" : ((r.data && r.data.message) || "Erro."), r.success ? "success" : "error"); });
  };
  window.szV2CopyIntUrl = function () {
    var el = document.getElementById("szv2-int-url");
    if (el) { el.select(); document.execCommand("copy"); toast("URL copiada!", "success"); }
  };

  /* ── Suporte ─────────────────────────────────────────────────────────── */
  var _spCurrentTicket = 0;
  function spGetSec() {
    // Support is embedded in Settings; sec-settings holds ajax-url and nonce
    return document.getElementById("sec-support") || document.getElementById("sec-settings");
  }
  function spList() { return document.getElementById("szv2-support-list-inner") || document.getElementById("szv2-support-list"); }
  function spDetailEl() { return document.getElementById("szv2-support-detail-inner") || document.getElementById("szv2-support-detail"); }
  function spAjax(body) {
    var sec = spGetSec();
    return ajaxPost(sec ? sec.getAttribute("data-szv2-ajax-url") : "/wp-admin/admin-ajax.php",
      Object.assign({ _ajax_nonce: sec ? sec.getAttribute("data-szv2-nonce") : "" }, body));
  }
  function spStatusBadge(st) {
    var map = { aberto: "brand", respondido: "success", em_analise: "warning", fechado: "neutral" };
    var lbl = { aberto: "Aberto", respondido: "Respondido", em_analise: "Em analise", fechado: "Fechado" };
    return '<span class="sz-badge szv2-badge-' + (map[st] || "neutral") + '">' + (lbl[st] || st) + '</span>';
  }
  window.szV2SpLoad = function () {
    var list = spList();
    if (!list) return;
    list.innerHTML = '<p class="szv2-td-sub" style="padding:20px">Carregando...</p>';
    spAjax({ szaction: "tickets_list" }).then(function (r) {
      if (!r.success) { list.innerHTML = '<p style="padding:20px;color:var(--szv2-danger)">Erro ao carregar chamados.</p>'; return; }
      var tickets = r.tickets || [];
      if (!tickets.length) { list.innerHTML = '<div class="szv2-card" style="padding:32px;text-align:center"><p class="szv2-td-sub">Nenhum chamado aberto. Use o botao "Novo chamado" para comecar.</p></div>'; return; }
      var html = '<div class="szv2-card" style="padding:0;overflow:hidden"><div class="szv2-table-wrap"><table class="szv2-table"><thead><tr><th>#</th><th>Assunto</th><th>Categoria</th><th>Status</th><th>Atualizado</th><th></th></tr></thead><tbody>';
      tickets.forEach(function (t) {
        var date = t.updated_at ? t.updated_at.substring(0, 10) : "";
        html += '<tr><td class="szv2-num" style="font-size:12px">' + t.id + '</td><td class="szv2-td-main">' + escHtml(t.assunto || "—") + '</td><td class="szv2-td-sub">' + escHtml(t.categoria || "—") + '</td><td>' + spStatusBadge(t.status) + '</td><td class="szv2-td-sub">' + date + '</td><td><button type="button" class="szv2-btn szv2-btn-sm szv2-btn-secondary" onclick="szV2SpOpen(' + t.id + ')">Abrir</button></td></tr>';
      });
      html += '</tbody></table></div></div>';
      list.innerHTML = html;
    });
  };
  window.szV2SpOpen = function (tid) {
    _spCurrentTicket = tid;
    var list = spList();
    var detail = spDetailEl();
    if (list) list.style.display = "none";
    if (detail) detail.style.display = "block";
    spAjax({ szaction: "ticket_msgs", ticket_id: tid }).then(function (r) {
      if (!r.success) return;
      var t = r.ticket || {};
      var el = document.getElementById("szv2-ticket-title");
      if (el) el.textContent = "Chamado #" + t.id + ": " + (t.assunto || "");
      var badge = document.getElementById("szv2-ticket-badge");
      if (badge) badge.innerHTML = spStatusBadge(t.status);
      var closeBtn = document.getElementById("szv2-ticket-close-btn");
      if (closeBtn) closeBtn.style.display = t.status === "fechado" ? "none" : "";
      var reply = document.getElementById("szv2-ticket-reply-area");
      if (reply) reply.style.display = t.status === "fechado" ? "none" : "flex";
      var msgs = document.getElementById("szv2-ticket-msgs");
      if (!msgs) return;
      var mhtml = "";
      (r.msgs || []).forEach(function (m) {
        var isClient = m.autor_tipo === "cliente";
        mhtml += '<div style="display:flex;flex-direction:column;align-items:' + (isClient ? "flex-end" : "flex-start") + ';margin-bottom:12px">';
        mhtml += '<div style="max-width:80%;background:' + (isClient ? "var(--szv2-brand-muted)" : "var(--szv2-surface-alt)") + ';border-radius:12px;padding:10px 14px">';
        mhtml += '<p style="font-size:12px;color:var(--szv2-text-muted);margin-bottom:4px">' + escHtml(m.autor_nome || "") + ' · ' + (m.created_at || "").substring(0, 16) + '</p>';
        mhtml += '<p style="margin:0;white-space:pre-wrap">' + escHtml(m.mensagem || "") + '</p></div></div>';
      });
      msgs.innerHTML = mhtml;
      msgs.scrollTop = msgs.scrollHeight;
    });
  };
  window.szV2SpBack = function () {
    var list = spList();
    var detail = spDetailEl();
    if (list) list.style.display = "block";
    if (detail) detail.style.display = "none";
    szV2SpLoad();
  };
  window.szV2SpSendMsg = function () {
    var ta = document.getElementById("szv2-ticket-reply");
    var msg = ta ? ta.value.trim() : "";
    if (!msg) return;
    spAjax({ szaction: "ticket_send_msg", ticket_id: _spCurrentTicket, mensagem: msg }).then(function (r) {
      if (r.success) { if (ta) ta.value = ""; szV2SpOpen(_spCurrentTicket); }
      else toast((r.data && r.data.message) || "Erro ao enviar.", "error");
    });
  };
  window.szV2SpCloseTicket = function () {
    window.szV2Confirm({ title: "Fechar chamado", message: "Tem certeza que deseja fechar este chamado de suporte?", btn: "Fechar chamado" }, function () {
      spAjax({ szaction: "ticket_close", ticket_id: _spCurrentTicket }).then(function (r) {
        if (r.success) { toast("Chamado fechado.", "success"); szV2SpOpen(_spCurrentTicket); }
        else toast((r.data && r.data.message) || "Erro.", "error");
      });
    });
  };
  window.szV2SpShowCreate = function () {
    var mo = document.getElementById("szv2-sp-create-modal");
    if (mo) mo.style.display = "flex";
  };
  window.szV2SpCreateTicket = function () {
    var assunto = (document.getElementById("szv2-sp-assunto") || {}).value || "";
    var cat = (document.getElementById("szv2-sp-categoria") || {}).value || "outro";
    var msg = (document.getElementById("szv2-sp-mensagem") || {}).value || "";
    var err = document.getElementById("szv2-sp-create-err");
    if (assunto.length < 5) { if (err) { err.textContent = "Assunto muito curto."; err.style.display = "block"; } return; }
    if (msg.length < 10) { if (err) { err.textContent = "Mensagem muito curta."; err.style.display = "block"; } return; }
    spAjax({ szaction: "ticket_create", assunto: assunto, categoria: cat, mensagem: msg }).then(function (r) {
      if (r.success) {
        var mo = document.getElementById("szv2-sp-create-modal"); if (mo) mo.style.display = "none";
        if (err) err.style.display = "none";
        toast("Chamado aberto com sucesso!", "success");
        szV2SpLoad();
        if (r.ticket_id) szV2SpOpen(r.ticket_id);
      } else { if (err) { err.textContent = (r.data && r.data.message) || "Erro ao criar."; err.style.display = "block"; } }
    });
  };

  /* ── Usuarios ────────────────────────────────────────────────────────── */
  window.szV2UsrShowCreate = function () {
    var mo = document.getElementById("szv2-usr-create-modal");
    if (mo) mo.style.display = "flex";
    var err = document.getElementById("szv2-usr-err"); if (err) err.style.display = "none";
  };
  window.szV2UsrCreate = function (btn) {
    var name = (document.getElementById("szv2-usr-name") || {}).value || "";
    var email = (document.getElementById("szv2-usr-email") || {}).value || "";
    var pw = (document.getElementById("szv2-usr-pw") || {}).value || "";
    var err = document.getElementById("szv2-usr-err");
    if (!name || !email || pw.length < 8) {
      if (err) { err.textContent = "Preencha todos os campos. Senha minimo 8 caracteres."; err.style.display = "block"; } return;
    }
    btn.disabled = true;
    ajaxPost(btn.getAttribute("data-ajax") || getAjax(), { szaction: "create_sub_user", name: name, email: email, password: pw, _ajax_nonce: btn.getAttribute("data-nonce") })
      .then(function (r) { btn.disabled = false; if (r.success) { var mo = document.getElementById("szv2-usr-create-modal"); if (mo) mo.style.display = "none"; toast("Usuario criado! Recarregue para ver.", "success"); } else { if (err) { err.textContent = (r.data && r.data.message) || "Erro ao criar."; err.style.display = "block"; } } });
  };
  window.szV2UsrDelete = function (btn) {
    var name = btn.getAttribute("data-user-name") || "este usuario";
    window.szV2Confirm({ title: "Remover usuário", message: "\"" + name + "\" perderá acesso ao painel.", btn: "Remover", danger: true }, function () {
      btn.disabled = true;
      ajaxPost(getAjax(), { szaction: "delete_sub_user", user_id: btn.getAttribute("data-user-id"), _ajax_nonce: btn.getAttribute("data-nonce") })
        .then(function (r) { btn.disabled = false; if (r.success) { var row = btn.closest("tr"); if (row) row.remove(); toast("Usuario removido.", "success"); } else toast((r.data && r.data.message) || "Erro.", "error"); });
    });
  };

  /* ── Configuracoes ───────────────────────────────────────────────────── */
  window.szV2StSaveEmail = function () {
    var inp     = document.getElementById('szv2-st-email');
    var confirm = document.getElementById('szv2-st-email-confirm');
    var msg     = document.getElementById('szv2-st-email-msg');
    if (!inp) return;
    var email1 = (inp.value || '').trim().toLowerCase();
    var email2 = confirm ? (confirm.value || '').trim().toLowerCase() : email1;
    if (!email1) { if (msg) { msg.textContent = 'Informe o novo e-mail.'; msg.style.color = 'var(--szv2-danger)'; msg.style.display = 'block'; } return; }
    if (email1 !== email2) { if (msg) { msg.textContent = 'Os e-mails informados não coincidem.'; msg.style.color = 'var(--szv2-danger)'; msg.style.display = 'block'; } return; }
    var ajax  = inp.getAttribute('data-ajax') || getAjax();
    var nonce = inp.getAttribute('data-nonce') || getNonce();
    ajaxPost(ajax, { szaction: 'update_email', new_email: email1, _ajax_nonce: nonce })
      .then(function (r) { if (msg) { msg.textContent = r.success ? 'E-mail atualizado. Você será desconectado.' : ((r.data && r.data.message) || 'Erro.'); msg.style.color = r.success ? 'var(--szv2-success)' : 'var(--szv2-danger)'; msg.style.display = 'block'; } });
  };
  window.szV2StSavePw = function () {
    var cur = (document.getElementById("szv2-st-pw-cur") || {}).value || "";
    var nw = (document.getElementById("szv2-st-pw-new") || {}).value || "";
    var cf = (document.getElementById("szv2-st-pw-confirm") || {}).value || "";
    var msg = document.getElementById("szv2-st-pw-msg");
    if (nw !== cf) { if (msg) { msg.textContent = "As senhas nao coincidem."; msg.style.color = "var(--szv2-danger)"; msg.style.display = "block"; } return; }
    var btn = event && event.target;
    var ajax = btn ? btn.getAttribute("data-ajax") : getAjax();
    var nonce = btn ? btn.getAttribute("data-nonce") : getNonce();
    ajaxPost(ajax || getAjax(), { szaction: "update_password", current_password: cur, new_password: nw, _ajax_nonce: nonce })
      .then(function (r) { if (msg) { msg.textContent = r.success ? "Senha alterada. Voce sera desconectado." : ((r.data && r.data.message) || "Erro."); msg.style.color = r.success ? "var(--szv2-success)" : "var(--szv2-danger)"; msg.style.display = "block"; } });
  };

  /* ── Init ────────────────────────────────────────────────────────────── */
  function escHtml(s) { return String(s == null ? "" : s).replace(/[&<>'"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[c]; }); }

  document.addEventListener("DOMContentLoaded", function () {
    // D6: Event delegation para section Produtos (substitui onclick= inline)
    var szProdRoot = document.getElementById("sec-products");
    if (szProdRoot) {
      szProdRoot.addEventListener("click", function (e) {
        var btn;

        btn = e.target.closest('[data-action="cd-select"]');
        if (btn) { szV2PrCdSelectBtn(parseInt(btn.getAttribute("data-cd-id") || "0", 10)); return; }

        btn = e.target.closest('[data-action="prod-select"]');
        if (btn) { szV2ProdSelect(parseInt(btn.getAttribute("data-product-id") || "0", 10)); return; }

        btn = e.target.closest('[data-action="prod-sub"]');
        if (btn) { szV2ProdSub(parseInt(btn.getAttribute("data-product-id") || "0", 10), btn.getAttribute("data-sub") || "checkouts", btn); return; }

        btn = e.target.closest('[data-action="new-checkout"]');
        if (btn) { szV2ShowNewCheckout(parseInt(btn.getAttribute("data-product-id") || "0", 10), btn.getAttribute("data-name") || ""); return; }

        btn = e.target.closest('[data-action="save-comm"]');
        if (btn) { szV2SaveComm(btn); return; }

        btn = e.target.closest('[data-action="copy-url"]');
        if (btn) { szV2CopyUrl(btn, btn.getAttribute("data-url") || ""); return; }

        btn = e.target.closest('[data-action="delete-link"]');
        if (btn) { szV2DeleteLink(btn); return; }

        btn = e.target.closest('[data-action="np-add-var"]');
        if (btn) { szV2NpAddVar(); return; }

        btn = e.target.closest('[data-action="submit-new-product"]');
        if (btn) { szV2SubmitNewProduct(btn); return; }
      });
    }

    // Inicializa filtro motoboy com "Hoje"
    var todayBtn = document.querySelector(".szv2-mb-period-btn[data-days='0']");
    if (todayBtn && typeof window.szV2MbPeriod === 'function') window.szV2MbPeriod(todayBtn);
    // Fecha modais ao clicar fora
    document.addEventListener("click", function (e) {
      [
        "szv2-new-checkout-modal", "szv2-mb-detail-modal", "szv2-mb-resched-modal",
        "szv2-usr-create-modal", "szv2-sp-create-modal"
      ].forEach(function (id) {
        var mo = document.getElementById(id);
        if (mo && e.target === mo) mo.style.display = "none";
      });
    });
    // Auto-carrega Suporte quando a aba Suporte em Configurações é clicada
    document.addEventListener("click", function(e) {
      var btn = e.target && e.target.closest ? e.target.closest("[onclick*=\"st-support\"]") : null;
      if (btn) { setTimeout(szV2SpLoad, 50); }
    });
    // Fallback: também carrega se sec-support clássico estiver visível
    var supSec = document.getElementById("sec-support");
    if (supSec && supSec.offsetHeight > 0) { szV2SpLoad(); }
  });
})();

/* ── Frete: salvar preferidas/bloqueadas ─────────────────────────────── */
window.szV2FrAutoCheck = function (cb, type) {
  if (type === 'pref' && cb.checked) {
    document.querySelectorAll('.szv2-fr-pref-cb').forEach(function(c){ c.checked = false; });
  }
};
window.szV2FrSave = function (type, btn) {
  var isPref = type === 'pref';
  var cbs = document.querySelectorAll(isPref ? '.szv2-fr-pref-cb' : '.szv2-fr-blk-cb');
  var ids = [];
  cbs.forEach(function(c){ if (c.checked) ids.push(c.value); });
  var szaction = isPref ? 'set_preferred_carrier' : 'set_blocked_carrier';
  var msg = document.getElementById(isPref ? 'szv2-fr-pref-msg' : 'szv2-fr-blk-msg');
  btn.disabled = true;
  window._szAjaxPost(btn.getAttribute('data-ajax') || window._szGetAjax(), {
    szaction: szaction, method_ids: ids.join(','), _ajax_nonce: btn.getAttribute('data-nonce')
  }).then(function(r) {
    btn.disabled = false;
    if (msg) { msg.textContent = r.success ? (r.message || 'Salvo!') : ((r.data && r.data.message) || 'Erro.'); msg.style.color = r.success ? 'var(--szv2-success)' : 'var(--szv2-danger)'; msg.style.display = 'block'; setTimeout(function(){ msg.style.display='none'; }, 3000); }
  });
};

/* ── Checkout modal: multi-produto ──────────────────────────────────── */
window.szV2SaveNewCheckout = function (btn) {
  var valor = (document.getElementById('szv2-cl-valor') || {}).value || '0';
  var commRaw = (document.getElementById('szv2-cl-comm') || {}).value;
  var comm = (commRaw !== '' && commRaw !== null && commRaw !== undefined) ? commRaw : '0';
  var affVis = ((document.getElementById('szv2-cl-aff-vis') || {}).checked ? 1 : 0);
  var err = document.getElementById('szv2-modal-err');
  // Coleta produtos selecionados
  var components = [];
  var nameParts = [];
  document.querySelectorAll('.szv2-cl-prod-cb:checked').forEach(function(cb) {
    var item = cb.closest('.szv2-prod-select-item');
    var qtyEl = item ? item.querySelector('.szv2-cl-prod-qty') : null;
    var qty = qtyEl ? (parseInt(qtyEl.value) || 1) : 1;
    var productName = item ? (item.querySelector('.szv2-prod-select-name') || {}).textContent || '' : '';
    components.push({ product_id: parseInt(cb.value), qty: qty });
    nameParts.push((qty > 1 ? qty + 'x ' : '') + productName.trim());
  });
  if (!components.length) { if (err) { err.textContent = 'Adicione pelo menos um produto.'; err.style.display = 'block'; } return; }
  // Nome auto-gerado: "2x Produto A + 1x Produto B"
  // Diferenciador: conta quantos checkouts com mesma composição já existem e adiciona sufixo
  var baseName = nameParts.join(' + ');
  var existingWithSameComposition = document.querySelectorAll('[data-comp="' + encodeURIComponent(baseName) + '"]').length;
  var autoName = existingWithSameComposition > 0 ? baseName + ' (' + (existingWithSameComposition + 1) + ')' : baseName;
  btn.disabled = true;
  window._szAjaxPost(btn.getAttribute('data-ajax') || window._szGetAjax(), {
    szaction: 'checkout_link_generate', cl_name: autoName, cl_valor: valor,
    cl_commission_pct: comm, cl_affiliate_visible: affVis,
    cl_components: JSON.stringify(components), _ajax_nonce: btn.getAttribute('data-nonce')
  }).then(function(r) {
    btn.disabled = false;
    if (r.success) { szV2CloseNewCheckout(); toast('Checkout criado! Recarregue para ver.', 'success'); }
    else { var msg2 = (r.data && r.data.message) || 'Erro ao criar.'; if (err) { err.textContent = msg2; err.style.display = 'block'; } }
  });
};

/* ── Produto: enviar para aprovação ─────────────────────────────────── */
window.szV2SubmitNewProduct = function (btn) {
  var name = (document.getElementById('szv2-np-name') || {}).value || '';
  var cat = (document.getElementById('szv2-np-cat') || {}).value || 'outro';
  var peso = (document.getElementById('szv2-np-peso') || {}).value || '';
  var alt = (document.getElementById('szv2-np-alt') || {}).value || '';
  var larg = (document.getElementById('szv2-np-larg') || {}).value || '';
  var comp = (document.getElementById('szv2-np-comp') || {}).value || '';
  var foto = (document.getElementById('szv2-np-foto') || {}).value || '';
  var obs = (document.getElementById('szv2-np-obs') || {}).value || '';
  var vitrineDesc = (document.getElementById('szv2-np-vitrine-desc') || {}).value || '';
  var err = document.getElementById('szv2-np-err');
  if (!name.trim()) { if (err) { err.textContent = 'Informe o nome do produto.'; err.style.display = 'block'; } return; }
  btn.disabled = true;
  // Usa ticket_create para encaminhar o produto como chamado de suporte
  window._szAjaxPost(btn.getAttribute('data-ajax') || window._szGetAjax(), {
    szaction: 'ticket_create',
    assunto: 'Solicitacao de novo produto: ' + name,
    categoria: 'outro',
    mensagem: 'Produto: ' + name + '\nCategoria: ' + cat + '\nPeso: ' + peso + 'g\nAltura: ' + alt + 'cm | Largura: ' + larg + 'cm | Comprimento: ' + comp + 'cm\nFoto: ' + foto + '\nDescricao Vitrine: ' + vitrineDesc + '\nObs: ' + obs,
    _ajax_nonce: btn.getAttribute('data-nonce')
  }).then(function(r) {
    btn.disabled = false;
    if (r.success) {
      document.getElementById('szv2-add-product-modal').style.display = 'none';
      toast('Produto enviado para aprovacao!', 'success');
    } else {
      if (err) { err.textContent = (r.data && r.data.message) || 'Erro ao enviar.'; err.style.display = 'block'; }
    }
  });
};

/* ── Notificações ────────────────────────────────────────────────────── */
window.szV2NotifSave = function (cb) {
  var sec = document.getElementById('sec-settings');
  var ajax = sec ? sec.getAttribute('data-ajax') : window._szGetAjax();
  var nonce = cb.getAttribute('data-nonce') || getNonce();
  window._szAjaxPost(ajax, { szaction: 'save_notification_pref', event: cb.getAttribute('data-event'), enabled: cb.checked ? 1 : 0, _ajax_nonce: nonce })
    .catch(function(){});  // best-effort
};

/* ── PIX: adicionar conta ────────────────────────────────────────────── */
window.szV2PixAdd = function () {
  var sec = document.getElementById('sec-settings');
  var ajax = sec ? sec.getAttribute('data-ajax') : window._szGetAjax();
  var nonce = sec ? sec.getAttribute('data-nonce') : getNonce();
  var name = (document.getElementById('szv2-pix-name') || {}).value || '';
  var cpf = (document.getElementById('szv2-pix-cpf') || {}).value || '';
  var banco = (document.getElementById('szv2-pix-banco') || {}).value || '';
  var tipo = (document.getElementById('szv2-pix-tipo') || {}).value || 'CPF';
  var chave = (document.getElementById('szv2-pix-chave') || {}).value || '';
  var msg = document.getElementById('szv2-pix-msg');
  if (!name || !chave) { if (msg) { msg.textContent = 'Preencha titular e chave PIX.'; msg.style.color = 'var(--szv2-danger)'; msg.style.display = 'block'; } return; }
  window._szAjaxPost(ajax, { szaction: 'add_pix_account', titular: name, cpf: cpf, banco: banco, tipo_pix: tipo, chave: chave, _ajax_nonce: nonce })
    .then(function(r) {
      if (msg) { msg.textContent = r.success ? 'Conta adicionada!' : ((r.data && r.data.message) || 'Erro.'); msg.style.color = r.success ? 'var(--szv2-success)' : 'var(--szv2-danger)'; msg.style.display = 'block'; }
      if (r.success) { toast('Conta PIX adicionada. Recarregue para ver.', 'success'); }
    });
};

/* ── Relatórios: filtrar e exportar CSV ─────────────────────────────── */
window.szV2RpFilter = function () {
  var from = (document.getElementById('szv2-rp-from') || {}).value || '';
  var to   = (document.getElementById('szv2-rp-to')   || {}).value || '';
  var note = document.getElementById('szv2-rp-note');
  if (note) note.textContent = 'Filtro aplicado: ' + (from || '—') + ' a ' + (to || '—') + '. Recarregue para dados em tempo real.';
};

window.szV2RpExport = function (btn, label) {
  var from = (document.getElementById('szv2-rp-from') || {}).value || '';
  var to   = (document.getElementById('szv2-rp-to')   || {}).value || '';
  var st   = (document.getElementById('szv2-rp-status') || {}).value || '';
  var mode = btn.getAttribute('data-mode') || 'motoboy';
  var ajax = btn.getAttribute('data-ajax') || window._szGetAjax();
  var nonce = btn.getAttribute('data-nonce') || getNonce();
  btn.disabled = true; btn.textContent = 'Gerando...';
  window._szAjaxPost(ajax, { szaction: 'export_csv', date_from: from, date_to: to, status: st, mode: mode, _ajax_nonce: nonce })
    .then(function (r) {
      btn.disabled = false; btn.textContent = '↓ Exportar ' + label;
      if (r.success && r.data && r.data.csv) {
        var blob = new Blob([r.data.csv], { type: 'text/csv;charset=utf-8;' });
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url; a.download = 'senderzz-' + mode + '-' + (from || 'all') + '.csv';
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
        setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
      } else if (r.data && r.data.url) {
        window.open(r.data.url, '_blank');
      } else {
        toast((r.data && r.data.message) || 'Erro ao exportar.', 'error');
      }
    }).catch(function () { btn.disabled = false; btn.textContent = '↓ Exportar ' + label; toast('Erro de conexao.', 'error'); });
};

/* ── Produtos: filtro de CD ─────────────────────────────────────────── */
window.szV2PrCdSelectBtn = function (cdId) {
  // Atualiza botões do seletor de CD (suporta data-action="cd-select" e onclick legado)
  document.querySelectorAll('#sec-products .szv2-btn[data-action="cd-select"]').forEach(function (btn) {
    btn.classList.remove('szv2-btn-brand');
    btn.classList.add('szv2-btn-secondary');
  });
  var activeBtn = document.querySelector('#sec-products .szv2-btn[data-action="cd-select"][data-cd-id="' + cdId + '"]');
  if (activeBtn) { activeBtn.classList.remove('szv2-btn-secondary'); activeBtn.classList.add('szv2-btn-brand'); }

  // Filtra tabs e painéis de produto
  var tabs   = document.querySelectorAll('#sec-products .szv2-prod-tab');
  var panels = document.querySelectorAll('#sec-products .szv2-prod-panel');
  var firstVisible = null;

  tabs.forEach(function (tab) {
    tab.style.display = '';
    if (!firstVisible) firstVisible = tab.dataset.prod;
  });

  panels.forEach(function (panel) {
    panel.classList.remove('szv2-prod-panel--hidden');
  });

  // Seleciona o primeiro produto visível
  if (firstVisible) { szV2ProdSelect(parseInt(firstVisible, 10)); }
};

/* ── Add variação ao modal de produto ───────────────────────────────── */
var _varCount = 0;
window.szV2NpAddVar = function () {
  var container = document.getElementById('szv2-np-vars');
  if (!container) return;
  _varCount++;
  var id = 'var_' + _varCount;
  var div = document.createElement('div');
  div.className = 'szv2-np-var-row';
  div.id = id;
  div.innerHTML = '<input type="text" class="szv2-input szv2-np-var-name" placeholder="Nome (ex: P, M, G, 100ml)" style="flex:1">'
    + '<input type="number" class="szv2-input szv2-np-var-peso" placeholder="Peso (g)" min="1" style="width:90px">'
    + '<input type="number" class="szv2-input szv2-np-var-alt" placeholder="Alt" min="1" style="width:70px">'
    + '<input type="number" class="szv2-input szv2-np-var-larg" placeholder="Larg" min="1" style="width:70px">'
    + '<input type="number" class="szv2-input szv2-np-var-comp" placeholder="Comp" min="1" style="width:70px">'
    + '<button type="button" class="szv2-lk-del-btn" onclick="document.getElementById(\'' + id + '\').remove()" title="Remover">✕</button>';
  container.appendChild(div);
};

// Override szV2SubmitNewProduct to include variations
var _origSubmit = window.szV2SubmitNewProduct;
window.szV2SubmitNewProduct = function (btn) {
  // Collect variations
  var vars = [];
  document.querySelectorAll('.szv2-np-var-row').forEach(function (row) {
    var name = (row.querySelector('.szv2-np-var-name') || {}).value || '';
    var peso = (row.querySelector('.szv2-np-var-peso') || {}).value || '';
    var alt  = (row.querySelector('.szv2-np-var-alt')  || {}).value || '';
    var larg = (row.querySelector('.szv2-np-var-larg') || {}).value || '';
    var comp = (row.querySelector('.szv2-np-var-comp') || {}).value || '';
    if (name) vars.push(name + (peso ? ' / ' + peso + 'g' : '') + (alt ? ' / ' + alt + 'x' + larg + 'x' + comp + 'cm' : ''));
  });
  // Append variations to obs
  var obsEl = document.getElementById('szv2-np-obs');
  if (obsEl && vars.length) {
    obsEl.value = (obsEl.value ? obsEl.value + '\n' : '') + 'Variações: ' + vars.join(' | ');
  }
  _origSubmit(btn);
};

/* ── Dashboard + Relatórios top tab ─────────────────────────────────── */
window.szV2DashTopTab = function (tab, btn) {
  var visao = document.getElementById('szv2-dash-top-visao');
  var rp    = document.getElementById('szv2-dash-top-relatorios');
  if (!visao || !rp) return;
  visao.style.display = tab === 'visao' ? '' : 'none';
  rp.style.display    = tab === 'relatorios' ? '' : 'none';
  document.querySelectorAll('.szv2-dash-top-tab').forEach(function(b) {
    b.classList.toggle('szv2-dash-top-tab--active', b === btn);
  });
};

/* ── 2FA toggle ─────────────────────────────────────────────────────── */
window.szV2Toggle2FA = function (cb) {
  var ajax  = cb.getAttribute('data-ajax') || window._szGetAjax();
  var nonce = cb.getAttribute('data-nonce') || getNonce();
  window._szAjaxPost(ajax, { szaction: 'toggle_2fa', require: cb.checked ? 1 : 0, _ajax_nonce: nonce })
    .then(function (r) {
      toast(r.success ? (r.message || (cb.checked ? '2FA ativado.' : '2FA desativado.')) : ((r.data && r.data.message) || 'Erro.'),
            r.success ? 'success' : 'error');
      if (!r.success) cb.checked = !cb.checked;
    }).catch(function () { cb.checked = !cb.checked; toast('Erro de conexao.', 'error'); });
};
