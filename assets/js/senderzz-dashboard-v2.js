/**
 * Senderzz Dashboard V2 — JS de casca (Fase 1)
 * --------------------------------------------------------------
 * Responsabilidade: APENAS comportamento visual do shell —
 * sidebar, tema, navegação entre seções, tabs, modal e toast.
 *
 * Proibido aqui (regra da Fase 1): fetch/AJAX, leitura de dados,
 * qualquer regra de negócio. A lógica de dados continua no
 * pipeline legado e será religada seção a seção nas Fases 2–7.
 *
 * Compatibilidade: define window.szGo(key, el) com a MESMA
 * assinatura usada pelo markup legado (onclick="szGo('reports',el)"),
 * para que seções portadas futuramente funcionem sem mudança.
 * Storage próprio (szV2Theme / szV2Sidebar) para não colidir com
 * as chaves legadas (szTheme / sz_sb_collapsed); lê szTheme como
 * fallback inicial para continuidade da preferência do usuário.
 */
(function () {
  "use strict";

  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  /* ── Tema ──────────────────────────────────────────────────── */
  function getTheme() {
    try {
      return (
        localStorage.getItem("szV2Theme") ||
        localStorage.getItem("szTheme") || // continuidade com a V1
        "light"
      );
    } catch (e) {
      return "light";
    }
  }
  function applyTheme(t) {
    root.setAttribute("data-theme", t === "dark" ? "dark" : "light");
  }
  function toggleTheme() {
    var next = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    applyTheme(next);
    try { localStorage.setItem("szV2Theme", next); } catch (e) {}
  }
  applyTheme(getTheme());

  /* ── Sidebar aberta/recolhida ──────────────────────────────── */
  function getSidebar() {
    try { return localStorage.getItem("szV2Sidebar") || "open"; } catch (e) { return "open"; }
  }
  function applySidebar(state) {
    root.setAttribute("data-sidebar", state === "collapsed" ? "collapsed" : "open");
  }
  function toggleSidebar() {
    var next = root.getAttribute("data-sidebar") === "collapsed" ? "open" : "collapsed";
    applySidebar(next);
    try { localStorage.setItem("szV2Sidebar", next); } catch (e) {}
  }
  applySidebar(getSidebar());

  /* ── Navegação de seções (data-nav → #sec-{key}) ───────────── */
  var navItems = root.querySelectorAll(".szv2-nav .sz-ni[data-nav]");
  var titleEl = root.querySelector("[data-szv2-title]");

  function activate(key, push) {
    var section = root.querySelector("#sec-" + key);
    if (!section) return false;

    root.querySelectorAll(".sz-sec.szv2-active").forEach(function (s) {
      s.classList.remove("szv2-active");
    });
    section.classList.add("szv2-active");

    navItems.forEach(function (item) {
      var active = item.getAttribute("data-nav") === key;
      if (active) item.setAttribute("aria-current", "true");
      else item.removeAttribute("aria-current");
    });

    if (titleEl) {
      var label = section.getAttribute("data-szv2-label") || key;
      titleEl.textContent = label;
    }
    if (push) {
      try {
        var base = window.location.pathname + (window.location.search.indexOf('sz_v2') >= 0 ? window.location.search : (window.location.search ? window.location.search + '&sz_v2=1' : '?sz_v2=1'));
        history.replaceState(null, "", base + "#" + key);
      } catch (e) {}
    }
    return true;
  }

  /* Compat: mesma assinatura do legado. */
  if (typeof window.szGo !== "function") {
    window.szGo = function (key, el) { activate(String(key || ""), true); };
  }

  navItems.forEach(function (item) {
    item.addEventListener("click", function () {
      activate(item.getAttribute("data-nav"), true);
    });
  });

  /* Deep-link por hash; default = dashboard. */
  var initial = (location.hash || "").replace(/^#/, "");
  if (!initial || !activate(initial, false)) activate("dashboard", false);

  /* ── Tabs visuais ──────────────────────────────────────────── */
  root.querySelectorAll("[data-szv2-tabs]").forEach(function (group) {
    // Supports both .szv2-tab (components) and .szv2-dash-tab (dashboard switcher)
    var tabs = group.querySelectorAll(".szv2-tab, .szv2-dash-tab");
    tabs.forEach(function (tab) {
      tab.addEventListener("click", function () {
        var target = tab.getAttribute("data-szv2-tab");
        tabs.forEach(function (t) {
          t.setAttribute("aria-selected", t === tab ? "true" : "false");
          t.classList.toggle("szv2-dash-tab--active", t === tab);
        });
        var scope = group.getAttribute("data-szv2-tabs");
        // Supports both .szv2-tab-panel (components) and .szv2-dash-panel (dashboard)
        root
          .querySelectorAll('[data-szv2-panel-group="' + scope + '"]')
          .forEach(function (panel) {
            if (panel.getAttribute("data-szv2-panel") === target) panel.removeAttribute("hidden");
            else panel.setAttribute("hidden", "");
          });
      });
    });
  });

  /* ── Modal (abrir/fechar por data-attributes) ──────────────── */
  // D4: focus trap helper para modais regulares
  function _szV2ModalTrapFocus(overlay, e) {
    var focusable = Array.from(overlay.querySelectorAll('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'));
    if (!focusable.length) return;
    var idx = focusable.indexOf(document.activeElement);
    e.preventDefault();
    var next = e.shiftKey ? (idx <= 0 ? focusable.length - 1 : idx - 1) : (idx >= focusable.length - 1 ? 0 : idx + 1);
    focusable[next].focus();
  }
  function closeModal(overlay) {
    overlay.classList.remove("szv2-open");
    if (overlay._szV2FocusBefore && overlay._szV2FocusBefore.focus) overlay._szV2FocusBefore.focus();
  }
  root.addEventListener("click", function (e) {
    var opener = e.target.closest("[data-szv2-modal-open]");
    if (opener) {
      var target = root.querySelector(
        '.szv2-modal-overlay[data-szv2-modal="' + opener.getAttribute("data-szv2-modal-open") + '"]'
      );
      if (target) {
        target._szV2FocusBefore = document.activeElement;
        target.classList.add("szv2-open");
        // auto-focus primeiro campo focável
        setTimeout(function () {
          var first = target.querySelector('button:not([disabled]),input:not([disabled]),[tabindex]:not([tabindex="-1"])');
          if (first) first.focus();
        }, 50);
      }
      return;
    }
    var closer = e.target.closest("[data-szv2-modal-close]");
    if (closer) {
      var overlay = closer.closest(".szv2-modal-overlay");
      if (overlay) closeModal(overlay);
      return;
    }
    if (e.target.classList && e.target.classList.contains("szv2-modal-overlay")) {
      closeModal(e.target);
    }
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      root.querySelectorAll(".szv2-modal-overlay.szv2-open").forEach(closeModal);
    }
    // D4: focus trap para modal aberto
    if (e.key === "Tab") {
      var open = root.querySelector(".szv2-modal-overlay.szv2-open");
      if (open) _szV2ModalTrapFocus(open, e);
    }
  });

  /* ── Toast (helper global da V2) ───────────────────────────── */
  window.szV2Toast = function (message, kind) {
    var holder = root.querySelector(".szv2-toasts");
    if (!holder) return;
    var el = document.createElement("div");
    el.className = "szv2-toast szv2-toast-" + (kind || "info");
    el.setAttribute("role", "status");
    el.textContent = String(message || "");
    holder.appendChild(el);
    setTimeout(function () {
      if (el.parentNode) el.parentNode.removeChild(el);
    }, 4000);
  };

  /* ── Botões do rodapé da sidebar ───────────────────────────── */
  var themeBtn = root.querySelector("[data-szv2-theme-toggle]");
  if (themeBtn) themeBtn.addEventListener("click", toggleTheme);

  var sbBtn = root.querySelector("[data-szv2-sidebar-toggle]");
  if (sbBtn) sbBtn.addEventListener("click", toggleSidebar);

  var saldoEye = root.querySelector("[data-szv2-saldo-eye]");
  if (saldoEye) {
    saldoEye.addEventListener("click", function () {
      var v = root.querySelector("[data-szv2-saldo-value]");
      if (!v) return;
      var hidden = v.getAttribute("data-szv2-hidden") === "1";
      v.textContent = hidden ? v.getAttribute("data-szv2-display") || "—" : "••••";
      v.setAttribute("data-szv2-hidden", hidden ? "0" : "1");
    });
  }
})();

/* ── Filtros e busca client-side (Fase 3) ────────────────────────
   Zero fetch. Opera sobre data-szv2-row-filter / data-szv2-row-search
   já embutidos no HTML pelo PHP. ─────────────────────────────── */
