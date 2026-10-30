<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <div class="rounded-lg border border-dashed bg-background px-6 py-12 text-center">
    <h2 class="text-base font-medium">{{ title }}</h2>
    <p v-if="description" class="mx-auto mt-1 max-w-md text-sm text-muted-foreground">{{ description }}</p>
    <div class="mt-4"><slot /></div>
  </div>
</template>
