import { useState, useEffect, useCallback } from "react"
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { Layout } from "./components/Layout"
import { OfflineOverlay } from "./components/OfflineOverlay"
import { TokenPrompt } from "./components/TokenPrompt"
import { ServerProvider } from "./components/ServerProvider"
import { AgentsPage } from "./pages/Agents"
import { BoardPage } from "./pages/Board"
import { SettingsPage } from "./pages/Settings"
import { authHeaders, clearToken } from "./lib/auth"

export default function App() {
  const [authState, setAuthState] = useState<"checking" | "ok" | "needs_token">("checking")

  const checkAuth = useCallback(async () => {
    try {
      const res = await fetch("/api/settings", { headers: authHeaders() })
      if (res.status === 401) {
        clearToken()
        setAuthState("needs_token")
      } else {
        setAuthState("ok")
      }
    } catch {
      // Network error — let the offline overlay handle it.
      setAuthState("ok")
    }
  }, [])

  useEffect(() => {
    checkAuth()
  }, [checkAuth])

  if (authState === "checking") {
    return null
  }

  if (authState === "needs_token") {
    return <TokenPrompt onAuthenticated={() => setAuthState("ok")} />
  }

  return (
    <ServerProvider>
      <BrowserRouter>
        <OfflineOverlay />
        <Routes>
          <Route element={<Layout />}>
            <Route path="/agents" element={<AgentsPage />} />
            <Route path="/board" element={<BoardPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/agents" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </ServerProvider>
  )
}
