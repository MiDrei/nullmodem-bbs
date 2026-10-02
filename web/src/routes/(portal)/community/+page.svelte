<script lang="ts">
	// What callers share beyond messages: the sysop's polls, the BBS
	// list callers keep, and the FTN nodelists to look systems up in.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import LastCallersList from '$lib/LastCallersList.svelte';
	import DoorBulletins from '$lib/DoorBulletins.svelte';
	import {
		listBBSPolls,
		voteBBSPoll,
		listBBSList,
		saveBBSListEntry,
		deleteBBSListEntry,
		searchNodelist,
		getDoorBulletins,
		ApiError,
		type DoorBulletinView,
		type Poll,
		type BBSListEntry,
		type BBSListInput,
		type NodelistEntry,
		type NodelistImport
	} from '$lib/api';

	type Tab = 'polls' | 'bbs' | 'callers' | 'doors' | 'nodelist';
	let tab = $state<Tab>((page.url.searchParams.get('tab') as Tab) || 'polls');

	let polls = $state<Poll[]>([]);
	let bbs = $state<BBSListEntry[]>([]);
	let editing = $state<{ id?: number; entry: BBSListInput } | null>(null);
	let query = $state('');
	let found = $state<NodelistEntry[]>([]);
	let imports = $state<NodelistImport[]>([]);
	let searched = $state(false);
	let bulletins = $state<DoorBulletinView[]>([]);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && err.status === 401) {
			bbsAuth.clear();
			await goto('/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		const t = bbsAuth.token;
		try {
			[polls, bbs] = await Promise.all([listBBSPolls(t), listBBSList(t)]);
			imports = (await searchNodelist(t, '__nothing__')).imports;
			bulletins = await getDoorBulletins(t);
		} catch (err) {
			await failed(err, 'Could not load the page.');
		}
	});

	function select(t: Tab) {
		tab = t;
		history.replaceState(history.state, '', `?tab=${t}`);
	}

	async function vote(p: Poll, optionId: number) {
		if (!bbsAuth.token) return;
		try {
			const updated = await voteBBSPoll(bbsAuth.token, p.id, optionId);
			polls = polls.map((x) => (x.id === p.id ? updated : x));
		} catch (err) {
			await failed(err, 'Could not vote.');
		}
	}

	const mine = (e: BBSListEntry) => bbsAuth.isSysop || e.added_by.toLowerCase() === (bbsAuth.username ?? '').toLowerCase();

	async function saveEntry(e: SubmitEvent) {
		e.preventDefault();
		if (!bbsAuth.token || !editing) return;
		try {
			const saved = await saveBBSListEntry(bbsAuth.token, editing.entry, editing.id);
			bbs = editing.id ? bbs.map((x) => (x.id === saved.id ? saved : x)) : [...bbs, saved].sort((a, b) => a.name.localeCompare(b.name));
			editing = null;
		} catch (err) {
			await failed(err, 'Could not save it.');
		}
	}

	async function removeEntry(e: BBSListEntry) {
		if (!bbsAuth.token || !confirm(`Remove ${e.name} from the list?`)) return;
		try {
			await deleteBBSListEntry(bbsAuth.token, e.id);
			bbs = bbs.filter((x) => x.id !== e.id);
		} catch (err) {
			await failed(err, 'Could not remove it.');
		}
	}

	async function search(e?: SubmitEvent) {
		e?.preventDefault();
		if (!bbsAuth.token || !query.trim()) return;
		try {
			const res = await searchNodelist(bbsAuth.token, query.trim());
			found = res.entries;
			imports = res.imports;
			searched = true;
		} catch (err) {
			await failed(err, 'Could not search.');
		}
	}

	const pct = (p: Poll, votes: number) => (p.total ? Math.round((votes * 100) / p.total) : 0);

	// The online check's verdict, for a dot and a tooltip.
	const seen = (iso: string) => (iso && !iso.startsWith('0001') ? new Date(iso) : null);
	function status(e: BBSListEntry): { cls: string; label: string; text: string } {
		const checked = seen(e.checked_at);
		if (!checked) return { cls: 'bg-line-strong', label: 'not checked yet', text: 'not checked yet' };
		if (e.online) return { cls: 'bg-emerald-400', label: 'online', text: `online · checked ${checked.toLocaleString()}` };
		const up = seen(e.last_up_at);
		return { cls: 'bg-red-400', label: 'offline', text: up ? `offline · last seen ${up.toLocaleDateString()}` : 'not reachable' };
	}
</script>

<div class="mb-5">
	<h1 class="page-title">Community</h1>
	<p class="page-subtitle">Polls, the BBS list our callers keep, who called around the network, and the FTN nodelists.</p>
</div>

