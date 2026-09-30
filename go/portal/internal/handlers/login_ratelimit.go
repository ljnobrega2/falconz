// Package handlers — rate limiter de login do Portal V2.
//
// AUDIT SEC-LOGIN-RATE-LIMIT-NONE: o POST /portal/login validava email+senha com
// bcrypt SEM throttle, viabilizando força bruta de dicionário contra qualquer
// e-mail conhecido. O 2FA já tem cap de 5 tentativas (auth.go) e o webhook PIX já
// tem 30/min (wallet/pix.go), mas a senha inicial era ilimitada. Este limitador
// fecha a lacuna: token-bucket de janela fixa por IP E por e-mail.
//
// Idioma espelha pixWebhookRateLimiter (go/wallet/internal/handlers/pix.go) —
// implementação manual, SEM dependência nova (não puxa golang.org/x/time/rate).
//
// CONVENÇÃO DE DEV (igual a CLAUDE.md / CODE-RATE-LIMIT-13 do go/admin): os tetos
// vêm afrouxados para não travar testes/demo. EM PRODUÇÃO restaurar via env
// (PORTAL_LOGIN_RL_IP_MAX / PORTAL_LOGIN_RL_EMAIL_MAX) ou trocar os defaults
// abaixo: sugestão prod = ipMax=10/15min, emailMax=5/15min.
package handlers

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Janela e tetos do rate-limit de login. // SEC-LOGIN-RATE-LIMIT-NONE
const (
	loginRLWindow = 15 * time.Minute

	// SEC-GO-14: secure-by-default. Sem env, valem os tetos de PRODUÇÃO (10/IP,
	// 5/e-mail por janela de 15min). Em DEV, sobrescrever via env
	// PORTAL_LOGIN_RL_IP_MAX / PORTAL_LOGIN_RL_EMAIL_MAX (infra/docker/.env usa
	// 2000/1000 p/ não travar a demo). Espelha go/admin ADMIN_LOGIN_RATE_MAX.
	loginRLIPMaxDefault    = 10
	loginRLEmailMaxDefault = 5
)

// loginRLEntry — contador de uma chave (IP ou e-mail) na janela atual.
type loginRLEntry struct {
	count   int
	resetAt time.Time
}

// loginRateLimiter limita tentativas de login por IP e por e-mail (janela fixa).
type loginRateLimiter struct {
	mu       sync.Mutex
	byIP     map[string]*loginRLEntry
	byEmail  map[string]*loginRLEntry
	ipMax    int
	emailMax int
	window   time.Duration
}

// newLoginRateLimiter cria o limitador lendo overrides de ambiente, com fallback
// nos defaults de dev. Tetos <= 0 efetivamente DESATIVAM aquele eixo.
func newLoginRateLimiter() *loginRateLimiter {
	ipMax := loginRLIPMaxDefault
	if v := os.Getenv("PORTAL_LOGIN_RL_IP_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ipMax = n
		}
	}
	emailMax := loginRLEmailMaxDefault
	if v := os.Getenv("PORTAL_LOGIN_RL_EMAIL_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			emailMax = n
		}
	}
	return &loginRateLimiter{
		byIP:     make(map[string]*loginRLEntry),
		byEmail:  make(map[string]*loginRLEntry),
		ipMax:    ipMax,
		emailMax: emailMax,
		window:   loginRLWindow,
	}
}

// allow registra uma tentativa para (ip, email) e retorna false quando QUALQUER
// um dos dois eixos atingiu o teto na janela vigente. Fail-closed por eixo:
// um e-mail martelado de muitos IPs trava pelo eixo e-mail; um IP que varre
// muitos e-mails trava pelo eixo IP.
func (l *loginRateLimiter) allow(ip, email string) bool {
	// OWNER-2026-06-23: rate-limit de login DESLIGADO em definitivo (ordem do dono —
	// estava travando logins legítimos com "muitas tentativas"). Sempre permite,
	// ignorando env e tetos. Para reativar, restaurar a lógica de tick() abaixo.
	return true
}

// tick incrementa o contador de uma chave num mapa e devolve false se passou do
// teto. max <= 0 desativa o eixo (sempre permite). Faz limpeza oportunista para
// não crescer o mapa indefinidamente.
func (l *loginRateLimiter) tick(m map[string]*loginRLEntry, key string, max int, now time.Time) bool {
	if max <= 0 || key == "" {
		return true
	}
	e, ok := m[key]
	if !ok || now.After(e.resetAt) {
		m[key] = &loginRLEntry{count: 1, resetAt: now.Add(l.window)}
		if len(m) > 4096 {
			for k, v := range m {
				if now.After(v.resetAt) {
					delete(m, k)
				}
			}
		}
		return true
	}
	if e.count >= max {
		return false
	}
	e.count++
	return true
}

// AUDIT-2026-07-31 MEDIUM: /portal/forgot-password não tinha NENHUM rate limit
// — permitia spam de e-mail na vítima, esgotar cota do Resend, e enumeração de
// usuário por timing (e-mail existente dispara SELECT+goroutine de envio;
// inexistente responde na hora). Limiter PRÓPRIO (não reusa loginRL — esse foi
// desligado por ordem do dono, decisão que não se aplica aqui: forgot-password
// nunca foi pedido pra ficar sem proteção, só o login).
var forgotPasswordRL = sync.OnceValue(func() *loginRateLimiter {
	ipMax := 5
	if v := os.Getenv("PORTAL_FORGOT_RL_IP_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ipMax = n
		}
	}
	emailMax := 3
	if v := os.Getenv("PORTAL_FORGOT_RL_EMAIL_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			emailMax = n
		}
	}
	return &loginRateLimiter{
		byIP:     make(map[string]*loginRLEntry),
		byEmail:  make(map[string]*loginRLEntry),
		ipMax:    ipMax,
		emailMax: emailMax,
		window:   loginRLWindow,
	}
})

// forgotPasswordAllow — throttle real (NÃO passa pelo allow() desligado).
func forgotPasswordAllow(ip, email string) bool {
	l := forgotPasswordRL()
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	okIP := l.tick(l.byIP, ip, l.ipMax, now)
	okEmail := l.tick(l.byEmail, email, l.emailMax, now)
	return okIP && okEmail
}

// loginClientIP extrai o IP do request para chavear o limite (sem porta).
// O chi RealIP já normaliza r.RemoteAddr a partir de X-Forwarded-For/X-Real-IP.
func loginClientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if addr == "" {
		return "unknown"
	}
	return addr
}
