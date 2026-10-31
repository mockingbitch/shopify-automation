<script setup lang="ts">
import type { components } from '~/types/api'

defineProps<{ rows: components['schemas']['AuditLog'][]; compact?: boolean }>()
const outcomeLabels: Record<string, string> = { success: 'Thành công', failure: 'Thất bại', denied: 'Bị từ chối' }
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="overflow-x-auto rounded-lg border bg-background">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Thời gian</TableHead>
          <TableHead>Hành động</TableHead>
          <TableHead>Kết quả</TableHead>
          <TableHead v-if="!compact">Đối tượng</TableHead>
          <TableHead v-if="!compact">IP</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="r in rows" :key="r.id">
          <TableCell class="whitespace-nowrap"><time :datetime="r.created_at">{{ fmt.format(new Date(r.created_at)) }}</time></TableCell>
          <TableCell class="font-mono text-xs">{{ r.action }}</TableCell>
          <TableCell>
            <Badge :variant="r.outcome === 'success' ? 'secondary' : 'destructive'">{{ outcomeLabels[r.outcome] ?? r.outcome }}</Badge>
          </TableCell>
          <TableCell v-if="!compact" class="text-xs text-muted-foreground">{{ r.target_type ?? '—' }}</TableCell>
          <TableCell v-if="!compact" class="font-mono text-xs text-muted-foreground">{{ r.ip ?? '—' }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>
