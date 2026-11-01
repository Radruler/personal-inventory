import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
  preprocess: vitePreprocess(),
  kit: {
    // Static SPA embedded into the Go binary (internal/webui, go:embed).
    adapter: adapter({
      pages: '../internal/webui/dist',
      assets: '../internal/webui/dist',
      fallback: 'index.html',
      precompress: true,
      strict: true
    }),
    alias: { $styles: 'src/styles' }
  }
};
