<script lang="ts">
	// (portal) is a SvelteKit route group -- it organizes these files
	// under one shared layout without adding "portal" to the URL, so
	// this still serves at the site root (/, /message-areas, /netmail,
	// /file-areas, ...) alongside the unrelated routes/admin/* tree,
	// which has its own +layout.svelte instead of this one.
	//
	// The chrome follows design D: the board's name, icon links, the
	// caller and "Log out" in one header row; version and Telnet
	// address in a quiet footer.
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { getBBSProfile } from '$lib/api';
	import { site } from '$lib/site.svelte';
	import Icon from '$lib/Icon.svelte';
	import Toaster from '$lib/Toaster.svelte';

	let { children } = $props();

	const links = [
		{ href: '/message-areas', label: 'Areas', icon: 'areas', match: ['/message-areas', '/messages'] },
		{ href: '/netmail', label: 'Netmail', icon: 'netmail', match: ['/netmail'] },
		{ href: '/file-areas', label: 'Files', icon: 'files', match: ['/file-areas', '/files'] },
		{ href: '/qwk', label: 'QWK Mail', icon: 'qwk', match: ['/qwk'] },
		{ href: '/last-callers', label: 'Last Callers', icon: 'users', match: ['/last-callers'] },
		{ href: '/community', label: 'Community', icon: 'users', match: ['/community'] },
		{ href: '/profile', label: 'Profile', icon: 'profile', match: ['/profile'] }
	] as const;

	function active(match: readonly string[]): boolean {
		return match.some((m) => page.url.pathname === m || page.url.pathname.startsWith(m + '/'));
	}

	onMount(async () => {
		await site.load();
		// Pick up a time zone changed elsewhere (e.g. via Telnet/SSH)
		// since this browser logged in -- see $lib/datetime.
		if (bbsAuth.token) {
			try {
				const profile = await getBBSProfile(bbsAuth.token);
				bbsAuth.setTimezone(profile.timezone);
				bbsAuth.setSecurityLevel(profile.security_level);
			} catch {
				// Non-critical: keep the stored zone; pages handle auth errors.
			}
		}
	});

	function logout() {
		bbsAuth.clear();
		goto('/login');
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{site.info.name}</title>
</svelte:head>

<div class="flex min-h-screen flex-col bg-ground text-ink">
	{#if bbsAuth.username}
		<header
			class="flex flex-wrap items-center justify-between gap-x-8 gap-y-3 border-b border-line px-6 py-5 md:px-10"
		>
			<a href="/message-areas" class="text-[15px] font-bold tracking-tight text-ink hover:text-ink">
				{site.info.name}
			</a>
			<nav class="flex flex-wrap items-center gap-x-6 gap-y-2 text-[13px] text-muted">
				{#each links as l (l.href)}
					<a
						href={l.href}
						class="flex items-center gap-1.5 transition-colors hover:text-accent {active(l.match)
							? 'text-accent'
							: ''}"
						aria-current={active(l.match) ? 'page' : undefined}
					>
						<Icon name={l.icon} />{l.label}
					</a>
				{/each}
			</nav>
			<div class="flex items-center gap-2.5 text-[13px] text-faint">
				<span>{bbsAuth.username}</span>
				{#if bbsAuth.isSysop}
					<span class="text-line-strong" aria-hidden="true">·</span>
					<a href="/admin" class="transition-colors hover:text-accent">Admin</a>
				{/if}
				<span class="text-line-strong" aria-hidden="true">·</span>
				<button class="transition-colors hover:text-accent" onclick={logout}>Log out</button>
			</div>
		</header>
	{/if}

	<main class="mx-auto w-full max-w-4xl flex-1 px-6 py-7 md:px-10">
		{@render children()}
	</main>

	<footer
		class="flex justify-between gap-4 border-t border-line px-6 py-4 font-mono text-[10.5px] text-dim md:px-10"
	>
		<span>NullModem BBS{site.info.version ? ` v${site.info.version}` : ''}</span>
		{#if site.telnetAddress}
			<span>telnet · {site.telnetAddress} · <a href="/terminal" class="hover:text-ink">in the browser</a></span>
		{/if}
	</footer>
</div>
<Toaster />
