<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Chọn shop' })

type Selection = components['schemas']['Selection']

const api = useApi()
const route = useRoute()
const { orgId } = useOrg()
const stateId = computed(() => String(route.query.state ?? ''))

const selection = ref<Selection>()
const chosen = ref<string[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')

onMounted(async () => {
  try {
    selection.value = unwrap(await api.GET('/orgs/{orgID}/shops/connect/{stateID}', {
      params: { path: { orgID: orgId.value, stateID: stateId.value } },
    })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
})

async function submit() {
  busy.value = true
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/shops/connect/{stateID}/select', {
      params: { path: { orgID: orgId.value, stateID: stateId.value } },
      body: { shopee_shop_ids: chosen.value },
    }))
    const first = res.data[0]
    await navigateTo(res.data.length === 1 && first ? `/orgs/${orgId.value}/shops/${first.id}?connected=1` : `/orgs/${orgId.value}/shops`)
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}

const fmt = new Intl.DateTimeFormat('vi-VN', { timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-6">
    <PageHeader title="Chọn shop" description="Tài khoản chính Shopee của bạn đã ủy quyền cho nhiều shop. Hãy chọn những shop muốn kết nối với tổ chức này." />
    <p v-if="loading" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <FormError :message="error" />
    <EmptyState v-if="!loading && !selection" title="Không còn lựa chọn này" description="Có thể lựa chọn đã hết hạn sau 30 phút hoặc đã được dùng. Hãy bắt đầu lại từ trang Shop.">
      <Button as-child variant="outline"><NuxtLink :to="`/orgs/${orgId}/shops`">Quay lại danh sách shop</NuxtLink></Button>
    </EmptyState>

    <form v-if="selection" class="space-y-4" @submit.prevent="submit">
      <fieldset class="space-y-2 rounded-lg border bg-background p-4">
        <legend class="px-1 text-sm font-medium">Shop ({{ selection.shops.length }})</legend>
        <label v-for="s in selection.shops" :key="s.shopee_shop_id" class="flex items-center gap-3 rounded p-2 hover:bg-muted">
          <input v-model="chosen" type="checkbox" :value="s.shopee_shop_id" class="size-4">
          <span>
            <span class="font-medium">{{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }}</span>
            <span class="block text-xs text-muted-foreground">ID {{ s.shopee_shop_id }}<span v-if="s.region"> · {{ s.region }}</span></span>
          </span>
        </label>
      </fieldset>
      <p class="text-xs text-muted-foreground">Có hiệu lực đến {{ fmt.format(new Date(selection.expires_at)) }}. Shop bạn không chọn sẽ không được lưu.</p>
      <Button type="submit" :disabled="busy || !chosen.length">{{ busy ? 'Đang kết nối…' : `Kết nối ${chosen.length || ''} shop` }}</Button>
    </form>
  </div>
</template>
