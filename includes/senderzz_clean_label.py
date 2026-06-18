#!/usr/bin/env python3
import sys, os, io
import fitz
from PIL import Image, ImageDraw

if len(sys.argv) < 4:
    print("Uso: python3 senderzz_clean_label.py input.pdf output.pdf logo.png")
    sys.exit(1)

input_pdf = sys.argv[1]
output_pdf = sys.argv[2]
logo_path = sys.argv[3]

if not os.path.exists(input_pdf):
    print(f"ERRO: PDF origem nao encontrado: {input_pdf}")
    sys.exit(1)
if not os.path.exists(logo_path):
    print(f"ERRO: Logo nao encontrado: {logo_path}")
    sys.exit(1)

doc = fitz.open(input_pdf)
new_doc = fitz.open()
zoom = 3

for page in doc:
    # localiza TODAS as ocorrencias de "melhorenvio.com" no PDF (antes do rasterize)
    url_hits = page.search_for("melhorenvio.com")

    pix = page.get_pixmap(matrix=fitz.Matrix(zoom, zoom), alpha=False)
    img = Image.open(io.BytesIO(pix.tobytes("png"))).convert("RGB")
    draw = ImageDraw.Draw(img)
    w, h = img.size
    sx = w / 768
    sy = h / 1024

    # remove Melhor Envio (header) — sem mexer no peso
    draw.rectangle(
        (int(515 * sx), int(55 * sy), int(755 * sx), int(165 * sy)),
        fill="white"
    )
    draw.rectangle(
        (int(515 * sx), int(118 * sy), int(700 * sx), int(165 * sy)),
        fill="white"
    )

    # corrige borda superior
    draw.line(
        (int(378 * sx), int(38 * sy), int(755 * sx), int(38 * sy)),
        fill="black",
        width=max(2, int(2 * sy))
    )

    # apaga cirurgicamente cada ocorrencia de "melhorenvio.com" onde quer que esteja
    pad = 3
    for rect in url_hits:
        x0 = int(rect.x0 * zoom) - pad
        y0 = int(rect.y0 * zoom) - pad
        x1 = int(rect.x1 * zoom) + pad
        y1 = int(rect.y1 * zoom) + pad
        draw.rectangle((x0, y0, x1, y1), fill="white")

    # insere logo Senderzz
    logo = Image.open(logo_path).convert("RGBA")
    logo.thumbnail((int(200 * sx), int(55 * sy)), Image.LANCZOS)
    img.paste(logo, (int(545 * sx), int(74 * sy)), logo)

    # exporta
    img_bytes = io.BytesIO()
    img.save(img_bytes, format="PNG")
    img_bytes.seek(0)
    new_page = new_doc.new_page(width=page.rect.width, height=page.rect.height)
    new_page.insert_image(page.rect, stream=img_bytes.getvalue())

new_doc.save(output_pdf)
new_doc.close()
doc.close()
print("OK")