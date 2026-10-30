const publicPaths = ['/login', '/register', '/verify-email', '/forgot-password', '/reset-password']

export default defineNuxtRouteMiddleware(async (to) => {
  const session = useSessionStore()
  await session.load()
  const isPublic = publicPaths.includes(to.path)
  if (!isPublic && !session.user) {
    return navigateTo({ path: '/login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} })
  }
  if (session.user && (to.path === '/login' || to.path === '/register')) {
    return navigateTo('/')
  }
})
