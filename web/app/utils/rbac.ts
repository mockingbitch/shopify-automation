// Mirrors internal/rbac for UI decisions. The API is always authoritative;
// these helpers only hide actions that would be rejected.
export type Role = 'owner' | 'admin' | 'operator' | 'viewer'

const rank: Record<Role, number> = { viewer: 10, operator: 20, admin: 30, owner: 40 }

export const assignableRoles: Role[] = ['admin', 'operator', 'viewer']

export function canAssign(actor: Role, target: Role): boolean {
  return target !== 'owner' && (actor === 'owner' || actor === 'admin') && rank[target] <= rank[actor]
}

export function canManageMember(actor: Role, member: Role): boolean {
  if (member === 'owner' || (actor !== 'owner' && actor !== 'admin')) return false
  return actor === 'owner' || rank[member] < rank[actor]
}

export const roleLabels: Record<Role, string> = {
  owner: 'Owner',
  admin: 'Admin',
  operator: 'Operator',
  viewer: 'Viewer',
}
