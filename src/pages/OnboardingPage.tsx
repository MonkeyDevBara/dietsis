import { Navigate, useNavigate } from 'react-router'
import { ProfileForm } from '../components/ProfileForm'
import { useAuth } from '../context/AuthContext'
import { useActions, useLocalState } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { todayKey } from '../lib/date'

export default function OnboardingPage() {
  const { user } = useAuth()
  const { profile } = useLocalState()
  const actions = useActions()
  const navigate = useNavigate()
  const toast = useToast()

  if (profile) return <Navigate to="/" replace />

  return (
    <main className="mx-auto max-w-xl px-5 pb-10 pt-safe">
      <header className="mb-5 pt-2">
        <h1 className="text-2xl font-semibold tracking-tight">Kenalan dulu, yuk</h1>
        <p className="mt-1 text-sm text-fg-muted">Data ini dipakai untuk menghitung saran target harianmu. Semuanya bisa kamu ubah nanti.</p>
      </header>
      <ProfileForm
        defaultName={user?.name.split(' ')[0] ?? ''}
        submitLabel="Mulai"
        onSubmit={async (input) => {
          await actions.saveProfile(input, { logWeightFor: todayKey() })
          toast({ message: 'Profil tersimpan. Selamat datang!' })
          void navigate('/', { replace: true })
        }}
      />
    </main>
  )
}
