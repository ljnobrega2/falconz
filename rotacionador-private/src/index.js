import { DurableObject } from "cloudflare:workers";
import { APP_HTML } from "./app.js";
import { destinationRedirectResponse } from "./whatsapp-redirect.js";

const encoder = new TextEncoder();
const SESSION_COOKIE = "rotacionador_private_session";
const SESSION_SECONDS = 60 * 60 * 24 * 30;

function json(data, status = 200, extraHeaders = {}) {
  return Response.json(data, {
    status,
    headers: { "Cache-Control": "no-store", ...extraHeaders },
  });
}

function bytesToHex(value) {
  return [...new Uint8Array(value)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function randomHex(bytes) {
  const value = new Uint8Array(bytes);
  crypto.getRandomValues(value);
  return bytesToHex(value);
}

async function sha256(value) {
  return bytesToHex(await crypto.subtle.digest("SHA-256", encoder.encode(value)));
}

async function passwordDigest(email, password, salt, pepper) {
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(pepper),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  return bytesToHex(await crypto.subtle.sign("HMAC", key, encoder.encode(`${email}\0${salt}\0${password}`)));
}

function safeEqualHex(left, right) {
  if (typeof left !== "string" || typeof right !== "string" || left.length !== right.length || left.length % 2 !== 0) return false;
  const a = new Uint8Array(left.match(/.{2}/g).map((part) => Number.parseInt(part, 16)));
  const b = new Uint8Array(right.match(/.{2}/g).map((part) => Number.parseInt(part, 16)));
  return crypto.subtle.timingSafeEqual(a, b);
}

function normalizedEmail(value) {
  return typeof value === "string" ? value.trim().toLowerCase() : "";
}

function validEmail(email) {
  return email.length <= 254 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
}

function cookieValue(request, name) {
  const cookie = request.headers.get("Cookie") || "";
  const match = cookie.match(new RegExp(`(?:^|;\\s*)${name}=([^;]+)`));
  return match ? decodeURIComponent(match[1]) : null;
}

function sessionCookie(request, value, maxAge) {
  const secure = new URL(request.url).protocol === "https:" ? "; Secure" : "";
  return `${SESSION_COOKIE}=${encodeURIComponent(value)}; Path=/; HttpOnly; SameSite=Strict; Max-Age=${maxAge}${secure}`;
}

function sameOrigin(request) {
  const origin = request.headers.get("Origin");
  return origin === new URL(request.url).origin;
}

async function bodyJson(request) {
  const length = Number(request.headers.get("Content-Length") || "0");
  if (length > 65536) throw new Error("Corpo muito grande.");
  try {
    return await request.json();
  } catch {
    throw new Error("Dados inválidos.");
  }
}

function publicHeaders() {
  return {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
    "Referrer-Policy": "no-referrer",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  };
}

export class AccountDirectory extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS users (
          id TEXT PRIMARY KEY,
          email TEXT NOT NULL UNIQUE,
          password_hash TEXT NOT NULL,
          password_salt TEXT NOT NULL,
          created_at TEXT NOT NULL
        );
        CREATE TABLE IF NOT EXISTS sessions (
          token_hash TEXT PRIMARY KEY,
          user_id TEXT NOT NULL,
          expires_at INTEGER NOT NULL
        );
        CREATE INDEX IF NOT EXISTS sessions_user_id ON sessions(user_id);
        CREATE TABLE IF NOT EXISTS routes (
          slug TEXT PRIMARY KEY,
          user_id TEXT NOT NULL
        );
        CREATE TABLE IF NOT EXISTS rate_limits (
          bucket TEXT PRIMARY KEY,
          attempts INTEGER NOT NULL,
          window_started INTEGER NOT NULL
        );
      `);
    });
  }

  allowAttempt(bucket, limit, windowSeconds) {
    const now = Math.floor(Date.now() / 1000);
    const row = this.ctx.storage.sql.exec("SELECT attempts, window_started FROM rate_limits WHERE bucket = ?", bucket).toArray()[0];
    if (!row || now - row.window_started >= windowSeconds) {
      this.ctx.storage.sql.exec("INSERT OR REPLACE INTO rate_limits (bucket, attempts, window_started) VALUES (?, 1, ?)", bucket, now);
      return true;
    }
    if (row.attempts >= limit) return false;
    this.ctx.storage.sql.exec("UPDATE rate_limits SET attempts = attempts + 1 WHERE bucket = ?", bucket);
    return true;
  }

  register(user) {
    if (this.ctx.storage.sql.exec("SELECT id FROM users WHERE email = ?", user.email).toArray()[0]) return { error: "Este e-mail já possui conta." };
    this.ctx.storage.sql.exec(
      "INSERT INTO users (id, email, password_hash, password_salt, created_at) VALUES (?, ?, ?, ?, ?)",
      user.id, user.email, user.passwordHash, user.passwordSalt, user.createdAt,
    );
    return { user: { id: user.id, email: user.email } };
  }

  authRecord(email) {
    return this.ctx.storage.sql.exec("SELECT id, email, password_hash, password_salt FROM users WHERE email = ?", email).toArray()[0] || null;
  }

  createSession(tokenHash, userId, expiresAt) {
    this.ctx.storage.sql.exec("DELETE FROM sessions WHERE expires_at <= ?", Math.floor(Date.now() / 1000));
    this.ctx.storage.sql.exec("INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)", tokenHash, userId, expiresAt);
  }

  session(tokenHash) {
    const now = Math.floor(Date.now() / 1000);
    return this.ctx.storage.sql.exec(
      "SELECT users.id, users.email FROM sessions JOIN users ON users.id = sessions.user_id WHERE sessions.token_hash = ? AND sessions.expires_at > ?",
      tokenHash, now,
    ).toArray()[0] || null;
  }

  deleteSession(tokenHash) {
    this.ctx.storage.sql.exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash);
  }

  allocateRoute(userId) {
    for (let attempt = 0; attempt < 5; attempt += 1) {
      const slug = randomHex(7);
      if (!this.ctx.storage.sql.exec("SELECT slug FROM routes WHERE slug = ?", slug).toArray()[0]) {
        this.ctx.storage.sql.exec("INSERT INTO routes (slug, user_id) VALUES (?, ?)", slug, userId);
        return slug;
      }
    }
    throw new Error("Não foi possível gerar o link.");
  }

  resolveRoute(slug) {
    return this.ctx.storage.sql.exec("SELECT user_id FROM routes WHERE slug = ?", slug).toArray()[0]?.user_id || null;
  }
}

export class UserCampaigns extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS campaigns (
          slug TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          mode TEXT NOT NULL,
          destinations TEXT NOT NULL,
          position INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL
        );
        CREATE TABLE IF NOT EXISTS access_log (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          campaign_slug TEXT NOT NULL,
          group_index INTEGER NOT NULL,
          accessed_at TEXT NOT NULL,
          country TEXT,
          city TEXT
        );
        CREATE INDEX IF NOT EXISTS access_log_campaign ON access_log(campaign_slug);
      `);
    });
  }

  createCampaign(campaign) {
    this.ctx.storage.sql.exec(
      "INSERT INTO campaigns (slug, name, mode, destinations, created_at) VALUES (?, ?, ?, ?, ?)",
      campaign.slug, campaign.name, campaign.mode, JSON.stringify(campaign.destinations), campaign.createdAt,
    );
    return { slug: campaign.slug };
  }

  stats() {
    const campaigns = this.ctx.storage.sql.exec("SELECT slug, name, mode, destinations, created_at FROM campaigns ORDER BY created_at DESC").toArray();
    const counts = this.ctx.storage.sql.exec("SELECT campaign_slug, group_index, COUNT(*) total FROM access_log GROUP BY campaign_slug, group_index").toArray();
    const byCampaign = {};
    for (const count of counts) {
      byCampaign[count.campaign_slug] ||= {};
      byCampaign[count.campaign_slug][count.group_index] = count.total;
    }
    const hydrated = campaigns.map((campaign) => {
      const destinations = JSON.parse(campaign.destinations).map((destination, index) => ({
        ...destination,
        accesses: byCampaign[campaign.slug]?.[index] || 0,
      }));
      return { ...campaign, destinations, accesses: destinations.reduce((sum, destination) => sum + destination.accesses, 0) };
    });
    const bySlug = Object.fromEntries(hydrated.map((campaign) => [campaign.slug, campaign]));
    const recent = this.ctx.storage.sql.exec("SELECT campaign_slug, group_index, accessed_at, country, city FROM access_log ORDER BY id DESC LIMIT 100").toArray().map((item) => ({
      ...item,
      campaign_name: bySlug[item.campaign_slug]?.name || item.campaign_slug,
      destination_name: bySlug[item.campaign_slug]?.destinations[item.group_index]?.name || null,
    }));
    return { campaigns: hydrated, recent };
  }

  next(slug, location) {
    const campaign = this.ctx.storage.sql.exec("SELECT mode, destinations, position FROM campaigns WHERE slug = ?", slug).toArray()[0];
    if (!campaign) return null;
    const destinations = JSON.parse(campaign.destinations);
    if (!destinations.length) return null;
    const index = campaign.mode === "random" ? Math.floor(Math.random() * destinations.length) : campaign.position % destinations.length;
    this.ctx.storage.sql.exec("UPDATE campaigns SET position = ? WHERE slug = ?", (index + 1) % destinations.length, slug);
    this.ctx.storage.sql.exec(
      "INSERT INTO access_log (campaign_slug, group_index, accessed_at, country, city) VALUES (?, ?, ?, ?, ?)",
      slug, index, new Date().toISOString(), location.country, location.city,
    );
    return destinations[index].url;
  }
}

