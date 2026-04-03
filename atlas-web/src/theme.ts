/**
 * Atlas Theming Engine — V1.1
 *
 * Manages dark / light / system mode + accent colour.
 * ThemeConfig is the extensibility seam for V2:
 * add borderRadius, fontScale, etc. here and wire through applyTheme().
 */

export type ThemeMode = 'system' | 'light' | 'dark'
export type ThemePreset = 'atlas' | 'studio' | 'terminal'

export type DensityMode = 'compact' | 'comfortable' | 'spacious'

export type ChatFontSize = 'small' | 'default' | 'large'
export type ChatRadius   = 'sharp' | 'default' | 'rounded'
export type ChatFont     = 'default' | 'mono' | 'serif'
export type ChatAvatarStyle = 'glyph' | 'initial' | 'minimal'

export interface ThemeConfig {
  preset:       ThemePreset
  mode:         ThemeMode
  accent:       string
  density:      DensityMode
  chatFontSize: ChatFontSize
  chatRadius:   ChatRadius
  chatFont:     ChatFont
  chatAvatarStyle: ChatAvatarStyle
}

const STORAGE_KEY = 'atlas.theme'

export const DEFAULT_ACCENT = '#4D86C8'

type PresetModeTokens = Record<string, string>

export type ThemePresetOption = {
  id: ThemePreset
  label: string
  description: string
  preview: {
    light: {
      surface: string
      surfaceAlt: string
      accent: string
    }
    dark: {
      surface: string
      surfaceAlt: string
      accent: string
    }
  }
}

export const THEME_PRESETS: ThemePresetOption[] = [
  {
    id: 'atlas',
    label: 'Atlas',
    description: 'Quiet',
    preview: {
      light: {
        surface: '#f4f4f2',
        surfaceAlt: '#ffffff',
        accent: DEFAULT_ACCENT,
      },
      dark: {
        surface: '#1a1a1a',
        surfaceAlt: '#262626',
        accent: DEFAULT_ACCENT,
      },
    },
  },
  {
    id: 'studio',
    label: 'Studio',
    description: 'Refined',
    preview: {
      light: {
        surface: '#f4f1ec',
        surfaceAlt: '#fffdf9',
        accent: '#6D8FC9',
      },
      dark: {
        surface: '#1b1d23',
        surfaceAlt: '#2a2e38',
        accent: '#6D8FC9',
      },
    },
  },
  {
    id: 'terminal',
    label: 'Terminal',
    description: 'Focused',
    preview: {
      light: {
        surface: '#edf2ec',
        surfaceAlt: '#f7faf7',
        accent: '#7FA08A',
      },
      dark: {
        surface: '#121612',
        surfaceAlt: '#222822',
        accent: '#7FA08A',
      },
    },
  },
]

