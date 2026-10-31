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
    /** Returns a challenge when two-step verification is needed. */
    async login(email: string, password: string): Promise<{ challenge: string } | null> {
      const res = unwrap(await useApi().POST('/auth/login', { body: { email, password } }))
      if ('mfa_required' in res.data && res.data.mfa_required) return { challenge: res.data.challenge }
      this.csrfToken = (res.data as { csrf_token: string }).csrf_token
      await this.load(true)
      return null
    },
    async loginMFA(challenge: string, code: string) {
      const res = unwrap(await useApi().POST('/auth/login/mfa', { body: { challenge, code } }))
      this.csrfToken = res.data.csrf_token
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
