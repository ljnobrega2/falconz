import assert from "node:assert/strict";
import test from "node:test";

import worker from "../rotacionador/_worker.js";

test("página pública cria links sem disparar autenticação de estatísticas", async () => {
  const response = await worker.fetch(new Request("https://rotacionador.pages.dev/"), {});
  const html = await response.text();

  assert.equal(response.status, 200);
  assert.equal(response.headers.has("WWW-Authenticate"), false);
  assert.match(html, /\/api\/public\/campaigns/);
  assert.match(html, /href="\/painel"[^>]*>Abrir painel<\/a>/);
  assert.doesNotMatch(html, /Histórico de campanhas|Últimos acessos/);
  assert.doesNotMatch(html, /addLink\(\);addLink\(\);load\(\);/);
});

test("criação pública, painel e estatísticas não exigem senha", async () => {
  const originalFetch = globalThis.fetch;
  const forwarded = [];
  globalThis.fetch = async (url, init) => {
    forwarded.push({ url: String(url), method: init.method });
    if (String(url).endsWith("/stats")) return Response.json({ campaigns: [], recent: [] });
    return Response.json({ slug: "campanha-publica" }, { status: 201 });
  };

  try {
    const createResponse = await worker.fetch(new Request("https://rotacionador.pages.dev/api/public/campaigns", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Campanha pública", destinations: [{ name: "Grupo", url: "https://example.com" }] }),
    }), {});
    const statsResponse = await worker.fetch(new Request("https://rotacionador.pages.dev/api/stats"), {});
    const panelResponse = await worker.fetch(new Request("https://rotacionador.pages.dev/painel"), {});

    assert.equal(createResponse.status, 201);
    assert.deepEqual(forwarded, [
      {
        url: "https://rotacionador-counter.lucasjesusnobrega.workers.dev/campaigns",
        method: "POST",
      },
      {
        url: "https://rotacionador-counter.lucasjesusnobrega.workers.dev/stats",
        method: "GET",
      },
    ]);
    assert.equal(statsResponse.status, 200);
    assert.equal(statsResponse.headers.has("WWW-Authenticate"), false);
    assert.equal(panelResponse.status, 200);
    assert.equal(panelResponse.headers.has("WWW-Authenticate"), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
