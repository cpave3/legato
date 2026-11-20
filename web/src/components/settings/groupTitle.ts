import { createContext, useContext } from "react"

// The heading of the Settings group a card sits in: a card whose own title
// says the same leaves its title out.
export const GroupTitleContext = createContext("")

export function useGroupTitle(): string {
  return useContext(GroupTitleContext)
}
