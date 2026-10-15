import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// The typefaces of the new look, which the studio and the board use.
import '@fontsource/geist-sans/400.css'
import '@fontsource/geist-sans/500.css'
import '@fontsource/geist-sans/600.css'
import '@fontsource/geist-mono/400.css'
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
