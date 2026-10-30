<script setup lang="ts">
definePageMeta({
  middleware: 'org',
  validate: (route) => ['products', 'imports'].includes(String(route.params.section)),
})

const route = useRoute()
const sections: Record<string, { title: string; phase: string; text: string }> = {
  products: { title: 'Products', phase: 'Phase 5', text: 'Manage products, variations and images, and publish them to your shops.' },
  imports: { title: 'Product Imports', phase: 'Phase 5', text: 'Import products from CSV or Excel with row-by-row validation.' },
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
