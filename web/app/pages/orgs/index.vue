<script setup lang="ts">
useHead({ title: 'Tổ chức' })
const session = useSessionStore()
onMounted(() => session.load(true))
</script>

<template>
  <div class="mx-auto max-w-4xl">
    <PageHeader title="Tổ chức" description="Các tổ chức bạn tham gia. Mỗi tổ chức có shop, mẫu shop và thành viên riêng.">
      <template #actions>
        <Button as-child><NuxtLink to="/orgs/new">Tạo tổ chức mới</NuxtLink></Button>
      </template>
    </PageHeader>
    <EmptyState v-if="!session.memberships.length" title="Bạn chưa thuộc tổ chức nào" description="Hãy tạo tổ chức mới, hoặc nhờ quản trị viên mời bạn.">
      <Button as-child><NuxtLink to="/orgs/new">Tạo tổ chức</NuxtLink></Button>
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
