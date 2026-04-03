import { defineConfig, type Plugin } from 'vite'
import preact from '@preact/preset-vite'

// Remove crossorigin attributes from generated HTML.
// When the web UI is served from a LAN IP over plain HTTP, crossorigin="anonymous"
// on <script type="module"> causes browsers to apply CORS restrictions on same-origin
// requests, which breaks the page. Atlas doesn't use SRI so crossorigin isn't needed.
function removeCrossorigin(): Plugin {
  return {
    name: 'remove-crossorigin',
    transformIndexHtml(html) {
      return html.replace(/ crossorigin/g, '')
    },
  }
}

export default defineConfig({
  plugins: [preact(), removeCrossorigin()],
  base: '/web/',
  build: {
    // Output to dist/ — served by the Go binary via -web-dir flag.
    outDir: 'dist',
    emptyOutDir: true,
  },
})
