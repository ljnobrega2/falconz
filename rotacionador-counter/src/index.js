import { DurableObject } from "cloudflare:workers";

const LEGACY_DESTINATIONS = [
  { name: "Bobby", url: "https://t.me/bobby_oficial7" },
  { name: "GerenteLingn", url: "https://t.me/GerenteLingn" },
  { name: "gerente_zikzik", url: "https://t.me/gerente_zikzik" },
  { name: "gerente_Jian", url: "https://t.me/gerente_Jian" },
  { name: "Vincent_toya", url: "https://t.me/Vincent_toya" }
];

const json = (data, init = {}) => Response.json(data, {
  ...init,
  headers: { "Cache-Control": "no-store", ...(init.headers || {}) }
});

function randomCampaignSlug() {
  const alphabet = "abcdefghijkmnopqrstuvwxyz23456789";
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return "rt-" + Array.from(bytes, (byte) => alphabet[byte % alphabet.length]).join("");
}

function parseJson(request) {
  return request.json().catch(() => null);
}

export class GlobalRotator extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.env = env;
    ctx.blockConcurrencyWhile(async () => {
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS rotator_state (
          id INTEGER PRIMARY KEY CHECK (id = 1), position INTEGER NOT NULL
        )
      `);
      ctx.storage.sql.exec("INSERT OR IGNORE INTO rotator_state (id, position) VALUES (1, 0)");
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS rotator_stats (
          id INTEGER PRIMARY KEY CHECK (id = 1), total INTEGER NOT NULL,
          group_0 INTEGER NOT NULL, group_1 INTEGER NOT NULL, group_2 INTEGER NOT NULL,
          group_3 INTEGER NOT NULL, group_4 INTEGER NOT NULL
        )
      `);
      ctx.storage.sql.exec("INSERT OR IGNORE INTO rotator_stats (id, total, group_0, group_1, group_2, group_3, group_4) VALUES (1, 0, 0, 0, 0, 0, 0)");
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS access_log (
          id INTEGER PRIMARY KEY AUTOINCREMENT, campaign_slug TEXT NOT NULL DEFAULT 'legacy',
          group_index INTEGER NOT NULL, accessed_at TEXT NOT NULL,
          country TEXT, city TEXT, ip TEXT
        )
      `);
      // The first version of the counter had no campaign_slug column.
      // Keep existing access history when the Durable Object upgrades.
      try {
        ctx.storage.sql.exec("ALTER TABLE access_log ADD COLUMN campaign_slug TEXT NOT NULL DEFAULT 'principal'");
      } catch (_) {
        // Column already exists on new installations or after a previous upgrade.
      }
      try { ctx.storage.sql.exec("ALTER TABLE access_log ADD COLUMN country TEXT"); } catch (_) {}
      try { ctx.storage.sql.exec("ALTER TABLE access_log ADD COLUMN city TEXT"); } catch (_) {}
      try { ctx.storage.sql.exec("ALTER TABLE access_log ADD COLUMN ip TEXT"); } catch (_) {}
      ctx.storage.sql.exec(`
        CREATE TABLE IF NOT EXISTS campaigns (
          slug TEXT PRIMARY KEY, name TEXT NOT NULL, mode TEXT NOT NULL,
          destinations TEXT NOT NULL, position INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1
        )
      `);
      const legacy = ctx.storage.sql.exec("SELECT slug FROM campaigns WHERE slug = 'principal'").toArray()[0];
      if (legacy) {
        const generatedSlug = randomCampaignSlug();
        ctx.storage.sql.exec("UPDATE campaigns SET slug = ? WHERE slug = 'principal'", generatedSlug);
        ctx.storage.sql.exec("UPDATE access_log SET campaign_slug = ? WHERE campaign_slug = 'principal'", generatedSlug);
      }
      const defaultSlug = ctx.storage.sql.exec("SELECT slug FROM campaigns ORDER BY created_at ASC LIMIT 1").toArray()[0]?.slug || randomCampaignSlug();
      ctx.storage.sql.exec("UPDATE campaigns SET name = 'Campanha inicial' WHERE slug = ? AND name = 'Campanha principal'", defaultSlug);
      ctx.storage.sql.exec("UPDATE access_log SET campaign_slug = ? WHERE campaign_slug = 'principal'", defaultSlug);
      ctx.storage.sql.exec(
        "INSERT OR IGNORE INTO campaigns (slug, name, mode, destinations, created_at) VALUES (?, ?, ?, ?, ?)",
        defaultSlug, "Campanha inicial", "rotation", JSON.stringify(LEGACY_DESTINATIONS), new Date().toISOString()
      );
    });
  }

  async nextLegacy(request) {
    const legacySlug = this.ctx.storage.sql.exec("SELECT slug FROM campaigns ORDER BY created_at ASC LIMIT 1").toArray()[0]?.slug || "legacy";
    const state = this.ctx.storage.sql.exec("SELECT position FROM rotator_state WHERE id = 1").one();
    const index = state.position % LEGACY_DESTINATIONS.length;
    this.ctx.storage.sql.exec("UPDATE rotator_stats SET total = total + 1, group_" + index + " = group_" + index + " + 1 WHERE id = 1");
    this.recordAccess(legacySlug, index, request);
    this.ctx.storage.sql.exec("UPDATE rotator_state SET position = ? WHERE id = 1", (index + 1) % LEGACY_DESTINATIONS.length);
    return { index, url: LEGACY_DESTINATIONS[index].url };
  }

  recordAccess(slug, index, request) {
    const location = request?.cf || {};
    const country = String(location.country || request?.headers.get("CF-IPCountry") || "").slice(0, 8) || null;
    const city = String(location.city || "").slice(0, 80) || null;
    const ip = String(request?.headers.get("CF-Connecting-IP") || "").slice(0, 64) || null;
    const inserted = this.ctx.storage.sql.exec(
      "INSERT INTO access_log (campaign_slug, group_index, accessed_at, country, city, ip) VALUES (?, ?, ?, ?, ?, ?) RETURNING id",
      slug, index, new Date().toISOString(), country, city, ip
    );
    // Avoid scanning the whole history on every ad click. Trim occasionally.
    if (inserted.one().id % 500 === 0) {
      this.ctx.storage.sql.exec("DELETE FROM access_log WHERE id NOT IN (SELECT id FROM access_log ORDER BY id DESC LIMIT 5000)");
    }
  }

  async nextCampaign(slug, request) {
    const campaign = this.ctx.storage.sql.exec("SELECT * FROM campaigns WHERE slug = ? AND active = 1", slug).one();
    if (!campaign) return null;
    const destinations = JSON.parse(campaign.destinations);
    if (!destinations.length) return null;
    const index = campaign.mode === "random"
      ? crypto.getRandomValues(new Uint32Array(1))[0] % destinations.length
      : campaign.position % destinations.length;
    this.ctx.storage.sql.exec("UPDATE campaigns SET position = ? WHERE slug = ?", (index + 1) % destinations.length, slug);
    this.recordAccess(slug, index, request);
    return { index, url: destinations[index].url };
  }

  listCampaigns() {
    const rows = this.ctx.storage.sql.exec("SELECT * FROM campaigns ORDER BY created_at DESC").toArray();
    const counts = this.ctx.storage.sql.exec("SELECT campaign_slug, group_index, COUNT(*) AS total FROM access_log GROUP BY campaign_slug, group_index").toArray();
    const bySlug = {};
    for (const row of counts) {
      bySlug[row.campaign_slug] ||= {};
      bySlug[row.campaign_slug][row.group_index] = row.total;
    }
    return rows.map((row) => ({
      slug: row.slug, name: row.name, mode: row.mode,
      destinations: JSON.parse(row.destinations).map((destination, index) => ({
        ...destination, accesses: bySlug[row.slug]?.[index] || 0
      })), created_at: row.created_at,
      active: Boolean(row.active), accesses: Object.values(bySlug[row.slug] || {}).reduce((total, value) => total + value, 0)
    }));
  }

  createCampaign(input) {
    const slug = randomCampaignSlug();
    const name = String(input?.name || "").trim();
    const mode = input?.mode === "random" ? "random" : "rotation";
    const destinations = Array.isArray(input?.destinations)
      ? input.destinations.filter((item) => item && typeof item.url === "string" && /^https?:\/\//i.test(item.url.trim()))
        .map((item) => ({ name: String(item.name || item.url).trim().slice(0, 80), url: item.url.trim() }))
      : [];
    if (!name || destinations.length < 2) return { error: "Informe um nome e pelo menos dois links." };
    try {
      this.ctx.storage.sql.exec("UPDATE campaigns SET active = 0");
      this.ctx.storage.sql.exec(
        "INSERT INTO campaigns (slug, name, mode, destinations, created_at, active) VALUES (?, ?, ?, ?, ?, 1)",
        slug, name.slice(0, 100), mode, JSON.stringify(destinations), new Date().toISOString()
      );
    } catch (_) {
      return { error: "Esse slug já está em uso." };
    }
    return { campaign: this.listCampaigns().find((item) => item.slug === slug) };
  }

  stats() {
    const defaultSlug = this.ctx.storage.sql.exec("SELECT slug FROM campaigns ORDER BY created_at ASC LIMIT 1").toArray()[0]?.slug;
    if (defaultSlug) {
      this.ctx.storage.sql.exec("UPDATE access_log SET campaign_slug = ? WHERE campaign_slug = 'principal'", defaultSlug);
      this.ctx.storage.sql.exec("UPDATE campaigns SET name = 'Campanha inicial' WHERE slug = ? AND name = 'Campanha principal'", defaultSlug);
    }
    const campaigns = this.listCampaigns();
    const bySlug = Object.fromEntries(campaigns.map((campaign) => [campaign.slug, campaign]));
    const recent = this.ctx.storage.sql.exec("SELECT campaign_slug, group_index, accessed_at, country, city, ip FROM access_log ORDER BY id DESC LIMIT 100").toArray();
    return {
      campaigns,
      recent: recent.map((item) => ({
        ...item,
        campaign_name: bySlug[item.campaign_slug]?.name || item.campaign_slug,
        destination: bySlug[item.campaign_slug]?.destinations[item.group_index] || null
      }))
    };
  }

  reset() {
    this.ctx.storage.sql.exec("UPDATE rotator_state SET position = 0 WHERE id = 1");
    this.ctx.storage.sql.exec("UPDATE rotator_stats SET total = 0, group_0 = 0, group_1 = 0, group_2 = 0, group_3 = 0, group_4 = 0 WHERE id = 1");
    this.ctx.storage.sql.exec("UPDATE campaigns SET position = 0");
    this.ctx.storage.sql.exec("DELETE FROM access_log");
    return { reset: true };
  }

  async fetch(request) {
    const url = new URL(request.url);
    if (url.pathname === "/next") return json(await this.nextLegacy(request));
    if (url.pathname === "/stats") return json(this.stats());
    if (url.pathname === "/campaigns" && request.method === "GET") return json({ campaigns: this.listCampaigns() });
    if (url.pathname === "/campaigns" && request.method === "POST") return json(this.createCampaign(await parseJson(request)));
    if (url.pathname === "/campaigns/active/next") {
      // This is the hot path used by the ad link. Never aggregate access_log here:
      // the dashboard is the only place that needs campaign totals.
      const active = this.ctx.storage.sql.exec("SELECT slug FROM campaigns WHERE active = 1 ORDER BY created_at DESC LIMIT 1").toArray()[0]
        || this.ctx.storage.sql.exec("SELECT slug FROM campaigns ORDER BY created_at ASC LIMIT 1").toArray()[0];
      const result = active ? await this.nextCampaign(active.slug, request) : null;
      return result ? json(result) : new Response("Nenhuma campanha ativa", { status: 404 });
    }
    const match = url.pathname.match(/^\/campaigns\/([a-z0-9-]+)\/next$/);
    if (match) {
      const result = await this.nextCampaign(match[1], request);
      return result ? json(result) : new Response("Campanha não encontrada", { status: 404 });
    }
    if (url.pathname === "/reset" && request.method === "POST") {
      if (request.headers.get("X-Reset-Token") !== this.env.RESET_TOKEN) return new Response("Unauthorized", { status: 401 });
      return json(this.reset());
    }
    return new Response("Not Found", { status: 404 });
  }
}

export default {
  async fetch(request, env) {
    const stub = env.COUNTER.getByName("global");
    const url = new URL(request.url);
    const response = await stub.fetch(request);
    if (url.pathname === "/stats") {
      const data = await response.json();
      return json({ campaigns: data.campaigns || [], recent: data.recent || [] });
    }
    return response;
  }
};
