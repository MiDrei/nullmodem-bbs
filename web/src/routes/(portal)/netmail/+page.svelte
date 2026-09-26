<script lang="ts">
	import { formatDate } from '$lib/datetime';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listBBSNetmail,
		listBBSNetmailSent,
		sendBBSNetmail,
		isFTNAddress,
		ApiError,
		type BBSNetmailSummary
	} from '$lib/api';

	let tab = $state<'inbox' | 'sent'>('inbox');
	let inbox = $state<BBSNetmailSummary[]>([]);
	let sent = $state<BBSNetmailSummary[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let composing = $state(false);
	let composeTo = $state('');
	let composeToName = $state('');
	let composeSubject = $state('');
	let composeBody = $state('');
	let composeCrash = $state(false);
	let sending = $state(false);

	// A local username needs no extra recipient name (it's unambiguous
	// on its own); an FTN address does, since a node has many possible
	// recipients -- mirrors the Telnet/SSH composer's "Recipient name"
	// prompt, which only appears for an FTN address too.
	let composeToIsFTN = $derived(isFTNAddress(composeTo.trim()));

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!bbsAuth.token) return;
		try {
			[inbox, sent] = await Promise.all([
				listBBSNetmail(bbsAuth.token),
				listBBSNetmailSent(bbsAuth.token)
			]);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load netmail.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		await load();
	});

	function relativeTime(iso: string): string {
		const diffMs = Date.now() - new Date(iso).getTime();
		const mins = Math.round(diffMs / 60000);
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins}m ago`;
		const hours = Math.round(mins / 60);
		if (hours < 24) return `${hours}h ago`;
		const days = Math.round(hours / 24);
		if (days < 7) return `${days}d ago`;
		return formatDate(iso);
	}

	async function send() {
		if (!bbsAuth.token) return;
		sending = true;
		try {
			await sendBBSNetmail(
				bbsAuth.token,
				composeTo,
				composeSubject,
				composeBody,
				composeToName,
				composeCrash
			);
			composing = false;
			composeTo = '';
			composeToName = '';
			composeSubject = '';
			composeBody = '';
			composeCrash = false;
			toast.push('Netmail sent.', 'success');
			await load();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not send netmail.', 'error');
		} finally {
			sending = false;
		}
	}
</script>

<div class="mb-5 flex items-center justify-between gap-4">
	<div>
		<h1 class="page-title">Netmail</h1>
		<p class="page-subtitle">Private mail, local or over FidoNet</p>
	</div>
	{#if !composing}
		<button class="btn-primary" onclick={() => (composing = true)}>+ New Netmail</button>
	{/if}
</div>

{#if composing}
	<form
		class="mb-7 flex flex-col gap-3.5"
		onsubmit={(e) => {
			e.preventDefault();
			send();
		}}
	>
		<label class="flex flex-col gap-2">
			<span class="card-label">To · username or FTN address, e.g. 1:234/56</span>
			<input class="field" bind:value={composeTo} />
		</label>
		{#if composeToIsFTN}
			<label class="flex flex-col gap-2">
				<span class="card-label">Recipient name · who at that address?</span>
				<input class="field" placeholder={composeTo} bind:value={composeToName} />
			</label>
			<label class="flex items-center gap-2 text-[13px] text-muted">
				<input type="checkbox" class="accent-accent" bind:checked={composeCrash} />
				Crash priority (immediate delivery)
			</label>
		{/if}
		<label class="flex flex-col gap-2">
			<span class="card-label">Subject</span>
			<input class="field" bind:value={composeSubject} />
		</label>
		<label class="flex flex-col gap-2">
			<span class="card-label">Message</span>
			<textarea
				class="body-panel h-64 resize-y outline-none focus:border-accent"
				bind:value={composeBody}
				placeholder="Write your message…"
			></textarea>
		</label>
		<div class="flex justify-end gap-2.5">
			<button type="button" class="btn-secondary" onclick={() => (composing = false)}>Cancel</button>
			<button
				type="submit"
				class="btn-primary"
				disabled={sending || !composeTo || !composeSubject || !composeBody}
			>
				{sending ? 'Sending…' : 'Send netmail'}
			</button>
		</div>
	</form>
{/if}

<div class="mb-1 flex gap-6 border-b border-line" role="tablist">
	<button
		role="tab"
		aria-selected={tab === 'inbox'}
		class="tab {tab === 'inbox' ? 'tab-active' : ''}"
		onclick={() => (tab = 'inbox')}
	>
		Inbox{inbox.some((m) => m.unread) ? ` · ${inbox.filter((m) => m.unread).length} new` : ''}
	</button>
	<button
		role="tab"
		aria-selected={tab === 'sent'}
		class="tab {tab === 'sent' ? 'tab-active' : ''}"
		onclick={() => (tab = 'sent')}
	>
		Sent
	</button>
</div>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-muted">Loading…</p>
{:else if (tab === 'inbox' ? inbox : sent).length === 0}
	<p class="mt-4 text-sm text-muted">
		{tab === 'inbox' ? 'No netmail yet.' : 'Nothing sent yet.'}
	</p>
{:else}
	<div class="flex flex-col">
		{#each tab === 'inbox' ? inbox : sent as m, i (m.id)}
			{@const unread = tab === 'inbox' && m.unread}
			<a href="/netmail/{m.id}" class="list-row group {unread ? 'list-row-unread' : ''}">
				<span class="list-num">{String(i + 1).padStart(2, '0')}</span>
				<div class="min-w-0 flex-1">
					<div
						class="truncate text-[13.5px] {unread
							? 'font-semibold text-white'
							: 'font-medium text-slate-100'} group-hover:text-accent"
					>
						{m.subject}
					</div>
					<div class="mt-0.5 truncate text-xs text-faint">
						{tab === 'inbox' ? m.from_name : `To ${m.to_name}`}
					</div>
				</div>
				{#if unread}
					<span class="badge-new">NEW</span>
				{/if}
				<span class="list-meta w-16 shrink-0 text-right">{relativeTime(m.posted_at)}</span>
			</a>
		{/each}
	</div>
{/if}
