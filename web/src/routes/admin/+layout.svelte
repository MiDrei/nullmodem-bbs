<script lang="ts">
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
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

	let systemMenuOpen = $state(false);

	function logout() {
		auth.clear();
		goto('/admin/login');
	}

	function closeSystemMenu() {
		systemMenuOpen = false;
	}

	// clickOutside closes the System dropdown on any click that lands
	// outside it -- the layout persists across route changes, so
	// without this the menu would stay open after navigating.
	function clickOutside(node: HTMLElement, callback: () => void) {
		function handleClick(event: MouseEvent) {
			if (!node.contains(event.target as Node)) callback();
		}
		document.addEventListener('click', handleClick, true);
		return {
			destroy() {
				document.removeEventListener('click', handleClick, true);
			}
		};
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{bbsName} Admin</title>
</svelte:head>

<div class="min-h-screen bg-slate-950 text-slate-100">
	<header
		class="grid grid-cols-1 items-center gap-3 border-b border-slate-800 px-6 py-4 md:grid-cols-[1fr_auto_1fr]"
	>
		<div class="flex items-center justify-center md:justify-start">
			<span class="font-mono text-lg font-semibold tracking-wide text-cyan-400"
				>{bbsName} &middot; Admin</span
			>
		</div>

		{#if auth.username}
			<nav class="flex flex-wrap items-center justify-center gap-x-5 gap-y-1 text-sm text-slate-400">
				<a href="/admin/dashboard" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="h-4 w-4">
						<rect x="3" y="3" width="8" height="8" rx="1" />
						<rect x="13" y="3" width="8" height="8" rx="1" />
						<rect x="3" y="13" width="8" height="8" rx="1" />
						<rect x="13" y="13" width="8" height="8" rx="1" />
					</svg>
					Dashboard
				</a>
				<a href="/admin/message-areas" class="flex items-center gap-1.5 hover:text-slate-100">
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
				<a href="/admin/file-areas" class="flex items-center gap-1.5 hover:text-slate-100">
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
				<a href="/admin/designer" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<path d="M4 16l4.5-9 4.5 9M6 12h5" />
						<path d="M14 5l6 6-6 6M20 11h-8" />
					</svg>
					Designer
				</a>
				<a href="/admin/screens" class="flex items-center gap-1.5 hover:text-slate-100">
					<svg
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						stroke-linejoin="round"
						class="h-4 w-4"
					>
						<rect x="3" y="4" width="18" height="13" rx="1" />
						<path d="M8 20h8M12 17v3" />
					</svg>
					Screens
				</a>
				<div class="relative" use:clickOutside={closeSystemMenu}>
					<button
						type="button"
						class="flex items-center gap-1.5 hover:text-slate-100"
						onclick={() => (systemMenuOpen = !systemMenuOpen)}
					>
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
						System
						<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-3 w-3">
							<path d="M6 9l6 6 6-6" />
						</svg>
					</button>
					{#if systemMenuOpen}
						<div
							class="absolute top-full left-1/2 z-10 mt-2 w-44 -translate-x-1/2 rounded border border-slate-800 bg-slate-900 py-1 shadow-lg md:left-0 md:translate-x-0"
						>
							<a
								href="/admin/users"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="h-4 w-4">
									<circle cx="8" cy="8" r="3.2" />
									<path d="M2.5 19c0-3 2.5-5.4 5.5-5.4s5.5 2.4 5.5 5.4" />
									<circle cx="17" cy="8.5" r="2.6" />
									<path d="M14.8 13.8c2.6.3 4.7 2.5 4.7 5.2" />
								</svg>
								Users
							</a>
							<a
								href="/admin/sl-matrix"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
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
							<a
								href="/admin/logs"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
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
							<a
								href="/admin/binkp"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
									stroke-linejoin="round"
									class="h-4 w-4"
								>
									<path d="M4 4l16 8-16 8V4z" />
									<path d="M4 12h6" />
								</svg>
								BinkP
							</a>
							<a
								href="/admin/binkp/uplinks"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 py-2 pr-3 pl-7 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								Uplinks (Nodes/Points)
							</a>
							<a
								href="/admin/areafix"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
									stroke-linejoin="round"
									class="h-4 w-4"
								>
									<rect x="4" y="4" width="16" height="16" rx="2" />
									<path d="M9 9h6M9 12.5h6M9 16h3" />
								</svg>
								Areafix / Filefix
							</a>
							<a
								href="/admin/pending-areas"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
									stroke-linejoin="round"
									class="h-4 w-4"
								>
									<circle cx="12" cy="12" r="9" />
									<path d="M12 7v5l3 3" />
								</svg>
								Pending Areas
							</a>
							<a
								href="/admin/netmail"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
									stroke-linejoin="round"
									class="h-4 w-4"
								>
									<path d="M3 6h18v13a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6z" />
									<path d="M3 6l9 7 9-7" />
									<path d="M15 3.5l4 4" />
									<path d="M19 3.5l-4 4" />
								</svg>
								Undeliverable Netmail
							</a>
							<a
								href="/admin/archive"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
								<svg
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									stroke-width="1.5"
									stroke-linejoin="round"
									class="h-4 w-4"
								>
									<rect x="3" y="4" width="18" height="5" rx="1" />
									<path d="M4 9v9a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1V9" />
									<path d="M10 13h4" />
								</svg>
								Packet Analyzer
							</a>
							<div class="my-1 border-t border-slate-800"></div>
							<a
								href="/admin/settings"
								onclick={closeSystemMenu}
								class="flex items-center gap-2 px-3 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-slate-100"
							>
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
						</div>
					{/if}
				</div>
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
