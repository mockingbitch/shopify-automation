<script setup lang="ts">
import { computed } from 'vue';
import type { TaskSummary } from '@/lib/api/types';
import type { TasksView } from '@/lib/messages';
import type { RunState } from '@/lib/runner/run';
import TaskPreview from './TaskPreview.vue';

const props = defineProps<{ tasks: TasksView; run: RunState | null; busy: boolean }>();
defineEmits<{ run: [taskId: string]; refresh: [] }>();

const running = computed(() => !!props.run && ['preparing', 'running', 'paused'].includes(props.run.phase));

const groups = computed(() => {
  const m = new Map<string, { key: string; name: string; region: string; shopeeShopId: string; tasks: TaskSummary[] }>();
  for (const t of props.tasks.items) {
    const key = t.shop?.id ?? 'unknown';
    if (!m.has(key)) m.set(key, { key, name: t.shop?.name ?? 'Unknown shop', region: t.shop?.region ?? '', shopeeShopId: String(t.shop?.shopee_shop_id ?? ''), tasks: [] });
    m.get(key)!.tasks.push(t);
  }
  return [...m.values()];
});
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="task-list">
    <div class="flex items-center justify-between">
      <h2 class="text-sm font-semibold">Tasks to run</h2>
      <button class="btn btn-ghost text-xs" :disabled="busy" @click="$emit('refresh')">Refresh</button>
    </div>
    <p v-if="tasks.error" class="text-xs text-destructive">Could not load tasks: {{ tasks.error }}</p>
    <p v-if="groups.length === 0" class="card p-3 text-sm text-muted-foreground" data-testid="no-tasks">Nothing to run right now.</p>
    <div v-for="g in groups" :key="g.key" class="card overflow-hidden" data-testid="shop-group">
      <div class="border-b border-border bg-secondary px-3 py-1.5 text-xs">
        <strong class="text-sm">{{ g.name }}</strong>
        <span class="text-muted-foreground"> · {{ g.region }} · Shop ID {{ g.shopeeShopId }}</span>
      </div>
      <ul class="divide-y divide-border">
        <li v-for="t in g.tasks" :key="t.id" class="flex flex-col gap-1.5 px-3 py-2" data-testid="task" :data-task-id="t.id">
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <p class="font-medium leading-snug">{{ t.title }}</p>
              <p class="font-mono text-xs text-muted-foreground">{{ t.capability }}</p>
            </div>
            <button
              v-if="t.status === 'QUEUED'"
              class="btn btn-primary shrink-0"
              :disabled="busy || running"
              :aria-label="`Run ${t.title}`"
              @click="$emit('run', t.id)"
            >
              Run
            </button>
            <span v-else class="shrink-0 text-xs font-medium text-muted-foreground">{{ t.status === 'WAITING_USER' ? 'Waiting' : 'Running' }}</span>
          </div>
          <TaskPreview v-if="t.preview !== undefined && t.preview !== null" :preview="t.preview" />
        </li>
      </ul>
    </div>
    <p class="text-xs text-muted-foreground">Open the shop's Seller Centre in this window before clicking Run. The task runs in that tab.</p>
  </section>
</template>
