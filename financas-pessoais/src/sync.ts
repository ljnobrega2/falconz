import { pbkdf2 } from '@noble/hashes/pbkdf2'
import { sha256 } from '@noble/hashes/sha256'
import { bytesToHex } from '@noble/hashes/utils'
import type { FinanceData } from './types'

const ACCOUNT_KEY = 'finanz-account-v1'

export function savedAccountId() {
  return localStorage.getItem(ACCOUNT_KEY)
}

export async function accountIdFromCode(code: string) {
  const encoder = new TextEncoder()
  return bytesToHex(pbkdf2(sha256, encoder.encode(code.trim()), encoder.encode('finanz:v1:access'), { c: 200000, dkLen: 32 }))
}

export function rememberAccount(accountId: string) {
  localStorage.setItem(ACCOUNT_KEY, accountId)
}

export function forgetAccount() {
  localStorage.removeItem(ACCOUNT_KEY)
}

export type CloudDocument = { data: FinanceData; version: number }

export class CloudConflictError extends Error {
  constructor(public latest: CloudDocument | null) { super('Os dados foram alterados em outra aba.') }
}

export async function loadCloudData(accountId: string): Promise<CloudDocument | null> {
  const response = await fetch('/api/data', { headers: { 'x-finanz-account': accountId }, cache: 'no-store' })
  if (response.status === 404) return null
  if (!response.ok) throw new Error('Não foi possível carregar os dados salvos.')
  const body = await response.json() as CloudDocument
  return body
}

export async function saveCloudData(accountId: string, data: FinanceData, expectedVersion: number) {
  const response = await fetch('/api/data', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'x-finanz-account': accountId },
    body: JSON.stringify({ data, expectedVersion }),
  })
  if (response.status === 409) {
    const body = await response.json() as { data: FinanceData | null; version: number }
    throw new CloudConflictError(body.data ? { data: body.data, version: body.version } : null)
  }
  if (!response.ok) throw new Error('Não foi possível salvar na nuvem.')
  return response.json() as Promise<{ version: number }>
}