const PRESET_TOKENS: Record<ThemePreset, { light: PresetModeTokens; dark: PresetModeTokens }> = {
  atlas: {
    light: {},
    dark: {},
  },
  studio: {
    light: {
      '--bg': '#f4f1ec',
      '--surface': '#fcf8f2',
      '--surface-2': '#fffdf9',
      '--surface-3': '#e9dfd2',
      '--hover': 'rgba(54,38,28,0.05)',
      '--active-bg': 'rgba(54,38,28,0.082)',
      '--border': 'rgba(54,38,28,0.10)',
      '--border-2': 'rgba(54,38,28,0.18)',
      '--text': '#1b1510',
      '--text-2': '#6a5d52',
      '--text-3': '#9a8c80',
      '--shadow-bubble-ai': '0 10px 20px rgba(35,26,18,0.07), 0 2px 6px rgba(35,26,18,0.04)',
      '--shadow-bubble-user': '0 12px 24px color-mix(in srgb, var(--accent) 20%, transparent), 0 3px 8px rgba(35,26,18,0.08)',
      '--shadow-avatar': '0 8px 16px rgba(35,26,18,0.08), 0 1px 3px rgba(35,26,18,0.04)',
      '--theme-shadow-card': '0 20px 42px rgba(35, 26, 18, 0.09), 0 4px 12px rgba(35, 26, 18, 0.05)',
      '--theme-shadow-soft': '0 10px 20px rgba(35, 26, 18, 0.05)',
      '--theme-shadow-pop': '0 24px 46px rgba(35, 26, 18, 0.10)',
    },
    dark: {
      '--bg': '#0d0f14',
      '--surface': '#131720',
      '--surface-2': '#1a1f2a',
      '--surface-3': '#242b38',
      '--hover': 'rgba(255,255,255,0.045)',
      '--active-bg': 'rgba(255,255,255,0.07)',
      '--border': 'rgba(255,255,255,0.085)',
      '--border-2': 'rgba(255,255,255,0.15)',
      '--text': '#f3f1ed',
      '--text-2': '#9fa7b6',
      '--text-3': '#5b6272',
      '--shadow-bubble-ai': '0 0 16px rgba(142,152,178,0.06)',
      '--shadow-bubble-user': '0 0 20px color-mix(in srgb, var(--accent) 32%, transparent)',
      '--shadow-avatar': '0 0 12px rgba(142,152,178,0.05)',
      '--theme-shadow-card': '0 16px 36px rgba(0, 0, 0, 0.30)',
      '--theme-shadow-soft': '0 4px 12px rgba(0, 0, 0, 0.06)',
      '--theme-shadow-pop': '0 22px 42px rgba(0, 0, 0, 0.12)',
    },
  },
  terminal: {
    light: {
      '--bg': '#e4e8e3',
      '--surface': '#edf2ec',
      '--surface-2': '#f7faf7',
      '--surface-3': '#cfd7cf',
      '--hover': 'rgba(12,18,12,0.055)',
      '--active-bg': 'rgba(12,18,12,0.09)',
      '--border': 'rgba(12,18,12,0.16)',
      '--border-2': 'rgba(12,18,12,0.26)',
      '--text': '#0d120d',
      '--text-2': '#445044',
      '--text-3': '#687568',
      '--shadow-bubble-ai': '0 8px 16px rgba(18,24,18,0.06), 0 2px 5px rgba(18,24,18,0.03)',
      '--shadow-bubble-user': '0 10px 20px color-mix(in srgb, var(--accent) 18%, transparent), 0 2px 6px rgba(18,24,18,0.06)',
      '--shadow-avatar': '0 5px 12px rgba(18,24,18,0.08), 0 1px 2px rgba(18,24,18,0.04)',
      '--theme-shadow-card': '0 14px 30px rgba(18, 24, 18, 0.07), 0 2px 8px rgba(18, 24, 18, 0.04)',
      '--theme-shadow-soft': '0 6px 14px rgba(18, 24, 18, 0.04)',
      '--theme-shadow-pop': '0 18px 34px rgba(18, 24, 18, 0.08)',
    },
    dark: {
      '--bg': '#070907',
      '--surface': '#0d100d',
      '--surface-2': '#131813',
      '--surface-3': '#1a201a',
      '--hover': 'rgba(255,255,255,0.032)',
      '--active-bg': 'rgba(255,255,255,0.055)',
      '--border': 'rgba(255,255,255,0.075)',
      '--border-2': 'rgba(255,255,255,0.14)',
      '--text': '#e7efe7',
      '--text-2': '#94a093',
      '--text-3': '#505c50',
      '--shadow-bubble-ai': '0 0 12px rgba(130,152,130,0.04)',
      '--shadow-bubble-user': '0 0 16px color-mix(in srgb, var(--accent) 22%, transparent)',
      '--shadow-avatar': '0 0 8px rgba(130,152,130,0.04)',
      '--theme-shadow-card': '0 10px 22px rgba(0, 0, 0, 0.26)',
      '--theme-shadow-soft': '0 2px 8px rgba(0, 0, 0, 0.04)',
      '--theme-shadow-pop': '0 16px 32px rgba(0, 0, 0, 0.10)',
    },
  },
}

const PRESET_TOKEN_KEYS = Array.from(
  new Set(
    Object.values(PRESET_TOKENS).flatMap((preset) => [
      ...Object.keys(preset.light),
      ...Object.keys(preset.dark),
    ]),
  ),
)

export const DEFAULT_THEME: ThemeConfig = {
  preset:       'atlas',
  mode:         'system',
  accent:       DEFAULT_ACCENT,
  density:      'comfortable',
  chatFontSize: 'default',
  chatRadius:   'default',
  chatFont:     'default',
  chatAvatarStyle: 'glyph',
}

// ── Persistence ──────────────────────────────────────────────

export function loadTheme(): ThemeConfig {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_THEME
    return { ...DEFAULT_THEME, ...JSON.parse(raw) }
  } catch {
    return DEFAULT_THEME
  }
}

export function saveTheme(config: ThemeConfig): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(config))
}

// ── Application ──────────────────────────────────────────────

