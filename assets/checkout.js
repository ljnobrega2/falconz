/* global jQuery */
(function ($) {
    'use strict';

    var observer = null;
    var isApplying = false;
    var applyTimer = null;
    var releaseTimer = null;
    var lastSignature = '';
    var selectedBySignature = {};
    var cepUpdateActive = false;
    var lastCepKey = '';
    var FAST_HIDE_MS = 260;
    var szLastCepDigits = '';
    var szFullNamePopupLastValue = '';
    var szFullNamePopupLastShownAt = 0;
    var szPhonePopupLastShownAt = 0;


    // ─── MOTOBOY: detecta se é link motoboy ──────────────────────────────────
    var SZ_IS_MOTOBOY = (function() {
        var params = new URLSearchParams(window.location.search);
        return params.get('frete') === 'motoboy' ||
               params.get('szm') === '1' ||
               params.get('m') === '1' ||
               (window.sz_checkout_vars && window.sz_checkout_vars.is_motoboy === '1') ||
               (window.sz_motoboy_checkout && window.sz_motoboy_checkout === '1');
    })();

    var SZ_API = (window.sz_checkout_vars && window.sz_checkout_vars.rest_url)
        ? window.sz_checkout_vars.rest_url
        : (window.location.origin + '/wp-json/sz-motoboy/v1');

    // Token do link motoboy (parâmetro sz na URL)
    var SZ_TOKEN = new URLSearchParams(window.location.search).get('sz') || '';

    // Motoboy e Expedição são fluxos isolados.
    // Link motoboy nunca carrega/usa URL de expedição como fallback.
    var SZ_URL_EXPEDICAO = null;

    // ─── POPUP HELPERS ────────────────────────────────────────────────────────
    function szInjectPopupStyles() {
        if (document.getElementById('sz-popup-style')) return;
        var s = document.createElement('style');
        s.id = 'sz-popup-style';
        s.textContent = [
            '#sz-popup-overlay{position:fixed;inset:0;background:rgba(17,24,39,.62);z-index:99999;display:flex;align-items:center;justify-content:center;padding:16px}',
            '#sz-popup{background:#fff;border-radius:18px;padding:30px 26px;max-width:430px;width:100%;text-align:center;box-shadow:0 22px 70px rgba(0,0,0,.24);animation:szPopIn .18s ease;font-family:var(--sz-font);color:#111827}',
            '@keyframes szPopIn{from{transform:scale(.92);opacity:0}to{transform:scale(1);opacity:1}}',
            '#sz-popup .sz-pop-icon{font-size:var(--sz-text-hero);line-height:1;margin-bottom:14px}',
            '#sz-popup h3{font-family:var(--sz-font);font-size:var(--sz-text-xl);line-height:1.25;font-weight:700;color:#111827;margin:0 0 10px;letter-spacing:-.015em}',
            '#sz-popup p{font-family:var(--sz-font);font-size:var(--sz-text-lg);color:#4b5563;margin:0 0 22px;line-height:1.55;font-weight:500}',
            '#sz-popup .sz-pop-address{display:block;margin-top:12px;padding:12px 14px;border:1px solid #fed7aa;border-radius:12px;background:#fff7ed;color:#7c2d12;text-align:left;font-size:var(--sz-text-md);line-height:1.45;font-weight:700}',
            '#sz-popup .sz-pop-address span{display:block;color:#374151;font-weight:700}',
            '#sz-popup .sz-pop-address strong{color:#E8650A;font-weight:700}',
            '#sz-popup .sz-pop-btn{background:#E8650A;color:#fff;border:none;padding:13px 28px;border-radius:11px;font-family:var(--sz-font);font-size:var(--sz-text-lg);font-weight:700;cursor:pointer;width:100%;box-shadow:0 8px 20px rgba(232,101,10,.18)}',
            '#sz-popup .sz-pop-btn:hover{background:#c8540a}',
            '#sz-popup .sz-pop-btn-sec{background:#f3f4f6;color:#374151;border:none;padding:10px 28px;border-radius:10px;font-family:var(--sz-font);font-size:var(--sz-text-base);font-weight:700;cursor:pointer;width:100%;margin-top:8px}',
            'body.senderzz-motoboy-cep-blocked .wfacp-payment-tab-list:not(.wfacp-tab1),body.senderzz-motoboy-cep-blocked [data-step="2"],body.senderzz-motoboy-cep-blocked [data-current-step="2"],body.senderzz-motoboy-cep-blocked .wfacp_breadcrumb_step_2{pointer-events:none!important;opacity:.48!important;cursor:not-allowed!important}',
            'body.senderzz-cep-invalid-blocking .wfacp-payment-tab-list:not(.wfacp-tab1),body.senderzz-cep-invalid-blocking [data-step="2"],body.senderzz-cep-invalid-blocking [data-current-step="2"],body.senderzz-cep-invalid-blocking .wfacp_breadcrumb_step_2{pointer-events:none!important;opacity:.48!important;cursor:not-allowed!important}',
        ].join('');
        document.head.appendChild(s);
    }

    function szShowPopup(icon, title, body, btnLabel, btnAction, secBtn) {
        szInjectPopupStyles();
        szClosePopup();
        var overlay = document.createElement('div');
        overlay.id = 'sz-popup-overlay';
        overlay.innerHTML = '<div id="sz-popup">'
            + '<div class="sz-pop-icon">' + icon + '</div>'
            + '<h3>' + title + '</h3>'
            + '<p>' + body + '</p>'
            + '<button class="sz-pop-btn" id="sz-pop-btn-main">' + btnLabel + '</button>'
            + (secBtn ? '<button class="sz-pop-btn-sec" id="sz-pop-btn-sec">' + secBtn.label + '</button>' : '')
            + '</div>';
        document.body.appendChild(overlay);
        document.getElementById('sz-pop-btn-main').addEventListener('click', function() {
            szClosePopup(); if (btnAction) btnAction();
        });
        if (secBtn) {
            document.getElementById('sz-pop-btn-sec').addEventListener('click', function() {
                szClosePopup(); if (secBtn.action) secBtn.action();
            });
        }
        overlay.addEventListener('click', function(e) { if (e.target === overlay) szClosePopup(); });
    }

    function szClosePopup() {
        var el = document.getElementById('sz-popup-overlay');
        if (el) el.remove();
    }

    function szGetNameField() {
        return $('#billing_first_name, input[name="billing_first_name"], #billing_name, input[name="billing_name"]').filter(function() {
            return $(this).length && !$(this).closest('.hidden, [aria-hidden="true"]').length;
        }).first();
    }

    function szGetPhoneFields() {
        return $('#billing_phone, input[name="billing_phone"], #billing_cellphone, input[name="billing_cellphone"]');
    }

    function szShowFullNamePopup(force) {
        var $f = szGetNameField();
        if (!$f.length) return false;
        var val = String($f.val() || '').trim().replace(/\s+/g, ' ');
        if (!val || szNameIsCompound(val)) return false;
        var now = Date.now();
        if (!force && szFullNamePopupLastValue === val && (now - szFullNamePopupLastShownAt) < 2200) return true;
        szFullNamePopupLastValue = val;
        szFullNamePopupLastShownAt = now;
        szShowPopup(
            '👤',
            'Informe o nome completo',
            'Digite <strong>nome e sobrenome</strong> para continuar com o pedido.',
            'Corrigir nome',
            function() { $f.focus().select(); }
        );
        return true;
    }


    function szShowPhonePopup() {
        var $f = szGetPhoneFields().filter(function(){
            return !$(this).closest('.hidden, [aria-hidden="true"]').length;
        }).first();
        var now = Date.now();
        if ((now - szPhonePopupLastShownAt) < 2200) return true;
        szPhonePopupLastShownAt = now;
        szShowPopup(
            '📱',
            'Telefone inválido',
            'Informe um <strong>celular ou telefone válido</strong> com DDD para continuar.',
            'Corrigir telefone',
            function() {
                if ($f.length) $f.focus().select();
            }
        );
        return true;
    }

    function szPhoneFieldIsInvalid() {
        // Retorna true quando o campo tem dígitos mas insuficientes (< 10).
        // NÃO cobre campo completamente vazio — para isso existe szPhoneShouldShowPopup().
        var found = false;
        szGetPhoneFields().filter(function(){
            return !$(this).closest('.hidden, [aria-hidden="true"]').length;
        }).each(function(){
            var digits = String($(this).val() || '').replace(/\D/g,'');
            if (digits.length > 0 && digits.length < 10) { found = true; return false; }
        });
        return found;
    }

    function szPhoneShouldShowPopup() {
        // Mostra popup de telefone quando:
        // (a) campo tem dígitos mas são insuficientes, OU
        // (b) campo está completamente vazio/não preenchido.
        // Usado apenas ao tentar avançar — não em preenchimento contínuo.
        var $fields = szGetPhoneFields().filter(function(){
            return !$(this).closest('.hidden, [aria-hidden="true"]').length;
        });
        if (!$fields.length) return false;
        var hasInvalid = false;
        $fields.each(function(){
            var digits = String($(this).val() || '').replace(/\D/g,'');
            if (digits.length < 10) { hasInvalid = true; return false; }
        });
        return hasInvalid;
    }

    function szNormalizePhoneValidationUi() {
        // Evita mensagens nativas/tema em inglês sobre telefone. A validação Senderzz controla o botão e os avisos.
        $('form.checkout, form.woocommerce-checkout, form.wfacp_main_form, form').filter(function(){
            return $(this).find('#billing_phone, input[name="billing_phone"], #billing_cellphone, input[name="billing_cellphone"]').length > 0;
        }).attr('novalidate', 'novalidate');

        szGetPhoneFields().each(function() {
            try { this.setCustomValidity(''); } catch(e) {}
            $(this)
                .attr('inputmode', 'tel')
                .attr('autocomplete', 'tel')
                .removeAttr('pattern')
                .removeAttr('title')
                .removeAttr('minlength')
                .removeAttr('maxlength')
                .attr('aria-invalid', 'false')
                .removeClass('woocommerce-invalid input-text-error wfacp_error wfacp-invalid');
            $(this).closest('.form-row, .wfacp-form-control-wrapper, p')
                .removeClass('woocommerce-invalid woocommerce-invalid-required-field input-text-error wfacp_error wfacp-invalid');
        });
    }

    function szHideEnglishPhoneNotices() {
        var phoneEnglishRe = /(phone|telephone|tel[eé]fono|billing phone|valid phone|phone number)/i;
        $('.woocommerce-error li, .woocommerce-error, .woocommerce-NoticeGroup li, .woocommerce-NoticeGroup, .wfacp_error, .wfacp-error, .wfacp_notices li').each(function() {
            var txt = String($(this).text() || '').trim();
            if (phoneEnglishRe.test(txt) && /[a-z]/i.test(txt) && !/telefone|celular/i.test(txt)) {
                $(this).remove();
            }
        });
    }

    function szClearManualAddressForCepChange(oldCep, newCep) {
        oldCep = String(oldCep || '').replace(/\D/g, '');
        newCep = String(newCep || '').replace(/\D/g, '');
        if (!oldCep || oldCep === newCep) return;
        // Ao mudar o CEP depois de preencher um endereço válido, número/complemento pertencem ao CEP anterior.
        // Eles precisam ser limpos imediatamente para evitar pedido com CEP novo e número antigo.
        $(CEP_NUMBER_SELECTORS.concat(CEP_COMPLEMENT_SELECTORS).join(',')).each(function() {
            $(this).val('').trigger('change');
        });
        neutralizeAddressValidation();
        neutralizePostcodeValidationUnlessRealError();
        szSetNextButtonDisabled(true);
    }

    // ─── CEP NOT FOUND POPUP ─────────────────────────────────────────────────
    var szCepNotFoundPopupCep = '';
    var szCepNotFoundLastShownAt = 0;

    var SZ_CEP_FIELDS_ALL = [
        '#billing_postcode','#shipping_postcode',
        'input[name="billing_postcode"]','input[name="shipping_postcode"]',
        '#billing_address_1','#shipping_address_1',
        '#billing_city','#shipping_city',
        '#billing_neighborhood','#shipping_neighborhood',
        'input[name="billing_neighborhood"]','input[name="shipping_neighborhood"]',
        '#billing_address_2','#shipping_address_2',
    ];
    var SZ_CEP_SELECTS_ALL = ['#billing_state','#shipping_state'];

    function szNormalizeCep(cep) {
        return String(cep || '').replace(/\D/g, '');
    }

    function szClearCheckoutExceptNamePhone() {
        var selectors = [];
        try {
            selectors = CEP_POSTCODE_SELECTORS
                .concat(CEP_ALL_ADDRESS_INPUTS || [])
                .concat(CEP_SELECT_SELECTORS || [])
                .concat(['#billing_country','#shipping_country']);
        } catch(e) {
            selectors = SZ_CEP_FIELDS_ALL.concat(SZ_CEP_SELECTS_ALL || []);
        }
        var $targets = $(selectors.join(','));
        $targets.each(function(){
            var $el = $(this);
            if (!$el.length) return;
            $el.val('')
               .prop('readonly', false)
               .removeAttr('aria-invalid')
               .removeClass('woocommerce-invalid woocommerce-invalid-required-field input-text-error wfacp_error wfacp-invalid sz-mb-cep-invalid sz-mb-cep-ok');
            $el.closest('.form-row, .wfacp-form-control-wrapper, p')
               .removeClass('woocommerce-invalid woocommerce-invalid-required-field input-text-error wfacp_error wfacp-invalid');
            if (this.style) { this.style.borderColor=''; this.style.boxShadow=''; }
        });
        // Salva telefone antes do dispatchEvent para protegê-lo de re-render do FunnelKit.
        var _szSavedPhone = {};
        szGetPhoneFields().each(function(){
            var n = this.name || this.id;
            if (n) _szSavedPhone[n] = $(this).val();
        });
        // Dispara input somente no CEP para atualizar máscara/estado sem provocar refresh do select Estado.
        $(CEP_POSTCODE_SELECTORS.join(',')).each(function(){
            try { this.dispatchEvent(new Event('input', { bubbles: true })); } catch(e) {}
        });
        // Restaura telefone se foi apagado OU deformado pelo re-render do FunnelKit.
        // Compara com o valor salvo antes do dispatchEvent — qualquer divergência restaura.
        szGetPhoneFields().each(function(){
            var n = this.name || this.id;
            if (!n || !_szSavedPhone[n]) return;
            var nowVal = $(this).val();
            if (nowVal !== _szSavedPhone[n]) {
                $(this).val(_szSavedPhone[n]);
            }
        });
        szMotoboyCobertura = null;
        szMotoboyLastCheckedCep = '';
        szMotoboyBlockedCep = '';
        szMotoboyLastCheckReal = false;
        szMotoboyCheckingCep = '';
        szMotoboyRealCheckingCep = '';
        szMotoboyLastErrorCode = '';
        szMotoboyLastErrorPayload = null;
        $('body').removeClass('senderzz-motoboy-cep-blocked senderzz-motoboy-cep-pending senderzz-cep-invalid senderzz-cep-valid senderzz-cep-generic');
        if (typeof neutralizeAddressValidation === 'function') neutralizeAddressValidation();
        if (typeof neutralizePostcodeValidationUnlessRealError === 'function') neutralizePostcodeValidationUnlessRealError();
        if (typeof lockAllAddressExceptCep === 'function') lockAllAddressExceptCep();
        var $cep = $('#billing_postcode, input[name="billing_postcode"], #shipping_postcode, input[name="shipping_postcode"]').first();
        if ($cep.length) {
            $cep.prop('readonly', false).prop('disabled', false).removeClass('senderzz-address-locked senderzz-cep-locked sz-mb-cep-invalid').css({'background':'#fff','color':'','opacity':'','border-color':'','box-shadow':''}).focus();
        }
        if (typeof scheduleLockFields === 'function') scheduleLockFields(40);
        if (typeof szSetNextButtonDisabled === 'function') szSetNextButtonDisabled(true);
    }

    function szCepErrorIsNotFound(r) {
        var code = String((r && (r.code || r.tipo || r.reason || r.erro_code)) || '').toLowerCase();
        var msg  = String((r && (r.mensagem || r.erro || r.message)) || '').toLowerCase();
        return code === 'cep_not_found' || code === 'cep-nao-encontrado' || code === 'not_found' || msg.indexOf('cep não encontrado') >= 0 || msg.indexOf('cep nao encontrado') >= 0;
    }


    function szClearAddressOnlyForInvalidCep() {
        $(CEP_STREET_SELECTORS.concat(CEP_CITY_SELECTORS).concat(CEP_NEIGHBORHOOD_SELECTORS).concat(CEP_NUMBER_SELECTORS).concat(CEP_COMPLEMENT_SELECTORS).join(',')).val('').trigger('change');
        $(CEP_SELECT_SELECTORS.join(',')).val('').trigger('change');
        if (typeof neutralizeAddressValidation === 'function') neutralizeAddressValidation();
    }

    function szShowCepNotFound(cep) {
        cep = szNormalizeCep(cep || getCepValue());
        if (!cep || cep.length !== 8) return;
        var now = Date.now();
        if (document.getElementById('sz-popup-overlay') && szCepNotFoundPopupCep === cep) return;
        if (szCepNotFoundPopupCep === cep && (now - szCepNotFoundLastShownAt) < 1800) return;
        szCepNotFoundPopupCep = cep;
        szCepNotFoundLastShownAt = now;

        szClearAddressOnlyForInvalidCep();
        if (typeof lockAllAddressExceptCep === 'function') lockAllAddressExceptCep();
        if (typeof neutralizeAddressValidation === 'function') neutralizeAddressValidation();

        // Bloquear acesso ao step 2 enquanto o CEP estiver inválido
        $('body').addClass('senderzz-cep-invalid-blocking senderzz-cep-invalid');
        resetCepValidation(cep);
        if (typeof szSetNextButtonDisabled === 'function') szSetNextButtonDisabled(true);

        // Forçar volta ao step 1 (cobre todos os fluxos, não só motoboy)
        setTimeout(function() {
            var $step1 = $('.wfacp-payment-tab-list.wfacp-tab1, .wfacp-tab1, [data-step="1"], [data-current-step="1"]').first();
            if ($step1.length) $step1.trigger('click');
        }, 30);

        var cepFmt = cep.slice(0,5) + '-' + cep.slice(5);
        szShowPopup(
            '📍',
            'CEP não encontrado',
            'O CEP <strong>' + cepFmt + '</strong> não foi localizado.<br>Confira o número e ajuste somente o CEP para tentar novamente.',
            'Corrigir CEP',
            function() {
                szClearCheckoutExceptNamePhone();
                // Limpar o bloqueio do tab2 para que o usuário possa avançar após corrigir
                $('body').removeClass('senderzz-cep-invalid-blocking');
            }
        );
    }

    // ─── MOTOBOY: bloqueio por cobertura de CEP ──────────────────────────────
    var szMotoboyChecked = false;
    var szMotoboyCobertura = null; // null=não verificado, true=cobre, false=não cobre
    var szMotoboyLastCheckedCep = '';
    var szMotoboyCheckingCep = '';
    var szSemCoberturaPopupCep = '';
    var szSemCoberturaPopupOpen = false;
    var szSemCoberturaLastShownAt = 0;
    var szSemCoberturaPopupClosedCep = '';
    var szMotoboyBlockedCep = '';
    var szMotoboyLastCheckReal = false;
    var szMotoboyRealCheckTimer = null;
    var szMotoboyRealCheckingCep = '';
    var szMotoboyLastErrorCode = '';
    var szMotoboyLastErrorPayload = null;

    function szVerificarCoberturaMotoboy(cep, callback, realCheck) {
        if (!SZ_IS_MOTOBOY) { if (typeof callback === 'function') callback(true); return; }
        cep = String(cep || '').replace(/\D/g, '');
        realCheck = realCheck === true;
        if (cep.length !== 8) {
            szMotoboyCobertura = null;
            szMotoboyLastCheckReal = false;
            if (typeof callback === 'function') callback(false);
            return;
        }

        // Cache local: a checagem simples não substitui a checagem real.
        if (szMotoboyLastCheckedCep === cep && szMotoboyCobertura !== null && (!realCheck || szMotoboyLastCheckReal)) {
            if (typeof callback === 'function') callback(szMotoboyCobertura);
            return;
        }
        if (szMotoboyCheckingCep === cep && (!realCheck || szMotoboyRealCheckingCep === cep)) {
            setTimeout(function(){ szVerificarCoberturaMotoboy(cep, callback, realCheck); }, 180);
            return;
        }

        szMotoboyCheckingCep = cep;
        if (realCheck) szMotoboyRealCheckingCep = cep;
        szMotoboyLastCheckedCep = cep;

        // Consulta simples: faixa/zona local. Consulta real: só quando necessário e com cache no servidor.
        $.ajax({
            url: SZ_API + '/zona-cep',
            type: 'GET',
            data: { cep: cep, real: realCheck ? 1 : 0 },
            success: function(r) {
                if (getCepValue() !== cep) { szMotoboyCheckingCep = ''; szMotoboyRealCheckingCep = ''; return; }
                szMotoboyCobertura = !!(r && r.ok && r.zona_id);
                szMotoboyLastErrorPayload = r || null;
                szMotoboyLastErrorCode = szCepErrorIsNotFound(r) ? 'cep_not_found' : (r && (r.code || r.tipo) ? String(r.code || r.tipo) : '');
                szMotoboyLastCheckReal = realCheck;
                szMotoboyCheckingCep = '';
                szMotoboyRealCheckingCep = '';
                if (szMotoboyCobertura === true) {
                    szMotoboyLastErrorCode = '';
                    $('body').removeClass('senderzz-motoboy-cep-pending');
                    szLimparBloqueioMotoboy();
                    if (typeof scheduleLockFields === 'function') scheduleLockFields(40);
                } else if (szMotoboyLastErrorCode === 'cep_not_found') {
                    szAplicarBloqueioMotoboy(cep);
                    if (realCheck) szShowCepNotFound(cep);
                } else {
                    szAplicarBloqueioMotoboy(cep);
                    if (typeof szLockAddressBecauseMotoboyPending === 'function') szLockAddressBecauseMotoboyPending(cep);
                }
                if (typeof callback === 'function') callback(szMotoboyCobertura);
            },
            error: function() {
                if (getCepValue() !== cep) { szMotoboyCheckingCep = ''; szMotoboyRealCheckingCep = ''; return; }
                // Em erro de rede da validação real, não derruba o checkout.
                szMotoboyCobertura = true;
                szMotoboyLastCheckReal = realCheck;
                szMotoboyCheckingCep = '';
                szMotoboyRealCheckingCep = '';
                $('body').removeClass('senderzz-motoboy-cep-pending');
                szLimparBloqueioMotoboy();
                if (typeof scheduleLockFields === 'function') scheduleLockFields(40);
                if (typeof callback === 'function') callback(true);
            }
        });
    }

    function szAplicarBloqueioMotoboy(cep) {
        cep = String(cep || getCepValue() || '').replace(/\D/g, '');
        szMotoboyBlockedCep = cep;
        $('body').addClass('senderzz-motoboy-cep-blocked').removeClass('senderzz-motoboy-cep-pending');
        if (typeof lockAllAddressExceptCep === 'function') lockAllAddressExceptCep();
        if (typeof neutralizeAddressValidation === 'function') neutralizeAddressValidation();
        szBloqueiarBotaoEtapa(true, null);
        szForceStep1WhenBlocked();
    }

    function szLimparBloqueioMotoboy() {
        szSemCoberturaPopupOpen = false;
        szSemCoberturaPopupCep = '';
        szSemCoberturaPopupClosedCep = '';
        szMotoboyBlockedCep = '';
        $('body').removeClass('senderzz-motoboy-cep-blocked senderzz-motoboy-cep-pending');
        szBloqueiarBotaoEtapa(false, null);
        if (typeof scheduleLockFields === 'function') scheduleLockFields(40);
        setTimeout(szCheckRequiredFields, 60);
    }

    function szMotoboyCepBloqueado() {
        if (!SZ_IS_MOTOBOY) return false;
        var cep = getCepValue();
        return cep.length === 8 && szMotoboyCobertura === false && (!szMotoboyBlockedCep || szMotoboyBlockedCep === cep);
    }

    function szMotoboyCoverageAllowsAddress(cep) {
        if (!SZ_IS_MOTOBOY) return true;
        cep = String(cep || getCepValue() || '').replace(/\D/g, '');
        // Regra Senderzz: em checkout Motoboy, endereço só libera quando o CEP
        // já foi validado E a cobertura também foi confirmada. Enquanto a
        // região está pendente/não atendida, nenhum campo além do CEP libera.
        return cep.length === 8 && szMotoboyCobertura === true && szMotoboyLastCheckedCep === cep;
    }

    function szLockAddressBecauseMotoboyPending(cep) {
        if (!SZ_IS_MOTOBOY) return;
        if (typeof lockAllAddressExceptCep === 'function') lockAllAddressExceptCep();
        if (typeof neutralizeAddressValidation === 'function') neutralizeAddressValidation();
        $('body').addClass('senderzz-motoboy-cep-pending');
    }

    function szForceStep1WhenBlocked() {
        setTimeout(function() {
            if (!szMotoboyCepBloqueado()) return;
            var $step1 = $('.wfacp-payment-tab-list.wfacp-tab1, .wfacp-tab1, [data-step="1"], [data-current-step="1"]').first();
            if ($step1.length && !$step1.hasClass('wfacp-active')) {
                $step1.trigger('click');
            }
            $('.wfacp_next_page_button, .wfacp_next_step_btn, .wfacp-submit-btn, #place_order, .wfacp_place_order, button[name="woocommerce_checkout_place_order"]').prop('disabled', true).attr('aria-disabled','true').addClass('senderzz-next-disabled');
        }, 30);
    }

    function szResetMotoboyCoverageForCep(cep) {
        cep = String(cep || '').replace(/\D/g, '');
        if (!SZ_IS_MOTOBOY) return;
        if (cep !== szMotoboyLastCheckedCep) {
            szMotoboyCobertura = null;
            szMotoboyCheckingCep = '';
            szMotoboyRealCheckingCep = '';
            szMotoboyLastCheckReal = false;
            szMotoboyLastErrorCode = '';
            szMotoboyLastErrorPayload = null;
            $('[data-sz-real-cep-ok]').removeAttr('data-sz-real-cep-ok');
            szLimparBloqueioMotoboy();
            if (cep.length === 8) szLockAddressBecauseMotoboyPending(cep);
        }
        if (cep.length !== 8) {
            szMotoboyCobertura = null;
            szMotoboyCheckingCep = '';
            szMotoboyRealCheckingCep = '';
            szMotoboyLastCheckReal = false;
            $('[data-sz-real-cep-ok]').removeAttr('data-sz-real-cep-ok');
            szLimparBloqueioMotoboy();
        }
    }

    function szBloquearAvanco(e) {
        if (!SZ_IS_MOTOBOY) return;
        var cep = getCepValue();
        if (!cep || cep.length < 8) return;
        if (szMotoboyCobertura === true) return; // já verificado e ok
        if (szMotoboyCobertura === false) {
            // Já sabe que não tem cobertura — bloqueia imediatamente.
            // Se o erro real foi CEP inexistente, nunca mostra região não atendida.
            if (e) { e.preventDefault(); e.stopImmediatePropagation(); }
            if (szMotoboyLastErrorCode === 'cep_not_found') szShowCepNotFound(cep);
            else szShowSemCobertura(cep, true);
            return;
        }
        // Ainda não verificou — bloqueia e verifica
        if (e) { e.preventDefault(); e.stopImmediatePropagation(); }
        szVerificarCoberturaMotoboy(cep, function(ok) {
            if (!ok) {
                if (szMotoboyLastErrorCode === 'cep_not_found') szShowCepNotFound(cep);
                else szShowSemCobertura(cep, true);
            }
            // Se ok, deixa o usuário clicar novamente
        }, true);
    }

    function szGetCheckoutAddressLine() {
        var rua = String($('#billing_address_1').val() || $('[name="billing_address_1"]').val() || $('#shipping_address_1').val() || $('[name="shipping_address_1"]').val() || '').trim();
        var numero = String($('#billing_number').val() || $('[name="billing_number"]').val() || $('#shipping_number').val() || $('[name="shipping_number"]').val() || '').trim();
        var bairro = String($('#billing_neighborhood').val() || $('[name="billing_neighborhood"]').val() || $('#shipping_neighborhood').val() || $('[name="shipping_neighborhood"]').val() || '').trim();
        var cidade = String($('#billing_city').val() || $('[name="billing_city"]').val() || $('#shipping_city').val() || $('[name="shipping_city"]').val() || '').trim();
        var uf = String($('#billing_state').val() || $('[name="billing_state"]').val() || $('#shipping_state').val() || $('[name="shipping_state"]').val() || '').trim();
        var linha1 = rua ? rua + (numero ? ', ' + numero : '') : '';
        var linha2 = [bairro, cidade, uf].filter(Boolean).join(' - ');
        if (!linha1 && !linha2) return '';
        return '<span>Endereço informado:</span>' + (linha1 ? linha1 + '<br>' : '') + linha2;
    }

    function szShowSemCobertura(cep, force) {
        cep = String(cep || '').replace(/\D/g, '');
        if (!cep || cep.length !== 8) return;
        if (szMotoboyLastErrorCode === 'cep_not_found') { szShowCepNotFound(cep); return; }

        szAplicarBloqueioMotoboy(cep);

        var now = Date.now();
        if (document.getElementById('sz-popup-overlay') && szSemCoberturaPopupOpen && szSemCoberturaPopupCep === cep) return;
        // Automático: mostra uma vez por CEP inválido. Clique/submit só mostra de novo se o comprador tentou avançar após fechar.
        if (!force && (szSemCoberturaPopupCep === cep || szSemCoberturaPopupClosedCep === cep)) return;
        if (force && szSemCoberturaPopupCep === cep && (now - szSemCoberturaLastShownAt) < 1200) return;

        szSemCoberturaPopupOpen = true;
        szSemCoberturaPopupCep = cep;
        szSemCoberturaLastShownAt = now;

        var cepFmt = cep.slice(0,5) + '-' + cep.slice(5);
        var addr = szGetCheckoutAddressLine();
        var body = 'O CEP <strong>' + cepFmt + '</strong> não é atendido pelo Motoboy Senderzz.'
            + (addr ? '<span class="sz-pop-address"><span>CEP:</span><strong>' + cepFmt + '</strong><br>' + addr + '</span>' : '<span class="sz-pop-address"><span>CEP informado:</span><strong>' + cepFmt + '</strong></span>')
            + '<br>Informe um CEP dentro da área de cobertura para continuar.';
        szShowPopup( '🏍️', 'Região não atendida', body, 'Alterar CEP', function(){
            szSemCoberturaPopupOpen = false;
            szSemCoberturaPopupClosedCep = cep;
            szClearCheckoutExceptNamePhone();
        } );
    }

    function szBloqueiarBotaoEtapa(bloquear, urlExpedicao) {
        var $btn = $('.wfacp_next_page_button, .wfacp_next_step_btn, .wfacp-submit-btn, button[name="woocommerce_checkout_place_order"]').first();
        if (!$btn.length) return;
        if (bloquear) {
            if (!$btn.attr('data-sz-original-text')) $btn.attr('data-sz-original-text', $btn.text());
            if (urlExpedicao && !SZ_IS_MOTOBOY) {
                // CEP sem cobertura confirmado — botão vira expedição apenas em fluxo de expedição
                $btn.prop('disabled', false)
                    .attr('data-sz-sem-cobertura','1')
                    .attr('data-sz-expedition-redirect','1')
                    .addClass('senderzz-expedition-redirect')
                    .css({'opacity':'1','cursor':'pointer','pointer-events':'auto','background':'linear-gradient(135deg,#ff4b00,#ff9f0a)','border-color':'#ff4b00','color':'#fff','font-family':'inherit','filter':'none'})
                    .off('click.sz_exp').on('click.sz_exp', function(e) {
                        e.preventDefault(); e.stopImmediatePropagation();
                        window.location.href = urlExpedicao;
                    })
                    .text('📦 Fazer pedido por Expedição');
            } else {
                // Sem cobertura confirmada — só desabilita, mantém texto original
                $btn.prop('disabled', true)
                    .attr('data-sz-sem-cobertura','1')
                    .css({'opacity':'.58','cursor':'not-allowed','pointer-events':'none','background':'linear-gradient(135deg,#ff4b00,#ff9f0a)','color':'#fff','filter':'none'})
                    .off('click.sz_exp');
                // Não muda o texto — mantém "Próxima Etapa"
            }
        } else {
            $btn.prop('disabled', false)
                .removeAttr('data-sz-sem-cobertura data-sz-expedition-redirect')
                .removeClass('senderzz-expedition-redirect')
                .css({'opacity':'','cursor':'','pointer-events':'','background':'','border-color':'','filter':''})
                .off('click.sz_exp')
                .text($btn.attr('data-sz-original-text') || 'Próxima Etapa');
        }
    }

    // Intercepta botão "Próxima Etapa" / "Finalizar" do FunnelKit
    $(document).on('click', '.wfacp_next_step_btn, .wfacp-submit-btn, #place_order, .wfacp_place_order, [name="woocommerce_checkout_place_order"]', function(e) {
        if (!SZ_IS_MOTOBOY) return;
        var cep = getCepValue();
        if (!cep || cep.length < 8) return;

        var $btn = $(this);
        if ($btn.attr('data-sz-real-cep-ok') === cep && szMotoboyCobertura === true && szMotoboyLastCheckReal) return;

        if (szMotoboyCobertura === false) {
            e.preventDefault(); e.stopImmediatePropagation();
            szShowSemCobertura(cep, true);
            return false;
        }

        e.preventDefault(); e.stopImmediatePropagation();
        szBloqueiarBotaoEtapa(true, null);
        szVerificarCoberturaMotoboy(cep, function(ok) {
            if (!ok) {
                szShowSemCobertura(cep, true);
                return;
            }
            $btn.attr('data-sz-real-cep-ok', cep);
            szBloqueiarBotaoEtapa(false, null);
            setTimeout(function(){ $btn.trigger('click'); }, 80);
        }, true);
        return false;
    });

    // Verifica cobertura quando CEP é preenchido (8 dígitos)
    function szCheckMotoboyOnCep() {
        if (!SZ_IS_MOTOBOY) return;
        var cep = getCepValue();
        if (cep.length !== 8) { szMotoboyCobertura = null; szMotoboyLastCheckReal = false; return; }
        szLockAddressBecauseMotoboyPending(cep);
        szVerificarCoberturaMotoboy(cep, function(ok) {
            if (!ok) {
                // Fora das faixas/zona: mostra o mesmo pop-up antigo.
                szShowSemCobertura(cep);
                return;
            }
            // Dentro da zona: valida existência real sem interceptar fetch global nem quebrar FunnelKit.
            clearTimeout(szMotoboyRealCheckTimer);
            szMotoboyRealCheckTimer = setTimeout(function(){
                if (getCepValue() !== cep || szMotoboyLastCheckReal) return;
                szVerificarCoberturaMotoboy(cep, function(realOk){
                    if (!realOk) szShowSemCobertura(cep, true);
                }, true);
            }, 900);
        }, false);
    }

    // ─── CEP ADDRESS LOCK ────────────────────────────────────────────────────
    // Regra Senderzz: checkout nasce com endereço travado; CEP é o único campo livre.
    // Após CEP validado, libera número/complemento e libera logradouro somente quando
    // o CEP for genérico/único e a base do CEP não retornar logradouro.
    var CEP_POSTCODE_SELECTORS = [
        '#billing_postcode', '#shipping_postcode',
        'input[name="billing_postcode"]', 'input[name="shipping_postcode"]'
    ];
    var CEP_STREET_SELECTORS = [
        '#billing_address_1', '#shipping_address_1',
        'input[name="billing_address_1"]', 'input[name="shipping_address_1"]'
    ];
    var CEP_NUMBER_SELECTORS = [
        '#billing_number', '#shipping_number',
        '#billing_address_number', '#shipping_address_number',
        'input[name="billing_number"]', 'input[name="shipping_number"]',
        'input[name="billing_address_number"]', 'input[name="shipping_address_number"]'
    ];
    var CEP_COMPLEMENT_SELECTORS = [
        '#billing_address_2', '#shipping_address_2',
        'input[name="billing_address_2"]', 'input[name="shipping_address_2"]'
    ];
    var CEP_CITY_SELECTORS = [
        '#billing_city', '#shipping_city',
        'input[name="billing_city"]', 'input[name="shipping_city"]'
    ];
    var CEP_NEIGHBORHOOD_SELECTORS = [
        '#billing_neighborhood', '#shipping_neighborhood', '#billing_bairro', '#shipping_bairro',
        'input[name="billing_neighborhood"]', 'input[name="shipping_neighborhood"]',
        'input[name="billing_bairro"]', 'input[name="shipping_bairro"]'
    ];
    var CEP_SELECT_SELECTORS   = [ '#billing_state', '#shipping_state' ];
    var ALWAYS_LOCKED_SELECTORS = [ '#billing_country', '#shipping_country' ];

    var CEP_ALL_ADDRESS_INPUTS = CEP_STREET_SELECTORS
        .concat(CEP_NUMBER_SELECTORS)
        .concat(CEP_COMPLEMENT_SELECTORS)
        .concat(CEP_CITY_SELECTORS)
        .concat(CEP_NEIGHBORHOOD_SELECTORS);

    var szCepValidation = {
        cep: '',
        valid: false,
        pending: false,
        logradouro: '',
        bairro: '',
        localidade: '',
        uf: '',
        erro: false
    };
    var szCepValidationTimer = null;

    function destroySelect2AndDisable( el ) {
        if ( ! el ) return;
        var $el = $( el );
        try { if ( $el.hasClass( 'select2-hidden-accessible' ) ) $el.select2( 'destroy' ); } catch(e) {}
        $el.prop( 'disabled', true ).attr( 'aria-invalid', 'false' ).addClass( 'senderzz-cep-locked senderzz-address-locked' );
        $el.closest( '.form-row, .wfacp-form-control-wrapper, p' ).addClass( 'senderzz-address-row-locked' );
        $el.next( '.select2-container' ).hide();
        var name = $el.attr( 'name' ), val = $el.val();
        if ( name && ! $el.siblings( 'input[data-sz-backup="1"][name="' + name + '"]' ).length ) {
            $( '<input type="hidden" data-sz-backup="1">' ).attr( 'name', name ).val( val ).insertAfter( $el );
        } else if ( name ) {
            $el.siblings( 'input[data-sz-backup="1"][name="' + name + '"]' ).val( val );
        }
    }

    function restoreSelect2( el ) {
        if ( ! el ) return;
        var $el = $( el );
        $el.prop( 'disabled', false ).removeClass( 'senderzz-cep-locked senderzz-address-locked' );
        $el.closest( '.form-row, .wfacp-form-control-wrapper, p' ).removeClass( 'senderzz-address-row-locked' );
        $el.next( '.select2-container' ).show();
        $el.siblings( 'input[data-sz-backup="1"]' ).remove();
        try { if ( typeof $.fn.select2 === 'function' && ! $el.hasClass( 'select2-hidden-accessible' ) ) $el.select2(); } catch(e) {}
    }

    function lockCountryAlways() {
        ALWAYS_LOCKED_SELECTORS.forEach( function( sel ) {
            var el = document.querySelector( sel );
            if ( el ) destroySelect2AndDisable( el );
        });
    }

    function lockSelect( $f ) { destroySelect2AndDisable( $f[0] ); }
    function unlockSelect( $f ) { restoreSelect2( $f[0] ); }

    var cepLockObserver = null;
    var cepLockTimer    = null;

    function getCepValue() {
        return String(
            $( '#billing_postcode' ).val() ||
            $( '#shipping_postcode' ).val() ||
            $( 'input[name="billing_postcode"]' ).val() || ''
        ).replace( /\D/g, '' );
    }

    function setReadonly( selectorList, locked ) {
        $( selectorList.join( ',' ) ).each( function () {
            var $f = $( this );
            if ( ! $f.length || $f.is( CEP_POSTCODE_SELECTORS.join(',') ) ) return;
            $f.prop( 'readonly', !!locked )
                .attr( 'aria-readonly', locked ? 'true' : 'false' )
                .attr( 'aria-invalid', locked ? 'false' : ( $f.attr( 'aria-invalid' ) || 'false' ) )
                .toggleClass( 'senderzz-cep-locked senderzz-address-locked', !!locked );
            $f.closest( '.form-row, .wfacp-form-control-wrapper, p' ).toggleClass( 'senderzz-address-row-locked', !!locked );
            if ( locked ) neutralizeAddressValidation();
        } );
    }

    function neutralizeAddressValidation() {
        var selectors = CEP_ALL_ADDRESS_INPUTS.concat( CEP_SELECT_SELECTORS ).join( ',' );
        $( selectors ).each( function () {
            var $f = $( this );
            $f.attr( 'aria-invalid', 'false' )
                .removeClass( 'woocommerce-invalid input-text-error wfacp_error wfacp-invalid' );
            $f.closest( '.form-row, .wfacp-form-control-wrapper, p, .woocommerce-invalid, .validate-required' )
                .removeClass( 'woocommerce-invalid woocommerce-invalid-required-field input-text-error wfacp_error wfacp-invalid' );
        } );
    }

    function neutralizePostcodeValidationUnlessRealError() {
        var cep = getCepValue ? getCepValue() : '';
        var realError = $('body').hasClass('senderzz-cep-invalid') || !!$('.sz-mb-cep-global-error, #sz-popup-overlay').filter(function(){
            var t = String($(this).text() || '').toLowerCase();
            return t.indexOf('cep') >= 0 && (t.indexOf('não') >= 0 || t.indexOf('fora') >= 0 || t.indexOf('inexist') >= 0);
        }).length;
        if (realError) return;
        $( CEP_POSTCODE_SELECTORS.join(',') ).each(function(){
            var $f = $(this);
            $f.attr('aria-invalid','false')
              .removeClass('woocommerce-invalid input-text-error wfacp_error wfacp-invalid sz-mb-cep-invalid');
            $f.closest('.form-row, .wfacp-form-control-wrapper, p')
              .removeClass('woocommerce-invalid woocommerce-invalid-required-field input-text-error wfacp_error wfacp-invalid');
            if (cep.length === 8) {
                $f.addClass('sz-mb-cep-ok');
            }
        });
    }

    function clearAddressFieldsExceptCep( silent ) {
        var $inputs = $( CEP_ALL_ADDRESS_INPUTS.join( ',' ) );
        var $selects = $( CEP_SELECT_SELECTORS.join( ',' ) );
        $inputs.val( '' );
        $selects.val( '' );
        if ( ! silent ) {
            $inputs.trigger( 'change' );
            $selects.trigger( 'change' );
        }
        neutralizeAddressValidation();
    }

    function resetCepValidation( keepCep ) {
        szCepValidation = { cep: keepCep || '', valid: false, pending: false, logradouro: '', bairro: '', localidade: '', uf: '', erro: false };
        $( 'body' ).removeClass( 'senderzz-cep-valid senderzz-cep-generic senderzz-cep-invalid' );
    }

    function lockAllAddressExceptCep() {
        $( CEP_POSTCODE_SELECTORS.join( ',' ) )
            .prop( 'readonly', false )
            .prop( 'disabled', false )
            .removeClass( 'senderzz-cep-locked senderzz-address-locked' );

        setReadonly( CEP_ALL_ADDRESS_INPUTS, true );
        $( CEP_SELECT_SELECTORS.join( ',' ) ).each( function () { lockSelect( $( this ) ); } );
        lockCountryAlways();
        neutralizeAddressValidation();
    }

    function isFieldRequired( $f ) {
        return !!(
            $f.prop( 'required' ) ||
            $f.attr( 'aria-required' ) === 'true' ||
            $f.closest( '.form-row, .wfacp-form-control-wrapper, p' ).hasClass( 'validate-required' )
        );
    }

    function applyFieldLock() {
        var cep = getCepValue();
        if ( cep.length < 8 ) {
            resetCepValidation( cep );
            lockAllAddressExceptCep();
            return;
        }

        // Motoboy: o endereço só libera após validação do CEP E confirmação de cobertura.
        // Isso impede a janela em que o usuário conseguia digitar logradouro/número
        // antes do pop-up de região não atendida aparecer.
        if ( SZ_IS_MOTOBOY && ! szMotoboyCoverageAllowsAddress( cep ) ) {
            lockAllAddressExceptCep();
            neutralizeAddressValidation();
            return;
        }

        // Enquanto o CEP não foi validado, o endereço continua travado.
        if ( szCepValidation.cep !== cep || ! szCepValidation.valid ) {
            lockAllAddressExceptCep();
            return;
        }

        var isGenericCep = String( szCepValidation.logradouro || '' ).trim() === '';
        $( 'body' )
            .toggleClass( 'senderzz-cep-valid', true )
            .toggleClass( 'senderzz-cep-generic', isGenericCep )
            .removeClass( 'senderzz-cep-invalid' );

        // Campos automáticos continuam inalteráveis.
        setReadonly( CEP_CITY_SELECTORS, true );
        $( CEP_SELECT_SELECTORS.join( ',' ) ).each( function () { lockSelect( $( this ) ); } );
        lockCountryAlways();

        // Bairro fica travado quando veio da base do CEP; se a base não trouxe e o campo for obrigatório, libera para não bloquear checkout válido.
        $( CEP_NEIGHBORHOOD_SELECTORS.join( ',' ) ).each( function () {
            var $f = $( this );
            var shouldUnlock = ! String( $f.val() || szCepValidation.bairro || '' ).trim() && isFieldRequired( $f );
            $f.prop( 'readonly', ! shouldUnlock )
                .attr( 'aria-readonly', shouldUnlock ? 'false' : 'true' )
                .toggleClass( 'senderzz-cep-locked senderzz-address-locked', ! shouldUnlock );
        } );

        // Logradouro só libera quando o CEP validado não tem logradouro na base.
        setReadonly( CEP_STREET_SELECTORS, ! isGenericCep );

        // Número e complemento são manuais; liberam somente depois do CEP validado.
        setReadonly( CEP_NUMBER_SELECTORS.concat( CEP_COMPLEMENT_SELECTORS ), false );
    }

    function unlockAddressFields() {
        // Nome mantido para compatibilidade com chamadas antigas.
        // Pela nova regra Senderzz, não existe mais “destravar tudo”: volta ao estado seguro.
        lockAllAddressExceptCep();
    }

    function scheduleLockFields( delay ) {
        clearTimeout( cepLockTimer );
        cepLockTimer = setTimeout( applyFieldLock, delay || 300 );
    }

    function validateCepAddress( cep, callback ) {
        cep = String( cep || '' ).replace( /\D/g, '' );
        clearTimeout( szCepValidationTimer );

        if ( cep.length !== 8 ) {
            resetCepValidation( cep );
            scheduleLockFields( 40 );
            if ( typeof callback === 'function' ) callback( false, szCepValidation );
            return;
        }

        if ( szCepValidation.cep === cep && szCepValidation.valid ) {
            scheduleLockFields( 40 );
            if ( typeof callback === 'function' ) callback( true, szCepValidation );
            return;
        }

        resetCepValidation( cep );
        szCepValidation.pending = true;
        lockAllAddressExceptCep();

        $.ajax( {
            url: 'https://viacep.com.br/ws/' + cep + '/json/',
            type: 'GET',
            dataType: 'json',
            timeout: 4500,
            success: function( r ) {
                if ( getCepValue() !== cep ) return;
                if ( ! r || r.erro ) {
                    resetCepValidation( cep );
                    szCepValidation.erro = true;
                    $( 'body' ).addClass( 'senderzz-cep-invalid' ).removeClass( 'senderzz-cep-valid senderzz-cep-generic' );
                    clearAddressFieldsExceptCep( true );
                    lockAllAddressExceptCep();
                    neutralizeAddressValidation();
                    szShowCepNotFound( cep );
                    if ( typeof callback === 'function' ) callback( false, szCepValidation );
                    return;
                }

                // CEP válido: remover bloqueio de tab2
                $('body').removeClass('senderzz-cep-invalid-blocking');
                szLastCepDigits = cep;
                szCepValidation = {
                    cep: cep,
                    valid: true,
                    pending: false,
                    logradouro: String( r.logradouro || '' ).trim(),
                    bairro: String( r.bairro || '' ).trim(),
                    localidade: String( r.localidade || '' ).trim(),
                    uf: String( r.uf || '' ).trim(),
                    erro: false
                };
                scheduleLockFields( 120 );
                if ( typeof callback === 'function' ) callback( true, szCepValidation );
            },
            error: function() {
                if ( getCepValue() !== cep ) return;
                // Fallback anti-quebra: se a consulta pública falhar, não libera edição ampla.
                // Libera apenas campos manuais/logradouro para o usuário concluir, mantendo cidade/UF protegidos pelo autofill do checkout.
                szCepValidation = { cep: cep, valid: true, pending: false, logradouro: '', bairro: '', localidade: '', uf: '', erro: false };
                scheduleLockFields( 120 );
                if ( typeof callback === 'function' ) callback( true, szCepValidation );
            }
        } );
    }

    function startCepLockObserver() {
        if ( cepLockObserver ) cepLockObserver.disconnect();
        var allSelectors = CEP_ALL_ADDRESS_INPUTS.concat( CEP_SELECT_SELECTORS );
        var targets = [];
        allSelectors.forEach( function ( sel ) { $( sel ).each( function () { targets.push( this ); } ); } );
        if ( ! targets.length ) return;
        cepLockObserver = new MutationObserver( function () { scheduleLockFields( 120 ); } );
        targets.forEach( function ( el ) { cepLockObserver.observe( el, { attributes: true, attributeFilter: [ 'value', 'readonly', 'disabled' ] } ); } );
    }

    $( document ).on( 'change input', CEP_ALL_ADDRESS_INPUTS.join( ',' ), function () {
        var cep = getCepValue();
        if ( cep.length === 8 ) scheduleLockFields( 120 );
        else lockAllAddressExceptCep();
    } );

    $( document ).on( 'input', CEP_POSTCODE_SELECTORS.join( ',' ), function () {
        var raw = String( $( this ).val() || '' ).replace( /\D/g, '' );
        var previousCepDigits = szLastCepDigits;
        // Formata CEP como 00000-000
        if ( raw.length > 5 ) {
            var fmt = raw.slice(0,5) + '-' + raw.slice(5,8);
            if ( $( this ).val() !== fmt ) $( this ).val( fmt );
        }
        var cep = raw;
        if ( previousCepDigits && previousCepDigits !== cep ) szClearManualAddressForCepChange( previousCepDigits, cep );
        szLastCepDigits = cep;
        if (cep !== szCepNotFoundPopupCep) { szCepNotFoundPopupCep = ''; szCepNotFoundLastShownAt = 0; }
        if (SZ_IS_MOTOBOY) szResetMotoboyCoverageForCep(cep);
        if ( cep.length < 8 ) {
            resetCepValidation( cep );
            lockAllAddressExceptCep();
            szMotoboyCobertura = null;
        } else {
            if ( SZ_IS_MOTOBOY ) {
                // Inicia a cobertura imediatamente e mantém endereço travado até confirmar.
                szLockAddressBecauseMotoboyPending(cep);
                szCheckMotoboyOnCep();
            }
            validateCepAddress( cep, function( ok ) {
                if ( ok ) {
                    scheduleLockFields( 80 );
                    if ( SZ_IS_MOTOBOY && szMotoboyCobertura === null ) szCheckMotoboyOnCep();
                }
            } );
        }
        setTimeout(szCheckRequiredFields, 120);
    } );

    $( document ).ajaxComplete( function ( _evt, _xhr, settings ) {
        var url  = ( settings && settings.url  ) ? String( settings.url  ) : '';
        var data = ( settings && settings.data ) ? String( settings.data ) : '';
        if ( url.indexOf( 'update_order_review' ) >= 0 || data.indexOf( 'update_order_review' ) >= 0 ||
             data.indexOf( 'calc_shipping' ) >= 0 || url.indexOf( 'viacep' ) >= 0 ||
             url.indexOf( 'postmon' ) >= 0 || url.indexOf( 'opencep' ) >= 0 ) {
            var cep = getCepValue();
            if ( cep.length === 8 && ( szCepValidation.cep !== cep || ! szCepValidation.valid ) ) {
                validateCepAddress( cep );
            } else {
                scheduleLockFields( 180 );
            }
        }
    } );

    // Não intercepta window.fetch globalmente. Interceptar consultas de CEP de terceiros
    // quebrava scripts do checkout/FunnelKit e podia gerar loop de AJAX.
    // A cobertura Senderzz agora é validada somente pelo endpoint próprio /zona-cep.

    $( document.body ).on( 'updated_checkout wc_fragments_refreshed wfacp_updated_checkout wfacp_reload_order_review', function () {
        szNormalizePhoneValidationUi();
        szHideEnglishPhoneNotices();
        neutralizePostcodeValidationUnlessRealError();
        neutralizePostcodeValidationUnlessRealError();
        lockCountryAlways();
        startCepLockObserver();
        var cep = getCepValue();
        if ( SZ_IS_MOTOBOY && cep.length === 8 && szMotoboyCobertura !== true ) szLockAddressBecauseMotoboyPending(cep);
        if ( cep.length === 8 && ( szCepValidation.cep !== cep || ! szCepValidation.valid ) ) validateCepAddress( cep );
        else scheduleLockFields( 80 );
    } );

    $( document ).on( 'blur change', '#billing_first_name, input[name="billing_first_name"], #billing_name, input[name="billing_name"]', function() {
        var val = String($(this).val() || '').trim();
        if (val && !szNameIsCompound(val)) szShowFullNamePopup(false);
        neutralizePostcodeValidationUnlessRealError();
        setTimeout(szCheckRequiredFields, 80);
    } );

    $( document ).on( 'input blur change', '#billing_phone, input[name="billing_phone"], #billing_cellphone, input[name="billing_cellphone"]', function() {
        szNormalizePhoneValidationUi();
        szHideEnglishPhoneNotices();
        setTimeout(szCheckRequiredFields, 80);
    } );

    $( document ).on( 'submit', 'form.checkout, form.woocommerce-checkout, form.wfacp_main_form', function(e) {
        szNormalizePhoneValidationUi();
        szHideEnglishPhoneNotices();
        if (szShowFullNamePopup(true)) {
            e.preventDefault();
            e.stopImmediatePropagation();
            return false;
        }
    } );

    $( document ).ajaxComplete( function () {
        setTimeout(function(){ szNormalizePhoneValidationUi(); szHideEnglishPhoneNotices(); }, 50);
    } );

    $( document ).ready( function () {
        szLastCepDigits = getCepValue();
        szNormalizePhoneValidationUi();
        szHideEnglishPhoneNotices();
        neutralizePostcodeValidationUnlessRealError();
        lockCountryAlways();
        startCepLockObserver();
        lockAllAddressExceptCep();
        if ( getCepValue().length === 8 ) {
            if ( SZ_IS_MOTOBOY ) {
                szLockAddressBecauseMotoboyPending(getCepValue());
                szCheckMotoboyOnCep();
            }
            validateCepAddress( getCepValue(), function() {
                if ( SZ_IS_MOTOBOY && szMotoboyCobertura === null ) szCheckMotoboyOnCep();
            } );
        }
    } );
    // ─────────────────────────────────────────────────────────────────────────

    function getShippingLists() {
        return $('#shipping_method, ul.woocommerce-shipping-methods, .woocommerce-shipping-methods').filter(function () {
            return $(this).find('[data-tp-preferida="1"]').length || $(this).find('input[type="radio"][name^="shipping_method"]').length;
        });
    }
    function getBestList() {
        var $lists = getShippingLists();
        if (!$lists.length) return $();
        var $withPreferred = $lists.filter(function () { return $(this).find('[data-tp-preferida="1"]').length > 0; }).first();
        return $withPreferred.length ? $withPreferred : $lists.first();
    }
    function getShippingAreas() {
        return getShippingLists().map(function () {
            var $area = $(this).closest('#shipping_calculator_field, .wfacp_shipping_table, .woocommerce-shipping-totals, .wfacp_order_summary, .woocommerce-checkout-review-order-table, .wfacp_shipping_options');
            return ($area.length ? $area[0] : this);
        });
    }
    function getShippingLi($el) { return $el.closest('li.wfacp_single_shipping_method, li.woocommerce-shipping-method, li'); }
    function getSignature($lista) {
        var parts = [];
        $lista.find('input[type="radio"][name^="shipping_method"]').each(function () {
            var $r = $(this), $li = getShippingLi($r);
            parts.push(($r.val() || '') + '|' + $.trim($li.text()).replace(/\s+/g, ' '));
        });
        return parts.join('||');
    }
    function getCepKey() {
        return String($('#billing_postcode').val() || '').replace(/\D/g, '') + '|' + String($('#shipping_postcode').val() || '').replace(/\D/g, '');
    }
    function hardHideShipping() {
        $('body').addClass('senderzz-checkout-updating-shipping senderzz-shipping-hard-blank');
        getShippingLists().addClass('senderzz-hard-hide-shipping').removeClass('tp-loading');
        getShippingAreas().addClass('senderzz-shipping-is-updating');
    }
    function hardShowShipping() {
        $('body').removeClass('senderzz-checkout-updating-shipping senderzz-shipping-hard-blank');
        getShippingLists().removeClass('senderzz-hard-hide-shipping tp-loading');
        $('.senderzz-shipping-is-updating').removeClass('senderzz-shipping-is-updating');
        cepUpdateActive = false;
    }
    function scheduleApply(delay) { clearTimeout(applyTimer); applyTimer = setTimeout(aplicarCheckoutSenderzz, delay || 40); }
    function scheduleRelease(delay) {
        clearTimeout(releaseTimer);
        releaseTimer = setTimeout(function () { aplicarCheckoutSenderzz(); requestAnimationFrame(function () { hardShowShipping(); }); }, delay || FAST_HIDE_MS);
    }
    function reconnectObserver() {
        if (!window.MutationObserver || !document.body) return;
        try {
            if (!observer) {
                observer = new MutationObserver(function () {
                    if (isApplying) return;
                    scheduleApply(120);
                });
            }
            observer.disconnect();
            observer.observe(document.body, { childList: true, subtree: true });
        } catch (e) {}
    }
    function ocultarFormaPagamento() {
        $('.wc_payment_methods, ul.payment_methods, a.woocommerce-privacy-policy-link').hide();
        var termos = ['forma de pagamento', 'método de pagamento', 'metodo de pagamento', 'payment method'];
        $('h1,h2,h3,h4,.wfacp_section_heading,.wfacp_section_title,.wfacp_section_heading_wrap,.wfacp-payment-title,.woocommerce-checkout-payment-title').each(function () {
            var $e = $(this), texto = ($e.text() || '').replace(/\s+/g, ' ').trim().toLowerCase();
            if (!texto) return;
            for (var i = 0; i < termos.length; i++) {
                if (texto === termos[i] || texto.indexOf(termos[i]) >= 0) {
                    var $wrap = $e.closest('.wfacp_section_heading_wrap, .wfacp-section-heading, .wfacp-heading-row, tr');
                    ($wrap.length ? $wrap : $e).addClass('senderzz-hide-payment-headline').hide();
                    break;
                }
            }
        });
    }
    function neutralizarNaoRecomendados($lista, $preferida) {
        $lista.children('li').not($preferida).not('.tp-outros-header').each(function () {
            $(this).removeClass('tp-shipping-preferida').addClass('tp-shipping-normal').css({'background':'','background-color':'','box-shadow':'','border-radius':'','color':''});
        });
    }
    function markSelected($radio) {
        var $lista = getBestList();
        $lista.find('li').removeClass('senderzz-user-selected');
        if ($radio && $radio.length) getShippingLi($radio).addClass('senderzz-user-selected');
    }
    function aplicarCheckoutSenderzz() {
        if (isApplying) return;
        isApplying = true;
        if (observer) observer.disconnect();
        var $lista = getBestList();
        if (!$lista.length) { ocultarFormaPagamento(); isApplying = false; reconnectObserver(); return; }
        var signature = getSignature($lista);
        var quoteChanged = signature && signature !== lastSignature;
        if (quoteChanged) lastSignature = signature;
        $lista.removeClass('tp-loading');
        $lista.find('li.tp-outros-header').remove();
        $lista.children('li.tp-shipping-preferida').removeClass('tp-shipping-preferida').addClass('tp-shipping-normal');
        var $marker = $lista.find('[data-tp-preferida="1"]').first();
        var $preferida = $marker.length ? getShippingLi($marker) : $();
        if ($preferida.length) {
            $preferida.removeClass('tp-shipping-normal').addClass('tp-shipping-preferida');
            neutralizarNaoRecomendados($lista, $preferida);
            if ($lista.children('li').first()[0] !== $preferida[0]) $preferida.detach().prependTo($lista);
        }
        var savedValue = selectedBySignature[signature];
        var $savedRadio = savedValue ? $lista.find('input[type="radio"][name^="shipping_method"]').filter(function(){ return this.value === savedValue; }).first() : $();
        var $checked = $lista.find('input[type="radio"][name^="shipping_method"]:checked').first();
        if (quoteChanged && $preferida.length) {
            var $prefRadio = $preferida.find('input[type="radio"][name^="shipping_method"]').first();
            if ($prefRadio.length) { $prefRadio.prop('checked', true); selectedBySignature[signature] = $prefRadio.val(); markSelected($prefRadio); }
        } else if ($savedRadio.length) {
            $savedRadio.prop('checked', true); markSelected($savedRadio);
        } else if ($checked.length) {
            selectedBySignature[signature] = $checked.val(); markSelected($checked);
        }
        ocultarFormaPagamento();
        setTimeout(function () { isApplying = false; reconnectObserver(); }, 60);
    }
    function handleUserShippingSelection(input, evt) {
        var $input = $(input), $lista = getBestList(), signature = getSignature($lista);
        $input.prop('checked', true);
        selectedBySignature[signature] = $input.val();
        markSelected($input);
        scheduleApply(20);
        if (evt) { evt.stopImmediatePropagation(); evt.stopPropagation(); }
        return false;
    }
    document.addEventListener('change', function (evt) {
        var t = evt.target;
        if (t && t.matches && t.matches('input[type="radio"][name^="shipping_method"]')) handleUserShippingSelection(t, evt);
    }, true);
    document.addEventListener('click', function (evt) {
        var t = evt.target;
        if (t && t.matches && t.matches('input[type="radio"][name^="shipping_method"]')) setTimeout(function () { handleUserShippingSelection(t, null); }, 0);
    }, true);
    $(document).on('input change blur', '#billing_postcode, #shipping_postcode, input[name="billing_postcode"], input[name="shipping_postcode"]', function () {
        var key = getCepKey();
        if (key && key !== lastCepKey) { lastCepKey = key; cepUpdateActive = true; hardHideShipping(); scheduleRelease(FAST_HIDE_MS + 180); }
    });
    $(document.body).on('update_checkout wfacp_before_update_checkout wfacp_update_checkout', function () { if (cepUpdateActive) hardHideShipping(); });
    $(document.body).on('updated_checkout wc_fragments_refreshed wfacp_updated_checkout wfacp_reload_order_review', function () { scheduleApply(40); if (cepUpdateActive) scheduleRelease(FAST_HIDE_MS); });
    $(document).ajaxSend(function (_evt, _xhr, settings) {
        var url = (settings && settings.url) ? String(settings.url) : '', data = (settings && settings.data) ? String(settings.data) : '';
        if (cepUpdateActive && (url.indexOf('update_order_review') >= 0 || url.indexOf('wc-ajax=update_order_review') >= 0 || data.indexOf('update_order_review') >= 0 || data.indexOf('calc_shipping') >= 0)) hardHideShipping();
    });
    $(document).ajaxComplete(function (_evt, _xhr, settings) {
        var url = (settings && settings.url) ? String(settings.url) : '', data = (settings && settings.data) ? String(settings.data) : '';
        if (cepUpdateActive && (url.indexOf('update_order_review') >= 0 || url.indexOf('wc-ajax=update_order_review') >= 0 || data.indexOf('update_order_review') >= 0 || data.indexOf('calc_shipping') >= 0)) scheduleRelease(FAST_HIDE_MS);
    });

    // ── Botão global desabilitado até dados mínimos preenchidos ─────────────
    // Regra única para QUALQUER checkout Senderzz: expedição, motoboy ou variações futuras.
    // Não depende do tipo de frete. Só libera a próxima etapa quando os dados mínimos existem.
    var SZ_REQUIRED_FIELDS = [
        '#billing_first_name, input[name="billing_first_name"], #billing_name, input[name="billing_name"]',
        '#billing_cpf, input[name="billing_cpf"], #billing_cnpj, input[name="billing_cnpj"]',
        '#billing_phone, input[name="billing_phone"], #billing_cellphone, input[name="billing_cellphone"]',
        '#billing_postcode, input[name="billing_postcode"]',
        '#billing_address_1, input[name="billing_address_1"]',
        '#billing_number, input[name="billing_number"], #billing_address_number, input[name="billing_address_number"]',
        '#billing_neighborhood, input[name="billing_neighborhood"], #billing_bairro, input[name="billing_bairro"]',
        '#billing_city, input[name="billing_city"]',
        '#billing_state, select[name="billing_state"]',
        '#billing_country, select[name="billing_country"]'
    ];


    function szCepCurrentlyValid() {
        var cep = getCepValue ? getCepValue() : '';
        if (!cep || cep.length !== 8) return false;
        return szCepValidation && szCepValidation.cep === cep && szCepValidation.valid === true && !szCepValidation.erro;
    }

    function szGetNextStepButton() {
        return $('.wfacp_next_page_button, .wfacp_next_step_btn, .wfacp-submit-btn, #place_order, button[name="woocommerce_checkout_place_order"]').filter(':visible').first();
    }

    function szNameIsCompound(val) {
        val = (val || '').toString().trim().replace(/\s+/g, ' ');
        var parts = val.split(' ').filter(function(p){ return p.length >= 2; });
        return parts.length >= 2;
    }

    function szFieldHasValue(sel) {
        var $fields = $(sel).filter(function() {
            var $f = $(this);
            return $f.length && !$f.closest('.hidden, [aria-hidden="true"]').length;
        });
        if (!$fields.length) return true;

        var ok = false;
        $fields.each(function() {
            var $f = $(this);
            var val = ($f.val() || '').toString().trim();
            if ($f.is(':disabled')) {
                var name = $f.attr('name');
                var backup = name ? $('input[data-sz-backup="1"][name="' + name + '"]').val() : '';
                if (backup) val = backup.toString().trim();
            }
            if (!val || val === '0') return;
            var nameAttr = ($f.attr('name') || $f.attr('id') || '').toLowerCase();
            var digits = val.replace(/\D/g,'');
            if (nameAttr.indexOf('postcode') >= 0 && digits.length !== 8) return;
            if ((nameAttr.indexOf('phone') >= 0 || nameAttr.indexOf('cellphone') >= 0) && digits.length < 10) return;
            if ((nameAttr.indexOf('cpf') >= 0 || nameAttr.indexOf('cnpj') >= 0) && digits.length && digits.length !== 11 && digits.length !== 14) return;
            if ((nameAttr.indexOf('first_name') >= 0 || nameAttr.indexOf('billing_name') >= 0) && !szNameIsCompound(val)) return;
            ok = true;
        });
        return ok;
    }

    function szRequiredDataIsComplete() {
        var allFilled = true;
        SZ_REQUIRED_FIELDS.forEach(function(sel) {
            if (!szFieldHasValue(sel)) allFilled = false;
        });
        var cep = getCepValue ? getCepValue() : '';
        if (!cep || cep.replace(/\D/g,'').length !== 8) allFilled = false;
        else if (!szCepCurrentlyValid()) allFilled = false; // CEP com 8 dígitos mas não validado/inválido
        if (SZ_IS_MOTOBOY) {
            var date = ($('#sz_delivery_date').val() || '').toString().trim();
            var dateBlockVisible = $('[data-sz-mb-date-checkout]').filter(':visible').length > 0;
            if (dateBlockVisible && !date) allFilled = false;
        }
        return allFilled;
    }

    function szSetNextButtonDisabled(disabled) {
        var $btn = szGetNextStepButton();
        if (!$btn.length) return;
        if ($btn.attr('data-sz-expedition-redirect') === '1' || $btn.hasClass('senderzz-expedition-redirect')) return;
        disabled = !!disabled;
        // v418: evita reescrever estilo/classe a cada tecla — só aplica quando o estado muda.
        // (data-attr no próprio botão: reaplica naturalmente se o FunnelKit recriar o elemento.)
        if ($btn.attr('data-sz-next-state') === (disabled ? '1' : '0')) return;
        $btn.attr('data-sz-next-state', disabled ? '1' : '0');
        $btn.prop('disabled', !!disabled)
            .attr('aria-disabled', disabled ? 'true' : 'false')
            .toggleClass('senderzz-next-disabled', !!disabled);
        if (disabled) {
            $btn.css({'opacity':'1','cursor':'not-allowed','filter':'none','pointer-events':'auto','background':'#d9d9d9','border-color':'#d9d9d9','color':'#fff'});
        } else {
            $btn.css({'opacity':'','cursor':'','filter':'','pointer-events':'','background':'linear-gradient(135deg,#ff4b00,#ff9f0a)','border-color':'#ff4b00','color':'#fff'});
        }
    }

    function szCheckRequiredFields() {
        var $btn = szGetNextStepButton();
        if (!$btn.length) return;

        var allFilled = szRequiredDataIsComplete();
        if (!allFilled) {
            szSetNextButtonDisabled(true);
            return;
        }

        if (SZ_IS_MOTOBOY && szMotoboyCobertura === false) {
            szAplicarBloqueioMotoboy(getCepValue());
            return;
        }

        // Em motoboy, além dos dados mínimos, respeita a validação de cobertura.
        if (SZ_IS_MOTOBOY) {
            if (szMotoboyCobertura === true) {
                szSetNextButtonDisabled(false);
                return;
            }
            if (szMotoboyCobertura === null) {
                var cep = getCepValue();
                if (cep.length === 8) {
                    szSetNextButtonDisabled(true);
                    szVerificarCoberturaMotoboy(cep, function(ok) {
                        if (ok && szRequiredDataIsComplete()) {
                            szSetNextButtonDisabled(false);
                        } else if (!ok) {
                            szSetNextButtonDisabled(true);
                        }
                    });
                }
                return;
            }
            szSetNextButtonDisabled(true);
            return;
        }

        szSetNextButtonDisabled(false);
    }

    // Guarda em fase de captura: impede FunnelKit/tema de trocar para a etapa 2 antes do nosso handler jQuery.
    document.addEventListener('click', function(e) {
        var target = e.target;
        if (!target || !target.closest) return;
        var hit = target.closest('.wfacp-payment-tab-list:not(.wfacp-tab1), [data-step="2"], [data-current-step="2"], .wfacp_breadcrumb_step_2, .wfacp_next_page_button, .wfacp_next_step_btn, .wfacp-submit-btn, #place_order, .wfacp_place_order, button[name="woocommerce_checkout_place_order"]');
        if (!hit) return;

        if (szShowFullNamePopup(true)) {
            e.preventDefault();
            e.stopImmediatePropagation();
            return false;
        }

        if (szPhoneShouldShowPopup()) {
            e.preventDefault();
            e.stopImmediatePropagation();
            szShowPhonePopup();
            return false;
        }

        if ($('body').hasClass('senderzz-cep-invalid-blocking')) {
            e.preventDefault();
            e.stopImmediatePropagation();
            szSetNextButtonDisabled(true);
            return false;
        }

        if (!SZ_IS_MOTOBOY || !szMotoboyCepBloqueado()) return;
        e.preventDefault();
        e.stopImmediatePropagation();
        szAplicarBloqueioMotoboy(getCepValue());
        szShowSemCobertura(getCepValue(), true);
        return false;
    }, true);

    // Bloqueio defensivo: se algum tema/plugin ignorar disabled, o clique ainda não passa.
    $( document ).on( 'click.senderzzRequiredGuard', '.wfacp_next_page_button, .wfacp_next_step_btn, .wfacp-submit-btn, #place_order, button[name="woocommerce_checkout_place_order"]', function(e) {
        if ($(this).attr('data-sz-expedition-redirect') === '1' || $(this).hasClass('senderzz-expedition-redirect')) return true;
        if (!szRequiredDataIsComplete()) {
            e.preventDefault();
            e.stopImmediatePropagation();
            if (szShowFullNamePopup(true)) {
                szSetNextButtonDisabled(true);
                return false;
            }
            if (szPhoneShouldShowPopup()) {
                szShowPhonePopup();
            }
            szNormalizePhoneValidationUi();
            szHideEnglishPhoneNotices();
            szSetNextButtonDisabled(true);
            return false;
        }
        if (SZ_IS_MOTOBOY && szMotoboyCobertura !== true) {
            e.preventDefault();
            e.stopImmediatePropagation();
            var cep = getCepValue();
            szSetNextButtonDisabled(true);
            if (cep.length === 8) {
                if (szMotoboyCobertura === false) szShowSemCobertura(cep, true);
                else szVerificarCoberturaMotoboy(cep, function(ok){ if (!ok) szShowSemCobertura(cep, true); else szCheckRequiredFields(); });
            }
            return false;
        }
    });

    $( document ).on( 'input change blur keyup', 'input, select', function() {
        neutralizePostcodeValidationUnlessRealError();
        setTimeout(szCheckRequiredFields, 80);
    });

    $( document ).ready(function() {
        // Todo checkout nasce travado até os dados mínimos estarem preenchidos.
        setTimeout(function() { szSetNextButtonDisabled(true); szCheckRequiredFields(); }, 250);
        setTimeout(szCheckRequiredFields, 800);
    });
    $( document.body ).on( 'updated_checkout wfacp_updated_checkout wfacp_reload_order_review', function() { setTimeout(szCheckRequiredFields, 300); });

    // Ao voltar para step 1: limpa campos APÓS o FunnelKit terminar de renderizar
    function szPersistCheckoutFields() {
        try {
            var data = {};
            $('input[name^="billing_"], select[name^="billing_"], textarea[name^="billing_"]').each(function(){
                var name = this.name || this.id;
                if (!name) return;
                data[name] = $(this).val();
            });
            sessionStorage.setItem('senderzz_checkout_step1_data', JSON.stringify(data));
        } catch(e) {}
    }

    function szRestoreCheckoutFields() {
        try {
            var raw = sessionStorage.getItem('senderzz_checkout_step1_data');
            if (!raw) return;
            var data = JSON.parse(raw) || {};
            Object.keys(data).forEach(function(name){
                var $el = $('[name="' + name + '"], #' + name).first();
                if (!$el.length) return;
                if ($el.val() && String($el.val()).trim() !== '') return;
                $el.val(data[name]).trigger('change');
            });
            // Se o CEP foi restaurado, verificar se a validação em cache ainda bate.
            // Caso contrário, resetar para forçar re-validação — impede que o usuário
            // chegue à etapa 2 com um CEP inválido que ficou no cache de uma visita anterior.
            var restoredCep = String(data['billing_postcode'] || data['shipping_postcode'] || '').replace(/\D/g, '');
            if (restoredCep.length === 8 && szCepValidation.cep !== restoredCep) {
                resetCepValidation(restoredCep);
                validateCepAddress(restoredCep);
            }
        } catch(e) {}
    }

    function szClearStep1() {
        // V95: não limpar dados da primeira etapa. Mantém os campos preenchidos ao navegar entre etapas.
        szPersistCheckoutFields();
        szRestoreCheckoutFields();
        szCheckRequiredFields();
    }

    // ─── Proteção do telefone contra re-render do FunnelKit ─────────────────
    var _szPhoneGuardObserver = null;
    var _szPhoneGuardActive = false;

    function szGuardPhoneFromFunnelKit() {
        try {
            var raw = sessionStorage.getItem('senderzz_checkout_step1_data');
            if (!raw) return;
            var data = JSON.parse(raw) || {};
            var phoneKeys = ['billing_phone', 'billing_cellphone'];
            phoneKeys.forEach(function(key) {
                if (!data[key]) return;
                var saved = String(data[key]);
                var $el = $('[name="' + key + '"], #' + key).first();
                if (!$el.length || $el.closest('.hidden, [aria-hidden="true"]').length) return;
                var current = String($el.val() || '');
                if (current !== saved) {
                    $el.val(saved);
                }
            });
        } catch(e) {}
    }

    // Instala MutationObserver no campo de telefone para blindar contra
    // qualquer reescrita posterior do FunnelKit (máscara, re-render parcial).
    function szInstallPhoneGuardObserver() {
        if (!window.MutationObserver) return;
        try {
            if (_szPhoneGuardObserver) { _szPhoneGuardObserver.disconnect(); _szPhoneGuardObserver = null; }
            var raw = sessionStorage.getItem('senderzz_checkout_step1_data');
            if (!raw) return;
            var data = JSON.parse(raw) || {};
            var phoneKeys = ['billing_phone', 'billing_cellphone'];
            phoneKeys.forEach(function(key) {
                if (!data[key]) return;
                var saved = String(data[key]);
                var el = document.querySelector('[name="' + key + '"], #' + key);
                if (!el) return;
                // Observa o elemento pai para detectar substituição do nó inteiro
                var parent = el.parentElement;
                if (!parent) return;
                _szPhoneGuardObserver = new MutationObserver(function() {
                    // Rebusca o campo pois o FunnelKit pode ter recriado o DOM
                    var cur = document.querySelector('[name="' + key + '"], #' + key);
                    if (!cur) return;
                    var curVal = cur.value || '';
                    // Só restaura se o valor for diferente e não estiver vazio por escolha do usuário
                    // (verificar sessionStorage atualizado)
                    try {
                        var latestRaw = sessionStorage.getItem('senderzz_checkout_step1_data');
                        var latestSaved = latestRaw ? (JSON.parse(latestRaw)[key] || '') : saved;
                        if (latestSaved && curVal !== latestSaved) {
                            cur.value = latestSaved;
                        }
                    } catch(ex) {
                        if (saved && curVal !== saved) cur.value = saved;
                    }
                });
                _szPhoneGuardObserver.observe(parent, { childList: true, subtree: true, attributes: true, attributeFilter: ['value'] });
            });
        } catch(e) {}
    }

    function szDisconnectPhoneGuardObserver() {
        if (_szPhoneGuardObserver) { _szPhoneGuardObserver.disconnect(); _szPhoneGuardObserver = null; }
    }

    $( document ).on( 'input change blur keyup', 'input[name^="billing_"], select[name^="billing_"], textarea[name^="billing_"]', function() {
        szPersistCheckoutFields();
    });

    $( document ).on( 'click.senderzzMotoboyTabGuard', '.wfacp-payment-tab-list, .wfacp-payment-tab-list *', function(e) {
        var $tab = $(this).closest('.wfacp-payment-tab-list');
        if (!$tab.length || $tab.hasClass('wfacp-tab1')) return true;
        if (!szRequiredDataIsComplete() || (SZ_IS_MOTOBOY && szMotoboyCobertura !== true) || $('body').hasClass('senderzz-cep-invalid-blocking')) {
            e.preventDefault();
            e.stopImmediatePropagation();
            szSetNextButtonDisabled(true);
            var cep = getCepValue();
            if (SZ_IS_MOTOBOY && cep.length === 8) {
                if (szMotoboyCobertura === false) szShowSemCobertura(cep, true);
                else szVerificarCoberturaMotoboy(cep, function(ok){ if (!ok) szShowSemCobertura(cep, true); });
            }
            return false;
        }
        return true;
    });

    $( document ).on( 'submit.senderzzMotoboyGuard', 'form.checkout, form.woocommerce-checkout', function(e) {
        if (SZ_IS_MOTOBOY && szMotoboyCobertura !== true) {
            e.preventDefault();
            e.stopImmediatePropagation();
            var cep = getCepValue();
            if (cep.length === 8) {
                if (szMotoboyCobertura === false) szShowSemCobertura(cep, true);
                else szVerificarCoberturaMotoboy(cep, function(ok){ if (!ok) szShowSemCobertura(cep, true); });
            }
            return false;
        }
    });

    $( document ).on( 'click', '.wfacp-payment-tab-list.wfacp-tab1', function() {
        setTimeout(function() { szRestoreCheckoutFields(); szGuardPhoneFromFunnelKit(); szCheckRequiredFields(); }, 120);
        setTimeout(function() { szRestoreCheckoutFields(); szGuardPhoneFromFunnelKit(); szCheckRequiredFields(); szInstallPhoneGuardObserver(); }, 420);
        setTimeout(function() { szGuardPhoneFromFunnelKit(); szCheckRequiredFields(); szInstallPhoneGuardObserver(); }, 900);
    });

    $( document.body ).on( 'wfacp_tab_change updated_checkout wfacp_updated_checkout wfacp_reload_order_review', function() {
        setTimeout(function(){
            szRestoreCheckoutFields(); szGuardPhoneFromFunnelKit(); szCheckRequiredFields();
            if (szMotoboyCepBloqueado()) szForceStep1WhenBlocked();
        }, 180);
        setTimeout(function(){
            szRestoreCheckoutFields(); szGuardPhoneFromFunnelKit(); szCheckRequiredFields();
            szInstallPhoneGuardObserver();
            if (szMotoboyCepBloqueado()) szForceStep1WhenBlocked();
        }, 600);
        setTimeout(function(){ szGuardPhoneFromFunnelKit(); szCheckRequiredFields(); }, 1100);
    });

    reconnectObserver();
}(jQuery));

