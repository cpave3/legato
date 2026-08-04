import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { ServerProvider } from './components/ServerProvider'
import { WebSocketProvider } from './components/WebSocketProvider'
import { QueryProvider } from './components/QueryProvider'
import { QuerySyncProvider } from './components/QuerySyncProvider'
import { ToastProvider } from './hooks/useToast'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ServerProvider>
      <QueryProvider>
        <WebSocketProvider>
          <QuerySyncProvider>
            <ToastProvider>
              <App />
            </ToastProvider>
          </QuerySyncProvider>
        </WebSocketProvider>
      </QueryProvider>
    </ServerProvider>
  </StrictMode>,
)
