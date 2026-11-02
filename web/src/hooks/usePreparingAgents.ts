import { useQuery } from "@tanstack/react-query"
import { queryKeys } from "../lib/query"
import { fetchPreparingAgents, fetchRepositoryCache } from "../lib/preparingAgents"
import { useServer } from "./useServer"

// The agents the server is starting in the background; agents_changed
// refetches them.
export function usePreparingAgents() {
  const { baseUrl } = useServer()
  return useQuery({
    queryKey: queryKeys.preparingAgents(baseUrl),
    queryFn: () => fetchPreparingAgents(baseUrl),
  })
}

// Each repository's copy in the server's cache; repository_cache messages
// update it as git goes.
export function useRepositoryCache() {
  const { baseUrl } = useServer()
  return useQuery({
    queryKey: queryKeys.repositoryCache(baseUrl),
    queryFn: () => fetchRepositoryCache(baseUrl),
  })
}
