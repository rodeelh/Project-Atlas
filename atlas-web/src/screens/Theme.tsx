import { type JSX } from 'preact'
import { useRef } from 'preact/hooks'
import { PageHeader } from '../components/PageHeader'
import {
  type ThemePreset,
  type ThemeMode,
  type DensityMode,
  type ChatFontSize,
  type ChatRadius,
  type ChatFont,
  type ChatAvatarStyle,
  DEFAULT_ACCENT,
  THEME_PRESETS,
} from '../theme'

interface Props {
  activePreset: ThemePreset
  onPresetChange: (preset: ThemePreset) => void
  activeTheme: ThemeMode
  onThemeChange: (mode: ThemeMode) => void
  activeAccent: string
  onAccentChange: (accent: string) => void
  activeDensity: DensityMode
  onDensityChange: (d: DensityMode) => void
  activeChatFontSize: ChatFontSize
  onChatFontSizeChange: (s: ChatFontSize) => void
  activeChatRadius: ChatRadius
  onChatRadiusChange: (r: ChatRadius) => void
  activeChatFont: ChatFont
  onChatFontChange: (f: ChatFont) => void
  activeChatAvatarStyle: ChatAvatarStyle
  onChatAvatarStyleChange: (style: ChatAvatarStyle) => void
}

const modes: { id: ThemeMode; label: string; sublabel: string; icon: preact.ComponentChild }[] = [
  {
    id: 'system',
    label: 'System',
    sublabel: 'Use system',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <rect x="2" y="3" width="20" height="14" rx="2" />
        <path d="M8 21h8M12 17v4" />
        <path d="M2 10h20" />
      </svg>
    ),
  },
  {
    id: 'light',
    label: 'Light',
    sublabel: 'Always light',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
      </svg>
    ),
  },
  {
    id: 'dark',
    label: 'Dark',
    sublabel: 'Always dark',
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 12.79A9 9 0 1111.21 3a7 7 0 009.79 9.79z" />
      </svg>
    ),
  },
]

const ACCENT_PRESETS = [
  { color: '#7E7E7E', label: 'Neutral' },
  { color: DEFAULT_ACCENT, label: 'Atlas Blue' },
  { color: '#6B8EC8', label: 'Cornflower' },
  { color: '#8B82C8', label: 'Lavender' },
  { color: '#6BA8A4', label: 'Sage Teal' },
  { color: '#8BA882', label: 'Sage Green' },
  { color: '#C88B82', label: 'Dusty Rose' },
]

const densities: { id: DensityMode; label: string; sublabel: string }[] = [
  { id: 'compact', label: 'Compact', sublabel: 'Tighter spacing' },
  { id: 'comfortable', label: 'Comfortable', sublabel: 'Default rhythm' },
  { id: 'spacious', label: 'Spacious', sublabel: 'Roomier bubbles' },
]

const fontSizes: { id: ChatFontSize; label: string; sublabel: string }[] = [
  { id: 'small', label: 'Small', sublabel: 'Smaller text' },
  { id: 'default', label: 'Default', sublabel: 'Balanced reading' },
  { id: 'large', label: 'Large', sublabel: 'Larger type' },
]

const radii: { id: ChatRadius; label: string; sublabel: string }[] = [
  { id: 'sharp', label: 'Sharp', sublabel: 'Clean corners' },
  { id: 'default', label: 'Default', sublabel: 'Soft rounding' },
  { id: 'rounded', label: 'Rounded', sublabel: 'Pill style' },
]

const fonts: { id: ChatFont; label: string; sublabel: string; sample: string }[] = [
  { id: 'default', label: 'Default', sublabel: 'Inter', sample: 'Aa' },
  { id: 'mono', label: 'Terminal', sublabel: 'JetBrains Mono', sample: 'Aa' },
  { id: 'serif', label: 'Serif', sublabel: 'Iowan', sample: 'Aa' },
]

