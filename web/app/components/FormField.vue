<script setup lang="ts">
const props = defineProps<{ id: string; label: string; error?: string; hint?: string }>()
const describedBy = computed(() => [props.error ? `${props.id}-error` : '', props.hint ? `${props.id}-hint` : ''].filter(Boolean).join(' ') || undefined)
</script>

<template>
  <div class="space-y-1.5">
    <Label :for="id">{{ label }}</Label>
    <slot :described-by="describedBy" :invalid="!!error" />
    <p v-if="hint && !error" :id="`${id}-hint`" class="text-xs text-muted-foreground">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="text-xs text-destructive">{{ error }}</p>
  </div>
</template>
