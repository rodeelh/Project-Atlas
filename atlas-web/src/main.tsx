import { render } from 'preact'
import { App } from './App'
import './styles.css'
import { loadTheme, applyTheme } from './theme'

// Apply persisted theme before first paint — prevents flash of wrong theme
applyTheme(loadTheme())

render(<App />, document.getElementById('app')!)
