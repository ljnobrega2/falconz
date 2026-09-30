// Package email — envio mínimo de e-mail transacional via SMTP (stdlib net/smtp).
//
// AUDIT-2026-06-22 forgot-pw — infraestrutura de e-mail nasce aqui para o fluxo
// de recuperação de senha do admin (forgot/reset). Não há fila/retry: é envio
// síncrono best-effort. O caller (ForgotPassword) NUNCA propaga falha de envio
// ao usuário final (anti-enumeração), apenas loga.
//
// Modo DEV (APP_ENV=development) com SMTP_HOST vazio: NÃO falha — loga o conteúdo
// via slog e retorna nil, para que o fluxo de teste rode sem servidor SMTP.
// Em PRODUÇÃO com SMTP_HOST vazio: retorna erro (fail-closed) — nunca finge que
// enviou um e-mail de segurança quando não há transporte.
//
// Este arquivo é a versão CANÔNICA. Cada serviço Go que precisa enviar e-mail
// mantém uma cópia em internal/email (o build Docker per-serviço usa contexto
// próprio e não enxerga o módulo shared — mesmo padrão de httpx/security_headers).
// Mantenha as cópias em sincronia.
package email

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
)

// Send envia um e-mail HTML para um destinatário.
//
// Envs:
//   SMTP_HOST, SMTP_PORT (default 587), SMTP_USER, SMTP_PASS, SMTP_FROM.
//
// Regras (AUDIT-2026-06-22 forgot-pw):
//   - SMTP_HOST vazio + APP_ENV=development → loga (slog) e retorna nil (modo dev).
//   - SMTP_HOST vazio em produção            → retorna erro (fail-closed).
//   - SMTP_HOST presente                     → envia via net/smtp com AUTH PLAIN
//     (quando SMTP_USER/SMTP_PASS presentes).
func Send(to, subject, htmlBody string) error {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))

	if host == "" {
		// Modo DEV: sem SMTP, não falha — só loga o conteúdo para inspeção.
		if isDev() {
			slog.Info("[email] modo DEV — SMTP_HOST vazio, e-mail NÃO enviado (logado)",
				"to", to, "subject", subject, "body", htmlBody)
			return nil
		}
		// Produção sem SMTP = fail-closed.
		return fmt.Errorf("[email] SMTP_HOST não configurado — fail-closed (produção)")
	}

	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	pass := os.Getenv("SMTP_PASS")
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user // fallback: usa o usuário SMTP como remetente.
	}

	addr := host + ":" + port

	// Cabeçalhos MIME — sem isto o cliente renderiza o HTML como texto plano.
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	if err := smtp.SendMail(addr, auth, from, []string{to}, []byte(msg.String())); err != nil {
		return fmt.Errorf("[email] falha ao enviar via SMTP %s: %w", addr, err)
	}
	slog.Info("[email] enviado", "to", to, "subject", subject)
	return nil
}

// isDev retorna true quando APP_ENV=development (case-insensitive, trim).
func isDev() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "development")
}
