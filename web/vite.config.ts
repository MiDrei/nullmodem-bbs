import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// The admin UI is a plain SPA served as static files by cmd/web
			// (internal/web.Server), so a static prerendered build is what
			// we want here rather than a Node/edge SSR adapter.
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html',
				strict: false
			})
		})
	],
	server: {
		proxy: {
			// API_PROXY points the dev server at another web daemon,
			// e.g. a live one to check a page against real data.
			'/api': { target: process.env.API_PROXY ?? 'http://localhost:8090', changeOrigin: true }
		}
	}
});
