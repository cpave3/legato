import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { Layout } from "./components/Layout"
import { OfflineOverlay } from "./components/OfflineOverlay"
import { AgentsPage } from "./pages/Agents"
import { BoardPage } from "./pages/Board"

export default function App() {
  return (
    <BrowserRouter>
      <OfflineOverlay />
      <Routes>
        <Route element={<Layout />}>
          <Route path="/agents" element={<AgentsPage />} />
          <Route path="/board" element={<BoardPage />} />
          <Route path="*" element={<Navigate to="/agents" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
