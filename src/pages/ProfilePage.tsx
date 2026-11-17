import { Settings } from 'lucide-react'
import { Link } from 'react-router'
import { PageHeader } from '../components/AppShell'
import { ProfileForm } from '../components/ProfileForm'
import { useActions, useLocalState } from '../context/DataContext'
import { useToast } from '../context/ToastContext'
import { todayKey } from '../lib/date'

export default function ProfilePage() {
  const { profile } = useLocalState()
  const actions = useActions()
  const toast = useToast()

  return (
    <>
      <PageHeader
        title="Profil"
        subtitle="Data tubuh dan target harianmu."
        actions={
          <Link to="/settings" aria-label="Pengaturan" title="Pengaturan" className="inline-flex size-11 items-center justify-center rounded-pill text-fg-muted hover:bg-surface-muted">
            <Settings className="size-5" aria-hidden />
          </Link>
        }
      />
      <ProfileForm
        // Remount when the saved profile changes so suggestion hints and manual flags reflect what was stored.
        key={profile?.version}
        initial={profile}
        submitLabel="Simpan perubahan"
        onSubmit={async (input) => {
          await actions.saveProfile(input, { logWeightFor: todayKey() })
          toast({ message: 'Profil disimpan.' })
        }}
      />
    </>
  )
}
