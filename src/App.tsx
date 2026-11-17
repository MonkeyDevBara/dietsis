import { BrowserRouter, Route, Routes } from 'react-router'
import { FoundationPage } from './pages/FoundationPage'

// M0 shell. Feature routes (dashboard, diary, progress, profile, settings) arrive in M1.
export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="*" element={<FoundationPage />} />
      </Routes>
    </BrowserRouter>
  )
}