/** Resolves 'system' to the actual OS preference. */
function resolveMode(mode: ThemeMode): 'dark' | 'light' {
  if (mode !== 'system') return mode
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

const DENSITY_TOKENS: Record<DensityMode, Record<string, string>> = {
  compact:     { '--bubble-pad-v': '6px',  '--bubble-pad-h': '10px', '--chat-msg-gap': '8px'  },
  comfortable: { '--bubble-pad-v': '10px', '--bubble-pad-h': '14px', '--chat-msg-gap': '14px' },
  spacious:    { '--bubble-pad-v': '14px', '--bubble-pad-h': '18px', '--chat-msg-gap': '22px' },
}

const FONT_SIZE_TOKENS: Record<ChatFontSize, Record<string, string>> = {
  small:   { '--bubble-font-size': '13px' },
  default: { '--bubble-font-size': '15px' },
  large:   { '--bubble-font-size': '17px' },
}

const RADIUS_TOKENS: Record<ChatRadius, Record<string, string>> = {
  sharp:   { '--bubble-radius': '6px',  '--bubble-radius-notch': '2px' },
  default: { '--bubble-radius': '10px', '--bubble-radius-notch': '3px' },
  rounded: { '--bubble-radius': '18px', '--bubble-radius-notch': '5px' },
}

const FONT_TOKENS: Record<ChatFont, Record<string, string>> = {
  default: { '--bubble-font': "'Inter', -apple-system, 'Helvetica Neue', sans-serif" },
  mono:    { '--bubble-font': "'JetBrains Mono', 'SF Mono', 'Menlo', 'Courier New', monospace" },
  serif:   { '--bubble-font': "'Iowan Old Style', 'Palatino Linotype', 'Book Antiqua', Palatino, Georgia, serif" },
}

function writeTokens(tokens: Record<string, string>): void {
  Object.entries(tokens).forEach(([key, value]) => {
    document.documentElement.style.setProperty(key, value)
  })
}

function clearTokens(keys: string[]): void {
  keys.forEach((key) => {
    document.documentElement.style.removeProperty(key)
  })
}

function semanticAccentTokens(accent: string): Record<string, string> {
  return {
    '--theme-accent-fill': accent,
    '--theme-accent-fill-strong': accent,
    '--theme-accent-outline': `color-mix(in srgb, ${accent} 12%, transparent)`,
    '--theme-border-accent': `color-mix(in srgb, ${accent} 45%, var(--border-2))`,
    '--theme-focus-ring': `color-mix(in srgb, ${accent} 26%, transparent)`,
    '--theme-surface-accent': `color-mix(in srgb, ${accent} 9%, var(--surface-2))`,
    '--theme-surface-accent-strong': `color-mix(in srgb, ${accent} 12%, var(--surface-2))`,
    '--theme-shadow-accent': `color-mix(in srgb, ${accent} 24%, transparent)`,
    '--control-focus-ring': `color-mix(in srgb, ${accent} 26%, transparent)`,
    '--control-selected-bg': `color-mix(in srgb, ${accent} 9%, var(--surface-2))`,
    '--control-selected-bg-strong': `color-mix(in srgb, ${accent} 12%, var(--surface-2))`,
    '--control-selected-border': `color-mix(in srgb, ${accent} 45%, var(--border-2))`,
    '--control-selected-outline': `color-mix(in srgb, ${accent} 12%, transparent)`,
  }
}

/**
 * Writes data-theme onto <html> and injects accent + density + font size + radius CSS variables.
 * V2 note: this now also writes semantic runtime aliases so the full web app can
 * consume theme values through semantic tokens instead of raw accent/chat vars.
 */
export function applyTheme(config: ThemeConfig): void {
  const resolvedMode = resolveMode(config.mode)
  document.documentElement.setAttribute('data-theme', resolvedMode)
  document.documentElement.setAttribute('data-chat-avatar-style', config.chatAvatarStyle)
  clearTokens(PRESET_TOKEN_KEYS)
  writeTokens(PRESET_TOKENS[config.preset][resolvedMode])
  document.documentElement.style.setProperty('--accent', config.accent)
  writeTokens(semanticAccentTokens(config.accent))

  const densityTokens = DENSITY_TOKENS[config.density]
  writeTokens({
    ...densityTokens,
    '--theme-chat-gap': densityTokens['--chat-msg-gap'],
    '--theme-chat-pad-y': densityTokens['--bubble-pad-v'],
    '--theme-chat-pad-x': densityTokens['--bubble-pad-h'],
  })

  const fontSizeTokens = FONT_SIZE_TOKENS[config.chatFontSize]
  writeTokens({
    ...fontSizeTokens,
    '--theme-chat-font-size': fontSizeTokens['--bubble-font-size'],
  })

  const radiusTokens = RADIUS_TOKENS[config.chatRadius]
  writeTokens({
    ...radiusTokens,
    '--theme-chat-radius': radiusTokens['--bubble-radius'],
    '--theme-chat-radius-notch': radiusTokens['--bubble-radius-notch'],
  })

  const fontTokens = FONT_TOKENS[config.chatFont]
  writeTokens({
    ...fontTokens,
    '--theme-chat-font': fontTokens['--bubble-font'],
  })
}

/**
 * Watches for OS-level theme changes when mode is 'system'.
 * Returns a cleanup function — call it in useEffect's return.
 */
export function watchSystemTheme(config: ThemeConfig, onChanged: () => void): () => void {
  if (config.mode !== 'system') return () => {}
  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', onChanged)
  return () => mq.removeEventListener('change', onChanged)
}
