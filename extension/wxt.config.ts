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
      name: cfg.testBuild ? 'Shopee Shop Automation (TEST)' : 'Shopee Shop Automation',
      description: 'Runs the Seller Centre steps your team queued, in your own tab, while you watch.',
      permissions: ['storage', 'alarms', 'activeTab', 'scripting'],
      host_permissions: [...new Set(hosts)].map(matchPattern),
      action: { default_title: 'Shopee Shop Automation' },
    };
  },
  vite: ({ mode }) => ({
    plugins: [tailwindcss()],
    define: {
      __BUILD_CONFIG__: JSON.stringify(readBuildConfig(mode)),
    },
  }),
});
