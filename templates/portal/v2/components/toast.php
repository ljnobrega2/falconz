<?php
/**
 * Componente: toast holder (Fase 1).
 * O JS expõe window.szV2Toast(msg, kind). O singleton legado
 * #toast do PWA não é tocado (PWA fora desta flag).
 */
defined( 'ABSPATH' ) || exit;

if ( ! function_exists( 'sz_v2_toast_holder' ) ) {
    function sz_v2_toast_holder(): string {
        return '<div class="szv2-toasts" aria-live="polite" aria-atomic="false"></div>';
    }
}
