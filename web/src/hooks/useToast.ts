import { createContext, useContext } from "react"

export interface ToastItem {
  id: string
  message: string
  variant: "success" | "error" | "info"
  duration: number
}

export interface ToastContextValue {
  toasts: ToastItem[]
  addToast: (message: string, variant: ToastItem["variant"]) => void
  removeToast: (id: string) => void
}

// ToastContext is filled by ToastProvider (components/ToastProvider.tsx).
export const ToastContext = createContext<ToastContextValue>(null as unknown as ToastContextValue)

export function useToast() {
  return useContext(ToastContext)
}