(function () {
  "use strict";

  function initFilters() {
    var r = document.querySelector(".sz-dashboard-v2");
    if (!r) return;

    // Preenche contadores de cada chip com base nos data-attributes
    function updateCounts(group) {
      var rows = r.querySelectorAll('[data-szv2-row-group="' + group + '"]');
      var counts = { todos: rows.length };
      rows.forEach(function (row) {
        var f = row.getAttribute("data-szv2-row-filter") || "";
        counts[f] = (counts[f] || 0) + 1;
      });
      r.querySelectorAll('[data-szv2-count]').forEach(function (el) {
        var key = el.getAttribute("data-szv2-count");
        var parentChip = el.closest("[data-szv2-filter-group]");
        if (!parentChip || parentChip.getAttribute("data-szv2-filter-group") !== group) return;
        var n = counts[key] || 0;
        el.textContent = n > 0 ? " (" + n + ")" : "";
      });
    }

    function applyFilter(group, filterKey, searchVal) {
      var rows = r.querySelectorAll('[data-szv2-row-group="' + group + '"]');
      var visible = 0;
      rows.forEach(function (row) {
        var matchFilter = filterKey === "todos" || row.getAttribute("data-szv2-row-filter") === filterKey;
        var matchSearch = !searchVal || (row.getAttribute("data-szv2-row-search") || "").indexOf(searchVal) !== -1;
        var show = matchFilter && matchSearch;
        row.style.display = show ? "" : "none";
        if (show) visible++;
      });
      var resultEl = r.querySelector('[data-szv2-result="' + group + '"]');
      if (resultEl) {
        resultEl.textContent = filterKey !== "todos" || searchVal
          ? visible + " pedido" + (visible !== 1 ? "s" : "") + " exibido" + (visible !== 1 ? "s" : "")
          : "";
      }
    }

    // State per group
    var state = {};

    function getState(group) {
      if (!state[group]) state[group] = { filter: "todos", search: "" };
      return state[group];
    }

    // Chip click
    r.addEventListener("click", function (e) {
      var chip = e.target.closest(".szv2-filter-chip[data-szv2-filter]");
      if (!chip) return;
      var group = chip.getAttribute("data-szv2-filter-group");
      var key = chip.getAttribute("data-szv2-filter");
      if (!group) return;
      // Toggle active state
      r.querySelectorAll('.szv2-filter-chip[data-szv2-filter-group="' + group + '"]').forEach(function (c) {
        var active = c === chip;
        c.setAttribute("aria-pressed", active ? "true" : "false");
        c.classList.toggle("szv2-chip-active", active);
      });
      getState(group).filter = key;
      applyFilter(group, key, getState(group).search);
    });

    // Search input
    r.addEventListener("input", function (e) {
      var input = e.target;
      if (!input.classList.contains("szv2-section-search")) return;
      var group = input.getAttribute("data-szv2-search");
      if (!group) return;
      getState(group).search = input.value.toLowerCase().trim();
      applyFilter(group, getState(group).filter, getState(group).search);
    });

    // Init counts for all groups on load
    ["motoboy", "expedicao"].forEach(updateCounts);
  }

  // Run after DOM is ready (script is footer-deferred)
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initFilters);
  } else {
    initFilters();
  }
})();

/* ── Dashboard V2 — recalculador de cards por período (v426) ─────
   Lê o JSON em #szv2-dash-data e recalcula todos os KPIs/listas
   ao mudar o filtro. Cada grupo (cod/exp) tem estado independente.
   Padrão: 30 dias. Zero fetch. Zero AJAX.
   ─────────────────────────────────────────────────────────────── */