/* ── Toggle CPF motoboy por produtor ── */
(function() {
  function szInitCpfHide() {
    if ( ! window.sz_dispensar_cpf || String(window.sz_motoboy_checkout||'0') !== '1' ) return;

    function szHideCpf() {
      var selectors = [
        '#billing_cpf_field',
        '.wfacp_billing_cpf_field',
        '[id$="_cpf_field"]',
        '[id$="-cpf-field"]',
        '.form-row[id*="cpf"]',
        'p[id*="cpf"]',
      ];
      selectors.forEach(function(sel) {
        document.querySelectorAll(sel).forEach(function(el) {
          el.style.setProperty('display', 'none', 'important');
          el.querySelectorAll('input, select').forEach(function(inp) {
            inp.removeAttribute('required');
            inp.removeAttribute('aria-required');
            inp.value = '';
          });
        });
      });
    }

    szHideCpf();
    ['wfacp_before_section_render','wfacp_after_section_render',
     'wfacp_on_load','updated_checkout','DOMContentLoaded'].forEach(function(ev) {
      document.addEventListener(ev, szHideCpf);
    });
    var _obs = new MutationObserver(function(muts) {
      muts.forEach(function(m) { if (m.addedNodes.length) szHideCpf(); });
    });
    _obs.observe(document.body || document.documentElement, { childList: true, subtree: true });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', szInitCpfHide);
  } else {
    szInitCpfHide();
  }
  setTimeout(szInitCpfHide, 500);
  setTimeout(szInitCpfHide, 1500);
})();