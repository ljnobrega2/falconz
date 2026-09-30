// DocumentChangeDrawer — troca CPF ⇄ CNPJ do titular da conta (FEAT-DOC-CHANGE-2026-07-03).
//
// Abre um drawer lateral a partir do card "Sua conta". O usuário (produtor,
// afiliado ou cliente) informa os dados da troca; a solicitação vai para o admin e
// só é aplicada após aprovação. Enquanto pendente, o drawer mostra "em análise".
//
//   cpf_to_cnpj → razão social + CNPJ + anexo do cartão CNPJ (obrigatório).
//   cnpj_to_cpf → nome civil + CPF (sem anexo).
//
// Endpoints:
//   GET  /portal/account/document-change  — tipo atual + pedido mais recente
//   POST /portal/account/document-change  — cria o pedido (multipart)
import { FormEvent, useEffect, useState } from 'react'
import Drawer from './Drawer'
import { api } from '../api'
import { useToast } from '../hooks/useToast'

interface PendingReq {
  id: number
  direction: string
  target_nome: string
  target_document: string
  status: string
  has_attachment: boolean
  notes?: string | null
  created_at: string
  reviewed_at?: string | null
}

interface DocChangeState {
  current_document: string
  current_type: '' | 'cpf' | 'cnpj'
  pending: PendingReq | null
}

interface Props {
  open: boolean
  onClose: () => void
  /** Chamado após criar a solicitação (o pai pode recarregar /portal/me). */
  onSubmitted?: () => void
}

// Formata CPF/CNPJ a partir dos dígitos crus (só display).
function fmtDoc(digits: string): string {
  const d = (digits || '').replace(/\D/g, '')
  if (d.length === 11) return d.replace(/(\d{3})(\d{3})(\d{3})(\d{2})/, '$1.$2.$3-$4')
  if (d.length === 14) return d.replace(/(\d{2})(\d{3})(\d{3})(\d{4})(\d{2})/, '$1.$2.$3/$4-$5')
  return digits || '—'
}