const avatarStyles: { id: ChatAvatarStyle; label: string; sublabel: string }[] = [
  { id: 'glyph', label: 'Glyph', sublabel: 'Branded icon' },
  { id: 'initial', label: 'Initial', sublabel: 'Letter mark' },
  { id: 'minimal', label: 'Minimal', sublabel: 'Quiet dot' },
]

const FONT_FAMILIES: Record<ChatFont, string> = {
  default: "'Inter', -apple-system, sans-serif",
  mono: "'JetBrains Mono', 'SF Mono', 'Menlo', monospace",
  serif: "'Iowan Old Style', 'Palatino Linotype', 'Book Antiqua', Palatino, Georgia, serif",
}

const FONT_SIZE_PX: Record<ChatFontSize, string> = {
  small: '13px',
  default: '15px',
  large: '17px',
}

const RADIUS_PX: Record<ChatRadius, string> = {
  sharp: '6px',
  default: '12px',
  rounded: '18px',
}

const DENSITY_GAP: Record<DensityMode, string> = {
  compact: '8px',
  comfortable: '13px',
  spacious: '18px',
}

const PREVIEW_BUBBLE_PADDING: Record<DensityMode, [string, string]> = {
  compact: ['7px', '11px'],
  comfortable: ['10px', '14px'],
  spacious: ['13px', '18px'],
}

const DENSITY_LINE_SCALE: Record<DensityMode, number[]> = {
  compact: [16, 24, 12],
  comfortable: [18, 28, 15],
  spacious: [20, 30, 18],
}

function SectionTitle({ children }: { children: preact.ComponentChild }) {
  return <div class="appearance-section-title">{children}</div>
}