function directory(env) {
  return env.DIRECTORY.getByName("accounts");
}

async function authenticatedUser(request, env) {
  const token = cookieValue(request, SESSION_COOKIE);
  if (!token || !/^[a-f0-9]{64}$/.test(token)) return null;
  return directory(env).session(await sha256(token));
}

function validateCampaign(input) {
  const name = typeof input?.name === "string" ? input.name.trim() : "";
  const mode = input?.mode === "random" ? "random" : "rotation";
  if (!name || name.length > 100) return { error: "Informe um nome válido." };
  if (!Array.isArray(input?.destinations) || input.destinations.length < 1 || input.destinations.length > 20) return { error: "Adicione de 1 a 20 destinos." };
  const destinations = [];
  for (const item of input.destinations) {
    const itemName = typeof item?.name === "string" ? item.name.trim() : "";
    const value = typeof item?.url === "string" ? item.url.trim() : "";
    let url;
    try { url = new URL(value); } catch { return { error: "Há um link de destino inválido." }; }
    if (!['https:', 'http:'].includes(url.protocol) || value.length > 2048 || itemName.length > 80) return { error: "Há um destino inválido." };
    destinations.push({ name: itemName || `Destino ${destinations.length + 1}`, url: url.toString() });
  }
  return { campaign: { name, mode, destinations } };
}

