<script setup lang="ts">
// Announces form-level errors to screen readers.
defineProps<{ message?: string }>()
</script>

<template>
  <div aria-live="assertive">
    <Alert v-if="message" variant="destructive">
      <AlertDescription>{{ message }}</AlertDescription>
    </Alert>
  </div>
</template>
