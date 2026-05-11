import { useState, useCallback, useRef, createContext, useContext } from "react"
import { cn } from "../lib/utils"

export interface ToastItem {
  id: string
  message: string
  variant: "success" | "error" | "info"
  duration: number
}

interface ToastContextValue {
  toasts: ToastItem[]
  addToast: (message: string, variant: ToastItem["variant"]) => void
  removeToast: (id: string) => void
}

const ToastContext = createContext<ToastContextValue>(null as unknown as ToastContextValue)

export function useToastProvider() {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const timersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
    if (timersRef.current[id]) {
      clearTimeout(timersRef.current[id])
      delete timersRef.current[id]
    }
  }, [])

  const addToast = useCallback((message: string, variant: ToastItem["variant"]) => {
    const id = `${Date.now()}-${Math.random()}`
    const duration = 4000
    setToasts((prev) => [...prev, { id, message, variant, duration }])
    timersRef.current[id] = setTimeout(() => removeToast(id), duration)
  }, [removeToast])

  return { toasts, addToast, removeToast, Provider: ToastContext.Provider }
}

export function useToast() {
  return useContext(ToastContext)
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const { Provider, ...value } = useToastProvider()
  return (
    <Provider value={value}>
      {children}
      <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
        {value.toasts.map((toast) => (
          <div
            key={toast.id}
            className={cn(
              "rounded border px-4 py-2 text-sm shadow-lg transition-all",
              toast.variant === "success" && "border-emerald-700 bg-emerald-950 text-emerald-200",
              toast.variant === "error" && "border-red-700 bg-red-950 text-red-200",
              toast.variant === "info" && "border-indigo-700 bg-indigo-950 text-indigo-200",
            )}
          >
            {toast.message}
          </div>
        ))}
      </div>
    </Provider>
  )
}
