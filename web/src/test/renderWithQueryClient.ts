import { render } from "@testing-library/react"
import type { ReactNode } from "react"
import { QueryProvider } from "../components/QueryProvider"

// Match the app's query context, isolated per render and kept across rerenders.
export function renderWithQueryClient(ui: ReactNode) {
  return render(ui, { wrapper: QueryProvider })
}
