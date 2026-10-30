// Sends users away from organizations they are not a member of. The API
// enforces this too; this only avoids rendering a broken page.
export default defineNuxtRouteMiddleware((to) => {
  const session = useSessionStore()
  const id = String(to.params.orgId ?? '')
  if (!session.membership(id)) return navigateTo('/orgs')
})
