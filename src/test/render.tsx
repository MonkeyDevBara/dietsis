import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { AppRoutes } from '../App'
import { PreviewAuthProvider } from '../context/AuthContext'
import { DataProvider } from '../context/DataContext'
import { ToastProvider } from '../context/ToastContext'
import { MemoryRepository } from '../services/local/repository'

export function renderApp(path = '/', repo = new MemoryRepository()) {
  localStorage.clear()
  const result = render(
    <ToastProvider>
      <PreviewAuthProvider>
        <DataProvider repo={repo}>
          <MemoryRouter initialEntries={[path]}>
            <AppRoutes />
          </MemoryRouter>
        </DataProvider>
      </PreviewAuthProvider>
    </ToastProvider>,
  )
  return { ...result, repo }
}
