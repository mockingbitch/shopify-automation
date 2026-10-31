<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { browser } from 'wxt/browser';
import PairingView from '@/components/PairingView.vue';
import RunPanel from '@/components/RunPanel.vue';
import TaskList from '@/components/TaskList.vue';
import type { Status } from '@/lib/messages';
import { send } from '@/lib/popup-client';

const status = ref<Status | null>(null);
const error = ref('');
const busy = ref(false);

async function load() {
  try {
    status.value = await send<Status>({ type: 'status' });
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function act(fn: () => Promise<unknown>) {
  busy.value = true;
  error.value = '';
  try {
    await fn();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
    await load();
  }
}

const onChanged = () => void load();

onMounted(async () => {
  await load();
  browser.storage.onChanged.addListener(onChanged);
  if (status.value?.auth) void act(() => send({ type: 'refresh' }));
});
onBeforeUnmount(() => browser.storage.onChanged.removeListener(onChanged));

const run = (taskId: string) => act(() => send({ type: 'run', taskId }));
const stop = () => act(() => send({ type: 'stop' }));
const cont = () => act(() => send({ type: 'continue' }));
const dismiss = () => act(() => send({ type: 'dismiss' }));
const refresh = () => act(() => send({ type: 'refresh' }));
const unpair = () => act(() => send({ type: 'unpair' }));
const pair = (code: string, deviceName: string) => act(() => send({ type: 'pair', code, deviceName }));
</script>

<template>
  <main class="flex flex-col gap-3 p-4">
    <header class="flex items-center justify-between">
      <div class="flex items-center gap-2">
        <span class="inline-block size-2.5 rounded-full bg-primary" aria-hidden="true" />
        <h1 class="text-base font-semibold">Tự động hóa shop</h1>
      </div>
      <button v-if="status?.auth" class="btn btn-ghost text-xs" :disabled="busy || !!status.run && ['preparing', 'running', 'paused'].includes(status.run.phase)" @click="unpair">
        Hủy ghép nối
      </button>
    </header>

    <p v-if="error" role="alert" data-testid="error" class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
      {{ error }}
    </p>
    <p v-if="status?.notice && !status.auth" role="status" class="rounded-md bg-secondary px-3 py-2 text-sm">{{ status.notice }}</p>

    <template v-if="status">
      <PairingView v-if="!status.auth" :busy="busy" @pair="pair" />
      <template v-else>
        <section class="text-sm text-muted-foreground" data-testid="account">
          Đã kết nối với <strong class="text-foreground" data-testid="org-name">{{ status.auth.org.name }}</strong>
          bằng tài khoản <span data-testid="user-email">{{ status.auth.user.email }}</span>
        </section>
        <RunPanel v-if="status.run" :run="status.run" :busy="busy" @stop="stop" @continue="cont" @dismiss="dismiss" />
        <TaskList :tasks="status.tasks" :run="status.run" :busy="busy" @run="run" @refresh="refresh" />
      </template>
    </template>
    <p v-else class="text-sm text-muted-foreground">Đang tải…</p>

    <footer class="border-t border-border pt-2 text-xs text-muted-foreground">
      Tiện ích chỉ chạy trong tab Kênh Người Bán bạn đang mở, từng bước một để bạn theo dõi. Tiện ích không bao giờ tự đăng nhập hay giải CAPTCHA.
    </footer>
  </main>
</template>
