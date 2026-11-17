/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Google OAuth Web client ID (public). Empty → preview mode without Google login or sync. */
  readonly VITE_GOOGLE_CLIENT_ID?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
