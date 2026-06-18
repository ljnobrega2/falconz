// Package httputil — helpers HTTP compartilhados entre serviços.
package httputil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultClient HTTP com timeout razoável para chamadas inter-serviço.
var DefaultClient = &http.Client{Timeout: 10 * time.Second}

// PostJSON faz POST JSON e decodifica a resposta em dest.
func PostJSON(ctx context.Context, client *http.Client, url string, body any, dest any) error {
	if client == nil {
		client = DefaultClient
	}
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, io.NopCloser(jsonReader(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	if dest != nil {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

// GetJSON faz GET e decodifica resposta JSON em dest.
func GetJSON(ctx context.Context, client *http.Client, url string, dest any) error {
	if client == nil {
		client = DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

type byteReader struct{ b []byte; pos int }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}

func jsonReader(b []byte) io.Reader { return &byteReader{b: b} }
