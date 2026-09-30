import Drawer from '../../components/Drawer'
import type { ReactNode } from 'react'

interface ActionDrawerProps {
  open: boolean
  onClose: () => void
  onApply: () => void
  applyLabel?: string
  onClear?: () => void
  clearLabel?: string
  title?: ReactNode
  width?: number
  children?: ReactNode
}

export default function ActionDrawer({
  open, onClose, onApply, applyLabel = 'Confirmar',
  onClear, clearLabel = 'Cancelar',
  title, width, children,
}: ActionDrawerProps) {
  return (
    <Drawer open={open} onClose={onClose} title={title} width={width}>
      {children}
      <div style={{ display: 'flex', gap: 8, paddingTop: 14, marginTop: 16, borderTop: '1px solid var(--szv2-divider)' }}>
        <button type="button" className="szv2-btn szv2-btn-brand" style={{ flex: 1 }} onClick={onApply}>
          {applyLabel}
        </button>
        {onClear && (
          <button type="button" className="szv2-btn szv2-btn-secondary" onClick={onClear}>
            {clearLabel}
          </button>
        )}
      </div>
    </Drawer>
  )
}
