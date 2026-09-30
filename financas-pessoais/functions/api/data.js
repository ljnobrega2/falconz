const headers = {
  'Cache-Control': 'no-store',
  'Content-Type': 'application/json; charset=utf-8',
  'X-Content-Type-Options': 'nosniff',
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers })
}

function accountId(request) {
  const value = request.headers.get('x-finanz-account') ?? ''
  return /^[a-f0-9]{64}$/.test(value) ? value : null
}

function validData(data) {
  if (!data || typeof data !== 'object') return false
  if (!Array.isArray(data.people) || !Array.isArray(data.entries) || !Array.isArray(data.investments)) return false
  if (typeof data.activePersonId !== 'string') return false
  if (data.people.length > 100 || data.entries.length > 100000 || data.investments.length > 10000) return false
  return data.people.every(person => person && typeof person.id === 'string' && typeof person.name === 'string')
    && data.entries.every(entry => entry && typeof entry.id === 'string' && typeof entry.personId === 'string' && Number.isFinite(entry.amount))
    && data.investments.every(investment => investment && typeof investment.id === 'string' && typeof investment.personId === 'string' && Number.isFinite(investment.investedAmount))
}

export async function onRequestGet({ request, env }) {
  const account = accountId(request)
  if (!account) return json({ error: 'Acesso inválido.' }, 401)

  const row = await env.DB.prepare(
    'SELECT data_json, version, updated_at FROM finance_documents WHERE account_id = ?',
  ).bind(account).first()
  if (!row) return json({ error: 'Conta ainda não possui dados.' }, 404)

  return json({ data: JSON.parse(row.data_json), version: row.version, updatedAt: row.updated_at })
}

export async function onRequestPut({ request, env }) {
  const account = accountId(request)
  if (!account) return json({ error: 'Acesso inválido.' }, 401)
  const length = Number(request.headers.get('content-length') ?? 0)
  if (length > 900000) return json({ error: 'Arquivo financeiro muito grande.' }, 413)

  let body
  try {
    body = await request.json()
  } catch {
    return json({ error: 'JSON inválido.' }, 400)
  }
  if (!validData(body?.data) || !Number.isInteger(body?.expectedVersion) || body.expectedVersion < 0) return json({ error: 'Dados financeiros inválidos.' }, 400)

  const serialized = JSON.stringify(body.data)
  if (serialized.length > 900000) return json({ error: 'Arquivo financeiro muito grande.' }, 413)

  const current = await env.DB.prepare(
    'SELECT data_json, version, updated_at FROM finance_documents WHERE account_id = ?',
  ).bind(account).first()
  if ((current?.version ?? 0) !== body.expectedVersion) {
    return json({ error: 'Os dados foram alterados em outra aba.', data: current ? JSON.parse(current.data_json) : null, version: current?.version ?? 0, updatedAt: current?.updated_at ?? null }, 409)
  }

  if (current) {
    await env.DB.prepare(`
        UPDATE finance_documents
        SET data_json = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
        WHERE account_id = ? AND version = ?
      `).bind(serialized, account, body.expectedVersion).run()
  } else {
    await env.DB.prepare(`
        INSERT OR IGNORE INTO finance_documents (account_id, data_json, version, updated_at)
        VALUES (?, ?, 1, CURRENT_TIMESTAMP)
      `).bind(account, serialized).run()
  }

  const saved = await env.DB.prepare(
    'SELECT data_json, version, updated_at FROM finance_documents WHERE account_id = ?',
  ).bind(account).first()
  if (!saved || saved.version !== body.expectedVersion + 1 || saved.data_json !== serialized) {
    return json({ error: 'Os dados foram alterados em outra aba.', data: saved ? JSON.parse(saved.data_json) : null, version: saved?.version ?? 0, updatedAt: saved?.updated_at ?? null }, 409)
  }
  return json({ saved: true, version: saved.version, updatedAt: saved.updated_at })
}
