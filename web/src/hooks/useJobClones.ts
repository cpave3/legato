import { useQuery } from "@tanstack/react-query"
import { listJobClones } from "../lib/jobClones"
import { queryKeys } from "../lib/query"

export function useJobClones(baseUrl: string) {
  return useQuery({
    queryKey: queryKeys.jobClones(baseUrl),
    queryFn: () => listJobClones(baseUrl),
    refetchOnMount: "always",
  })
}
