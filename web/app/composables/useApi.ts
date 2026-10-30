import createClient, { type Client, type Middleware } from 'openapi-fetch'
import type { paths } from '~/types/api'

let client: Client<paths> | undefined

const unsafe = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])

/**
 * useApi returns the typed API client. It attaches the CSRF token to unsafe
 * requests and sends the user to the login page when the session expires.
 */
export function useApi(): Client<paths> {
  if (client) return client
  const session = useSessionStore()

  const middleware: Middleware = {
    onRequest({ request }) {
      if (unsafe.has(request.method) && session.csrfToken) {
        request.headers.set('X-CSRF-Token', session.csrfToken)
      }
      return request
    },
    onResponse({ request, response }) {
      const path = new URL(request.url).pathname
      if (response.status === 401 && !path.startsWith('/api/v1/auth/') && session.user) {
        session.reset()
        const route = useRoute()
        void navigateTo({ path: '/login', query: { next: route.fullPath } })
      }
      return response
    },
  }

  client = createClient<paths>({ baseUrl: '/api/v1', credentials: 'same-origin' })
  client.use(middleware)
  return client
}
