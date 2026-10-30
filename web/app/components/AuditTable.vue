<script setup lang="ts">
import type { components } from '~/types/api'

defineProps<{ rows: components['schemas']['AuditLog'][]; compact?: boolean }>()
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="overflow-x-auto rounded-lg border bg-background">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>When</TableHead>
          <TableHead>Action</TableHead>
          <TableHead>Outcome</TableHead>
          <TableHead v-if="!compact">Target</TableHead>
          <TableHead v-if="!compact">IP</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="r in rows" :key="r.id">
          <TableCell class="whitespace-nowrap"><time :datetime="r.created_at">{{ fmt.format(new Date(r.created_at)) }}</time></TableCell>
          <TableCell class="font-mono text-xs">{{ r.action }}</TableCell>
          <TableCell>
            <Badge :variant="r.outcome === 'success' ? 'secondary' : 'destructive'">{{ r.outcome }}</Badge>
          </TableCell>
          <TableCell v-if="!compact" class="text-xs text-muted-foreground">{{ r.target_type ?? '—' }}</TableCell>
          <TableCell v-if="!compact" class="font-mono text-xs text-muted-foreground">{{ r.ip ?? '—' }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>
