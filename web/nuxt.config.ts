import tailwindcss from '@tailwindcss/vite'

// The dashboard is a static SPA (no server-side rendering). Nginx serves the
// generated files on the same origin as the API, so the session cookie and
// CSRF protection work without CORS. See docs/architecture.md (ADR-3).
export default defineNuxtConfig({
  compatibilityDate: '2026-10-01',
  future: { compatibilityVersion: 4 },
  ssr: false,
  devtools: { enabled: false },
  devServer: { port: 3100 },
  modules: ['@pinia/nuxt', 'shadcn-nuxt'],
  css: ['~/assets/css/main.css'],
  vite: {
    plugins: [tailwindcss()],
    // The in-app guide (/help) is read from docs/huong-dan-su-dung.md.
    server: { fs: { allow: ['..'] } },
  },
  shadcn: { prefix: '', componentDir: './app/components/ui' },
  typescript: { strict: true, typeCheck: false },
  app: {
    head: {
      htmlAttrs: { lang: 'vi' },
      title: 'Shopee Shop Automation',
      meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }],
    },
  },
  nitro: {
    // Screenshots of the user guide, served at /huong-dan/*.
    publicAssets: [{ dir: '../../docs/huong-dan', baseURL: '/huong-dan', maxAge: 3600 }],
    // Development only: forward API calls to the Go server. The browser keeps
    // the dashboard origin, so PUBLIC_BASE_URL must be http://localhost:3100.
    devProxy: {
      '/api': { target: 'http://localhost:8080/api', changeOrigin: false },
    },
  },
})
