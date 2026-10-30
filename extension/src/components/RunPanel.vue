<script setup lang="ts">
import { computed } from 'vue';
import type { RunState } from '@/lib/runner/run';

const props = defineProps<{ run: RunState; busy: boolean }>();
defineEmits<{ stop: []; continue: []; dismiss: [] }>();

const active = computed(() => ['preparing', 'running', 'paused'].includes(props.run.phase));
const tone = computed(() => {
  switch (props.run.phase) {
    case 'succeeded':
      return 'border-success/40 bg-success/5';
    case 'failed':
      return 'border-destructive/40 bg-destructive/5';
    case 'paused':
      return 'border-warning/50 bg-warning/10';
    default:
      return 'border-border bg-card';
  }
});
const label: Record<RunState['phase'], string> = {
  preparing: 'Preparing',
  running: 'Running',
  paused: 'Waiting for you',
  succeeded: 'Done',
  failed: 'Failed',
  lease_lost: 'Taken over',
  stopped: 'Stopped',
};
</script>

<template>
  <section class="flex flex-col gap-2 rounded-lg border p-3" :class="tone" data-testid="run-panel" :data-phase="run.phase" aria-live="polite">
    <div class="flex items-center justify-between gap-2">
      <strong class="truncate text-sm">{{ run.title }}</strong>
      <span class="shrink-0 text-xs font-medium uppercase tracking-wide" data-testid="run-phase">{{ label[run.phase] }}</span>
    </div>
    <div v-if="run.stepCount > 0 && active" class="h-1.5 overflow-hidden rounded bg-secondary">
      <div class="h-full bg-primary transition-all" :style="{ width: `${Math.round(((run.stepIndex + 1) / run.stepCount) * 100)}%` }" />
    </div>
    <p class="text-sm" data-testid="run-message">{{ run.message }}</p>
    <p v-if="run.errorCode && run.phase === 'failed'" class="font-mono text-xs text-muted-foreground" data-testid="run-error-code">{{ run.errorCode }}</p>
    <div class="flex gap-2">
      <button v-if="run.phase === 'paused'" class="btn btn-primary" :disabled="busy" @click="$emit('continue')">Continue</button>
      <button v-if="active" class="btn btn-secondary" :disabled="busy" @click="$emit('stop')">Stop</button>
      <button v-else class="btn btn-ghost" :disabled="busy" @click="$emit('dismiss')">Dismiss</button>
    </div>
  </section>
</template>
