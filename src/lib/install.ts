// "Add to home screen" support.
//  - Android/desktop Chrome: the browser fires `beforeinstallprompt` once; we keep the event so a button
//    can trigger the native install dialog later.
//  - iOS Safari has no such event: the only way is Share → "Tambahkan ke Layar Utama", so we show steps.

import { useSyncExternalStore } from 'react'

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export type InstallPlatform = 'ios' | 'other'

export interface InstallState {
  platform: InstallPlatform
  /** Already running as an installed app (home-screen icon). */
  standalone: boolean
  /** The browser offered a native install dialog. */
  canPrompt: boolean
  dismissed: boolean
}

const DISMISS_KEY = 'dietsis.install-dismissed'

export function detectPlatform(userAgent: string, platform: string, maxTouchPoints: number): InstallPlatform {
  if (/iphone|ipad|ipod/i.test(userAgent)) return 'ios'
  // iPadOS 13+ reports itself as a Mac, but has a touch screen.
  if (platform === 'MacIntel' && maxTouchPoints > 1) return 'ios'
  return 'other'
}

function isStandalone(): boolean {
  const mq = typeof window.matchMedia === 'function' && window.matchMedia('(display-mode: standalone)').matches
  return mq || (navigator as Navigator & { standalone?: boolean }).standalone === true
}

function readDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISS_KEY) === '1'
  } catch {
    return false
  }
}

let deferred: BeforeInstallPromptEvent | null = null
let installed = false
let dismissed = false
let snapshot: InstallState | null = null
const listeners = new Set<() => void>()

function compute(): InstallState {
  return {
    platform: detectPlatform(navigator.userAgent, navigator.platform, navigator.maxTouchPoints),
    standalone: installed || isStandalone(),
    canPrompt: deferred !== null,
    dismissed,
  }
}

function changed(): void {
  snapshot = compute()
  for (const l of listeners) l()
}

let started = false
/** Registers the browser listeners. Must run at startup: the event can fire before any component mounts. */
export function startInstallCapture(): void {
  if (started || typeof window === 'undefined') return
  started = true
  dismissed = readDismissed()
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault() // stop the mini-infobar; we show our own button
    deferred = e as BeforeInstallPromptEvent
    changed()
  })
  window.addEventListener('appinstalled', () => {
    deferred = null
    installed = true
    changed()
  })
  snapshot = compute()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  return () => listeners.delete(l)
}

function getSnapshot(): InstallState {
  startInstallCapture()
  snapshot ??= compute()
  return snapshot
}

export function useInstall(): InstallState {
  return useSyncExternalStore(subscribe, getSnapshot)
}

/** Opens the native install dialog (Android/Chrome). Returns whether the user accepted. */
export async function promptInstall(): Promise<boolean> {
  const e = deferred
  if (!e) return false
  deferred = null // the event can be used only once
  changed()
  await e.prompt()
  const { outcome } = await e.userChoice
  return outcome === 'accepted'
}

export function dismissInstallHint(): void {
  dismissed = true
  try {
    localStorage.setItem(DISMISS_KEY, '1')
  } catch {
    // Private mode: the hint just returns on the next visit.
  }
  changed()
}

/** The hint is useful only where installing is actually possible and not done yet. */
export function installable(s: InstallState): boolean {
  return !s.standalone && (s.canPrompt || s.platform === 'ios')
}

/** Test helper: forget captured state. */
export function resetInstallStateForTests(): void {
  deferred = null
  installed = false
  dismissed = false
  snapshot = null
  started = false
}
