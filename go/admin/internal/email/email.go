// Package email — envio de e-mail transacional via Resend (fallback SMTP).
//
// Prioridade:
//  1. RESEND_API_KEY presente → chama api.resend.com (produção).
//  2. SMTP_HOST presente      → envia via net/smtp (legado/self-hosted).
//  3. APP_ENV=development     → loga e retorna nil (sem envio real).
//  4. Nenhum configurado em produção → fail-closed (retorna erro).
package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"os"
	"strings"
)

// Send envia um e-mail HTML para um destinatário.
func Send(to, subject, htmlBody string) error {
	// 1. Resend (preferido em produção).
	if key := strings.TrimSpace(os.Getenv("RESEND_API_KEY")); key != "" {
		return sendResend(key, to, subject, htmlBody)
	}

	// 2. SMTP legado.
	if host := strings.TrimSpace(os.Getenv("SMTP_HOST")); host != "" {
		return sendSMTP(host, to, subject, htmlBody)
	}

	// 3. Dev: loga sem enviar.
	if isDev() {
		slog.Info("[email] modo DEV — e-mail NÃO enviado (logado)",
			"to", to, "subject", subject)
		return nil
	}

	// 4. Produção sem transporte configurado → fail-closed.
	return fmt.Errorf("[email] nenhum transporte configurado (RESEND_API_KEY ou SMTP_HOST) — fail-closed")
}

func sendResend(apiKey, to, subject, htmlBody string) error {
	from := strings.TrimSpace(os.Getenv("MAIL_FROM"))
	if from == "" {
		from = "contato@falklog.com.br"
	}
	body, _ := json.Marshal(map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    htmlBody,
	})
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("[email] Resend: erro ao criar request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("[email] Resend: erro na chamada HTTP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("[email] Resend: status %d: %s", resp.StatusCode, b)
	}
	slog.Info("[email] enviado via Resend", "to", to, "subject", subject)
	return nil
}

func sendSMTP(host, to, subject, htmlBody string) error {
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	pass := os.Getenv("SMTP_PASS")
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user
	}

	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	var a smtp.Auth
	if user != "" {
		a = smtp.PlainAuth("", user, pass, host)
	}
	if err := smtp.SendMail(host+":"+port, a, from, []string{to}, []byte(msg.String())); err != nil {
		return fmt.Errorf("[email] SMTP %s: %w", host, err)
	}
	slog.Info("[email] enviado via SMTP", "to", to, "subject", subject)
	return nil
}

// isDev retorna true quando APP_ENV=development (case-insensitive, trim).
func isDev() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "development")
}
