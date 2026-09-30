// Package track17 integra a API v2.4 do 17TRACK para eventos detalhados da
// transportadora. A chave nunca fica no código: é lida de TRACKING17_TOKEN.
package track17

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.17track.net/track/v2.4"
	JadlogCarrier  = 101052
	LoggiCarrier   = 100457
	CorreiosCarrier = 2151
)

type Client struct {
	token string
	base  string
	http  *http.Client
	mu    sync.Mutex
	last  time.Time
}

func NewClient() *Client {
	return &Client{
		token: strings.TrimSpace(os.Getenv("TRACKING17_TOKEN")),
		base:  strings.TrimRight(os.Getenv("TRACKING17_BASE_URL"), "/"),
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

type Event struct {
	Time        time.Time
	Description string
	Location    string
	Stage       string
}

type apiResponse struct {
	Code int `json:"code"`
	Data struct {
		Accepted []struct {
			Number    string `json:"number"`
			Carrier   int    `json:"carrier"`
			TrackInfo *struct {
				Tracking struct {
					Providers []struct {
						Events []struct {
							TimeISO     string `json:"time_iso"`
							Description string `json:"description"`
							Stage       string `json:"stage"`
							Location    string `json:"location"`
						} `json:"events"`
					} `json:"providers"`
				} `json:"tracking"`
			} `json:"track_info"`
		} `json:"accepted"`
		Rejected []struct {
			Number string `json:"number"`
			Error  struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"rejected"`
	} `json:"data"`
}

func (c *Client) enabled() bool { return c != nil && c.token != "" }

func (c *Client) call(ctx context.Context, path string, payload any) (*apiResponse, error) {
	if !c.enabled() {
		return nil, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("17track marshal: %w", err)
	}
	base := c.base
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("17track request: %w", err)
	}
	req.Header.Set("17token", c.token)
	req.Header.Set("Content-Type", "application/json")
	// O plano do 17TRACK limita chamadas concorrentes. Como o worker processa
	// várias etiquetas em paralelo, serializamos as consultas deste cliente e
	// deixamos um pequeno intervalo entre requisições.
	c.mu.Lock()
	if wait := 600*time.Millisecond - time.Since(c.last); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			c.mu.Unlock()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	resp, err := c.http.Do(req)
	c.last = time.Now()
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("17track http: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("17track read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("17track http status %d", resp.StatusCode)
	}
	var out apiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("17track decode: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("17track api code %d", out.Code)
	}
	return &out, nil
}

// Sync mantém compatibilidade com os chamadores antigos e consulta Jadlog.
func (c *Client) Sync(ctx context.Context, number string) ([]Event, error) {
	return c.SyncCarrier(ctx, number, JadlogCarrier)
}

// SyncCarrier registra o código na 17TRACK e devolve os eventos já disponíveis.
// O carrier precisa ser informado explicitamente: a autodetecção pode confundir
// códigos numéricos ou códigos alfanuméricos de transportadoras privadas.
func (c *Client) SyncCarrier(ctx context.Context, number string, carrier int) ([]Event, error) {
	if !c.enabled() || strings.TrimSpace(number) == "" {
		return nil, nil
	}
	number = strings.TrimSpace(number)
	if _, err := c.call(ctx, "/register", []map[string]any{{"number": number, "carrier": carrier}}); err != nil {
		// Código já registrado também pode ser consultado; o endpoint de leitura
		// abaixo é a fonte útil para o worker.
	}
	res, err := c.call(ctx, "/gettrackinfo", []map[string]any{{"number": number, "carrier": carrier}})
	if err != nil {
		return nil, err
	}
	if res == nil || len(res.Data.Accepted) == 0 || res.Data.Accepted[0].TrackInfo == nil {
		return nil, nil
	}
	var events []Event
	for _, provider := range res.Data.Accepted[0].TrackInfo.Tracking.Providers {
		for _, ev := range provider.Events {
			t, err := time.Parse(time.RFC3339, ev.TimeISO)
			if err != nil || strings.TrimSpace(ev.Description) == "" {
				continue
			}
			events = append(events, Event{Time: t, Description: strings.TrimSpace(ev.Description), Location: strings.TrimSpace(ev.Location), Stage: strings.TrimSpace(ev.Stage)})
		}
	}
	return events, nil
}
