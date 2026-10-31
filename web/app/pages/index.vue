<script setup lang="ts">
// The home page only routes to the first organization (or to creating one).
definePageMeta({
  middleware: () => {
    const first = useSessionStore().memberships[0]
    return navigateTo(first ? `/orgs/${first.organization.id}` : '/orgs/new', { replace: true })
  },
})
</script>

<template>
  <p class="sr-only">Đang chuyển hướng…</p>
</template>
