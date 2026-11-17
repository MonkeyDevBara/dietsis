import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { todayKey } from './lib/date'
import { renderApp } from './test/render'

async function signInAndOnboard(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: /masuk/i }))
  await screen.findByRole('heading', { name: /kenalan dulu/i })
  await user.clear(screen.getByLabelText('Nama'))
  await user.type(screen.getByLabelText('Nama'), 'Rina')
  await user.type(screen.getByLabelText('Usia'), '30')
  await user.type(screen.getByLabelText('Tinggi'), '160')
  await user.type(screen.getByLabelText('Berat sekarang'), '70')
  await user.type(screen.getByLabelText('Target berat'), '60')
  await user.click(screen.getByRole('radio', { name: /sedang/i }))
}

describe('app flow', () => {
  it('redirects anonymous users to login', async () => {
    renderApp('/diary')
    expect(await screen.findByRole('heading', { name: 'Dietsis' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /masuk/i })).toBeInTheDocument()
  })

  it('onboards with formula suggestions, then shows the dashboard and logs the first weight', async () => {
    const user = userEvent.setup()
    const { repo } = renderApp('/')
    await signInAndOnboard(user)

    // Suggestions appear live and match the shared vector (female 30y 160cm 70→60kg moderate).
    expect(screen.getByLabelText('Kalori')).toHaveValue('1650')
    expect(screen.getByLabelText('Protein')).toHaveValue('96')
    expect(screen.getByLabelText('Air')).toHaveValue('9')

    await user.click(screen.getByRole('button', { name: 'Mulai' }))
    expect(await screen.findByRole('heading', { name: 'Halo, Rina' })).toBeInTheDocument()
    expect(repo.getSnapshot().weights[todayKey()]?.weight_kg).toBe(70)
    expect(screen.getByText('1.650 kkal')).toBeInTheDocument()
  })

  it('keeps a manually edited target and offers to go back to the suggestion', async () => {
    const user = userEvent.setup()
    const { repo } = renderApp('/')
    await signInAndOnboard(user)
    await user.clear(screen.getByLabelText('Kalori'))
    await user.type(screen.getByLabelText('Kalori'), '1900')
    await user.click(screen.getByRole('button', { name: 'Mulai' }))
    await screen.findByRole('heading', { name: 'Halo, Rina' })
    expect(repo.getSnapshot().profile).toMatchObject({ calorie_target: 1900, calorie_target_manual: true, protein_target_manual: false })
  })

  it('warns when a manual calorie target is below the safe floor', async () => {
    const user = userEvent.setup()
    renderApp('/')
    await signInAndOnboard(user)
    await user.clear(screen.getByLabelText('Kalori'))
    await user.type(screen.getByLabelText('Kalori'), '1000')
    expect(screen.getByRole('alert')).toHaveTextContent(/batas aman/i)
  })

  it('shows validation errors instead of saving', async () => {
    const user = userEvent.setup()
    const { repo } = renderApp('/')
    await user.click(await screen.findByRole('button', { name: /masuk/i }))
    await user.click(await screen.findByRole('button', { name: 'Mulai' }))
    expect(screen.getAllByText(/wajib diisi angka/i).length).toBeGreaterThan(0)
    expect(repo.getSnapshot().profile).toBeNull()
  })
})

describe('daily logging', () => {
  async function dashboard() {
    const user = userEvent.setup()
    const ctx = renderApp('/')
    await signInAndOnboard(user)
    await user.click(screen.getByRole('button', { name: 'Mulai' }))
    await screen.findByRole('heading', { name: 'Halo, Rina' })
    return { user, ...ctx }
  }

  it('adds and removes water', async () => {
    const { user, repo } = await dashboard()
    await user.click(screen.getByRole('button', { name: /tambah satu gelas air/i }))
    await user.click(screen.getByRole('button', { name: /tambah satu gelas air/i }))
    expect(repo.getSnapshot().days[todayKey()]?.water_glasses).toBe(2)
    await user.click(screen.getByRole('button', { name: /kurangi satu gelas air/i }))
    expect(repo.getSnapshot().days[todayKey()]?.water_glasses).toBe(1)
  })

  it('logs food from search with a chosen quantity, updates totals, and undoes a quick add', async () => {
    const { user, repo } = await dashboard()
    await user.click(screen.getByRole('button', { name: /tambah makanan/i }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByRole('searchbox'), 'nasi putih')
    await user.click(within(dialog).getByRole('button', { name: /^nasi putih/i }))

    await user.click(screen.getByRole('button', { name: /tambah porsi/i })) // 1 → 1.5
    await user.click(screen.getByRole('button', { name: /^tambah ke /i }))
    await waitFor(() => expect(Object.values(repo.getSnapshot().foodLogs)).toHaveLength(1))
    const log = Object.values(repo.getSnapshot().foodLogs)[0]
    expect(log).toMatchObject({ quantity: 1.5, food_id: 'b:nasi-putih', date: todayKey() })
    expect(log?.totals.kcal).toBe(293)
    expect(await screen.findByText(/dimakan/i)).toBeInTheDocument()

    // Quick add + undo
    await user.click(screen.getByRole('button', { name: /tambah makanan/i }))
    await user.click(await screen.findByRole('button', { name: /tambah nasi putih \(1,5 porsi\)/i }))
    expect(Object.values(repo.getSnapshot().foodLogs).filter((l) => l.deleted_at === null)).toHaveLength(2)
    await user.click(await screen.findByRole('button', { name: 'Urungkan' }))
    expect(Object.values(repo.getSnapshot().foodLogs).filter((l) => l.deleted_at === null)).toHaveLength(1)
  })

  it('creates a custom food and logs it', async () => {
    const { user, repo } = await dashboard()
    await user.click(screen.getByRole('button', { name: /tambah makanan/i }))
    await user.click(await screen.findByRole('button', { name: /buat makanan kustom/i }))
    const form = within(await screen.findByRole('dialog', { name: 'Makanan kustom' }))
    await user.type(form.getByLabelText('Nama makanan'), 'Smoothie bayam')
    await user.type(form.getByLabelText('Kalori'), '180')
    await user.click(form.getByRole('button', { name: /simpan makanan/i }))
    await user.click(await screen.findByRole('button', { name: /^tambah ke /i }))
    await waitFor(() => expect(Object.keys(repo.getSnapshot().customFoods)).toHaveLength(1))
    const log = Object.values(repo.getSnapshot().foodLogs)[0]
    expect(log?.food_id).toMatch(/^c:/)
    expect(log?.kcal).toBe(180)
  })

  it('requires typing HAPUS before deleting all data', async () => {
    const { user, repo } = await dashboard()
    await user.click(screen.getAllByRole('link', { name: 'Profil' })[0] as HTMLElement)
    await user.click(await screen.findByRole('link', { name: 'Pengaturan' }))
    await user.click(await screen.findByRole('button', { name: 'Hapus seluruh data' }))
    const dialog = await screen.findByRole('dialog')
    const confirm = within(dialog).getByRole('button', { name: 'Hapus semua' })
    expect(confirm).toBeDisabled()
    await user.type(within(dialog).getByLabelText(/ketik hapus/i), 'HAPUS')
    expect(confirm).toBeEnabled()
    await user.click(confirm)
    await waitFor(() => expect(repo.getSnapshot().profile).toBeNull())
    expect(await screen.findByRole('heading', { name: /kenalan dulu/i })).toBeInTheDocument()
  })
})
