import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket } from "./useWebSocket"
import { fetchPlan, fetchPlanQueue, type PlanQueueItem, type PlanView } from "../lib/plan"

function usePlanData<T>(load:()=>Promise<T>, matches:(message:{type:string;plan_id?:string})=>boolean) {
  const { subscribe } = useWebSocket()
  const [data,setData] = useState<T|null>(null)
  const [loading,setLoading] = useState(true)
  const [error,setError] = useState<string|null>(null)
  const refresh = useCallback(async()=>{ try { setError(null); setData(await load()) } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { setLoading(false) } },[load])
  useEffect(()=>{ void refresh() },[refresh])
  useEffect(()=>subscribe(message=>{ if (matches(message)) void refresh() }),[matches,refresh,subscribe])
  return {data,loading,error,refresh}
}

export function usePlanQueue() {
  const {baseUrl}=useServer()
  const load=useCallback(()=>fetchPlanQueue(baseUrl),[baseUrl])
  const matches=useCallback((message:{type:string})=>message.type==="plan_changed",[])
  return usePlanData<PlanQueueItem[]>(load,matches)
}
export function usePlan(planId:string) {
  const {baseUrl}=useServer()
  const load=useCallback(()=>fetchPlan(baseUrl,planId),[baseUrl,planId])
  const matches=useCallback((message:{type:string;plan_id?:string})=>message.type==="plan_changed"&&message.plan_id===planId,[planId])
  return usePlanData<PlanView>(load,matches)
}
