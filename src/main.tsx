import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { PwaUpdatePrompt } from './components/PwaUpdatePrompt'
import { PreviewAuthProvider } from './context/AuthContext'
import { DataProvider } from './context/DataContext'
import { ToastProvider } from './context/ToastContext'
import { MemoryRepository } from './services/local/repository'
import './index.css'

const root = document.getElementById('root')
if (!root) throw new Error('Root element #root tidak ditemukan')

// M1: in-memory data (lost on reload). M6 swaps in the IndexedDB repository.
const repo = new MemoryRepository()

createRoot(root).render(
  <StrictMode>
    <ToastProvider>
      <PreviewAuthProvider>
        <DataProvider repo={repo}>
          <App />
          <PwaUpdatePrompt />
        </DataProvider>
      </PreviewAuthProvider>
    </ToastProvider>
  </StrictMode>,
)
