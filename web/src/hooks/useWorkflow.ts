import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket } from "./useWebSocket"
import { fetchWorkflowRun, fetchWorkflowRuns, fetchWorkflows, type WorkflowDiscovery, type WorkflowRun, type WorkflowRunView } from "../lib/workflow"

function useWorkflowData<T>(load:()=>Promise<T>, matches:(m:{type:string;workflow_run_id?:string})=>boolean) {
  const {subscribe}=useWebSocket(); const [data,setData]=useState<T|null>(null); const [loading,setLoading]=useState(true); const [error,setError]=useState<string|null>(null)
  const refresh=useCallback(async()=>{try{setError(null);setData(await load())}catch(c){setError(c instanceof Error?c.message:String(c))}finally{setLoading(false)}},[load])
  useEffect(()=>{void refresh()},[refresh]); useEffect(()=>subscribe(m=>{if(matches(m))void refresh()}),[matches,refresh,subscribe]); return {data,loading,error,refresh}
}
export function useWorkflows(){const {baseUrl}=useServer();const load=useCallback(()=>fetchWorkflows(baseUrl),[baseUrl]);const match=useCallback((m:{type:string})=>m.type==="workflow_changed",[]);return useWorkflowData<WorkflowDiscovery>(load,match)}
export function useWorkflowRuns(){const {baseUrl}=useServer();const load=useCallback(()=>fetchWorkflowRuns(baseUrl),[baseUrl]);const match=useCallback((m:{type:string})=>m.type==="workflow_changed",[]);return useWorkflowData<WorkflowRun[]>(load,match)}
export function useWorkflowRun(id:string){const {baseUrl}=useServer();const load=useCallback(()=>fetchWorkflowRun(baseUrl,id),[baseUrl,id]);const match=useCallback((m:{type:string;workflow_run_id?:string})=>m.type==="workflow_changed"&&m.workflow_run_id===id,[id]);return useWorkflowData<WorkflowRunView>(load,match)}