function ThemePresetPicker({
  activePreset,
  resolvedMode,
  onPresetChange,
}: {
  activePreset: ThemePreset
  resolvedMode: 'light' | 'dark'
  onPresetChange: (preset: ThemePreset) => void
}) {
  return (
    <div class="appearance-control-block">
      <div class="appearance-control-heading">Themes</div>
      <div class="appearance-theme-grid" role="listbox" aria-label="Themes">
        {THEME_PRESETS.map((preset) => {
          const active = activePreset === preset.id
          const preview = preset.preview[resolvedMode]
          return (
            <button
              key={preset.id}
              class={`appearance-choice-row appearance-theme-choice${active ? ' is-active' : ''}`}
              onClick={() => onPresetChange(preset.id)}
              role="option"
              aria-selected={active}
              style={{
                '--appearance-theme-preview-surface': preview.surface,
                '--appearance-theme-preview-surface-alt': preview.surfaceAlt,
                '--appearance-theme-preview-accent': preview.accent,
              } as JSX.CSSProperties}
            >
              <div class="appearance-choice-leading">
                <ThemePresetGlyph preset={preset.id} />
              </div>
              <div class="appearance-choice-copy">
                <div class="appearance-choice-label">{preset.label}</div>
                <div class="appearance-choice-sublabel">{preset.description}</div>
              </div>
              {active && <CheckIcon className="appearance-check" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ThemePresetGlyph({ preset }: { preset: ThemePreset }) {
  if (preset === 'atlas') {
    return (
      <svg class="appearance-theme-glyph" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 3l6.2 9L12 21 5.8 12 12 3z" />
        <path d="M8.7 10.5h6.6" />
      </svg>
    )
  }
  if (preset === 'studio') {
    return (
      <svg class="appearance-theme-glyph" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="6.5" />
        <circle cx="12" cy="12" r="2.2" />
        <path d="M12 5.5v2.2M12 16.3v2.2M5.5 12h2.2M16.3 12h2.2" />
      </svg>
    )
  }
  return (
    <svg class="appearance-theme-glyph" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">
      <path d="M5.5 7.5h13" />
      <path d="M5.5 12h7" />
      <path d="M5.5 16.5h10" />
      <path d="M15.5 10.5l3 1.5-3 1.5" />
    </svg>
  )
}

function OptionList({
  title,
  options,
  activeID,
  onChange,
  renderLeading,
}: {
  title: string
  options: { id: string; label: string; sublabel: string }[]
  activeID: string
  onChange: (id: string) => void
  renderLeading: (id: string, active: boolean) => preact.ComponentChild
}) {
  return (
    <div class="appearance-control-block">
      <div class="appearance-control-heading">{title}</div>
      <div class="appearance-choice-list" role="listbox" aria-label={title}>
        {options.map((option) => {
          const active = activeID === option.id
          return (
            <button
              key={option.id}
              class={`appearance-choice-row${active ? ' is-active' : ''}`}
              onClick={() => onChange(option.id)}
              role="option"
              aria-selected={active}
            >
              <div class="appearance-choice-leading">{renderLeading(option.id, active)}</div>
              <div class="appearance-choice-copy">
                <div class="appearance-choice-label">{option.label}</div>
                <div class="appearance-choice-sublabel">{option.sublabel}</div>
              </div>
              {active && <CheckIcon className="appearance-check" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function DensityGlyph({ id }: { id: DensityMode }) {
  return (
    <div class="appearance-density-glyph">
      {DENSITY_LINE_SCALE[id].map((width, index) => (
        <span key={index} style={{ width: `${width}px` }} />
      ))}
    </div>
  )
}

function RadiusGlyph({ id, active }: { id: ChatRadius; active: boolean }) {
  return (
    <div class="appearance-radius-glyph">
      <span
        style={{
          borderRadius: RADIUS_PX[id],
          borderColor: active ? 'var(--text)' : 'var(--text-2)',
        }}
      />
    </div>
  )
}

function TypeGlyph({ sample, fontFamily, fontSize = '15px' }: { sample: string; fontFamily: string; fontSize?: string }) {
  return (
    <div class="appearance-type-glyph" style={{ fontFamily, fontSize }}>
      {sample}
    </div>
  )
}

function InlineAvatarPreview({ id }: { id: ChatAvatarStyle }) {
  return (
    <div class={`appearance-inline-avatar appearance-inline-avatar-${id}`}>
      <span class="appearance-inline-avatar-content appearance-inline-avatar-content-glyph">
        <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
          <circle cx="8" cy="5.5" r="3" />
          <path d="M2.5 15c0-3 2.5-5.5 5.5-5.5S13.5 12 13.5 15" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" fill="none" />
        </svg>
      </span>
      <span class="appearance-inline-avatar-content appearance-inline-avatar-content-initial">A</span>
      <span class="appearance-inline-avatar-content appearance-inline-avatar-content-minimal">
        <span class="appearance-inline-avatar-dot" />
      </span>
    </div>
  )
}

function InlineAvatarControl({
  activeID,
  onChange,
}: {
  activeID: ChatAvatarStyle
  onChange: (id: ChatAvatarStyle) => void
}) {
  return (
    <div class="appearance-control-block appearance-inline-control appearance-inline-control-full">
      <div class="appearance-control-heading">Avatar</div>
      <div class="appearance-inline-choice-list" role="listbox" aria-label="Avatar">
        {avatarStyles.map((option) => {
          const active = activeID === option.id
          return (
            <button
              key={option.id}
              class={`appearance-inline-choice${active ? ' is-active' : ''}`}
              onClick={() => onChange(option.id)}
              role="option"
              aria-selected={active}
            >
              <span class="appearance-inline-choice-leading">
                <InlineAvatarPreview id={option.id} />
              </span>
              <span class="appearance-inline-choice-label">{option.label}</span>
              {active && <CheckIcon className="appearance-inline-choice-check" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg class={className} width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
      <path d="M3 8l3.5 3.5L13 4.5" />
    </svg>
  )
}

export function Theme({
  activePreset,
  onPresetChange,
  activeTheme,
  onThemeChange,
  activeAccent,
  onAccentChange,
  activeDensity,
  onDensityChange,
  activeChatFontSize,
  onChatFontSizeChange,
  activeChatRadius,
  onChatRadiusChange,
  activeChatFont,
  onChatFontChange,
  activeChatAvatarStyle,
  onChatAvatarStyleChange,
}: Props) {
  const colorInputRef = useRef<HTMLInputElement>(null)
  const currentPreset = THEME_PRESETS.find((preset) => preset.id === activePreset) ?? THEME_PRESETS[0]
  const resolvedMode: 'light' | 'dark' =
    activeTheme === 'system'
      ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : activeTheme

  const previewStyle = {
    '--appearance-accent': activeAccent,
    '--appearance-preview-font': FONT_FAMILIES[activeChatFont],
    '--appearance-preview-font-size': FONT_SIZE_PX[activeChatFontSize],
    '--appearance-preview-radius': RADIUS_PX[activeChatRadius],
    '--appearance-preview-gap': DENSITY_GAP[activeDensity],
    '--appearance-preview-pad-y': PREVIEW_BUBBLE_PADDING[activeDensity][0],
    '--appearance-preview-pad-x': PREVIEW_BUBBLE_PADDING[activeDensity][1],
  } as JSX.CSSProperties

  return (
    <div class="screen">
      <PageHeader title="Appearance" subtitle="Themes, accent colour, and chat display" />

      <div class="appearance-screen">
        <div class="appearance-layout">
          <div class="appearance-column">
            <div class="card appearance-card">
              <div class="card-body appearance-card-body">
                <SectionTitle>Theme</SectionTitle>
                <ThemePresetPicker
                  activePreset={activePreset}
                  resolvedMode={resolvedMode}
                  onPresetChange={onPresetChange}
                />
                <div class="appearance-mode-grid">
                  {modes.map((mode) => {
                    const active = activeTheme === mode.id
                    return (
                      <button
                        key={mode.id}
                        class={`appearance-choice-row${active ? ' is-active' : ''}`}
                        onClick={() => onThemeChange(mode.id)}
                        role="option"
                        aria-selected={active}
                      >
                        <div class="appearance-choice-leading">{mode.icon}</div>
                        <div class="appearance-choice-copy">
                          <div class="appearance-choice-label">{mode.label}</div>
                          <div class="appearance-choice-sublabel">{mode.sublabel}</div>
                        </div>
                        {active && <CheckIcon className="appearance-check" />}
                      </button>
                    )
                  })}
                </div>

                <SectionTitle>Accent</SectionTitle>
                <div class="appearance-accent-toolbar">
                  <div class="appearance-accent-swatches">
                    {ACCENT_PRESETS.map((preset) => {
                      const active = activeAccent.toLowerCase() === preset.color.toLowerCase()
                      return (
                        <button
                          key={preset.color}
                          class={`appearance-swatch${active ? ' is-active' : ''}`}
                          title={preset.label}
                          aria-label={preset.label}
                          onClick={() => onAccentChange(preset.color)}
                          style={{ background: preset.color }}
                        />
                      )
                    })}
                  </div>

                  <div class="appearance-accent-divider" />

                  <button
                    class="appearance-swatch appearance-swatch-custom"
                    title="Custom colour"
                    aria-label="Custom colour"
                    onClick={() => colorInputRef.current?.click()}
                  >
                    <input
                      ref={colorInputRef}
                      type="color"
                      value={activeAccent}
                      onInput={(e) => onAccentChange((e.target as HTMLInputElement).value)}
                      class="appearance-hidden-color-input"
                    />
                  </button>
                </div>
              </div>
            </div>

            <div class="card appearance-card appearance-preview-card">
              <div class="card-body appearance-card-body">
                <SectionTitle>Preview</SectionTitle>
                <div class={`appearance-preview-frame appearance-preview-avatar-style-${activeChatAvatarStyle}`} style={previewStyle}>
                  <div class="appearance-preview-toolbar">
                    <div class="appearance-preview-tab">{currentPreset.label} theme</div>
                    <div class="appearance-preview-status">Current</div>
                  </div>

                  <div class="appearance-preview-thread">
                    <div class="appearance-preview-row">
                      <div class="appearance-preview-avatar appearance-preview-avatar-ai">
                        <span class="appearance-preview-avatar-glyph">
                          <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
                            <circle cx="8" cy="5.5" r="3" />
                            <path d="M2.5 15c0-3 2.5-5.5 5.5-5.5S13.5 12 13.5 15" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" fill="none" />
                          </svg>
                        </span>
                        <span class="appearance-preview-avatar-initial">A</span>
                        <span class="appearance-preview-avatar-minimal"><span /></span>
                      </div>
                      <div class="appearance-preview-bubble appearance-preview-bubble-ai">
                        Your workspace is ready. Want a clean summary or should I keep digging?
                      </div>
                    </div>

                    <div class="appearance-preview-row appearance-preview-row-user">
                      <div class="appearance-preview-avatar appearance-preview-avatar-user">
                        <span class="appearance-preview-avatar-glyph">
                          <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
                            <circle cx="8" cy="5.5" r="3" />
                            <path d="M2.5 15c0-3 2.5-5.5 5.5-5.5S13.5 12 13.5 15" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" fill="none" />
                          </svg>
                        </span>
                        <span class="appearance-preview-avatar-initial">Y</span>
                        <span class="appearance-preview-avatar-minimal"><span /></span>
                      </div>
                      <div class="appearance-preview-bubble appearance-preview-bubble-user">
                        Keep digging, but make it easy to scan.
                      </div>
                    </div>

                    <div class="appearance-preview-row">
                      <div class="appearance-preview-avatar appearance-preview-avatar-ai">
                        <span class="appearance-preview-avatar-glyph">
                          <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
                            <circle cx="8" cy="5.5" r="3" />
                            <path d="M2.5 15c0-3 2.5-5.5 5.5-5.5S13.5 12 13.5 15" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" fill="none" />
                          </svg>
                        </span>
                        <span class="appearance-preview-avatar-initial">A</span>
                        <span class="appearance-preview-avatar-minimal"><span /></span>
                      </div>
                      <div class="appearance-preview-bubble appearance-preview-bubble-ai">
                        Clean, focused, and Atlas-native. Looking good already.
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>

          </div>

          <div class="appearance-column">
            <div class="card appearance-card">
              <div class="card-body appearance-card-body">
                <SectionTitle>Chat Display</SectionTitle>
                <div class="appearance-control-grid">
                  <InlineAvatarControl
                    activeID={activeChatAvatarStyle}
                    onChange={onChatAvatarStyleChange}
                  />

                  <OptionList
                    title="Density"
                    options={densities}
                    activeID={activeDensity}
                    onChange={(id) => onDensityChange(id as DensityMode)}
                    renderLeading={(id) => <DensityGlyph id={id as DensityMode} />}
                  />

                  <OptionList
                    title="Font Size"
                    options={fontSizes}
                    activeID={activeChatFontSize}
                    onChange={(id) => onChatFontSizeChange(id as ChatFontSize)}
                    renderLeading={(id) => (
                      <TypeGlyph
                        sample="Aa"
                        fontFamily={FONT_FAMILIES.default}
                        fontSize={FONT_SIZE_PX[id as ChatFontSize]}
                      />
                    )}
                  />

                  <OptionList
                    title="Corners"
                    options={radii}
                    activeID={activeChatRadius}
                    onChange={(id) => onChatRadiusChange(id as ChatRadius)}
                    renderLeading={(id, active) => <RadiusGlyph id={id as ChatRadius} active={active} />}
                  />

                  <OptionList
                    title="Font"
                    options={fonts}
                    activeID={activeChatFont}
                    onChange={(id) => onChatFontChange(id as ChatFont)}
                    renderLeading={(id) => (
                      <TypeGlyph sample="Aa" fontFamily={FONT_FAMILIES[id as ChatFont]} />
                    )}
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
