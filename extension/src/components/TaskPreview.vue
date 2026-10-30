<script setup lang="ts">
import { computed } from 'vue';

/**
 * The API's `preview` is free-form. Render a string as text; an object as a
 * small list of its plain fields, with *_url / image fields that look like
 * https images shown as thumbnails.
 */
const props = defineProps<{ preview: unknown }>();

const isImageUrl = (k: string, v: unknown) =>
  typeof v === 'string' && /^https:\/\//.test(v) && (/image|banner|logo|thumbnail/i.test(k) || /\.(png|jpe?g|webp|gif)(\?|$)/i.test(v));

const rows = computed(() => {
  const p = props.preview;
  if (!p || typeof p !== 'object' || Array.isArray(p)) return [];
  return Object.entries(p as Record<string, unknown>)
    .filter(([, v]) => ['string', 'number', 'boolean'].includes(typeof v))
    .slice(0, 8)
    .map(([k, v]) => ({ k, v: String(v), image: isImageUrl(k, v) }));
});
</script>

<template>
  <div class="rounded-md bg-secondary/60 px-2 py-1.5 text-xs" data-testid="task-preview">
    <p v-if="typeof preview === 'string'">{{ preview }}</p>
    <dl v-else class="grid grid-cols-[auto_1fr] gap-x-2 gap-y-1">
      <template v-for="r in rows" :key="r.k">
        <dt class="text-muted-foreground">{{ r.k.replace(/_/g, ' ') }}</dt>
        <dd class="min-w-0 break-words">
          <img v-if="r.image" :src="r.v" alt="" class="max-h-16 rounded border border-border" referrerpolicy="no-referrer" />
          <span v-else>{{ r.v }}</span>
        </dd>
      </template>
    </dl>
  </div>
</template>
