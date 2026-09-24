<script lang="ts">
	// (portal) is a SvelteKit route group -- it organizes these files
	// under one shared layout without adding "portal" to the URL, so
	// this still serves at the site root (/, /message-areas, /netmail,
	// /file-areas, ...) alongside the unrelated routes/admin/* tree,
	// which has its own +layout.svelte instead of this one.
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { getBBSInfo } from '$lib/api';
	import Toaster from '$lib/Toaster.svelte';

	let { children } = $props();

	// Falls back to the generic product name until the (unauthenticated,
	// so it also works on the login page) fetch resolves -- see
	// handleBBSInfo's own doc comment for why this needs no token.
	let bbsName = $state('NullModem BBS');
	onMount(async () => {
		try {
			const info = await getBBSInfo();
			if (info.name) bbsName = info.name;
		} catch {
			// Non-critical: keep the generic fallback name.
		}
	});

	function logout() {
		bbsAuth.clear();
		goto('/login');
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{bbsName}</title>
</svelte:head>

<div class="min-h-screen bg-slate-950 text-slate-100">
	<header
		class="grid grid-cols-1 items-center gap-3 border-b border-slate-800 px-6 py-4 md:grid-cols-[1fr_auto_1fr]"
	>
		<div class="flex items-center justify-center md:justify-start">
			<span
				class="bg-gradient-to-r from-cyan-400 to-fuchsia-500 bg-clip-text font-mono text-lg font-semibold tracking-wide text-transparent"
				>{bbsName}</span
			>
		</div>

		{#if bbsAuth.username}
			<nav class="flex flex-wrap items-center justify-center gap-x-5 gap-y-1 text-sm text-slate-400">
				<a href="/message-areas" class="transition hover:text-slate-100">Message Areas</a>
				<a href="/netmail" class="transition hover:text-slate-100">Netmail</a>
				<a href="/file-areas" class="transition hover:text-slate-100">Files</a>
				<a href="/qwk" class="transition hover:text-slate-100">QWK Mail</a>
			</nav>
		{:else}
			<div></div>
		{/if}

		{#if bbsAuth.username}
			<div class="flex items-center justify-center gap-4 text-sm md:justify-end">
				<span class="text-slate-400">Signed in as <strong>{bbsAuth.username}</strong></span>
				<button class="rounded border border-slate-700 px-3 py-1 hover:bg-slate-800" onclick={logout}>
					Log out
				</button>
			</div>
		{:else}
			<div></div>
		{/if}
	</header>
	<main class="mx-auto max-w-4xl px-6 py-8">
		{@render children()}
	</main>
</div>
<Toaster />
