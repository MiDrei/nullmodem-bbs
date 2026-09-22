<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listUnresolvedNetmail,
		getUnresolvedNetmail,
		deleteUnresolvedNetmail,
		ApiError,
		type UnresolvedNetmailSummary,
		type UnresolvedNetmail
	} from '$lib/api';

	let messages = $state<UnresolvedNetmailSummary[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);
	let expandedID = $state<number | null>(null);
	let expanded = $state<UnresolvedNetmail | null>(null);
	let expandError = $state<string | null>(null);
	let deletingID = $state<number | null>(null);

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!auth.token) return;
		try {
			messages = await listUnresolvedNetmail(auth.token);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load unresolved netmail.';
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	async function toggle(m: UnresolvedNetmailSummary) {
		if (expandedID === m.id) {
			expandedID = null;
			expanded = null;
			return;
		}
		if (!auth.token) return;
		expandedID = m.id;
		expanded = null;
		expandError = null;
		try {
			expanded = await getUnresolvedNetmail(auth.token, m.id);
		} catch (err) {
			if (await handleAuthError(err)) return;
			expandError = err instanceof ApiError ? err.message : 'Could not load message.';
		}
	}

	async function remove(m: UnresolvedNetmailSummary) {
		if (!auth.token) return;
		if (!confirm(`Delete this undeliverable message ("${m.subject}")? There's nowhere else it can go.`))
			return;
		deletingID = m.id;
		try {
			await deleteUnresolvedNetmail(auth.token, m.id);
			messages = messages.filter((x) => x.id !== m.id);
			if (expandedID === m.id) {
				expandedID = null;
				expanded = null;
			}
			toast.push('Message deleted.', 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not delete message.', 'error');
		} finally {
			deletingID = null;
		}
	}
</script>

<h1 class="mb-2 text-xl font-semibold text-slate-100">Undeliverable Netmail</h1>
<p class="mb-6 text-sm text-slate-400">
	Netmail addressed to a name that never resolved to a real local user, and with no remote FTN
	address either -- a mistyped recipient, or a reply from an automated robot (Areafix/Filefix,
	...) addressed back to whatever name this system used as its own request's From. Stored, never
	silently discarded, but otherwise invisible anywhere in the BBS.
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if messages.length === 0}
	<p class="text-sm text-slate-500">Nothing undeliverable right now.</p>
{:else}
	<div class="flex flex-col gap-3">
		{#each messages as m (m.id)}
			<div class="rounded border border-slate-800 p-4">
				<button
					type="button"
					class="flex w-full items-start justify-between gap-4 text-left"
					onclick={() => toggle(m)}
				>
					<div class="min-w-0">
						<div class="truncate text-sm font-medium text-slate-100">{m.subject}</div>
						<div class="mt-0.5 text-xs text-slate-500">
							From <span class="text-slate-300">{m.from_name}</span>
							{#if m.from_address}<span class="font-mono">({m.from_address})</span>{/if}
							to <span class="font-mono text-slate-300">{m.to_name}</span>
							&middot; {new Date(m.posted_at).toLocaleString()}
						</div>
					</div>
					<span class="shrink-0 text-xs text-slate-500">{expandedID === m.id ? '▲' : '▼'}</span>
				</button>

				{#if expandedID === m.id}
					<div class="mt-3 border-t border-slate-800 pt-3">
						{#if expandError}
							<p class="text-sm text-red-400">{expandError}</p>
						{:else if !expanded}
							<p class="text-sm text-slate-400">Loading…</p>
						{:else}
							<div class="inline-block font-mono text-sm leading-tight whitespace-pre text-slate-200">
								{@html expanded.body_html}
							</div>
						{/if}
						<div class="mt-3">
							<button
								class="rounded border border-red-800 px-3 py-1 text-sm text-red-400 hover:bg-red-950 disabled:opacity-50"
								disabled={deletingID === m.id}
								onclick={() => remove(m)}
							>
								{deletingID === m.id ? 'Deleting…' : 'Delete'}
							</button>
						</div>
					</div>
				{/if}
			</div>
		{/each}
	</div>
{/if}
