<script lang="ts">
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';

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
	<header class="flex items-center justify-between border-b border-slate-800 px-6 py-4">
		<div class="flex items-center gap-6">
			<span class="font-mono text-lg font-semibold tracking-wide text-cyan-400"
				>NullModem BBS &middot; Admin</span
			>
			{#if auth.username}
				<nav class="flex items-center gap-4 text-sm text-slate-400">
					<a href="/dashboard" class="hover:text-slate-100">Dashboard</a>
					<a href="/settings" class="hover:text-slate-100">Settings</a>
				</nav>
			{/if}
		</div>
		{#if auth.username}
			<div class="flex items-center gap-4 text-sm">
				<span class="text-slate-400">Signed in as <strong>{auth.username}</strong></span>
				<button
					class="rounded border border-slate-700 px-3 py-1 hover:bg-slate-800"
					onclick={logout}
				>
					Log out
				</button>
			</div>
		{/if}
	</header>
	<main class="mx-auto max-w-3xl px-6 py-8">
		{@render children()}
	</main>
</div>
