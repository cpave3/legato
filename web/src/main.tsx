import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { ServerProvider } from './components/ServerProvider'
import { WebSocketProvider } from './components/WebSocketProvider'
import { ToastProvider } from './hooks/useToast'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ServerProvider>
      <WebSocketProvider>
        <ToastProvider>
          <App />
        </ToastProvider>
      </WebSocketProvider>
    </ServerProvider>
  </StrictMode>,
)
