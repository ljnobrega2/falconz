<!DOCTYPE html>
<html <?php language_attributes(); ?>>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Criar conta — Senderzz</title>
<?php wp_head(); ?>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f8fafc;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px}
.szreg-wrap{width:100%;max-width:440px}
.szreg-logo{text-align:center;margin-bottom:32px}
.szreg-logo svg{width:40px;height:40px;fill:#ea580c}
.szreg-logo-text{font-size:22px;font-weight:800;color:#0f172a;letter-spacing:-.5px;margin-top:8px}
.szreg-card{background:#fff;border-radius:16px;padding:32px;box-shadow:0 4px 24px rgba(0,0,0,.08);border:1px solid rgba(0,0,0,.06)}
.szreg-title{font-size:20px;font-weight:800;color:#0f172a;margin-bottom:6px}
.szreg-sub{font-size:13px;color:#64748b;margin-bottom:24px}
.szreg-field{margin-bottom:16px}
.szreg-label{display:block;font-size:12px;font-weight:600;color:#374151;margin-bottom:6px}
.szreg-input{width:100%;padding:10px 14px;border:1.5px solid #e5e7eb;border-radius:10px;font-size:14px;color:#0f172a;outline:none;transition:border-color .15s;background:#fff}
.szreg-input:focus{border-color:#ea580c}
.szreg-row{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.szreg-btn{width:100%;padding:12px;background:#ea580c;border:none;border-radius:10px;color:#fff;font-size:14px;font-weight:700;cursor:pointer;margin-top:8px;transition:background .15s}
.szreg-btn:hover{background:#c2410c}
.szreg-btn:disabled{background:#94a3b8;cursor:not-allowed}
.szreg-msg{margin-top:12px;font-size:13px;padding:10px 14px;border-radius:8px;display:none}
.szreg-msg--ok{background:#dcfce7;color:#166534}
.szreg-msg--err{background:#fee2e2;color:#991b1b}
.szreg-login{text-align:center;margin-top:20px;font-size:13px;color:#64748b}
.szreg-login a{color:#ea580c;font-weight:600;text-decoration:none}
.szreg-invite{background:rgba(234,88,12,.06);border:1px solid rgba(234,88,12,.2);border-radius:10px;padding:12px 14px;margin-bottom:20px;font-size:12px;color:#7c3aed}
.szreg-invite strong{color:#ea580c}
<?php if ( ! empty( $sz_invite_ref ) ) : ?>
.szreg-invite{display:block}
<?php endif; ?>
</style>
</head>
<body>
<?php
$sz_invite_ref   = sanitize_text_field( wp_unslash( $_GET['ref'] ?? '' ) );
$sz_invite_token = sanitize_text_field( wp_unslash( $_GET['token'] ?? '' ) );
$sz_portal_url   = get_permalink( (int) get_option( 'senderzz_portal_page_id' ) ) ?: home_url('/');
$sz_reg_nonce    = wp_create_nonce( 'senderzz_portal' );
$sz_ajax_url     = admin_url( 'admin-ajax.php' );
?>
<div class="szreg-wrap">
    <div class="szreg-logo">
        <svg viewBox="0 0 40 40"><polygon points="20,2 38,32 2,32" opacity=".9"/></svg>
        <div class="szreg-logo-text">Senderzz</div>
    </div>

    <?php if ( $sz_invite_ref ) : ?>
    <div class="szreg-invite">
        Você foi convidado por <strong><?php echo esc_html( $sz_invite_ref ); ?></strong> para fazer parte da plataforma Senderzz. Complete o cadastro para começar.
    </div>
    <?php endif; ?>

    <div class="szreg-card">
        <div class="szreg-title">Criar sua conta</div>
        <div class="szreg-sub">Acesse produtos, checkouts e métricas de venda.</div>

        <div class="szreg-row">
            <div class="szreg-field">
                <label class="szreg-label">Nome completo</label>
                <input type="text" id="szreg-name" class="szreg-input" placeholder="Seu nome" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')">
            </div>
            <div class="szreg-field">
                <label class="szreg-label">CPF</label>
                <input type="text" id="szreg-cpf" class="szreg-input" placeholder="000.000.000-00" autocomplete="off" maxlength="14" oninput="szRegFmtCpf(this)">
            </div>
        </div>
        <div class="szreg-field">
            <label class="szreg-label">E-mail</label>
            <input type="text" id="szreg-email" class="szreg-input" placeholder="seu@email.com" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')">
        </div>
        <div class="szreg-field">
            <label class="szreg-label">Telefone</label>
            <input type="text" id="szreg-phone" class="szreg-input" placeholder="(11) 99999-9999" autocomplete="off" maxlength="15" oninput="szRegFmtPhone(this)">
        </div>
        <div class="szreg-row">
            <div class="szreg-field">
                <label class="szreg-label">Senha</label>
                <input type="password" id="szreg-pw" class="szreg-input" placeholder="Mínimo 8 caracteres" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')">
            </div>
            <div class="szreg-field">
                <label class="szreg-label">Confirmar senha</label>
                <input type="password" id="szreg-pw2" class="szreg-input" placeholder="Repita a senha" autocomplete="off" readonly onfocus="this.removeAttribute('readonly')">
            </div>
        </div>

        <div id="szreg-msg" class="szreg-msg"></div>
        <button type="button" class="szreg-btn" id="szreg-btn" onclick="szRegSubmit()">Criar conta</button>
    </div>
    <div class="szreg-login">Já tem conta? <a href="<?php echo esc_url( $sz_portal_url ); ?>">Entrar</a></div>
</div>
<script>
var SZ_REG_NONCE = '<?php echo esc_js( $sz_reg_nonce ); ?>';
var SZ_REG_AJAX  = '<?php echo esc_js( $sz_ajax_url ); ?>';
var SZ_REG_REF   = '<?php echo esc_js( $sz_invite_ref ); ?>';
var SZ_PORTAL_URL= '<?php echo esc_js( $sz_portal_url ); ?>';
function szRegFmtCpf(i){var v=i.value.replace(/\D/g,'').slice(0,11);i.value=v.replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d)/,'$1.$2').replace(/(\d{3})(\d{1,2})$/,'$1-$2');}
function szRegFmtPhone(i){var v=i.value.replace(/\D/g,'').slice(0,11);if(v.length>=2){v='('+v.slice(0,2)+') '+v.slice(2);}if(v.length>10){v=v.slice(0,10)+'-'+v.slice(10);}i.value=v;}
function szMsg(txt,ok){var m=document.getElementById('szreg-msg');m.textContent=txt;m.className='szreg-msg szreg-msg--'+(ok?'ok':'err');m.style.display=txt?'':'none';}
function szRegSubmit(){
    var name  = document.getElementById('szreg-name').value.trim();
    var cpf   = document.getElementById('szreg-cpf').value.trim();
    var email = document.getElementById('szreg-email').value.trim();
    var phone = document.getElementById('szreg-phone').value.trim();
    var pw    = document.getElementById('szreg-pw').value;
    var pw2   = document.getElementById('szreg-pw2').value;
    if (!name || !email || !pw) { szMsg('Preencha nome, e-mail e senha.',false); return; }
    if (pw.length < 8) { szMsg('Senha deve ter pelo menos 8 caracteres.',false); return; }
    if (pw !== pw2)   { szMsg('As senhas não coincidem.',false); return; }
    var btn = document.getElementById('szreg-btn'); btn.disabled = true; btn.textContent = 'Criando conta…';
    var fd = new FormData();
    fd.append('action','senderzz_portal'); fd.append('szaction','portal_register');
    fd.append('_ajax_nonce', SZ_REG_NONCE);
    fd.append('name', name); fd.append('email', email); fd.append('password', pw);
    fd.append('cpf', cpf); fd.append('phone', phone); fd.append('ref', SZ_REG_REF);
    fetch(SZ_REG_AJAX, {method:'POST',body:fd,credentials:'same-origin'})
        .then(function(r){return r.json();})
        .then(function(d){
            btn.disabled = false; btn.textContent = 'Criar conta';
            if (d && d.success) {
                szMsg('Conta criada! Redirecionando…',true);
                setTimeout(function(){ window.location.href = SZ_PORTAL_URL; }, 1500);
            } else {
                szMsg((d&&d.data&&d.data.message)||'Erro ao criar conta. Tente novamente.',false);
            }
        })
        .catch(function(){ btn.disabled=false; btn.textContent='Criar conta'; szMsg('Erro de conexão.',false); });
}
</script>
<?php wp_footer(); ?>
</body>
</html>
