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
      <h2 class="font-semibold">Connect this browser</h2>
      <p class="text-sm text-muted-foreground">
        In the dashboard open <em>Settings → Browser extension → Connect</em> and type the code shown there.
      </p>
    </div>
    <label class="flex flex-col gap-1 text-sm font-medium">
      Pairing code
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
    <p v-if="touched && code && !normalized" class="-mt-2 text-xs text-destructive">The code has 10 letters or digits, like ABCDE-12345.</p>
    <label class="flex flex-col gap-1 text-sm font-medium">
      Device name
      <input v-model="deviceName" class="input" name="device_name" maxlength="100" />
    </label>
    <button type="submit" class="btn btn-primary" :disabled="busy || !normalized">Connect</button>
  </form>
</template>
