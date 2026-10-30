<script setup lang="ts">
definePageMeta({
  middleware: 'org',
  validate: (route) => ['products', 'imports', 'workflows', 'logs'].includes(String(route.params.section)),
})

const route = useRoute()
const sections: Record<string, { title: string; phase: string; text: string }> = {
  products: { title: 'Products', phase: 'Phase 5', text: 'Manage products, variations and images, and publish them to your shops.' },
  imports: { title: 'Product Imports', phase: 'Phase 5', text: 'Import products from CSV or Excel with row-by-row validation.' },
  workflows: { title: 'Automation Workflows', phase: 'Phase 4', text: 'Run shop setup workflows, approve changes, and retry or resume them.' },
  logs: { title: 'Execution Logs', phase: 'Phase 4', text: 'Step-by-step logs of every workflow run.' },
}
const s = computed(() => sections[String(route.params.section)]!)
useHead({ title: () => s.value.title })
</script>

<template>
  <div class="mx-auto max-w-6xl">
    <PageHeader :title="s.title" />
    <EmptyState :title="`Coming in ${s.phase}`" :description="s.text" />
  </div>
</template>
