import assert from "node:assert/strict";
import test from "node:test";

import { redirectDestinationForUserAgent } from "../rotacionador-private/src/whatsapp-redirect.js";

const waLink = "https://wa.me/5513998022405?text=Ol%C3%A1%2C%20eu%20quero%20quitar%20minha%20pend%C3%AAncia.";

test("abre links wa.me diretamente pelo intent do WhatsApp no Android", () => {
  const destination = redirectDestinationForUserAgent(
    waLink,
    "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/140.0 Mobile Safari/537.36",
  );

  assert.match(destination, /^intent:\/\/send\?phone=5513998022405&text=/);
  assert.match(destination, /#Intent;scheme=whatsapp;package=com\.whatsapp;/);
  assert.match(destination, /S\.browser_fallback_url=https%3A%2F%2Fwa\.me%2F/);
  assert.match(destination, /;end$/);
});

test("abre links wa.me diretamente pelo esquema do WhatsApp no iPhone", () => {
  const destination = redirectDestinationForUserAgent(
    waLink,
    "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148",
  );

  assert.equal(
    destination,
    "whatsapp://send?phone=5513998022405&text=Ol%C3%A1%2C%20eu%20quero%20quitar%20minha%20pend%C3%AAncia.",
  );
});

test("mantém wa.me no computador e em aparelhos sem identificação móvel", () => {
  assert.equal(
    redirectDestinationForUserAgent(waLink, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"),
    waLink,
  );
});

test("não altera destinos que não são links oficiais do WhatsApp", () => {
  const destination = "https://example.com/atendimento?phone=5513998022405";
  assert.equal(redirectDestinationForUserAgent(destination, "Mozilla/5.0 (Linux; Android 14)"), destination);
});

test("aceita também o formato api.whatsapp.com/send", () => {
  const destination = redirectDestinationForUserAgent(
    "https://api.whatsapp.com/send?phone=5513998022405&text=Oi%20Lucas",
    "Mozilla/5.0 (iPad; CPU OS 18_6 like Mac OS X)",
  );

  assert.equal(destination, "whatsapp://send?phone=5513998022405&text=Oi%20Lucas");
});
