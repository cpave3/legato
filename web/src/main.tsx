import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { ServerProvider } from './components/ServerProvider'
import { WebSocketProvider } from './components/WebSocketProvider'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ServerProvider>
      <WebSocketProvider>
        <App />
      </WebSocketProvider>
    </ServerProvider>
  </StrictMode>,
)
