import { Modal } from './Modal'
import type { ModalProps } from './Modal'

export type ConfirmTone = 'default' | 'danger'

export interface ConfirmDialogProps extends Omit<ModalProps, 'footer'> {
  confirmLabel: string
  tone?: ConfirmTone
  onConfirm: () => void
}

const CONFIRM_CLASS: Record<ConfirmTone, string> = {
  default: 'bg-blue-600 hover:bg-blue-700 text-white',
  danger: 'bg-red-600 hover:bg-red-700 text-white',
}

export function ConfirmDialog({ confirmLabel, tone = 'default', onConfirm, size = 'sm', ...modal }: ConfirmDialogProps) {
  return (
    <Modal
      {...modal}
      size={size}
      footer={
        <>
          <button
            type="button"
            onClick={modal.onClose}
            className="px-3 py-1.5 rounded-lg text-sm font-medium text-gray-700 dark:text-gray-200 border dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium ${CONFIRM_CLASS[tone]}`}
          >
            {confirmLabel}
          </button>
        </>
      }
    />
  )
}
