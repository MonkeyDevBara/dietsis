import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { ConfirmDialog, Sheet } from './Sheet'

function Harness({ onClose = () => undefined }: { onClose?: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button onClick={() => setOpen(true)}>buka</button>
      <Sheet
        open={open}
        title="Judul"
        onClose={() => {
          onClose()
          setOpen(false)
        }}
        footer={<button>simpan</button>}
      >
        <input aria-label="isi" />
      </Sheet>
    </>
  )
}

describe('Sheet', () => {
  it('is a modal dialog; focus moves in, Tab wraps, Escape closes and returns focus', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    const opener = screen.getByRole('button', { name: 'buka' })
    await user.click(opener)

    const dialog = screen.getByRole('dialog', { name: 'Judul' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('button', { name: 'Tutup' })).toHaveFocus()

    await user.tab() // Tutup → isi
    await user.tab() // isi → simpan
    expect(screen.getByRole('button', { name: 'simpan' })).toHaveFocus()
    await user.tab() // wraps to the first focusable
    expect(screen.getByRole('button', { name: 'Tutup' })).toHaveFocus()
    await user.tab({ shift: true }) // wraps backwards
    expect(screen.getByRole('button', { name: 'simpan' })).toHaveFocus()

    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(opener).toHaveFocus()
    expect(document.body.style.overflow).not.toBe('hidden')
  })
})

describe('ConfirmDialog', () => {
  it('only confirms once the required word is typed exactly', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn()
    render(<ConfirmDialog open title="Hapus?" message="yakin" confirmLabel="Hapus" typeToConfirm="HAPUS" danger onConfirm={onConfirm} onClose={() => undefined} />)
    const confirm = screen.getByRole('button', { name: 'Hapus' })
    expect(confirm).toBeDisabled()
    await user.type(screen.getByLabelText(/ketik hapus/i), 'hapus')
    expect(confirm).toBeDisabled()
    await user.clear(screen.getByLabelText(/ketik hapus/i))
    await user.type(screen.getByLabelText(/ketik hapus/i), 'HAPUS')
    await user.click(confirm)
    expect(onConfirm).toHaveBeenCalledOnce()
  })
})
