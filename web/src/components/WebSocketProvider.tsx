import { type ReactNode } from "react"
import { useWebSocketProvider } from "../hooks/useWebSocket"

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const { Provider, ...value } = useWebSocketProvider()
  return <Provider value={value}>{children}</Provider>
}
