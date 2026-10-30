<script setup lang="ts">
useHead({ title: 'Organizations' })
const session = useSessionStore()
onMounted(() => session.load(true))
</script>

<template>
  <div class="mx-auto max-w-4xl">
    <PageHeader title="Organizations" description="Organizations you belong to. Each has its own shops, templates and members.">
      <template #actions>
        <Button as-child><NuxtLink to="/orgs/new">New organization</NuxtLink></Button>
      </template>
    </PageHeader>
    <EmptyState v-if="!session.memberships.length" title="No organizations yet" description="Create one, or ask an admin to invite you.">
      <Button as-child><NuxtLink to="/orgs/new">Create organization</NuxtLink></Button>
    </EmptyState>
    <ul v-else class="grid gap-3 sm:grid-cols-2">
      <li v-for="m in session.memberships" :key="m.organization.id">
        <NuxtLink :to="`/orgs/${m.organization.id}`" class="block rounded-lg border bg-background p-4 transition-colors hover:border-primary focus-visible:outline-2">
          <p class="font-medium">{{ m.organization.name }}</p>
          <p class="text-sm text-muted-foreground">{{ m.organization.slug }} · {{ roleLabels[m.role] }}</p>
        </NuxtLink>
      </li>
    </ul>
  </div>
</template>
