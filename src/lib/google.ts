// Google Identity Services loader. The ID token it returns is sent to POST /auth/google, where the
// server verifies it; nothing here is trusted on its own.

export interface GoogleCredentialResponse {
  credential: string
}

interface GoogleId {
  initialize(config: { client_id: string; callback: (r: GoogleCredentialResponse) => void; use_fedcm_for_prompt?: boolean }): void
  renderButton(parent: HTMLElement, options: Record<string, unknown>): void
}

declare global {
  interface Window {
    google?: { accounts: { id: GoogleId } }
  }
}

const SRC = 'https://accounts.google.com/gsi/client'
let loading: Promise<GoogleId> | null = null

export function loadGoogleIdentity(): Promise<GoogleId> {
  const ready = window.google?.accounts.id
  if (ready) return Promise.resolve(ready)
  loading ??= new Promise<GoogleId>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = SRC
    script.async = true
    script.onload = () => {
      const id = window.google?.accounts.id
      if (id) resolve(id)
      else reject(new Error('Google Identity Services tidak tersedia'))
    }
    script.onerror = () => {
      loading = null // allow a retry once the network is back
      script.remove()
      reject(new Error('Tidak dapat memuat Google. Periksa koneksi internetmu.'))
    }
    document.head.append(script)
  })
  return loading
}