async function handleAuth(request, env, action) {
  if (!sameOrigin(request)) return json({ error: "Origem inválida." }, 403);
  const body = await bodyJson(request);
  const email = normalizedEmail(body.email);
  const password = typeof body.password === "string" ? body.password : "";
  if (!validEmail(email) || password.length < 12 || password.length > 128) return json({ error: "Use um e-mail válido e uma senha com pelo menos 12 caracteres." }, 400);
  if (!env.AUTH_PEPPER || env.AUTH_PEPPER.length < 32) return json({ error: "Autenticação indisponível." }, 503);
  const ipKey = await sha256(request.headers.get("CF-Connecting-IP") || "local");
  if (!await directory(env).allowAttempt(`${action}:${ipKey}`, action === "register" ? 8 : 20, 900)) return json({ error: "Muitas tentativas. Aguarde alguns minutos." }, 429);

  let user;
  if (action === "register") {
    const salt = randomHex(16);
    const result = await directory(env).register({
      id: crypto.randomUUID(), email, passwordSalt: salt,
      passwordHash: await passwordDigest(email, password, salt, env.AUTH_PEPPER),
      createdAt: new Date().toISOString(),
    });
    if (result.error) return json({ error: result.error }, 409);
    user = result.user;
  } else {
    const record = await directory(env).authRecord(email);
    const salt = record?.password_salt || randomHex(16);
    const candidate = await passwordDigest(email, password, salt, env.AUTH_PEPPER);
    if (!record || !safeEqualHex(candidate, record.password_hash)) return json({ error: "E-mail ou senha incorretos." }, 401);
    user = { id: record.id, email: record.email };
  }

  const token = randomHex(32);
  await directory(env).createSession(await sha256(token), user.id, Math.floor(Date.now() / 1000) + SESSION_SECONDS);
  return json({ user }, 200, { "Set-Cookie": sessionCookie(request, token, SESSION_SECONDS) });
}

