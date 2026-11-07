import { useCallback, useRef, useState, type ReactNode } from "react"
import { ConfirmDialog } from "../components/ui/ConfirmDialog"

export interface ConfirmOptions {
  title: string
  message: ReactNode
  confirmLabel?: string
  // Styles the confirm button red: the action ends or removes something.
  destructive?: boolean
}

// useConfirm replaces window.confirm with the app's own small modal. Render
// `dialog` once in the component; `confirm` resolves true when the person
// confirms and false when they cancel, press Escape or click outside.
export function useConfirm(): { confirm: (options: ConfirmOptions) => Promise<boolean>; dialog: ReactNode } {
  const [request, setRequest] = useState<ConfirmOptions | null>(null)
  const resolver = useRef<((answer: boolean) => void) | null>(null)

  const confirm = useCallback((options: ConfirmOptions) => {
    resolver.current?.(false)
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve
      setRequest(options)
    })
  }, [])

  const settle = useCallback((answer: boolean) => {
    resolver.current?.(answer)
    resolver.current = null
    setRequest(null)
  }, [])

  const dialog = request && (
    <ConfirmDialog
      {...request}
      onConfirm={() => settle(true)}
      onCancel={() => settle(false)}
    />
  )
  return { confirm, dialog }
}
