import { defineStore } from 'pinia'
import type { components } from '~/types/api'

type User = components['schemas']['User']
type Membership = components['schemas']['Membership']

export const useSessionStore = defineStore('session', {
  state: () => ({
    user: null as User | null,
    csrfToken: '',
    memberships: [] as Membership[],
    loaded: false,
  }),
  getters: {
    membership: (s) => (orgId: string) => s.memberships.find((m) => m.organization.id === orgId),
  },
  actions: {
    reset() {
      this.user = null
      this.csrfToken = ''
      this.memberships = []
    },
    /** load fetches the current session; anonymous visitors end with user = null. */
    async load(force = false) {
      if (this.loaded && !force) return
      const res = await useApi().GET('/auth/session')
      if (res.data) {
        this.user = res.data.data.user
        this.csrfToken = res.data.data.csrf_token
        this.memberships = res.data.data.memberships ?? []
      } else {
        this.reset()
      }
      this.loaded = true
    },
    async login(email: string, password: string) {
      const data = unwrap(await useApi().POST('/auth/login', { body: { email, password } }))
      this.csrfToken = data.data.csrf_token
      await this.load(true)
    },
    async logout() {
      await useApi().POST('/auth/logout')
      this.reset()
    },
    can(orgId: string, permission: string): boolean {
      return this.membership(orgId)?.permissions.includes(permission) ?? false
    },
  },
})
