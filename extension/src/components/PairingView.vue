<script setup lang="ts">
import { computed, ref } from 'vue';
import { defaultDeviceName, normalizePairingCode } from '@/lib/pairing';

defineProps<{ busy: boolean }>();
const emit = defineEmits<{ pair: [code: string, deviceName: string] }>();

const code = ref('');
const deviceName = ref(defaultDeviceName());
const normalized = computed(() => normalizePairingCode(code.value));
const touched = ref(false);

function submit() {
  touched.value = true;
  if (normalized.value) emit('pair', normalized.value, deviceName.value);
}
</script>

<template>
  <form class="card flex flex-col gap-3 p-4" data-testid="pairing" @submit.prevent="submit">
    <div>
      <h2 class="font-semibold">Kết nối trình duyệt này</h2>
      <p class="text-sm text-muted-foreground">
        Trên trang quản lý, mở <em>Tiện ích trình duyệt → Tạo mã ghép nối</em>, rồi nhập mã hiện ra vào đây.
      </p>
    </div>
    <label class="flex flex-col gap-1 text-sm font-medium">
      Mã ghép nối
      <input
        v-model="code"
        class="input font-mono uppercase tracking-widest"
        name="code"
        placeholder="XXXXX-XXXXX"
        autocomplete="off"
        spellcheck="false"
        maxlength="16"
        :aria-invalid="touched && !normalized"
        @blur="touched = true"
      />
    </label>
    <p v-if="touched && code && !normalized" class="-mt-2 text-xs text-destructive">Mã gồm 10 chữ cái hoặc chữ số, ví dụ ABCDE-12345.</p>
    <label class="flex flex-col gap-1 text-sm font-medium">
      Tên trình duyệt
      <input v-model="deviceName" class="input" name="device_name" maxlength="100" />
    </label>
    <button type="submit" class="btn btn-primary" :disabled="busy || !normalized">Kết nối</button>
  </form>
</template>
