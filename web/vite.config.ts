import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 3012,
    strictPort: true,
    host: true,
    allowedHosts: true
  }
});
