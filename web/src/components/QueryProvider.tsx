import { QueryClientProvider } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"
import { createQueryClient } from "../lib/query"

export function QueryProvider({ children }: { children: ReactNode }) {
  const [client] = useState(createQueryClient)
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
