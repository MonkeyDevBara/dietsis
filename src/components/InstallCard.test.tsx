import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { detectPlatform, resetInstallStateForTests, startInstallCapture } from '../lib/install'
import { InstallCard } from './InstallCard'

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 Version/17.4 Mobile/15E148 Safari/604.1'
const ANDROID = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/124.0 Mobile Safari/537.36'

// jsdom lacks several navigator fields, so they are defined as own properties and removed after each test.
const defined: string[] = []
function defineNav(key: string, value: unknown) {
  Object.defineProperty(navigator, key, { value, configurable: true })
  defined.push(key)
}

function setUA(ua: string, platform = 'Linux armv81', touch = 5) {
  defineNav('userAgent', ua)
  defineNav('platform', platform)
  defineNav('maxTouchPoints', touch)
}

function installPromptEvent(outcome: 'accepted' | 'dismissed') {
  const e = new Event('beforeinstallprompt', { cancelable: true }) as Event & { prompt: ReturnType<typeof vi.fn>; userChoice: Promise<{ outcome: string }> }
  e.prompt = vi.fn(() => Promise.resolve())
  e.userChoice = Promise.resolve({ outcome })
  return e
}

beforeEach(() => {
  localStorage.clear()
  resetInstallStateForTests()
})
afterEach(() => {
  for (const key of defined.splice(0)) Reflect.deleteProperty(navigator, key)
})

describe('detectPlatform', () => {
  it('recognises iPhone, iPad and iPadOS pretending to be a Mac', () => {
    expect(detectPlatform(IPHONE, 'iPhone', 5)).toBe('ios')
    expect(detectPlatform('Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)', 'iPad', 5)).toBe('ios')
    expect(detectPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'MacIntel', 5)).toBe('ios')
    expect(detectPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'MacIntel', 0)).toBe('other')
    expect(detectPlatform(ANDROID, 'Linux armv81', 5)).toBe('other')
  })
})

describe('InstallCard', () => {
  it('shows nothing where installing is impossible (desktop browser without the install event)', () => {
    setUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Firefox/125.0', 'MacIntel', 0)
    startInstallCapture()
    const { container } = render(<InstallCard />)
    expect(container).toBeEmptyDOMElement()
  })

  it('Android/Chrome: captures the browser event, shows a button, and opens the native dialog', async () => {
    setUA(ANDROID)
    startInstallCapture()
    const user = userEvent.setup()
    render(<InstallCard />)
    expect(screen.queryByText(/pasang dietsis/i)).not.toBeInTheDocument()

    const ev = installPromptEvent('accepted')
    act(() => {
      window.dispatchEvent(ev)
    })
    expect(ev.defaultPrevented).toBe(true) // our button replaces the browser's own mini-infobar
    await user.click(await screen.findByRole('button', { name: /pasang sekarang/i }))
    expect(ev.prompt).toHaveBeenCalledOnce()
    // The event is single-use: the button goes away afterwards.
    expect(screen.queryByRole('button', { name: /pasang sekarang/i })).not.toBeInTheDocument()
  })

  it('hides once the app reports it was installed', () => {
    setUA(ANDROID)
    startInstallCapture()
    render(<InstallCard />)
    act(() => {
      window.dispatchEvent(installPromptEvent('dismissed'))
    })
    expect(screen.getByText(/pasang dietsis/i)).toBeInTheDocument()
    act(() => {
      window.dispatchEvent(new Event('appinstalled'))
    })
    expect(screen.queryByText(/pasang dietsis/i)).not.toBeInTheDocument()
  })

  it('iOS: explains Share → Tambahkan ke Layar Utama (no button, no native event exists)', () => {
    setUA(IPHONE, 'iPhone')
    startInstallCapture()
    render(<InstallCard />)
    expect(screen.getByText(/tambahkan ke layar utama/i)).toBeInTheDocument()
    expect(screen.getByText('Bagikan')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /pasang sekarang/i })).not.toBeInTheDocument()
  })

  it('dismissing hides the hint, survives a reload, but stays available in Settings', async () => {
    setUA(IPHONE, 'iPhone')
    startInstallCapture()
    const user = userEvent.setup()
    const { unmount } = render(<InstallCard />)
    await user.click(screen.getByRole('button', { name: /tutup saran/i }))
    expect(screen.queryByText(/pasang dietsis/i)).not.toBeInTheDocument()
    expect(localStorage.getItem('dietsis.install-dismissed')).toBe('1')
    unmount()

    resetInstallStateForTests() // simulates a fresh page load; localStorage persists
    startInstallCapture()
    const again = render(<InstallCard />)
    expect(again.container).toBeEmptyDOMElement()
    again.unmount()

    render(<InstallCard variant="settings" />)
    expect(screen.getByText(/pasang dietsis/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /tutup saran/i })).not.toBeInTheDocument()
  })

  it('shows nothing when already running as an installed app', () => {
    setUA(IPHONE, 'iPhone')
    defineNav('standalone', true)
    startInstallCapture()
    const { container } = render(<InstallCard />)
    expect(container).toBeEmptyDOMElement()
  })
})
