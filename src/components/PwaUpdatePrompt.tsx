import { useEffect } from 'react'
import { useRegisterSW } from 'virtual:pwa-register/react'
import { useToast } from '../context/ToastContext'

/**
 * registerType 'prompt': a new service worker waits until the user taps "Muat ulang", so a form
 * being filled is never reloaded underneath them.
 */
export function PwaUpdatePrompt() {
  const toast = useToast()
  const {
    needRefresh: [needRefresh],
    offlineReady: [offlineReady, setOfflineReady],
    updateServiceWorker,
  } = useRegisterSW()

  useEffect(() => {
    if (!needRefresh) return
    toast({
      message: 'Versi baru Dietsis tersedia.',
      action: { label: 'Muat ulang', onClick: () => void updateServiceWorker(true) },
      duration: 60_000,
    })
  }, [needRefresh, toast, updateServiceWorker])

  useEffect(() => {
    if (!offlineReady) return
    toast({ message: 'Dietsis siap dipakai offline.' })
    setOfflineReady(false)
  }, [offlineReady, setOfflineReady, toast])

  return null
}
