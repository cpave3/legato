import { type ReactNode } from "react"
import { useServerProvider } from "../hooks/useServer"

export function ServerProvider({ children }: { children: ReactNode }) {
  const { Provider, value } = useServerProvider()
  return <Provider value={value}>{children}</Provider>
}