(function () {
  "use strict";

  // ── Utilitários ───────────────────────────────────────────────
  function isoToday() {
    return new Date().toISOString().slice(0, 10);
  }
  function isoSubDays(n) {
    var d = new Date();
    d.setDate(d.getDate() - n + 1);
    return d.toISOString().slice(0, 10);
  }
  function fmt(v, currency) {
    currency = currency || "R$";
    return currency + "\u00A0" + v.toFixed(2).replace(".", ",").replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }
  function pct(n, total) {
    return total > 0 ? Math.round((n / total) * 100) : 0;
  }
  function setText(sel, text) {
    var els = document.querySelectorAll("[data-szv2-kpi='" + sel + "']");
    els.forEach(function (el) { el.textContent = text; });
  }
  function setBar(key, p) {
    var bars = document.querySelectorAll("[data-szv2-bar='" + key + "']");
    bars.forEach(function (b) { b.style.width = Math.min(100, Math.max(0, p)) + "%"; });
  }
  function setList(key, html) {
    var els = document.querySelectorAll("[data-szv2-list='" + key + "']");
    els.forEach(function (el) { el.innerHTML = html; });
  }
  function barRow(name, count, total) {
    var p = pct(count, total);
    return '<div class="szv2-region-row">'
      + '<span class="szv2-region-name">' + esc(name) + '</span>'
      + '<div class="szv2-region-bar-wrap"><div class="szv2-region-bar-fill" style="width:' + p + '%"></div></div>'
      + '<span class="szv2-region-meta szv2-num">' + count + ' <small>(' + p + '%)</small></span></div>';
  }
  function esc(s) {
    return String(s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
  }

  // ── Status groupings ──────────────────────────────────────────
  var DONE   = ["entregue","completed","completo","delivered"]; // pedidos concluídos com sucesso
  var ACTIVE = ["agendado","aprovado","on-hold","separado","embalado","coletado","acaminho","em-rota","em_rota","emretirada"];
  var FRUSTR = ["frustrado","devolvido"];
  var TRANSIT_EXP = ["enviado","acaminho","em-rota","emretirada","coletado"];
  var PEND_EXP    = ["on-hold","pending","processing","aprovado","separado","embalado"];

  function inSet(st, arr) { return arr.indexOf(st) !== -1; }

  // ── Status label map ──────────────────────────────────────────
  var STATUS_LABELS = {
    entregue:"Entregue", completed:"Concluído", completo:"Concluído",
    enviado:"Enviado", acaminho:"A caminho", "em-rota":"Em rota",
    coletado:"Coletado", emretirada:"Em retirada", embalado:"Embalado",
    separado:"Separado", aprovado:"Aprovado", "on-hold":"Aguardando",
    processing:"Processando", pending:"Pendente", cancelled:"Cancelado",
    cancelado:"Cancelado", emcancelamento:"Em cancelamento",
    frustrado:"Frustrado", devolvido:"Devolvido",
    saldoinsuficiente:"Saldo insuficiente"
  };
  function stLabel(st) {
    return STATUS_LABELS[st] || (st.charAt(0).toUpperCase() + st.slice(1).replace(/[-_]/g," "));
  }
  function stVariant(st) {
    if (inSet(st,DONE)) return "success";
    if (inSet(st,FRUSTR)) return "danger";
    if (inSet(st,["cancelled","cancelado","emcancelamento","devolvido"])) return "neutral";
    if (inSet(st,TRANSIT_EXP)) return "brand";
    if (inSet(st,ACTIVE)) return "warning";
    return "neutral";
  }
  function statusBadgeHtml(st) {
    return '<span class="sz-badge szv2-badge-' + stVariant(st) + '" data-status="' + esc(st) + '">' + esc(stLabel(st)) + '</span>';
  }

  // ── Core recalc ───────────────────────────────────────────────
  function recalc(group, days, data) {
    var from = days === 1 ? isoToday() : isoSubDays(days);
    var to   = isoToday();
    var orders = data.orders.filter(function (o) {
      return o.g === group && o.d >= from && o.d <= to;
    });
    var cur = data.currency || "R$";

    if (group === "cod") { recalcCod(orders, data, cur); }
    else                 { recalcExp(orders, data, cur); }
  }

  function recalcCod(orders, data, cur) {
    var total = orders.length;
    var rev = 0, comm = 0, done = 0, active = 0, frustr = 0, canc = 0;
    var products = {}, affiliates = {}, regions = {}, statuses = {};
    orders.forEach(function (o) {
      rev   += o.v;
      comm  += o.co;
      if (inSet(o.st, DONE))   done++;
      if (inSet(o.st, ACTIVE)) active++;
      if (inSet(o.st, FRUSTR)) frustr++;
      if (o.st === "cancelled" || o.st === "cancelado") canc++;
      // status breakdown
      if (o.st) { statuses[o.st] = (statuses[o.st] || 0) + 1; }
      // products
      if (o.p) {
        if (!products[o.p]) products[o.p] = {qty:0,rev:0,comm:0};
        products[o.p].qty++; products[o.p].rev += o.v; products[o.p].comm += o.co;
      }
      // affiliates
      if (o.af) {
        if (!affiliates[o.af]) affiliates[o.af] = {qty:0,rev:0,comm:0};
        affiliates[o.af].qty++; affiliates[o.af].rev += o.v; affiliates[o.af].comm += o.co;
      }
      // regions
      if (o.r) { regions[o.r] = (regions[o.r] || 0) + 1; }
    });
    var ticket = total > 0 ? rev / total : 0;
    // Eficiência: pedidos entregues/completos ÷ total criados no período
    var effPct = total > 0 ? pct(done, total) : 0;

    setText("cod-avail", fmt(data.cod_avail, cur));
    setText("cod-rev",   fmt(rev, cur));
    setText("cod-comm",  fmt(comm, cur));
    setText("cod-total", String(total));
    setText("cod-total2",String(total));
    setText("cod-done",  String(done));
    setText("cod-active",String(active));
    setText("cod-frustr",String(frustr));
    setText("cod-canc",  String(canc));
    setText("cod-pct",   effPct + "%");
    setText("cod-ticket",fmt(ticket, cur));
    setBar("cod-pct", effPct);
    // Status bars (novo painel integrado)
    if (typeof window.szV2RenderStatusBars === "function") {
      window.szV2RenderStatusBars(statuses, total);
    }

    // Regions
    var regEntries = Object.entries(regions).sort(function(a,b){return b[1]-a[1];}).slice(0,5);
    var regTotal = regEntries.reduce(function(s,e){return s+e[1];},0);
    setList("cod-regions", regEntries.length
      ? regEntries.map(function(e){return barRow(e[0],e[1],regTotal);}).join("")
      : '<p class="szv2-empty-inline">Sem dados no período</p>');

    // Products
    var prodEntries = Object.entries(products).sort(function(a,b){return b[1].rev-a[1].rev;}).slice(0,8);
    var isAff = data.is_aff;
    setList("cod-products", prodEntries.length
      ? prodEntries.map(function(e){
          return "<tr><td>"+esc(e[0].slice(0,52))+"</td><td class='szv2-td-num szv2-num'>"+e[1].qty+"</td>"
            +"<td class='szv2-td-num szv2-num'>"+fmt(e[1].rev,cur)+"</td>"
            +(isAff?"<td class='szv2-td-num szv2-num'>"+fmt(e[1].comm,cur)+"</td>":"")+"</tr>";}).join("")
      : "<tr><td colspan='4' class='szv2-empty-cell'>Sem produtos no período</td></tr>");

    // Affiliates
    var affEntries = Object.entries(affiliates).sort(function(a,b){return b[1].comm-a[1].comm;}).slice(0,6);
    setList("cod-affiliates", affEntries.length
      ? affEntries.map(function(e){
          return "<tr><td>"+esc(e[0])+"</td><td class='szv2-td-num szv2-num'>"+e[1].qty+"</td>"
            +"<td class='szv2-td-num szv2-num'>"+fmt(e[1].rev,cur)+"</td>"
            +"<td class='szv2-td-num szv2-num'>"+fmt(e[1].comm,cur)+"</td></tr>";}).join("")
      : "<tr><td colspan='4' class='szv2-empty-cell'>Sem afiliados no período</td></tr>");
  }

  function recalcExp(orders, data, cur) {
    var total = orders.length;
    var rev = 0, done = 0, transit = 0, pend = 0, canc = 0, items = 0;
    var clients = {}, products = {}, regions = {}, carriers = {}, statusMap = {};
    orders.forEach(function (o) {
      rev += o.v;
      items += (o.i || 1);
      if (inSet(o.st, DONE))           done++;
      if (inSet(o.st, TRANSIT_EXP))    transit++;
      if (inSet(o.st, PEND_EXP))       pend++;
      if (o.st === "cancelled" || o.st === "cancelado") canc++;
      if (o.e)  clients[o.e]    = true;
      if (o.p)  { if (!products[o.p])  products[o.p]  = {qty:0,rev:0}; products[o.p].qty++;  products[o.p].rev  += o.v; }
      if (o.ca) { if (!carriers[o.ca]) carriers[o.ca] = 0; carriers[o.ca]++; }
      if (o.r)  { regions[o.r] = (regions[o.r] || 0) + 1; }
      statusMap[o.st] = (statusMap[o.st] || 0) + 1;
    });
    var clientCount = Object.keys(clients).length;
    var ticket  = total > 0 ? rev / total : 0;
    var effPct  = total > 0 ? pct(done, total) : 0;
    var freteTot = 0; orders.forEach(function(o){ freteTot += (o.fr || 0); });
    var avgShip = total > 0 ? freteTot / total : 0; // custo médio de frete real (shipping_total_raw)

    setText("exp-avail",   fmt(data.tpc_avail, cur));
    setText("exp-rev",     fmt(rev, cur));
    setText("exp-total",   String(total));
    setText("exp-clients", String(clientCount));
    setText("exp-clients2",String(clientCount));
    setText("exp-items",   String(items));
    setText("exp-avgship", total > 0 ? fmt(avgShip, cur) : "—");
    setText("exp-ticket",  fmt(ticket, cur));
    setText("exp-pend",    String(pend));
    setText("exp-transit", String(transit));
    setText("exp-done",    String(done));
    setText("exp-canc",    String(canc));
    setText("exp-pct",     effPct + "%");

    // Transportadora destaque
    var carEntries = Object.entries(carriers).sort(function(a,b){return b[1]-a[1];});
    var carTotal   = carEntries.reduce(function(s,e){return s+e[1];},0);
    var topCar = carEntries[0];
    var carHtml = "";
    if (topCar && topCar[1] > 0) {
      var topPct = pct(topCar[1], carTotal);
      carHtml += '<div class="szv2-highlight-row"><span class="szv2-highlight-name">'+esc(topCar[0])+'</span>'
        +'<span class="szv2-highlight-meta szv2-num">'+topCar[1]+' expedições &middot; '+topPct+'%</span></div>';
      if (carEntries.length > 1) {
        carHtml += '<div class="szv2-region-list szv2-region-list-mt">'
          + carEntries.slice(0,5).map(function(e){return barRow(e[0],e[1],carTotal);}).join("") + '</div>';
      }
    } else {
      carHtml = '<p class="szv2-card-placeholder">— <small>Sem dados suficientes no período</small></p>';
    }
    setList("exp-carrier-highlight", carHtml);

    // Status breakdown
    var stEntries = Object.entries(statusMap).sort(function(a,b){return b[1]-a[1];}).slice(0,10);
    setList("exp-status", stEntries.length
      ? stEntries.map(function(e){
          return '<div class="szv2-region-row"><span class="szv2-region-name">'
            +statusBadgeHtml(e[0])+'</span>'
            +'<div class="szv2-region-bar-wrap"><div class="szv2-region-bar-fill" style="width:'+pct(e[1],total)+'%"></div></div>'
            +'<span class="szv2-region-meta szv2-num">'+e[1]+' <small>('+pct(e[1],total)+'%)</small></span></div>';}).join("")
      : '<p class="szv2-empty-inline">Sem dados no período</p>');

    // Products
    var prodEntries = Object.entries(products).sort(function(a,b){return b[1].rev-a[1].rev;}).slice(0,8);
    var prodRevTotal = prodEntries.reduce(function(s,e){return s+e[1].rev;},0);
    setList("exp-products", prodEntries.length
      ? prodEntries.map(function(e){
          var pp = pct(e[1].rev, prodRevTotal);
          return "<tr><td>"+esc(e[0].slice(0,52))+"</td><td class='szv2-td-num szv2-num'>"+e[1].qty+"</td>"
            +"<td class='szv2-td-num szv2-num'>"+fmt(e[1].rev,cur)+"</td>"
            +"<td class='szv2-td-num szv2-num'>"+pp+"%</td></tr>";}).join("")
      : "<tr><td colspan='4' class='szv2-empty-cell'>Sem produtos no período</td></tr>");

    // Regions
    var regEntries = Object.entries(regions).sort(function(a,b){return b[1]-a[1];}).slice(0,5);
    var regTotal = regEntries.reduce(function(s,e){return s+e[1];},0);
    setList("exp-regions", regEntries.length
      ? regEntries.map(function(e){return barRow(e[0],e[1],regTotal);}).join("")
      : '<p class="szv2-empty-inline">Sem dados no período</p>');
  }

  // ── Bootstrap ─────────────────────────────────────────────────
  function init() {
    var el = document.getElementById("szv2-dash-data");
    if (!el) return;
    var data;
    try { data = JSON.parse(el.textContent || el.innerHTML); } catch(e) { return; }
    if (!data || !Array.isArray(data.orders)) return;

    var state = { cod: 30, exp: 30 };
    var root  = document.querySelector(".sz-dashboard-v2");
    if (!root) return;

    function run(group) { recalc(group, state[group], data); }

    // Period button clicks
    root.addEventListener("click", function (e) {
      var btn = e.target.closest("[data-szv2-period]");
      if (!btn) return;
      var group = btn.getAttribute("data-szv2-period");
      var days  = parseInt(btn.getAttribute("data-days") || "30", 10);
      if (!group || isNaN(days)) return;
      state[group] = days;
      root.querySelectorAll('[data-szv2-period="' + group + '"]').forEach(function (b) {
        b.classList.toggle("szv2-period-btn--active",
          parseInt(b.getAttribute("data-days"), 10) === days);
      });
      run(group);
    });

    // Initial render (30 days each)
    run("cod");
    run("exp");
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();

/* ── Motoboy + Expedição — ações operacionais (v429) ────────────
   Chama os MESMOS backends da V1. Sem nova lógica de negócio.
   admin-ajax URL: lida do atributo data-szv2-ajax-url do <section>.
   Fallback hardcoded apenas como último recurso.
   Anti-duplo-clique: flag inFlight por pedido + botão disabled.
   ─────────────────────────────────────────────────────────────── */
(function () {
  "use strict";

  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  var inFlight = {};

  // ── helpers ───────────────────────────────────────────────────
  function getAjaxUrl(btn) {
    // Priority: section attribute → SZ_CONFIG → fallback (last resort)
    var section = btn.closest("[data-szv2-ajax-url]");
    if (section && section.dataset.szv2AjaxUrl) return section.dataset.szv2AjaxUrl;
    if (window.SZ_CONFIG && window.SZ_CONFIG.ajaxUrl) return window.SZ_CONFIG.ajaxUrl;
    return "/wp-admin/admin-ajax.php"; // last resort, documented
  }

  function setLoading(btn, on) {
    btn.disabled = on;
    btn.setAttribute("aria-busy", on ? "true" : "false");
    if (on) { btn.dataset.origText = btn.textContent; btn.textContent = "…"; }
    else { btn.textContent = btn.dataset.origText || btn.textContent; }
  }

  function toast(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    alert(msg);
  }

  function postAjax(ajaxUrl, data) {
    var body = Object.keys(data).map(function (k) {
      return encodeURIComponent(k) + "=" + encodeURIComponent(data[k]);
    }).join("&");
    return fetch(ajaxUrl, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body,
    }).then(function (r) { return r.json(); });
  }

  function postRest(url, nonce, payload) {
    return fetch(url, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-WP-Nonce": nonce },
      body: JSON.stringify(payload),
    }).then(function (r) { return r.json(); });
  }

  function reloadAfterAction(ms) {
    setTimeout(function () { window.location.reload(); }, ms || 700);
  }

  // S14: szConfirm aliasado para szV2Confirm quando disponível (V1 usava window.confirm)
  function szConfirm(msg) {
    return new Promise(function(resolve) {
      if (typeof window.szV2Confirm === 'function') {
        window.szV2Confirm({ title: 'Confirmar', message: msg, danger: false }, function() { resolve(true); });
      } else {
        resolve(window.confirm(msg));
      }
    });
  }

  // ── Action: Aprovar (Expedição only) ─────────────────────────
  function handleApprove(btn) {
    var oid = btn.dataset.orderId, num = btn.dataset.orderNum;
    var nonce = btn.dataset.nonce, ajaxUrl = getAjaxUrl(btn);
    if (inFlight[oid]) return;
    szConfirm("Aprovar pedido #" + num + " e liberar emissão de etiqueta?").then(function (ok) {
      if (!ok) return;
      inFlight[oid] = true; setLoading(btn, true);
      postAjax(ajaxUrl, { action: "senderzz_portal", szaction: "approve", order_id: oid, _ajax_nonce: nonce })
        .then(function (r) {
          var msg = (r.data && r.data.message) || r.message || (r.success ? "Pedido aprovado." : "Erro ao aprovar.");
          toast(msg, r.success ? "success" : "danger");
          if (r.success) { reloadAfterAction(); } else { setLoading(btn, false); delete inFlight[oid]; }
        }).catch(function () {
          toast("Erro de conexão ao aprovar.", "danger");
          setLoading(btn, false); delete inFlight[oid];
        });
    });
  }

  // ── Action: Cancelar (Motoboy + Expedição) ───────────────────
  function handleCancel(btn) {
    var oid = btn.dataset.orderId, num = btn.dataset.orderNum;
    var nonce = btn.dataset.nonce, hasLabel = btn.dataset.hasLabel === "1";
    var ajaxUrl = getAjaxUrl(btn);
    if (inFlight[oid]) return;
    var msg = hasLabel
      ? "Este pedido possui etiqueta. Será solicitado cancelamento ao Melhor Envio. Confirmar?"
      : "Cancelar o pedido #" + num + "?";
    var doCancel = function() {
      inFlight[oid] = true; setLoading(btn, true);
      postAjax(ajaxUrl, { action: "senderzz_portal", szaction: "cancel", order_id: oid, _ajax_nonce: nonce })
        .then(function (r) {
          var m = (r.data && r.data.message) || r.message || (r.success ? "Pedido cancelado." : "Erro ao cancelar.");
          toast(m, r.success ? "success" : "danger");
          if (r.success) { reloadAfterAction(); } else { setLoading(btn, false); delete inFlight[oid]; }
        }).catch(function () {
          toast("Erro de conexão ao cancelar.", "danger");
          setLoading(btn, false); delete inFlight[oid];
        });
    };
    if (typeof window.szV2Confirm === 'function') {
      window.szV2Confirm({ title: 'Cancelar pedido', message: msg, btn: 'Cancelar pedido', danger: true }, doCancel);
    } else {
      if (window.confirm(msg)) doCancel();
    }
  }

  // ── Action: Reprocessar etiqueta (Expedição) ─────────────────
  function handleRetry(btn) {
    var oid = btn.dataset.orderId, num = btn.dataset.orderNum;
    var nonce = btn.dataset.nonce, ajaxUrl = getAjaxUrl(btn);
    if (inFlight[oid]) return;
    szConfirm("Reprocessar etiqueta do pedido #" + num + "?").then(function (ok) {
      if (!ok) return;
      inFlight[oid] = true; setLoading(btn, true);
      postAjax(ajaxUrl, { action: "senderzz_portal", szaction: "retry", order_id: oid, _ajax_nonce: nonce })
        .then(function (r) {
          var m = (r.data && r.data.message) || r.message || (r.success ? "Reprocessamento iniciado." : "Erro ao reprocessar.");
          toast(m, r.success ? "success" : "danger");
          if (r.success) { reloadAfterAction(); } else { setLoading(btn, false); delete inFlight[oid]; }
        }).catch(function () {
          toast("Erro de conexão ao reprocessar.", "danger");
          setLoading(btn, false); delete inFlight[oid];
        });
    });
  }

  // ── Action: Reagendar (Motoboy via modal) ────────────────────
  var reschedModal = root.querySelector("#szv2-mb-reschedule-modal");
  var reschedOrderId = null;

  function handleReschedule(btn) {
    if (!reschedModal) return;
    reschedOrderId = btn.dataset.orderId;
    var numEl = root.querySelector("#szv2-mb-resched-num");
    if (numEl) numEl.textContent = "#" + (btn.dataset.orderNum || reschedOrderId);
    var dateInput = root.querySelector("#szv2-mb-resched-date");
    if (dateInput) dateInput.value = "";
    reschedModal.classList.add("szv2-open");
  }

  var reschedConfirmBtn = root.querySelector("#szv2-mb-resched-confirm");
  if (reschedConfirmBtn) {
    reschedConfirmBtn.addEventListener("click", function () {
      var dateInput = root.querySelector("#szv2-mb-resched-date");
      var date = dateInput ? dateInput.value : "";
      if (!date || date < new Date().toISOString().slice(0, 10)) {
        toast("Selecione uma data futura.", "warning"); return;
      }
      var oid = reschedOrderId;
      if (!oid || inFlight[oid]) return;
      /* v446: template injeta sz-portal/v2 diretamente em data-rest-base. */
      var restBase = reschedConfirmBtn.dataset.restBase || "/wp-json/sz-portal/v2";
      var nonce    = reschedConfirmBtn.dataset.nonce || "";
      inFlight[oid] = true; setLoading(reschedConfirmBtn, true);
      reschedModal.classList.remove("szv2-open");
      postRest(restBase + "/motoboy/reschedule", nonce, { order_ids: [parseInt(oid, 10)], date: date })
        .then(function (r) {
          var m = r.message || (r.success ? "Data reagendada." : "Erro ao reagendar.");
          toast(m, r.success ? "success" : "danger");
          setLoading(reschedConfirmBtn, false); delete inFlight[oid];
        }).catch(function () {
          toast("Erro de conexão ao reagendar.", "danger");
          setLoading(reschedConfirmBtn, false); delete inFlight[oid];
        });
    });
  }

  // ── Action: Copiar rastreio (client-side, sem fetch) ─────────
  function handleCopyTracking(btn) {
    var tracking = btn.dataset.tracking || "";
    if (!tracking) return;
    if (navigator.clipboard) {
      navigator.clipboard.writeText(tracking).then(function () {
        toast("Rastreio copiado: " + tracking, "success");
      }).catch(function () { toast("Não foi possível copiar.", "warning"); });
    } else {
      // Fallback seleção manual
      toast("Rastreio: " + tracking, "info");
    }
  }

  // ── Event delegation ─────────────────────────────────────────
  root.addEventListener("click", function (e) {
    var btn;
    if ((btn = e.target.closest(".szv2-mb-action-btn, .szv2-exp-action-btn"))) {
      var action = btn.dataset.action;
      if (action === "approve")    { handleApprove(btn); }
      else if (action === "cancel")     { handleCancel(btn); }
      else if (action === "retry")      { handleRetry(btn); }
      else if (action === "reschedule") { handleReschedule(btn); }
    } else if ((btn = e.target.closest(".szv2-exp-copy-tracking"))) {
      handleCopyTracking(btn);
    }
  });

})();

/* ── Checkouts & Links — ações (Fase 6) ──────────────────────────
   Backends chamados:
     Copiar link:        navigator.clipboard — sem AJAX
     Excluir:           szaction=checkout_link_delete
     Toggle afiliado:   szaction=checkout_link_affiliate_toggle
     Salvar comissão:   szaction=checkout_link_commission_update
   ─────────────────────────────────────────────────────────────── */
(function () {
  "use strict";

  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  var lkInFlight = {};

  function getAjaxUrlLinks(el) {
    var sec = el.closest("[data-szv2-ajax-url]");
    if (sec && sec.dataset.szv2AjaxUrl) return sec.dataset.szv2AjaxUrl;
    if (window.SZ_CONFIG && window.SZ_CONFIG.ajaxUrl) return window.SZ_CONFIG.ajaxUrl;
    return "/wp-admin/admin-ajax.php";
  }

  function toastLinks(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    alert(msg);
  }

  function postAjaxLinks(ajaxUrl, data) {
    var body = Object.keys(data).map(function (k) {
      return encodeURIComponent(k) + "=" + encodeURIComponent(data[k]);
    }).join("&");
    return fetch(ajaxUrl, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body,
    }).then(function (r) { return r.json(); });
  }

  // ── Copiar link ──────────────────────────────────────────────
  root.addEventListener("click", function (e) {
    var btn = e.target.closest(".szv2-link-copy-btn");
    if (!btn) return;
    var url = btn.dataset.url || "";
    if (!url) return;
    if (navigator.clipboard) {
      navigator.clipboard.writeText(url).then(function () {
        toastLinks("Link copiado!", "success");
      }).catch(function () { toastLinks("Não foi possível copiar.", "warning"); });
    } else {
      toastLinks("Link: " + url, "info");
    }
  });

  // ── Excluir link ─────────────────────────────────────────────
  root.addEventListener("click", function (e) {
    var btn = e.target.closest(".szv2-link-del-btn[data-action='delete_link']");
    if (!btn) return;
    var lid   = btn.dataset.linkId;
    var name  = btn.dataset.linkName || "este link";
    var nonce = btn.dataset.nonce;
    if (lkInFlight[lid]) return;
    window.szV2Confirm({
      title: 'Excluir checkout',
      message: '"' + name + '" será excluído permanentemente.',
      btn: 'Excluir',
      danger: true
    }, function() {
      lkInFlight[lid] = true;
      btn.disabled = true;
      postAjaxLinks(getAjaxUrlLinks(btn), {
        action: "senderzz_portal", szaction: "checkout_link_delete",
        link_id: lid, _ajax_nonce: nonce,
      }).then(function (r) {
        var msg = (r.data && r.data.message) || r.message || (r.success ? "Link excluído." : "Erro ao excluir.");
        toastLinks(msg, r.success ? "success" : "danger");
        if (r.success) {
          var card = root.querySelector("#szv2-link-card-" + lid);
          if (card) { card.style.transition = 'opacity .2s'; card.style.opacity = '0'; setTimeout(function(){ card.remove(); }, 200); }
        } else { btn.disabled = false; }
        delete lkInFlight[lid];
      }).catch(function () {
        toastLinks("Erro de conexão.", "danger");
        btn.disabled = false; delete lkInFlight[lid];
      });
    });
  });

  // ── Toggle afiliado ──────────────────────────────────────────
  root.addEventListener("change", function (e) {
    var cb = e.target.closest(".szv2-link-aff-toggle");
    if (!cb) return;
    var lid   = cb.dataset.linkId;
    var nonce = cb.dataset.nonce;
    var enabled = cb.checked ? 1 : 0;
    if (lkInFlight["aff_" + lid]) { cb.checked = !cb.checked; return; }
    lkInFlight["aff_" + lid] = true;
    cb.disabled = true;
    postAjaxLinks(getAjaxUrlLinks(cb), {
      action: "senderzz_portal", szaction: "checkout_link_affiliate_toggle",
      link_id: lid, enabled: enabled, _ajax_nonce: nonce,
    }).then(function (r) {
      var msg = (r.data && r.data.message) || r.message || (r.success ? "Visibilidade atualizada." : "Erro.");
      toastLinks(msg, r.success ? "success" : "danger");
      if (!r.success) cb.checked = !cb.checked; // revert
      cb.disabled = false; delete lkInFlight["aff_" + lid];
    }).catch(function () {
      toastLinks("Erro de conexão.", "danger");
      cb.checked = !cb.checked; cb.disabled = false; delete lkInFlight["aff_" + lid];
    });
  });

  // ── Salvar comissão ──────────────────────────────────────────
  root.addEventListener("click", function (e) {
    var btn = e.target.closest(".szv2-link-comm-save");
    if (!btn) return;
    var lid   = btn.dataset.linkId;
    var nonce = btn.dataset.nonce;
    var input = root.querySelector("#szv2-link-comm-" + lid);
    if (!input) return;
    var pct = parseFloat(input.value);
    if (isNaN(pct) || pct < 0 || pct > 100) {
      toastLinks("Comissão deve ser entre 0 e 100.", "warning"); return;
    }
    if (lkInFlight["comm_" + lid]) return;
    lkInFlight["comm_" + lid] = true;
    btn.disabled = true; input.disabled = true;
    postAjaxLinks(getAjaxUrlLinks(btn), {
      action: "senderzz_portal", szaction: "checkout_link_commission_update",
      link_id: lid, commission_pct: pct.toFixed(2), _ajax_nonce: nonce,
    }).then(function (r) {
      var msg = (r.data && r.data.message) || r.message || (r.success ? "Comissão salva." : "Erro.");
      toastLinks(msg, r.success ? "success" : "danger");
      btn.disabled = false; input.disabled = false; delete lkInFlight["comm_" + lid];
    }).catch(function () {
      toastLinks("Erro de conexão.", "danger");
      btn.disabled = false; input.disabled = false; delete lkInFlight["comm_" + lid];
    });
  });

})();

/* ── Afiliados — ações (Fase 7) ──────────────────────────────────
   Backends:
     Aprovar:   szaction=affiliate_action  aff_act=approve
     Recusar:   szaction=affiliate_action  aff_act=reject
     Comissão:  szaction=affiliate_action  aff_act=update_commission
   Anti-duplo-clique: flag inFlight por affiliate_id.
   Sem reload: remove/atualiza row no DOM diretamente.
   ─────────────────────────────────────────────────────────────── */
(function () {
  "use strict";
  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  var affInFlight = {};

  function getAjaxUrlAff(el) {
    var sec = el.closest("[data-szv2-ajax-url]");
    if (sec && sec.dataset.szv2AjaxUrl) return sec.dataset.szv2AjaxUrl;
    if (window.SZ_CONFIG && window.SZ_CONFIG.ajaxUrl) return window.SZ_CONFIG.ajaxUrl;
    return "/wp-admin/admin-ajax.php";
  }

  function toastAff(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    alert(msg);
  }

  function postAjaxAff(ajaxUrl, data) {
    var body = Object.keys(data).map(function (k) {
      return encodeURIComponent(k) + "=" + encodeURIComponent(data[k]);
    }).join("&");
    return fetch(ajaxUrl, {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body,
    }).then(function (r) { return r.json(); });
  }

  // ── Aprovar / Recusar ────────────────────────────────────────
  root.addEventListener("click", function (e) {
    var btn = e.target.closest(".szv2-aff-action-btn");
    if (!btn) return;
    var act   = btn.dataset.affAction;
    var aid   = btn.dataset.affiliateId;
    var name  = btn.dataset.affName || "este afiliado";
    var nonce = btn.dataset.nonce;
    if (affInFlight[aid]) return;
    var confirmMsg = act === "approve"
      ? "Aprovar " + name + " como afiliado?"
      : "Recusar a solicitação de " + name + "?";
    var _doAffAction = function () {
      affInFlight[aid] = true;
      btn.disabled = true;
      var siblingBtns = btn.closest("tr") ? btn.closest("tr").querySelectorAll(".szv2-aff-action-btn") : [];
      siblingBtns.forEach(function (b) { b.disabled = true; });
      postAjaxAff(getAjaxUrlAff(btn), {
        action: "senderzz_portal", szaction: "affiliate_action",
        aff_act: act, affiliate_id: aid, _ajax_nonce: nonce,
      }).then(function (r) {
        var msg = (r.data && r.data.message) || r.message || (r.success ? "Ação realizada." : "Erro.");
        toastAff(msg, r.success ? "success" : "danger");
        if (r.success) {
          var row = root.querySelector("#szv2-aff-row-" + aid);
          if (row) row.remove();
        } else {
          siblingBtns.forEach(function (b) { b.disabled = false; });
        }
        delete affInFlight[aid];
      }).catch(function () {
        toastAff("Erro de conexão.", "danger");
        siblingBtns.forEach(function (b) { b.disabled = false; });
        delete affInFlight[aid];
      });
    };
    if (typeof window.szV2Confirm === 'function') {
      window.szV2Confirm({ title: 'Confirmar', message: confirmMsg, btn: act === 'approve' ? 'Aprovar' : 'Recusar', danger: act !== 'approve' }, _doAffAction);
    } else {
      if (window.confirm(confirmMsg)) _doAffAction();
    }
  });

  // ── Salvar comissão individual ───────────────────────────────
  root.addEventListener("click", function (e) {
    var btn = e.target.closest(".szv2-aff-comm-save");
    if (!btn) return;
    var aid   = btn.dataset.affiliateId;
    var nonce = btn.dataset.nonce;
    var input = root.querySelector(".szv2-aff-comm-input[data-affiliate-id='" + aid + "']");
    if (!input) return;
    var pct = parseFloat(input.value);
    if (isNaN(pct) || pct < 0 || pct > 100) {
      toastAff("Comissão deve ser entre 0 e 100.", "warning"); return;
    }
    if (affInFlight["comm_" + aid]) return;
    affInFlight["comm_" + aid] = true;
    btn.disabled = true; input.disabled = true;
    postAjaxAff(getAjaxUrlAff(btn), {
      action: "senderzz_portal", szaction: "affiliate_action",
      aff_act: "update_commission", affiliate_id: aid,
      commission_pct: pct.toFixed(2), _ajax_nonce: nonce,
    }).then(function (r) {
      var msg = (r.data && r.data.message) || r.message || (r.success ? "Comissão atualizada." : "Erro.");
      toastAff(msg, r.success ? "success" : "danger");
      if (r.success) {
        // Atualiza a célula de exibição sem reload
        var row = root.querySelector("#szv2-aff-row-" + aid);
        if (row) {
          var cells = row.querySelectorAll("td");
          // Terceira coluna é comissão%
          if (cells[2]) cells[2].textContent = pct.toFixed(2).replace(".", ",") + "%";
        }
      }
      btn.disabled = false; input.disabled = false;
      delete affInFlight["comm_" + aid];
    }).catch(function () {
      toastAff("Erro de conexão.", "danger");
      btn.disabled = false; input.disabled = false;
      delete affInFlight["comm_" + aid];
    });
  });

})();

/* ── Configurações — alterar e-mail e senha (Fase 9) ──────────── */
(function () {
  "use strict";
  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  function getAjax(el) {
    var sec = el.closest("[data-szv2-ajax-url]");
    if (sec && sec.dataset.szv2AjaxUrl) return sec.dataset.szv2AjaxUrl;
    if (window.SZ_CONFIG && window.SZ_CONFIG.ajaxUrl) return window.SZ_CONFIG.ajaxUrl;
    return "/wp-admin/admin-ajax.php";
  }
  function szToast(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    alert(msg);
  }
  function postForm(url, data) {
    var body = Object.keys(data).map(function(k){return encodeURIComponent(k)+"="+encodeURIComponent(data[k]);}).join("&");
    return fetch(url,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/x-www-form-urlencoded"},body:body}).then(function(r){return r.json();});
  }

  // ── Alterar e-mail ──────────────────────────────────────────
  var emailBtn = root.querySelector("#szv2-save-email");
  if (emailBtn) {
    emailBtn.addEventListener("click", function() {
      var input = root.querySelector("#szv2-new-email");
      var email = input ? input.value.trim() : "";
      if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        szToast("Informe um e-mail válido.", "warning"); return;
      }
      var _doEmailChange = function () {
        emailBtn.disabled = true;
        postForm(getAjax(emailBtn), {
          action:"senderzz_portal", szaction:"change_email",
          new_email: email, _ajax_nonce: emailBtn.dataset.nonce,
        }).then(function(r) {
          var msg = (r.data && r.data.message) || r.message || (r.success ? "E-mail alterado. Faça login novamente." : "Erro.");
          szToast(msg, r.success ? "success" : "danger");
          if (r.success) { setTimeout(function(){window.location.reload();},1800); }
          else emailBtn.disabled = false;
        }).catch(function(){szToast("Erro de conexão.","danger");emailBtn.disabled=false;});
      };
      if (typeof window.szV2Confirm === 'function') {
        window.szV2Confirm({ title: 'Alterar e-mail', message: 'Alterar e-mail para ' + email + '? Você precisará fazer login novamente.', btn: 'Alterar', danger: false }, _doEmailChange);
      } else {
        if (window.confirm("Alterar e-mail para " + email + "?\nVocê precisará fazer login novamente.")) _doEmailChange();
      }
    });
  }

  // ── Alterar senha ───────────────────────────────────────────
  var pwBtn = root.querySelector("#szv2-save-password");
  if (pwBtn) {
    pwBtn.addEventListener("click", function() {
      var cur  = root.querySelector("#szv2-current-pw");
      var nw   = root.querySelector("#szv2-new-pw");
      var nw2  = root.querySelector("#szv2-new-pw2");
      if (!cur||!nw||!nw2) return;
      if (!cur.value) { szToast("Informe a senha atual.", "warning"); return; }
      if (nw.value.length < 10) { szToast("Nova senha: mínimo 10 caracteres.", "warning"); return; }
      if (nw.value !== nw2.value) { szToast("As senhas não coincidem.", "warning"); return; }
      var _doPwChange = function () {
        pwBtn.disabled = true;
        postForm(getAjax(pwBtn), {
          action:"senderzz_portal", szaction:"change_password",
          current_password: cur.value, new_password: nw.value, _ajax_nonce: pwBtn.dataset.nonce,
        }).then(function(r) {
          var msg = (r.data && r.data.message) || r.message || (r.success ? "Senha alterada." : "Erro.");
          szToast(msg, r.success ? "success" : "danger");
          if (r.success) { cur.value=""; nw.value=""; nw2.value=""; setTimeout(function(){window.location.reload();},1800); }
          else pwBtn.disabled = false;
        }).catch(function(){szToast("Erro de conexão.","danger");pwBtn.disabled=false;});
      };
      if (typeof window.szV2Confirm === 'function') {
        window.szV2Confirm({ title: 'Alterar senha', message: 'Confirmar alteração de senha? Outras sessões serão encerradas.', btn: 'Alterar senha', danger: false }, _doPwChange);
      } else {
        if (window.confirm("Alterar senha?\nOutras sessões serão encerradas.")) _doPwChange();
      }
    });
  }
})();

/* ── Suporte — tickets (Fase 9) ───────────────────────────────── */
(function () {
  "use strict";
  var root = document.querySelector(".sz-dashboard-v2");
  if (!root) return;

  var sec = root.querySelector("#sec-support");
  if (!sec) return;

  var ajaxUrl  = sec.dataset.szv2AjaxUrl || (window.SZ_CONFIG && window.SZ_CONFIG.ajaxUrl) || "/wp-admin/admin-ajax.php";
  var nonce    = sec.dataset.szv2Nonce || "";
  var listEl   = root.querySelector("#szv2-support-list");
  var detailEl = root.querySelector("#szv2-support-detail");
  var currentTicketId = null;
  var ticketInFlight  = {};

  function szToast(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind||"info"); return; }
    alert(msg);
  }
  function postAjax(data) {
    var body = Object.keys(data).map(function(k){return encodeURIComponent(k)+"="+encodeURIComponent(data[k]);}).join("&");
    return fetch(ajaxUrl,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/x-www-form-urlencoded"},body:body}).then(function(r){return r.json();});
  }
  function esc(s) { return String(s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;"); }

  var STATUS_MAP = {
    aberto:"Aberto", em_analise:"Em análise", respondido:"Respondido", fechado:"Fechado"
  };

  function loadTickets() {
    if (!listEl) return;
    listEl.innerHTML = '<p class="szv2-empty-inline">Carregando…</p>';
    postAjax({action:"senderzz_portal",szaction:"tickets_list",_ajax_nonce:nonce})
      .then(function(r){
        var data = r.data || r;
        if (!r.success || !data.tickets) { listEl.innerHTML='<p class="szv2-empty-inline">Erro ao carregar chamados.</p>'; return; }
        if (!data.tickets.length) { listEl.innerHTML='<div class="szv2-card"><p class="szv2-card-sub">Nenhum chamado aberto. Use o botão "Novo chamado" para começar.</p></div>'; return; }
        var html = '<div class="szv2-card szv2-card-table"><div class="szv2-table-wrap szv2-table-flush"><table class="szv2-table"><thead><tr><th>#</th><th>Assunto</th><th>Categoria</th><th>Status</th><th>Atualizado</th><th></th></tr></thead><tbody>';
        data.tickets.forEach(function(t){
          var st = STATUS_MAP[t.status] || t.status;
          var badge = t.status==="fechado"?"neutral":(t.status==="respondido"?"success":"warning");
          html += '<tr><td class="szv2-num">#'+esc(t.id)+'</td>'
            +'<td>'+esc(t.assunto)+'</td>'
            +'<td>'+esc(t.categoria||'—')+'</td>'
            +'<td><span class="sz-badge szv2-badge-'+badge+'">'+esc(st)+'</span></td>'
            +'<td>'+esc((t.updated_at||'').slice(0,10))+'</td>'
            +'<td><button class="szv2-btn szv2-btn-sm szv2-btn-secondary szv2-ticket-open" data-tid="'+esc(t.id)+'">Abrir</button></td></tr>';
        });
        html += '</tbody></table></div></div>';
        listEl.innerHTML = html;
      }).catch(function(){listEl.innerHTML='<p class="szv2-empty-inline">Erro de conexão.</p>';});
  }

  function openTicket(tid) {
    if (!detailEl) return;
    currentTicketId = tid;
    listEl.hidden   = true;
    detailEl.hidden = false;
    var title = root.querySelector("#szv2-ticket-title");
    if (title) title.textContent = "Chamado #" + tid;
    var msgsEl = root.querySelector("#szv2-ticket-msgs");
    if (msgsEl) msgsEl.innerHTML = '<p class="szv2-empty-inline">Carregando…</p>';
    postAjax({action:"senderzz_portal",szaction:"ticket_msgs",ticket_id:tid,_ajax_nonce:nonce})
      .then(function(r){
        var data = r.data || r;
        if (!msgsEl) return;
        if (!r.success) { msgsEl.innerHTML='<p class="szv2-empty-inline">Erro ao carregar mensagens.</p>'; return; }
        var html = "";
        (data.msgs||[]).forEach(function(m){
          var isClient = m.autor_tipo === "cliente";
          html += '<div class="szv2-ticket-msg szv2-ticket-msg-'+(isClient?"client":"staff")+'">'
            +'<span class="szv2-ticket-msg-author">'+esc(m.autor_nome||m.autor_tipo)+'</span>'
            +'<p class="szv2-ticket-msg-text">'+esc(m.mensagem)+'</p>'
            +'<span class="szv2-ticket-msg-date">'+esc((m.created_at||'').slice(0,16))+'</span></div>';
        });
        if (!html) html = '<p class="szv2-empty-inline">Sem mensagens ainda.</p>';
        msgsEl.innerHTML = html;
        // Disable reply if closed
        var replyArea = root.querySelector("#szv2-ticket-reply-area");
        if (replyArea) replyArea.hidden = (data.ticket && data.ticket.status === "fechado");
        var closeBtn = root.querySelector("#szv2-ticket-close-btn");
        if (closeBtn) closeBtn.hidden = (data.ticket && data.ticket.status === "fechado");
      }).catch(function(){if(msgsEl)msgsEl.innerHTML='<p class="szv2-empty-inline">Erro de conexão.</p>';});
  }

  // Back button
  var backBtn = root.querySelector("#szv2-ticket-back");
  if (backBtn) backBtn.addEventListener("click", function(){
    if (detailEl) detailEl.hidden = true;
    if (listEl)   listEl.hidden   = false;
    currentTicketId = null;
    loadTickets();
  });

  // Send reply
  var sendBtn = root.querySelector("#szv2-ticket-send");
  if (sendBtn) sendBtn.addEventListener("click", function(){
    if (!currentTicketId) return;
    var ta = root.querySelector("#szv2-ticket-reply");
    var msg = ta ? ta.value.trim() : "";
    if (msg.length < 3) { szToast("Escreva uma mensagem antes de enviar.","warning"); return; }
    if (ticketInFlight[currentTicketId]) return;
    ticketInFlight[currentTicketId] = true; sendBtn.disabled = true;
    postAjax({action:"senderzz_portal",szaction:"ticket_send_msg",ticket_id:currentTicketId,mensagem:msg,_ajax_nonce:nonce})
      .then(function(r){
        var m = (r.data&&r.data.message)||r.message||(r.success?"Resposta enviada.":"Erro.");
        szToast(m, r.success?"success":"danger");
        if (r.success && ta) { ta.value=""; openTicket(currentTicketId); }
        sendBtn.disabled = false; delete ticketInFlight[currentTicketId];
      }).catch(function(){szToast("Erro de conexão.","danger");sendBtn.disabled=false;delete ticketInFlight[currentTicketId];});
  });

  // Close ticket
  var closeBtn = root.querySelector("#szv2-ticket-close-btn");
  if (closeBtn) closeBtn.addEventListener("click", function(){
    if (!currentTicketId) return;
    var _doCloseTicket = function () {
      closeBtn.disabled = true;
      postAjax({action:"senderzz_portal",szaction:"ticket_close",ticket_id:currentTicketId,_ajax_nonce:nonce})
        .then(function(r){
          var m = (r.data&&r.data.message)||r.message||(r.success?"Chamado fechado.":"Erro.");
          szToast(m, r.success?"success":"danger");
          if (r.success) { openTicket(currentTicketId); }
          else closeBtn.disabled=false;
        }).catch(function(){szToast("Erro de conexão.","danger");closeBtn.disabled=false;});
    };
    if (typeof window.szV2Confirm === 'function') {
      window.szV2Confirm({ title: 'Fechar chamado', message: 'Fechar o chamado #' + currentTicketId + '?', btn: 'Fechar chamado', danger: false }, _doCloseTicket);
    } else {
      if (window.confirm("Fechar o chamado #"+currentTicketId+"?")) _doCloseTicket();
    }
  });

  // New ticket modal
  var newBtn = root.querySelector("#szv2-support-new-btn");
  if (newBtn) newBtn.addEventListener("click", function(){
    var modal = root.querySelector("#szv2-support-modal");
    if (modal) modal.classList.add("szv2-open");
  });

  var createBtn = root.querySelector("#szv2-ticket-create-confirm");
  if (createBtn) createBtn.addEventListener("click", function(){
    var assunto  = (root.querySelector("#szv2-ticket-assunto")||{}).value||"";
    var cat      = (root.querySelector("#szv2-ticket-cat")||{}).value||"outro";
    var mensagem = (root.querySelector("#szv2-ticket-msg")||{}).value||"";
    if (assunto.length < 5) { szToast("Assunto muito curto (mín. 5 caracteres).","warning"); return; }
    if (mensagem.length < 10){ szToast("Mensagem muito curta (mín. 10 caracteres).","warning"); return; }
    createBtn.disabled = true;
    postAjax({action:"senderzz_portal",szaction:"ticket_create",assunto:assunto,categoria:cat,mensagem:mensagem,_ajax_nonce:nonce})
      .then(function(r){
        var data = r.data || r;
        var m = (r.data&&r.data.message)||r.message||(r.success?"Chamado aberto!":"Erro.");
        szToast(m, r.success?"success":"danger");
        if (r.success) {
          var modal = root.querySelector("#szv2-support-modal");
          if (modal) modal.classList.remove("szv2-open");
          var assuntoEl = root.querySelector("#szv2-ticket-assunto");
          var msgEl     = root.querySelector("#szv2-ticket-msg");
          if(assuntoEl) assuntoEl.value=""; if(msgEl) msgEl.value="";
          if (data.ticket_id) openTicket(data.ticket_id);
          else loadTickets();
        }
        createBtn.disabled = false;
      }).catch(function(){szToast("Erro de conexão.","danger");createBtn.disabled=false;});
  });

  // Open ticket from list (delegated)
  root.addEventListener("click", function(e){
    var btn = e.target.closest(".szv2-ticket-open");
    if (!btn) return;
    openTicket(parseInt(btn.dataset.tid,10));
  });

  // Load tickets when section becomes visible
  // Use MutationObserver only on the section visibility, not a global interval
  var observer = new MutationObserver(function(){
    if (!sec.hidden && sec.offsetParent !== null) {
      observer.disconnect();
      loadTickets();
    }
  });
  // Also load if already visible
  if (!sec.hidden && sec.offsetParent !== null) { loadTickets(); }
  else { observer.observe(sec, {attributes:true,attributeFilter:["hidden","style","class"]}); }

  // Reload on nav to sec-support
  document.addEventListener("szv2:nav", function(ev){
    if (ev.detail && ev.detail.section === "support") loadTickets();
  });

})();

/* ─────────────────────────────────────────────────────────────────────────────
 * Saque COD V2 (v442)
 * Rotas: GET  sz-portal/v2/cod/accounts
 *        POST sz-portal/v2/cod/withdraw
 * Nonce: X-WP-Nonce (wp_rest) + X-Senderzz-Financial-Nonce (sessão portal)
 * Ambos passados via data attributes no botão — cookie é HttpOnly, nunca lido no JS.
 * Gating: botão renderizado pelo PHP somente com flag YES + saldo > 0
 * ─────────────────────────────────────────────────────────────────────────────*/
(function () {
  "use strict";

  var walletSec = document.getElementById("sec-wallet");
  if (!walletSec) return;

  var openBtn = walletSec.querySelector(".szv2-wd-open-btn");
  if (!openBtn) return; /* flag desligada ou saldo zero — encerra */

  var modal       = document.getElementById("szv2-wd-modal");
  if (!modal) return;

  var stepForm    = document.getElementById("szv2-wd-step-form");
  var stepConfirm = document.getElementById("szv2-wd-step-confirm");
  var amountInput = document.getElementById("szv2-wd-amount");
  var accountSel  = document.getElementById("szv2-wd-account-select");
  var noAcctHint  = document.getElementById("szv2-wd-no-acct");
  var amountHint  = document.getElementById("szv2-wd-amount-hint");
  var nextBtn     = document.getElementById("szv2-wd-next-btn");
  var backBtn     = document.getElementById("szv2-wd-back-btn");
  var cancelBtn   = document.getElementById("szv2-wd-cancel-btn");
  var submitBtn   = document.getElementById("szv2-wd-submit-btn");
  var cfAmount    = document.getElementById("szv2-wd-cf-amount");
  var cfFee       = document.getElementById("szv2-wd-cf-fee");
  var cfNet       = document.getElementById("szv2-wd-cf-net");
  var cfAccount   = document.getElementById("szv2-wd-cf-account");

  /* Dados injetados pelo PHP via data attributes */
  var restBase       = openBtn.dataset.restBase     || "/wp-json/sz-portal/v2";
  var nonce          = openBtn.dataset.nonce         || "";
  var financialNonce = openBtn.dataset.financialNonce || "";
  var available = parseFloat(openBtn.dataset.available || "0");
  var feeFlat   = parseFloat(openBtn.dataset.fee       || "0");

  var inFlight       = false;
  var accountsCache  = null;

  /* ── Utilitários ─────────────────────────────────────────────────────────── */
  function money(v) {
    return "R$ " + parseFloat(v || 0).toFixed(2)
      .replace(".", ",")
      .replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }

  function parseMoneyInput(s) {
    s = String(s || "").trim();
    /* "1.234,56" → "1234.56"  |  "1234,56" → "1234.56"  |  "1234.56" → keeps */
    if (s.indexOf(",") !== -1 && s.indexOf(".") !== -1) {
      s = s.replace(/\./g, "").replace(",", ".");
    } else {
      s = s.replace(",", ".");
    }
    return parseFloat(s) || 0;
  }

  function szToastWd(msg, kind) {
    if (window.szV2Toast) { window.szV2Toast(msg, kind || "info"); return; }
    alert(msg);
  }

  /* ── REST helpers (sempre V2, sempre com ambos os nonces) ───────────────── */
  function restGet(url) {
    return fetch(url, {
      credentials: "same-origin",
      headers: {
        "X-WP-Nonce": nonce,
        "X-Senderzz-Financial-Nonce": financialNonce,
      },
    }).then(function (r) { return r.json(); });
  }

  function restPost(url, body) {
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-WP-Nonce": nonce,
        "X-Senderzz-Financial-Nonce": financialNonce,
      },
      body: JSON.stringify(body),
    }).then(function (r) { return r.json(); });
  }

  /* ── Contas PIX ──────────────────────────────────────────────────────────── */
  function loadAccounts(cb) {
    if (accountsCache !== null) { cb(accountsCache); return; }
    /* Chama SOMENTE sz-portal/v2 */
    restGet(restBase + "/cod/accounts")
      .then(function (d) {
        accountsCache = (d.success && Array.isArray(d.accounts)) ? d.accounts : [];
        cb(accountsCache);
      })
      .catch(function () { accountsCache = []; cb([]); });
  }

  function populateAccountSelect(accounts) {
    accountSel.innerHTML = "";
    if (!accounts || !accounts.length) {
      noAcctHint.hidden = false;
      if (nextBtn) nextBtn.disabled = true;
      return;
    }
    noAcctHint.hidden = true;
    if (nextBtn) nextBtn.disabled = false;
    accounts.forEach(function (a) {
      var opt = document.createElement("option");
      opt.value = a.id;
      /* v445: usa o label mascarado fornecido pelo backend.
         O front nunca recebe nem exibe chave PIX/conta completa. */
      var label = a.label;
      if (!label) {
        label = (a.holder_name || "Titular") +
          (a.pix_type ? " — PIX " + String(a.pix_type).toUpperCase() : "") +
          (a.pix_key_masked ? ": " + a.pix_key_masked : "");
        if (a.bank_name) label += " (" + a.bank_name + ")";
      }
      opt.textContent = label;
      accountSel.appendChild(opt);
    });
  }

  /* ── Ciclo do modal ──────────────────────────────────────────────────────── */
  function openModal() {
    resetModal();
    modal.classList.add("szv2-open");
    loadAccounts(function (accounts) {
      populateAccountSelect(accounts);
      if (amountInput) amountInput.focus();
    });
  }

  function closeModal() {
    modal.classList.remove("szv2-open");
    resetModal();
  }

  function resetModal() {
    if (amountInput) { amountInput.value = ""; amountInput.disabled = false; }
    if (accountSel)  { accountSel.disabled = false; }
    if (amountHint)  { amountHint.textContent = ""; amountHint.className = "szv2-field-hint"; }
    showStep("form");
    setSubmitting(false);
  }

  function showStep(step) {
    var isConfirm = (step === "confirm");
    if (stepForm)    stepForm.hidden    = isConfirm;
    if (stepConfirm) stepConfirm.hidden = !isConfirm;
    if (nextBtn)     nextBtn.hidden     = isConfirm;
    if (cancelBtn)   cancelBtn.hidden   = isConfirm;
    if (backBtn)     backBtn.hidden     = !isConfirm;
    if (submitBtn)   submitBtn.hidden   = !isConfirm;
  }

  function setSubmitting(on) {
    inFlight = on;
    if (submitBtn) { submitBtn.disabled = on; submitBtn.textContent = on ? "Aguarde…" : "Confirmar saque"; }
    if (backBtn)   { backBtn.disabled   = on; }
  }

  /* ── Etapa 1 → 2: validar e ir para confirmação ──────────────────────────── */
  function handleNext() {
    var amount = parseMoneyInput(amountInput ? amountInput.value : "");

    if (!amount || amount <= 0) {
      setAmountHint("Informe um valor.", true); if (amountInput) amountInput.focus(); return;
    }
    if (amount < 10) {
      setAmountHint("Valor mínimo: R$ 10,00.", true); if (amountInput) amountInput.focus(); return;
    }
    if (amount > available) {
      setAmountHint("Valor superior ao saldo disponível (" + money(available) + ").", true);
      if (amountInput) amountInput.focus(); return;
    }
    if (!accountSel || !accountSel.value) {
      szToastWd("Selecione uma conta para recebimento.", "warning"); return;
    }

    setAmountHint("", false);

    var net     = Math.max(0, amount - feeFlat);
    var selText = accountSel.options[accountSel.selectedIndex]
      ? accountSel.options[accountSel.selectedIndex].textContent : "—";

    if (cfAmount)  cfAmount.textContent  = money(amount);
    if (cfFee)     cfFee.textContent     = money(feeFlat);
    if (cfNet)     cfNet.textContent     = money(net);
    if (cfAccount) cfAccount.textContent = selText;

    showStep("confirm");
  }

  function setAmountHint(msg, isErr) {
    if (!amountHint) return;
    amountHint.textContent = msg;
    amountHint.className = "szv2-field-hint" + (isErr ? " szv2-wd-hint-err" : "");
  }

  /* ── Submit ──────────────────────────────────────────────────────────────── */
  function handleSubmit() {
    if (inFlight) return;
    setSubmitting(true);

    var amount    = parseMoneyInput(amountInput ? amountInput.value : "");
    var accountId = accountSel ? parseInt(accountSel.value, 10) : 0;

    if (!amount || !accountId) {
      setSubmitting(false);
      szToastWd("Dados inválidos. Feche e tente novamente.", "danger");
      return;
    }

    /* Chama SOMENTE sz-portal/v2 — nunca v1 */
    restPost(restBase + "/cod/withdraw", {
      amount:     amount,
      account_id: accountId,
      /* tipo NÃO é enviado — backend V2 sempre força 'normal' */
    })
      .then(function (d) {
        var ok  = !!(d.success || d.ok);
        var msg = d.message || (ok ? "Saque solicitado." : "Erro ao solicitar saque.");

        /* Erros mapeados */
        if (!ok) {
          var status = d.data && d.data.status ? d.data.status : 0;
          if (status === 401) { msg = "Sessão expirada. Recarregue a página."; }
          if (status === 403) { msg = d.message || "Operação não permitida."; }
          if (status === 429) { msg = "Muitas tentativas. Aguarde antes de tentar novamente."; }
        }

        szToastWd(msg, ok ? "success" : "danger");

        if (ok) {
          accountsCache = null;
          closeModal();
          /* Recarregar a seção de carteira para refletir novo saldo e histórico */
          setTimeout(function () {
            if (window.szV2NavTo) {
              window.szV2NavTo("wallet");
            } else {
              window.location.reload();
            }
          }, 1200);
        } else {
          setSubmitting(false);
          showStep("form");
        }
      })
      .catch(function () {
        szToastWd("Erro de conexão. Tente novamente.", "danger");
        setSubmitting(false);
        showStep("form");
      });
  }

  /* ── Listeners ────────────────────────────────────────────────────────────── */
  openBtn.addEventListener("click", openModal);
  if (nextBtn)   nextBtn.addEventListener("click", handleNext);
  if (submitBtn) submitBtn.addEventListener("click", handleSubmit);
  if (backBtn)   backBtn.addEventListener("click", function () { showStep("form"); });
  if (cancelBtn) cancelBtn.addEventListener("click", closeModal);

  modal.addEventListener("click", function (e) { if (e.target === modal) closeModal(); });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && modal.classList.contains("szv2-open")) closeModal();
  });

  /* Validação inline do valor */
  if (amountInput) {
    amountInput.addEventListener("input", function () {
      var v = parseMoneyInput(this.value);
      if (v > available)    setAmountHint("Máximo: " + money(available), true);
      else if (v > 0 && v < 10) setAmountHint("Mínimo: R$ 10,00", true);
      else                  setAmountHint("", false);
    });
  }

})();
/* ── /Saque COD V2 ────────────────────────────────────────────────────────── */

