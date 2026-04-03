import { type ReactNode } from "react"
import { useWebSocketProvider } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const { wsUrl } = useServer()
  const { Provider, ...value } = useWebSocketProvider(wsUrl)
  return <Provider value={value}>{children}</Provider>
}
