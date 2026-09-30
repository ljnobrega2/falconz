// Upload de imagem de produto (sz_products.meta->>'image_url').
//
// Espelha o padrão de UploadProducerProof (cod_saques.go): ParseMultipartForm,
// validação por MAGIC BYTES (http.DetectContentType — não pelo Content-Type do
// cliente, que é spoofável), nome crypto/rand, grava em disco e devolve a URL
// pública. A diferença: aceita SÓ imagem (jpeg/png/webp) e o nome NÃO carrega id
// (o upload acontece ANTES do produto existir — o front recebe a URL e a persiste
// em meta.image_url no Create/Update). A URL retornada é a mesma chave que o
// checkout/portal lêem (productImageExpr) → a miniatura aparece no checkout.
//
// POST /products/upload-image  multipart: image
package handlers

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/senderzz/admin-service/internal/httpx"
)

// ProductImgDir retorna o diretório local onde imagens de produto são gravadas.
// Configurável via PRODUCT_IMG_UPLOAD_PATH. Default: ./uploads/products
//
// EXPORTADO porque o main.go monta o file-server estático público destas imagens
// (r.Handle("/uploads/products/*", …)) e precisa apontar para o mesmo diretório.
func ProductImgDir() string {
	if v := strings.TrimSpace(os.Getenv("PRODUCT_IMG_UPLOAD_PATH")); v != "" {
		return v
	}
	return "./uploads/products"
}

// productImgURL retorna o prefixo público das URLs das imagens de produto.
// Configurável via PRODUCT_IMG_UPLOAD_URL. Default: /uploads/products/
//
// Estas imagens são PÚBLICAS (storefront): aparecem no checkout/vitrine, servidas
// como estático (file-server no main.go + infra de proxy). Por isso não levam
// Content-Disposition: attachment como os comprovantes COD (que são privados).
func productImgURL() string {
	if v := strings.TrimSpace(os.Getenv("PRODUCT_IMG_UPLOAD_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/"
	}
	return "/uploads/products/"
}

// UploadImage recebe uma imagem via multipart (campo "image") e devolve a URL pública.
// Espelha UploadProducerProof (cod_saques.go:711) — só muda o conjunto de tipos
// aceitos (jpeg/png/webp), o teto (8MB) e o nome (sem id; upload antecede o produto).
//
// POST /products/upload-image  multipart: image
func (h *ProductsHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	// 12 MB de teto de parse (memória/spill); o limite real de 8MB é checado pelo
	// header.Size abaixo (ParseMultipartForm não é hard cap).
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		httpx.Err(w, 400, "bad_request", "multipart inválido: "+err.Error())
		return
	}

	file, header, ferr := r.FormFile("image")
	if ferr != nil {
		httpx.Err(w, 400, "file_missing", "campo image obrigatório")
		return
	}
	defer file.Close()

	if header.Size > 8<<20 {
		httpx.Err(w, 413, "file_too_large", "imagem excede 8MB")
		return
	}

	// Valida por MAGIC BYTES (não pelo Content-Type do cliente, spoofável).
	// http.DetectContentType lê os primeiros 512 bytes e assina o tipo real. Só
	// aceita jpeg/png/webp — qualquer outro tipo é rejeitado (400).
	sniff := make([]byte, 512)
	n, rerr := io.ReadFull(file, sniff)
	if rerr != nil && rerr != io.ErrUnexpectedEOF && rerr != io.EOF {
		httpx.Err(w, 400, "file_invalid", "não foi possível ler a imagem: "+rerr.Error())
		return
	}
	mime := http.DetectContentType(sniff[:n])

	// Rebobina após o sniff, senão os 512 bytes lidos somem do conteúdo gravado
	// e a imagem fica corrompida.
	if _, serr := file.Seek(0, io.SeekStart); serr != nil {
		httpx.Err(w, 500, "upload_error", "falha ao reposicionar o arquivo: "+serr.Error())
		return
	}

	// Aceite só jpeg/png/webp (match exato — DetectContentType devolve exatamente
	// estes para as assinaturas correspondentes). Extensão derivada do tipo real,
	// não do nome do arquivo enviado pelo cliente (também spoofável).
	var ext string
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		httpx.Err(w, 400, "file_invalid", "imagem precisa ser JPEG, PNG ou WEBP (detectado: "+mime+")")
		return
	}

	dir := ProductImgDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("mkdir uploads: %v", err))
		return
	}

	// Nome NÃO-previsível (anti-IDOR) — crypto/rand, não UnixNano. Sem id: o upload
	// acontece antes do produto existir (o front guarda a URL e persiste no save).
	rb := make([]byte, 16)
	if _, err := crand.Read(rb); err != nil {
		httpx.Err(w, 500, "upload_error", "falha ao gerar nome seguro")
		return
	}
	name := fmt.Sprintf("product-%s%s", hex.EncodeToString(rb), ext)
	full := filepath.Join(dir, name)

	dst, err := os.Create(full)
	if err != nil {
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("criar arquivo: %v", err))
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(full)
		httpx.Err(w, 500, "upload_error", fmt.Sprintf("gravar arquivo: %v", err))
		return
	}

	httpx.JSON(w, 200, map[string]any{"ok": true, "url": productImgURL() + name})
}
