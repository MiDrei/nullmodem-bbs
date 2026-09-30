<script lang="ts">
	// The reader's start: netmail, then the areas by network with their
	// unread counts -- only those with something unread unless "All"
	// is on (remembered on the device).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { listBBSMessageAreas, listBBSNetmail, type BBSMessageArea } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { readerToken, readerAuthFailed, errorText } from '$lib/reader/session';

	const SHOW_ALL_KEY = 'nullmodem.reader.showAll';

	let areas = $state<BBSMessageArea[]>([]);
	let netmailUnread = $state(0);
	let error = $state<string | null>(null);
	let loaded = $state(false);
	let showAll = $state(false);

	let visible = $derived(showAll ? areas : areas.filter((a) => a.new > 0));
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSMessageArea[] }[] = [];
		for (const a of visible) {
			const net = a.network || 'Local';
			let g = out.find((x) => x.network === net);
			if (!g) out.push((g = { network: net, areas: [] }));
			g.areas.push(a);
		}
		return out;
	});

	async function load() {
		const token = await readerToken();
		if (!token) return;
		try {
			const [a, n] = await Promise.all([listBBSMessageAreas(token), listBBSNetmail(token)]);
			areas = a;
			netmailUnread = n.filter((m) => m.unread).length;
			error = null;
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the areas.');
		} finally {
			loaded = true;
		}
	}

	function toggleAll() {
		showAll = !showAll;
		try {
			localStorage.setItem(SHOW_ALL_KEY, showAll ? '1' : '');
		} catch {
			// Not remembered then; still works for now.
		}
	}

	async function logout() {
		if (!confirm('Sign out of the reader?')) return;
		bbsAuth.clear();
		await goto('/reader/login', { replaceState: true });
	}

	onMount(() => {
		try {
			showAll = localStorage.getItem(SHOW_ALL_KEY) === '1';
		} catch {
			// Default: unread only.
		}
		load();
		// Back from the app switcher: fresh counts.
		const onVisible = () => document.visibilityState === 'visible' && load();
		document.addEventListener('visibilitychange', onVisible);
		return () => document.removeEventListener('visibilitychange', onVisible);
	});
</script>

<header class="r-bar">
	<span class="r-title">Reader</span>
	<button class="r-btn text-base" onclick={toggleAll}>{showAll ? 'Unread' : 'All'}</button>
	<button class="r-btn text-xl" onclick={load} aria-label="Refresh">↻</button>
	<button class="r-btn text-sm" onclick={logout}>Log out</button>
</header>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	<a class="r-row" href="/reader/netmail">
		<span class="flex-1 font-medium text-ink-strong">Netmail</span>
		{#if netmailUnread > 0}<span class="r-badge">{netmailUnread}</span>{/if}
		<span class="text-faint">›</span>
	</a>

	{#each groups as g (g.network)}
		<div class="r-section">{g.network}</div>
		{#each g.areas as a (a.id)}
			<a class="r-row" href="/reader/area/{a.id}">
				<span class="min-w-0 flex-1">
					<span class="block truncate {a.new > 0 ? 'font-medium text-ink-strong' : 'text-ink-soft'}"
						>{a.name}</span
					>
					<span class="block truncate font-mono text-xs text-faint">{a.tag}</span>
				</span>
				{#if a.new > 0}<span class="r-badge">{a.new}</span>{/if}
				<span class="text-faint">›</span>
			</a>
		{/each}
	{:else}
		<p class="r-note">{showAll ? 'No areas.' : 'Nothing unread.'}</p>
	{/each}
{/if}
