<script setup lang="ts">
import { CheckCircle2, Circle } from '@lucide/vue'
import type { components } from '~/types/api'

// "Bắt đầu" checklist on the overview page: the steps a new organization
// goes through, each marked done from real data, with a link to do it and
// a link to the matching part of the guide.
const props = defineProps<{ counts: components['schemas']['Overview'] }>()

const api = useApi()
const session = useSessionStore()
const { orgId, can } = useOrg()
const publishedTemplate = ref(false)
const memberCount = ref(1)
const storageKey = computed(() => `getting-started-hidden:${orgId.value}`)
const hidden = ref(false)

onMounted(async () => {
  hidden.value = localStorage.getItem(storageKey.value) === '1'
  try {
    if (can('templates.read')) {
      const t = unwrap(await api.GET('/orgs/{orgID}/templates', { params: { path: { orgID: orgId.value } } })).data
      publishedTemplate.value = t.some((x) => x.current_published_version_id)
    }
    if (can('members.read')) {
      memberCount.value = unwrap(await api.GET('/orgs/{orgID}/members', { params: { path: { orgID: orgId.value } } })).data.length
    }
  } catch { /* the checklist is a helper; missing data only leaves a step unticked */ }
})

const base = computed(() => `/orgs/${orgId.value}`)
const steps = computed(() => {
  const c = props.counts
  const out = [
    { title: 'Kết nối shop Shopee', text: 'Cho phép hệ thống làm việc với shop của bạn. Bạn sẽ đăng nhập Shopee và bấm đồng ý.',
      done: c.connected_shops > 0, to: `${base.value}/shops`, action: 'Kết nối shop', help: 'ket-noi-shop' },
    { title: 'Tạo và phát hành một mẫu shop', text: 'Mẫu shop chứa tên shop, mô tả, đơn vị vận chuyển, địa chỉ… để áp dụng cho nhiều shop cùng lúc.',
      done: publishedTemplate.value, to: `${base.value}/templates`, action: 'Tạo mẫu shop', help: 'mau-shop' },
    { title: 'Áp dụng mẫu cho shop và duyệt thay đổi', text: 'Hệ thống so sánh mẫu với shop, cho bạn xem trước, và chỉ thay đổi những gì bạn duyệt.',
      done: c.completed_setups > 0, to: `${base.value}/templates`, action: 'Chọn mẫu để áp dụng', help: 'ap-dung-mau' },
    { title: 'Thêm sản phẩm', text: 'Tạo từng sản phẩm, hoặc nhập nhiều sản phẩm một lúc từ file Excel/CSV.',
      done: c.products > 0, to: `${base.value}/products`, action: 'Thêm sản phẩm', help: 'san-pham' },
    { title: 'Đăng sản phẩm lên shop', text: 'Chọn sản phẩm và shop. Hệ thống kiểm tra theo quy định của Shopee rồi chờ bạn duyệt.',
      done: c.published_products > 0, to: `${base.value}/products`, action: 'Chọn sản phẩm để đăng', help: 'dang-san-pham' },
    { title: 'Bật xác minh hai bước', text: 'Bảo vệ tài khoản bằng mã từ ứng dụng xác thực trên điện thoại.',
      done: !!session.user?.mfa_enabled, to: '/account', action: 'Bật xác minh hai bước', help: 'bao-mat' },
  ]
  if (can('members.manage')) {
    out.push({ title: 'Mời thành viên (không bắt buộc)', text: 'Mời nhân viên cùng làm việc, mỗi người một vai trò phù hợp.',
      done: memberCount.value > 1, to: `${base.value}/members`, action: 'Mời thành viên', help: 'thanh-vien' })
  }
  return out
})
const doneCount = computed(() => steps.value.filter((s) => s.done).length)
const next = computed(() => steps.value.find((s) => !s.done))

function hide(v: boolean) {
  hidden.value = v
  localStorage.setItem(storageKey.value, v ? '1' : '0')
}
</script>

<template>
  <section v-if="!hidden" aria-labelledby="getting-started-heading" class="rounded-lg border bg-background p-4">
    <div class="flex flex-wrap items-start justify-between gap-2">
      <div>
        <h2 id="getting-started-heading" class="text-lg font-semibold">Bắt đầu</h2>
        <p class="text-sm text-muted-foreground">
          Đã xong {{ doneCount }}/{{ steps.length }} bước.
          <NuxtLink to="/help" class="underline underline-offset-4">Xem hướng dẫn đầy đủ</NuxtLink>
        </p>
      </div>
      <Button variant="ghost" size="sm" @click="hide(true)">Ẩn</Button>
    </div>
    <ol class="mt-3 space-y-2">
      <li v-for="(s, i) in steps" :key="s.title" class="flex gap-3 rounded-md p-2" :class="s === next ? 'bg-primary/5' : ''">
        <CheckCircle2 v-if="s.done" class="mt-0.5 size-5 shrink-0 text-green-700" aria-hidden="true" />
        <Circle v-else class="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <p class="font-medium" :class="s.done ? 'text-muted-foreground line-through' : ''">
            {{ i + 1 }}. {{ s.title }}<span class="sr-only">{{ s.done ? ' (đã xong)' : ' (chưa xong)' }}</span>
          </p>
          <p v-if="!s.done" class="text-sm text-muted-foreground">{{ s.text }}</p>
          <p v-if="!s.done" class="mt-1 flex flex-wrap gap-3 text-sm">
            <NuxtLink :to="s.to" class="font-medium text-primary underline underline-offset-4">{{ s.action }}</NuxtLink>
            <NuxtLink :to="`/help#${s.help}`" class="text-muted-foreground underline underline-offset-4">Cách làm</NuxtLink>
          </p>
        </div>
      </li>
    </ol>
  </section>
  <p v-else class="text-sm">
    <button type="button" class="text-muted-foreground underline underline-offset-4" @click="hide(false)">Hiện lại danh sách “Bắt đầu”</button>
  </p>
</template>