/* ── Notification bell ──────────────────────────────────────────────── */
(function () {
    var btn   = document.getElementById('szv2-notif-btn');
    var panel = document.getElementById('szv2-notif-panel');
    var badge = document.getElementById('szv2-notif-badge');
    var list  = document.getElementById('szv2-notif-list');
    if (!btn || !panel) return;

    var notifs = [];
    var loaded = false;

    window.szV2ToggleNotif = function () {
        var open = panel.style.display !== 'none';
        panel.style.display = open ? 'none' : '';
        btn.setAttribute('aria-expanded', String(!open));
        if (!open && !loaded) { loadNotifs(); }
    };

    window.szV2MarkAllRead = function () {
        notifs.forEach(function (n) { n.unread = false; });
        renderNotifs();
        if (badge) badge.style.display = 'none';
    };

    function loadNotifs() {
        loaded = true;
        var ajax = (document.querySelector('[data-ajax]') || {}).getAttribute
            ? document.querySelector('[data-ajax]').getAttribute('data-ajax')
            : '/wp-admin/admin-ajax.php';
        var nonce = (document.querySelector('[data-nonce]') || {}).getAttribute
            ? document.querySelector('[data-nonce]').getAttribute('data-nonce') : '';
        var fd = new FormData();
        fd.append('action', 'senderzz_portal');
        fd.append('szaction', 'get_notifications');
        fd.append('_ajax_nonce', nonce);
        fetch(ajax, { method: 'POST', body: fd, credentials: 'same-origin' })
            .then(function (r) { return r.json(); })
            .then(function (d) {
                if (d && d.success && Array.isArray(d.data)) {
                    notifs = d.data;
                    renderNotifs();
                    var unread = notifs.filter(function (n) { return n.unread; }).length;
                    if (badge && unread > 0) {
                        badge.textContent = String(unread);
                        badge.style.display = '';
                    }
                }
            })
            .catch(function () {});
    }

    function renderNotifs() {
        if (!list) return;
        if (!notifs.length) {
            list.innerHTML = '<div class="szv2-notif-empty">Nenhuma notificação</div>';
            return;
        }
        list.innerHTML = notifs.map(function (n) {
            return '<div class="szv2-notif-item' + (n.unread ? ' szv2-notif-unread' : '') + '">'
                + '<div class="szv2-notif-item-title">' + (n.title || '') + '</div>'
                + '<div class="szv2-notif-item-meta">' + (n.message || '') + ' · ' + (n.date || '') + '</div>'
                + '</div>';
        }).join('');
    }

    // Close on click outside
    document.addEventListener('click', function (e) {
        if (panel.style.display !== 'none' && !panel.contains(e.target) && !btn.contains(e.target)) {
            panel.style.display = 'none';
            btn.setAttribute('aria-expanded', 'false');
        }
    });
}());

