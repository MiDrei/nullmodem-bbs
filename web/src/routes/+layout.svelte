<script lang="ts">
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import Toaster from '$lib/Toaster.svelte';

	let { children } = $props();

	function logout() {
		auth.clear();
		goto('/login');
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>NullModem BBS Admin</title>
</svelte:head>

<div class="min-h-screen bg-slate-950 text-slate-100">
	<header
		class="grid grid-cols-1 items-center gap-3 border-b border-slate-800 px-6 py-4 md:grid-cols-[1fr_auto_1fr]"
	>
		<div class="flex items-center justify-center md:justify-start">
			<span class="font-mono text-lg font-semibold tracking-wide text-cyan-400"
				>NullModem BBS &middot; Admin</span
			>
		</div>

		{#if auth.username}
			<nav class="flex flex-wrap items-center justify-center gap-x-5 gap-y-1 text-sm text-slate-400">
				<a href="/dashboard" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="h-4 w-4">
						<rect x="3" y="3" width="8" height="8" rx="1" />
						<rect x="13" y="3" width="8" height="8" rx="1" />
						<rect x="3" y="13" width="8" height="8" rx="1" />
						<rect x="13" y="13" width="8" height="8" rx="1" />
					</svg>
					Dashboard
				</a>
				<a href="/users" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="h-4 w-4">
						<circle cx="8" cy="8" r="3.2" />
						<path d="M2.5 19c0-3 2.5-5.4 5.5-5.4s5.5 2.4 5.5 5.4" />
						<circle cx="17" cy="8.5" r="2.6" />
						<path d="M14.8 13.8c2.6.3 4.7 2.5 4.7 5.2" />
					</svg>
					Users
				</a>
				<a href="/message-areas" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<path d="M4 5h16v10H8l-4 4V5z" />
					</svg>
					Message Areas
				</a>
				<a href="/file-areas" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<path d="M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6z" />
					</svg>
					File Areas
				</a>
				<a href="/sl-matrix" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<path d="M12 3l7 3v5c0 4.5-3 7.8-7 9-4-1.2-7-4.5-7-9V6l7-3z" />
						<path d="M9 12l2 2 4-4" />
					</svg>
					SL Matrix
				</a>
				<a href="/settings" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<circle cx="12" cy="12" r="3" />
						<path
							d="M19.4 13a7.9 7.9 0 0 0 0-2l2-1.5-2-3.4-2.4 1a7.9 7.9 0 0 0-1.7-1L15 3h-4l-.3 2.6a7.9 7.9 0 0 0-1.7 1l-2.4-1-2 3.4L6.6 11a7.9 7.9 0 0 0 0 2l-2 1.5 2 3.4 2.4-1a7.9 7.9 0 0 0 1.7 1L11 21h4l.3-2.6a7.9 7.9 0 0 0 1.7-1l2.4 1 2-3.4-2-1.5z"
						/>
					</svg>
					Settings
				</a>
				<a href="/logs" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<path d="M5 3h9l5 5v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
						<path d="M14 3v5h5" />
						<path d="M8 13h8" />
						<path d="M8 17h8" />
					</svg>
					Logs
				</a>
			</nav>
		{:else}
			<div></div>
		{/if}

		{#if auth.username}
			<div class="flex items-center justify-center gap-4 text-sm md:justify-end">
				<span class="text-slate-400">Signed in as <strong>{auth.username}</strong></span>
				<button
					class="rounded border border-slate-700 px-3 py-1 hover:bg-slate-800"
					onclick={logout}
				>
					Log out
				</button>
			</div>
		{:else}
			<div></div>
		{/if}
	</header>
	<main class="mx-auto max-w-5xl px-6 py-8">
		{@render children()}
	</main>
</div>
<Toaster />