async function handleRequest(request, env) {
  const url = new URL(request.url);
  if (request.method === "GET" && url.pathname === "/") return new Response(APP_HTML, { headers: publicHeaders() });

  const route = url.pathname.match(/^\/r\/([a-f0-9]{14})$/);
  if (request.method === "GET" && route) {
    const userId = await directory(env).resolveRoute(route[1]);
    if (!userId) return new Response("Campanha não encontrada", { status: 404 });
    const destination = await env.CAMPAIGNS.getByName(userId).next(route[1], {
      country: request.cf?.country || null,
      city: request.cf?.city || null,
    });
    return destination
      ? destinationRedirectResponse(destination, request.headers.get("User-Agent") || "")
      : new Response("Campanha não encontrada", { status: 404 });
  }

  if (request.method === "POST" && url.pathname === "/api/register") return handleAuth(request, env, "register");
  if (request.method === "POST" && url.pathname === "/api/login") return handleAuth(request, env, "login");

  const user = await authenticatedUser(request, env);
  if (!user) return json({ error: "Faça login novamente." }, 401);

  if (request.method === "GET" && url.pathname === "/api/me") return json({ user });
  if (request.method === "POST" && url.pathname === "/api/logout") {
    if (!sameOrigin(request)) return json({ error: "Origem inválida." }, 403);
    const token = cookieValue(request, SESSION_COOKIE);
    if (token) await directory(env).deleteSession(await sha256(token));
    return json({ ok: true }, 200, { "Set-Cookie": sessionCookie(request, "", 0) });
  }
  if (request.method === "GET" && url.pathname === "/api/stats") return json(await env.CAMPAIGNS.getByName(user.id).stats());
  if (request.method === "POST" && url.pathname === "/api/campaigns") {
    if (!sameOrigin(request)) return json({ error: "Origem inválida." }, 403);
    const validated = validateCampaign(await bodyJson(request));
    if (validated.error) return json({ error: validated.error }, 400);
    const slug = await directory(env).allocateRoute(user.id);
    await env.CAMPAIGNS.getByName(user.id).createCampaign({ ...validated.campaign, slug, createdAt: new Date().toISOString() });
    return json({ campaign: { slug } }, 201);
  }
  return json({ error: "Não encontrado." }, 404);
}

export default {
  async fetch(request, env) {
    try {
      return await handleRequest(request, env);
    } catch (error) {
      console.error(JSON.stringify({ level: "error", message: "request_failed", path: new URL(request.url).pathname, error: String(error) }));
      return json({ error: "Não foi possível concluir agora." }, 500);
    }
  },
};
