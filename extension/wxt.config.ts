import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'wxt';
import { matchPattern, readBuildConfig } from './build/env';

export default defineConfig({
  srcDir: 'src',
  modules: ['@wxt-dev/module-vue'],
  manifestVersion: 3,
  manifest: ({ mode }) => {
    const cfg = readBuildConfig(mode);
    if (cfg.sellerOrigins.length === 0) {
      console.warn('[build] WXT_SELLER_ORIGINS is empty: the extension will not be able to act on any Seller Centre tab.');
    }
    if (Object.keys(cfg.recipePublicKeys).length === 0) {
      console.warn('[build] WXT_RECIPE_PUBLIC_KEYS is empty: every recipe will be rejected.');
    }
    const hosts = [cfg.apiOrigin, ...cfg.sellerOrigins, ...cfg.fileOrigins];
    return {
      name: cfg.testBuild ? 'Shopee Shop Automation (BẢN THỬ)' : 'Shopee Shop Automation',
      description: 'Chạy giúp bạn các bước trên Kênh Người Bán mà nhóm của bạn đã xếp lịch, ngay trong tab của bạn để bạn theo dõi.',
      permissions: ['storage', 'alarms', 'activeTab', 'scripting'],
      host_permissions: [...new Set(hosts)].map(matchPattern),
      action: { default_title: 'Shopee Shop Automation: việc cần chạy trên Kênh Người Bán' },
    };
  },
  vite: ({ mode }) => ({
    plugins: [tailwindcss()],
    define: {
      __BUILD_CONFIG__: JSON.stringify(readBuildConfig(mode)),
    },
  }),
});
