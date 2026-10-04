<script lang="ts">
	// Netmail, then the areas by network with their unread counts --
	// only those with something unread unless "All" is on (remembered
	// on the device).
	import { t, tn } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { listBBSMessageAreas, listBBSNetmail, setMyArea, type BBSMessageArea } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { readerToken, readerAuthFailed, errorText } from '$lib/reader/session';
	import { forgetOffline, offline, syncAhead } from '$lib/reader/offline.svelte';
	import { disablePush } from '$lib/reader/push';
	import SettingsSheet from '$lib/reader/SettingsSheet.svelte';
	import Icon from '$lib/Icon.svelte';

	let {
		selectedAreaId = null,
		netmailSelected = false,
		reloadKey = 0,
		onArea,
		onNetmail
	}: {
		selectedAreaId?: number | null;
		netmailSelected?: boolean;
		/** Changing it reloads the counts (after reading something). */
		reloadKey?: number;
		onArea: (id: number) => void;
		onNetmail: () => void;
	} = $props();

	const SHOW_ALL_KEY = 'nullmodem.reader.showAll';

	let areas = $state<BBSMessageArea[]>([]);
	let netmailUnread = $state(0);
	let error = $state<string | null>(null);
	let loaded = $state(false);
	let showAll = $state(false);
	let settingsOpen = $state(false);

	// Unread: in the caller's areas (the open one stays listed even once
	// it's all read). All: every area, ✓ marking theirs.
	let visible = $derived(showAll ? areas : areas.filter((a) => (a.mine && a.new > 0) || a.id === selectedAreaId));

	async function toggleMine(a: BBSMessageArea) {
		const token = await readerToken();
		if (!token) return;
		a.mine = !a.mine;
		try {
			await setMyArea(token, a.id, a.mine);
		} catch (err) {
			a.mine = !a.mine;
			if (await readerAuthFailed(err)) return;
			toast.push(errorText(err, t('web.common.save_failed')), 'error');
		}
	}
	let groups = $derived.by(() => {
		const out: { network: string; areas: BBSMessageArea[] }[] = [];
		for (const a of visible) {
			const net = a.network || t('web.areas.local');
			let g = out.find((x) => x.network === net);
			if (!g) out.push((g = { network: net, areas: [] }));
			g.areas.push(a);
		}
		return out;
	});

	export async function load() {
		const token = await readerToken();
		if (!token) return;
		try {
			const [a, n] = await Promise.all([listBBSMessageAreas(token), listBBSNetmail(token)]);
			areas = a;
			netmailUnread = n.filter((m) => m.unread).length;
			error = null;
			// Online: fetch ahead what's unread, for reading offline.
			syncAhead();
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, t('web.reader.areas_failed'));
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
		if (!confirm(t('web.reader.logout_confirm'))) return;
		// Nothing for this login on this device any more.
		if (bbsAuth.token) await disablePush(bbsAuth.token).catch(() => {});
		forgetOffline();
		bbsAuth.clear();
		await goto('/reader/login', { replaceState: true });
	}

	$effect(() => {
		reloadKey;
		load();
	});

	onMount(() => {
		try {
			showAll = localStorage.getItem(SHOW_ALL_KEY) === '1';
		} catch {
			// Default: unread only.
		}
		// Back from the app switcher: fresh counts.
		const onVisible = () => document.visibilityState === 'visible' && load();
		document.addEventListener('visibilitychange', onVisible);
		return () => document.removeEventListener('visibilitychange', onVisible);
	});
</script>

<header class="r-bar">
	<span class="r-title">{t('web.home.reader')}</span>
	<button class="r-btn text-base" onclick={toggleAll}>{showAll ? t('web.reader.unread') : t('web.common.all')}</button>
	<button class="r-btn inline-flex items-center justify-center" onclick={() => goto('/reader/search')} aria-label={t('web.common.search')}
		><Icon name="search" size={21} /></button
	>
	<button class="r-btn inline-flex items-center justify-center" onclick={load} aria-label={t('web.reader.refresh')}><Icon name="refresh" size={21} /></button>
	<button class="r-btn inline-flex items-center justify-center" onclick={() => (settingsOpen = true)} aria-label={t('web.reader.settings')}><Icon name="system" size={21} /></button>
</header>
{#if !offline.online || offline.outbox}
	<p class="border-b border-line bg-surface px-4 py-1.5 text-xs text-muted">
		{#if !offline.online}{t('web.reader.offline_note')}{/if}
		{#if offline.outbox}{tn('web.reader.outbox_waiting', offline.outbox)}{/if}
	</p>
{/if}
{#if settingsOpen}
	<SettingsSheet onClose={() => (settingsOpen = false)} onLogout={logout} />
{/if}

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	<button class="r-row {netmailSelected ? 'bg-surface' : ''}" onclick={onNetmail}>
		<span class="flex-1 font-medium text-ink-strong">{t('web.nav.netmail')}</span>
		{#if netmailUnread > 0}<span class="r-badge">{netmailUnread}</span>{/if}
		<span class="text-faint">›</span>
	</button>
	<button class="r-row" onclick={() => goto('/reader/chat')}>
		<span class="flex-1 font-medium text-ink-strong">{t('web.nav.chat')}</span>
		<span class="text-faint">›</span>
	</button>

	{#each groups as g (g.network)}
		<div class="r-section">{g.network}</div>
		{#each g.areas as a (a.id)}
			<div class="flex items-stretch {a.id === selectedAreaId ? 'bg-surface' : ''}">
				{#if showAll}
					<button
						class="w-11 shrink-0 border-b border-line text-lg {a.mine ? 'text-accent' : 'text-faint'}"
						aria-label={a.mine ? t('web.areas.take_out', { AREA: a.name }) : t('web.areas.add', { AREA: a.name })}
						aria-pressed={a.mine}
						onclick={() => toggleMine(a)}>{a.mine ? '✓' : '+'}</button
					>
				{/if}
				<button class="r-row min-w-0 flex-1 {showAll ? 'pl-1' : ''}" onclick={() => onArea(a.id)}>
					<span class="min-w-0 flex-1">
						<span class="block truncate {a.new > 0 && a.mine ? 'font-medium text-ink-strong' : 'text-ink-soft'}"
							>{a.name}</span
						>
						<span class="block truncate font-mono text-xs text-faint">{a.tag}</span>
					</span>
					{#if a.new > 0}<span class="r-badge {a.mine ? '' : 'opacity-50'}">{a.new}</span>{/if}
					<span class="text-faint">›</span>
				</button>
			</div>
		{/each}
	{:else}
		<p class="r-note">{showAll ? t('web.reader.no_areas') : t('web.reader.nothing_unread')}</p>
	{/each}
	{#if showAll}
		<p class="r-note text-xs">{t('web.reader.mine_hint')}</p>
	{/if}
{/if}
