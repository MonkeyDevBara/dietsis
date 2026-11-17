import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { PwaUpdatePrompt } from './components/PwaUpdatePrompt'
import { AuthProvider } from './context/AuthContext'
import { SessionProvider } from './context/SessionContext'
import { ToastProvider } from './context/ToastContext'
import { startInstallCapture } from './lib/install'
import './index.css'

// Must run before React mounts: the browser may fire beforeinstallprompt very early.
startInstallCapture()

const root = document.getElementById('root')
if (!root) throw new Error('Root element #root tidak ditemukan')

createRoot(root).render(
  <StrictMode>
    <ToastProvider>
      <AuthProvider>
        <SessionProvider>
          <App />
          <PwaUpdatePrompt />
        </SessionProvider>
      </AuthProvider>
    </ToastProvider>
  </StrictMode>,
)
