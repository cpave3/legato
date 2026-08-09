import { useEffect, useState } from "react"
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { Layout } from "./components/Layout"
import { TokenPrompt } from "./components/TokenPrompt"
import { AgentsPage } from "./pages/Agents"
import { BoardPage } from "./pages/Board"
import { SettingsPage } from "./pages/Settings"
import { ReviewQueuePage } from "./pages/ReviewQueuePage"
import { ReviewTourPage } from "./pages/ReviewTourPage"
import { PlanQueuePage } from "./pages/PlanQueuePage"
import { PlanPage } from "./pages/PlanPage"
import { WorkflowsPage } from "./pages/WorkflowsPage"
import { WorkflowRunPage } from "./pages/WorkflowRunPage"
import { AttentionPage } from "./pages/AttentionPage"
import { LearningsPage } from "./pages/LearningsPage"
import { LearningDetailPage } from "./pages/LearningDetailPage"
import { ReportsPage } from "./pages/ReportsPage"
import { clearToken } from "./lib/auth"
import { SettingsRequestError, useSettings } from "./hooks/useSettings"

export default function App() {
  const [authState, setAuthState] = useState<"checking" | "ok" | "needs_token">("checking")
  const settings = useSettings("")

  useEffect(() => {
    if (settings.isSuccess) {
      setAuthState("ok")
      return
    }
    if (!settings.isError) return
    if (settings.error instanceof SettingsRequestError && settings.error.status === 401) {
      clearToken()
      setAuthState("needs_token")
      return
    }
    // Network errors are handled by the offline dialog.
    setAuthState("ok")
  }, [settings.error, settings.isError, settings.isSuccess])

  // After a hot-reload rebuild, redirect to the home screen.
  useEffect(() => {
    const redirect = sessionStorage.getItem("legato:post-rebuild-redirect")
    if (redirect && window.location.pathname !== redirect) {
      sessionStorage.removeItem("legato:post-rebuild-redirect")
      window.location.replace(redirect)
    } else if (redirect) {
      sessionStorage.removeItem("legato:post-rebuild-redirect")
    }
  }, [])

  if (authState === "checking") {
    return null
  }

  if (authState === "needs_token") {
    return <TokenPrompt onAuthenticated={() => setAuthState("ok")} />
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/agents" element={<AgentsPage />} />
          <Route path="/board" element={<BoardPage />} />
          <Route path="/review" element={<ReviewQueuePage />} />
          <Route path="/review/:tourId" element={<ReviewTourPage />} />
          <Route path="/plans" element={<PlanQueuePage />} />
          <Route path="/plans/:planId" element={<PlanPage />} />
          <Route path="/workflows" element={<WorkflowsPage />} />
          <Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} />
          <Route path="/attention" element={<AttentionPage />} />
          <Route path="/learning" element={<LearningsPage />} />
          <Route path="/learning/:learningId" element={<LearningDetailPage />} />
          <Route path="/reports" element={<ReportsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/agents" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