/* ── Custom confirm dialog (replaces window.confirm) ───────────────── */
window.szV2Confirm = function (opts, onConfirm) {
    var title   = opts.title   || 'Confirmar ação';
    var message = opts.message || 'Tem certeza?';
    var btnText = opts.btn     || 'Confirmar';
    var danger  = opts.danger  !== false;

    var titleId = 'szv2-cfm-title-' + (Date.now() % 1e6);
    var overlay = document.createElement('div');
    overlay.className = 'szv2-confirm-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-labelledby', titleId);
    // S13: title/message/btnText via textContent — evita XSS se caller passar string com HTML
    overlay.innerHTML =
        '<div class="szv2-confirm-box">'
        + '<div class="szv2-confirm-icon">'
        +   '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 0 20A10 10 0 0 0 12 2zm0 11a1 1 0 0 1-1-1V8a1 1 0 0 1 2 0v4a1 1 0 0 1-1 1zm0 4a1.2 1.2 0 1 1 0-2.4A1.2 1.2 0 0 1 12 17z"/></svg>'
        + '</div>'
        + '<div class="szv2-confirm-title" id="' + titleId + '"></div>'
        + '<div class="szv2-confirm-msg"></div>'
        + '<div class="szv2-confirm-actions">'
        +   '<button type="button" class="szv2-btn szv2-btn-secondary szv2-cancel-btn">Cancelar</button>'
        +   '<button type="button" class="szv2-btn ' + (danger ? 'szv2-btn-danger' : 'szv2-btn-brand') + ' szv2-ok-btn"></button>'
        + '</div>'
        + '</div>';
    overlay.querySelector('.szv2-confirm-title').textContent = title;
    overlay.querySelector('.szv2-confirm-msg').textContent   = message;
    overlay.querySelector('.szv2-ok-btn').textContent        = btnText;

    document.body.appendChild(overlay);

    var _prevFocus = document.activeElement;
    function close() {
        if (overlay.parentNode) overlay.parentNode.removeChild(overlay);
        document.removeEventListener('keydown', _keyHandler);
        if (_prevFocus && _prevFocus.focus) _prevFocus.focus();
    }

    // D4: focus trap — Tab cycles entre Cancelar e Confirmar
    function _keyHandler(e) {
        if (e.key === 'Escape') { close(); return; }
        if (e.key !== 'Tab') return;
        var focusable = Array.from(overlay.querySelectorAll('button:not([disabled])'));
        if (!focusable.length) return;
        var idx = focusable.indexOf(document.activeElement);
        e.preventDefault();
        var next = e.shiftKey ? (idx <= 0 ? focusable.length - 1 : idx - 1) : (idx >= focusable.length - 1 ? 0 : idx + 1);
        focusable[next].focus();
    }
    document.addEventListener('keydown', _keyHandler);

    overlay.querySelector('.szv2-cancel-btn').addEventListener('click', close);
    overlay.querySelector('.szv2-ok-btn').addEventListener('click', function () {
        close();
        if (typeof onConfirm === 'function') onConfirm();
    });
    overlay.addEventListener('click', function (e) {
        if (e.target === overlay) close();
    });

    setTimeout(function () { overlay.querySelector('.szv2-ok-btn').focus(); }, 50);
};