<div class="mb-5 flex gap-1 border-b border-line">
	{#each [['polls', 'Polls'], ['bbs', 'BBS List'], ['callers', 'Last Callers'], ['nodelist', 'Nodelist']] as [t, label] (t)}
		<button
			class="border-b-2 px-3 py-2 text-sm font-medium transition {tab === t ? 'border-accent text-ink-strong' : 'border-transparent text-muted hover:text-ink'}"
			onclick={() => select(t as Tab)}>{label}</button
		>
	{/each}
</div>

{#if tab === 'polls'}
	{#if polls.length === 0}
		<p class="text-sm text-muted">No polls yet.</p>
	{/if}
	<div class="flex flex-col gap-4">
		{#each polls as p (p.id)}
			<section class="card">
				<div class="mb-3 flex items-baseline justify-between gap-3">
					<h2 class="font-semibold text-ink-strong">{p.question}</h2>
					<span class="shrink-0 text-xs text-faint">{p.total} vote{p.total === 1 ? '' : 's'}{p.closed ? ' · closed' : ''}</span>
				</div>
				<div class="flex flex-col gap-2">
					{#each p.options as o (o.id)}
						<button
							class="relative overflow-hidden rounded-lg border px-3 py-2 text-left text-sm transition {o.id === p.my_vote
								? 'border-accent'
								: 'border-line hover:border-line-strong'} disabled:cursor-default"
							disabled={p.closed}
							onclick={() => vote(p, o.id)}
						>
							{#if p.my_vote || p.closed}
								<span class="absolute inset-y-0 left-0 bg-accent/15" style="width: {pct(p, o.votes)}%"></span>
							{/if}
							<span class="relative flex justify-between gap-3">
								<span class="text-ink">{o.text}{o.id === p.my_vote ? ' ✓' : ''}</span>
								{#if p.my_vote || p.closed}<span class="text-muted">{pct(p, o.votes)}% · {o.votes}</span>{/if}
							</span>
						</button>
					{/each}
				</div>
				{#if !p.closed}
					<p class="mt-2 text-xs text-faint">{p.my_vote ? 'You can change your vote.' : 'Pick an answer to see the results.'}</p>
				{/if}
			</section>
		{/each}
	</div>
{:else if tab === 'bbs'}
	<div class="mb-3 flex justify-end">
		{#if !editing}
			<button class="btn-primary btn-sm" onclick={() => (editing = { entry: { name: '', address: '', sysop: '', software: '', description: '' } })}>+ Add a BBS</button>
		{/if}
	</div>
	{#if editing}
		<form class="card mb-4 grid gap-3 sm:grid-cols-2" onsubmit={saveEntry}>
			<input class="field" placeholder="Name" maxlength="40" bind:value={editing.entry.name} required />
			<input class="field font-mono" placeholder="Address (host:port)" maxlength="60" bind:value={editing.entry.address} required />
			<input class="field" placeholder="Sysop" maxlength="60" bind:value={editing.entry.sysop} />
			<input class="field" placeholder="Software" maxlength="60" bind:value={editing.entry.software} />
			<input class="field sm:col-span-2" placeholder="About it" maxlength="200" bind:value={editing.entry.description} />
			<div class="flex justify-end gap-2 sm:col-span-2">
				<button type="button" class="btn-secondary btn-sm" onclick={() => (editing = null)}>Cancel</button>
				<button type="submit" class="btn-primary btn-sm">Save</button>
			</div>
		</form>
	{/if}
	{#if bbs.length === 0}
		<p class="text-sm text-muted">The list is empty so far -- add the boards you call.</p>
	{/if}
	<div class="flex flex-col divide-y divide-line">
		{#each bbs as e (e.id)}
			<div class="flex flex-wrap items-start gap-3 py-3">
				<div class="min-w-0 flex-1">
					<div class="flex items-center gap-2 font-medium text-ink-strong">
						<span class="inline-block h-2 w-2 shrink-0 rounded-full {status(e).cls}" title={status(e).text}></span>
						{e.name}
						<span class="text-[11px] font-normal text-faint" title={status(e).text}>{status(e).label}</span>
					</div>
					<div class="font-mono text-xs text-accent">{e.address}</div>
					<div class="mt-0.5 text-xs text-muted">
						{[e.sysop && `Sysop ${e.sysop}`, e.software].filter(Boolean).join(' · ')}
					</div>
					{#if e.description}<p class="mt-1 text-sm text-ink-soft">{e.description}</p>{/if}
					<div class="mt-0.5 text-[11px] text-faint">added by {e.added_by}</div>
				</div>
				{#if mine(e)}
					<div class="flex gap-1.5">
						<button class="btn-secondary btn-xs" onclick={() => (editing = { id: e.id, entry: { name: e.name, address: e.address, sysop: e.sysop, software: e.software, description: e.description } })}>Edit</button>
						<button class="btn-secondary btn-xs" onclick={() => removeEntry(e)}>Remove</button>
					</div>
				{/if}
			</div>
		{/each}
	</div>
{:else if tab === 'doors'}
	{#if bulletins.length === 0}
		<p class="text-sm text-muted">No scoreboards yet -- the doors write them as they're played.</p>
	{:else}
		<DoorBulletins list={bulletins} />
	{/if}
{:else if tab === 'callers'}
	<p class="mb-3 text-xs text-muted">Who was on which board of the network lately, newest first.</p>
	<LastCallersList />
{:else}
	<p class="mb-3 text-xs text-muted">
		{#if imports.length}
			{imports.map((i) => `${i.network} (${i.entries})`).join(' · ')}
		{:else}
			No nodelists yet -- they arrive with the networks' file echoes.
		{/if}
	</p>
	<form class="mb-4 flex gap-2" onsubmit={search}>
		<input class="field min-w-0 flex-1" placeholder="Name, sysop, place or address (21:1/)" bind:value={query} />
		<button type="submit" class="btn-primary btn-sm" disabled={!query.trim()}>Search</button>
	</form>
	{#if searched && found.length === 0}
		<p class="text-sm text-muted">Nothing found.</p>
	{/if}
	<div class="flex flex-col divide-y divide-line">
		{#each found as n (n.network + n.address)}
			<div class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 py-2 text-sm">
				<span class="w-28 font-mono text-accent">{n.address}</span>
				<span class="min-w-0 flex-1 text-ink-strong">{n.name}{#if n.keyword && n.keyword.toLowerCase() !== 'pvt'} <span class="text-xs text-amber-400">{n.keyword}</span>{/if}</span>
				<span class="text-muted">{n.sysop}</span>
				<span class="w-full pl-0 text-xs text-faint sm:w-auto">{n.location}{n.host ? ` · ${n.host}` : ''} · {n.network}</span>
			</div>
		{/each}
	</div>
{/if}