export default function DocumentChangeDrawer({ open, onClose, onSubmitted }: Props) {
  const toast = useToast()
  const [loading, setLoading] = useState(true)
  const [state, setState] = useState<DocChangeState | null>(null)
  const [nome, setNome] = useState('')
  const [doc, setDoc] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [saving, setSaving] = useState(false)

  // Direção derivada do documento atual: CPF (ou vazio) → CNPJ; CNPJ → CPF.
  const direction: 'cpf_to_cnpj' | 'cnpj_to_cpf' =
    state?.current_type === 'cnpj' ? 'cnpj_to_cpf' : 'cpf_to_cnpj'
  const isToCnpj = direction === 'cpf_to_cnpj'

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setNome('')
    setDoc('')
    setFile(null)
    api<DocChangeState>('/portal/account/document-change')
      .then(setState)
      .catch(e => toast('err', e.message || 'Erro ao carregar dados do documento.'))
      .finally(() => setLoading(false))
  }, [open])

  async function submit(e: FormEvent) {
    e.preventDefault()
    const cleanDoc = doc.replace(/\D/g, '')
    if (nome.trim().length < 2) {
      toast('err', isToCnpj ? 'Informe a razão social.' : 'Informe o nome civil.')
      return
    }
    if (isToCnpj && cleanDoc.length !== 14) {
      toast('err', 'CNPJ deve ter 14 dígitos.')
      return
    }
    if (!isToCnpj && cleanDoc.length !== 11) {
      toast('err', 'CPF deve ter 11 dígitos.')
      return
    }
    if (isToCnpj && !file) {
      toast('err', 'Anexe o cartão CNPJ (JPEG, PNG ou PDF).')
      return
    }

    const fd = new FormData()
    fd.append('direction', direction)
    fd.append('target_nome', nome.trim())
    fd.append('target_document', cleanDoc)
    if (isToCnpj && file) fd.append('attachment', file)

    setSaving(true)
    try {
      await api('/portal/account/document-change', { method: 'POST', body: fd })
      toast('ok', 'Solicitação enviada. Aguarde a aprovação do admin.')
      onSubmitted?.()
      onClose()
    } catch (err: any) {
      toast('err', err.message || 'Erro ao enviar solicitação.')
    } finally {
      setSaving(false)
    }
  }

  const pending = state?.pending
  const isPending = pending && pending.status === 'pending'
  const wasRejected = pending && pending.status === 'rejected'

  return (
    <Drawer open={open} onClose={onClose} title="Alterar tipo de documento" width={460}>
      {loading ? (
        <p style={{ color: 'var(--szv2-text-muted)', fontSize: 13 }}>Carregando…</p>
      ) : isPending ? (
        <div>
          <div
            style={{
              background: 'var(--szv2-surface-2, rgba(30,111,242,0.08))',
              border: '1px solid var(--szv2-border)',
              borderRadius: 10,
              padding: 14,
              marginBottom: 12,
            }}
          >
            <p style={{ fontWeight: 600, margin: '0 0 6px', color: 'var(--szv2-text)' }}>
              Solicitação em análise
            </p>
            <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: 0 }}>
              {pending!.direction === 'cpf_to_cnpj'
                ? 'Troca de CPF para CNPJ'
                : 'Troca de CNPJ para CPF'}{' '}
              — <strong>{pending!.target_nome}</strong> ({fmtDoc(pending!.target_document)}).
              Você será notificado quando o admin aprovar.
            </p>
          </div>
          <button className="szv2-btn" onClick={onClose}>
            Fechar
          </button>
        </div>
      ) : (
        <form onSubmit={submit}>
          <p style={{ fontSize: 13, color: 'var(--szv2-text-muted)', margin: '0 0 14px' }}>
            {state?.current_type === 'cnpj'
              ? 'Sua conta está como CNPJ. Preencha abaixo para voltar a operar como pessoa física (CPF). Após a aprovação do admin, seu nome civil passa a valer em todo o site.'
              : 'Sua conta está como CPF. Preencha abaixo para passar a operar como empresa (CNPJ). Após a aprovação do admin, sua razão social passa a valer em todo o site.'}
          </p>

          {wasRejected && pending!.notes && (
            <div
              style={{
                fontSize: 12,
                color: 'var(--szv2-danger)',
                background: 'rgba(239,68,68,0.08)',
                border: '1px solid rgba(239,68,68,0.3)',
                borderRadius: 8,
                padding: 10,
                marginBottom: 12,
              }}
            >
              Solicitação anterior rejeitada: {pending!.notes}
            </div>
          )}

          <div className="szv2-input-group">
            <label className="szv2-label">{isToCnpj ? 'Razão social' : 'Nome civil'}</label>
            <input
              type="text"
              className="szv2-input"
              placeholder={isToCnpj ? 'Ex.: Falk Logística LTDA' : 'Ex.: Gabriel Matias'}
              value={nome}
              onChange={e => setNome(e.target.value)}
            />
          </div>

          <div className="szv2-input-group">
            <label className="szv2-label">{isToCnpj ? 'CNPJ' : 'CPF'}</label>
            <input
              type="text"
              className="szv2-input"
              inputMode="numeric"
              placeholder={isToCnpj ? '00.000.000/0000-00' : '000.000.000-00'}
              value={doc}
              onChange={e => setDoc(e.target.value)}
            />
          </div>

          {isToCnpj && (
            <div className="szv2-input-group">
              <label className="szv2-label">Cartão CNPJ (JPEG, PNG ou PDF)</label>
              <input
                type="file"
                className="szv2-input"
                accept="image/jpeg,image/png,application/pdf"
                onChange={e => setFile(e.target.files?.[0] || null)}
              />
              <span style={{ fontSize: 11, color: 'var(--szv2-text-muted)' }}>
                Documento confidencial — visível apenas para a equipe de aprovação.
              </span>
            </div>
          )}

          <button
            type="submit"
            className="szv2-btn szv2-btn-brand"
            disabled={saving}
            style={{ marginTop: 8 }}
          >
            {saving ? 'Enviando…' : 'Enviar solicitação'}
          </button>
        </form>
      )}
    </Drawer>
  )
}
