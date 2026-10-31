// Sends users away from organizations they are not a member of, and owners
// and admins without two-step verification away from organizations that
// require it. The API enforces both; this only avoids broken pages.
export default defineNuxtRouteMiddleware((to) => {
  const session = useSessionStore()
  const id = String(to.params.orgId ?? '')
  const m = session.membership(id)
  if (!m) return navigateTo('/orgs')
  const privileged = m.role === 'owner' || m.role === 'admin'
  if (m.organization.settings.require_mfa && privileged && !session.user?.mfa_enabled) {
    return navigateTo({ path: '/account', query: { required: m.organization.name } })
  }
})
