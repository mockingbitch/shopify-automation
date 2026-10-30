import type { Role } from '~/utils/rbac'

/** useOrg exposes the organization in the current route and the caller's access. */
export function useOrg() {
  const route = useRoute()
  const session = useSessionStore()
  const orgId = computed(() => String(route.params.orgId ?? ''))
  const membership = computed(() => session.membership(orgId.value))
  const role = computed<Role>(() => (membership.value?.role ?? 'viewer') as Role)
  const can = (permission: string) => session.can(orgId.value, permission)
  return { orgId, membership, role, can }
}
