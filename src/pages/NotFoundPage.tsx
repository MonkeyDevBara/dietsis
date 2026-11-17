import { Compass } from 'lucide-react'
import { Link } from 'react-router'
import { EmptyState } from '../components/ui/Card'

export default function NotFoundPage() {
  return (
    <EmptyState
      icon={<Compass className="size-6" />}
      title="Halaman tidak ditemukan"
      action={
        <Link to="/" className="inline-flex min-h-11 items-center rounded-control bg-primary px-4 font-semibold text-on-primary">
          Ke Beranda
        </Link>
      }
    >
      Alamat yang kamu buka tidak ada.
    </EmptyState>
  )
}
