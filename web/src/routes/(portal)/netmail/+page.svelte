<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { avatarGradient, initials } from '$lib/avatar';
	import { listBBSNetmail, sendBBSNetmail, ApiError, type BBSNetmailSummary } from '$lib/api';

	let inbox = $state<BBSNetmailSummary[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let composing = $state(false);
	let composeTo = $state('');
	let composeSubject = $state('');
	let composeBody = $state('');
	let sending = $state(false);

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
			inbox = await listBBSNetmail(bbsAuth.token);
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
		return new Date(iso).toLocaleDateString();
	}

	async function send() {
		if (!bbsAuth.token) return;
		sending = true;
		try {
			await sendBBSNetmail(bbsAuth.token, composeTo, composeSubject, composeBody);
			composing = false;
			composeTo = '';
			composeSubject = '';
			composeBody = '';
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

<div class="mb-6 flex items-center justify-between">
	<div>
		<h1 class="text-2xl font-bold tracking-tight text-slate-100">Netmail</h1>
		<p class="mt-1 text-sm text-slate-500">Private mail, local or over FidoNet.</p>
	</div>
	<button
		class="rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white shadow-lg shadow-fuchsia-500/20 transition hover:shadow-fuchsia-500/40"
		onclick={() => (composing = !composing)}
	>
		{composing ? 'Cancel' : '+ New Netmail'}
	</button>
</div>

{#if composing}
	<div class="mb-6 rounded-2xl border border-slate-800/60 bg-slate-900/40 p-4">
		<div class="flex flex-col gap-3">
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">To (username or FTN address, e.g. 1:234/56)</span>
				<input
					class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
					bind:value={composeTo}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Subject</span>
				<input
					class="rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 text-slate-100 focus:border-cyan-400 focus:outline-none"
					bind:value={composeSubject}
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">Message</span>
				<textarea
					class="h-32 rounded-lg border border-slate-700 bg-slate-900 px-3 py-1.5 font-mono text-sm text-slate-100 focus:border-cyan-400 focus:outline-none"
					bind:value={composeBody}
				></textarea>
			</label>
		</div>
		<button
			class="mt-4 rounded-full bg-gradient-to-r from-cyan-400 to-fuchsia-500 px-4 py-1.5 text-sm font-semibold text-white disabled:opacity-40"
			disabled={sending || !composeTo || !composeSubject || !composeBody}
			onclick={send}
		>
			{sending ? 'Sending…' : 'Send'}
		</button>
	</div>
{/if}

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if inbox.length === 0}
	<p class="text-sm text-slate-400">No netmail yet.</p>
{:else}
	<div class="overflow-hidden rounded-2xl border border-slate-800/60 bg-slate-900/40">
		{#each inbox as m, i (m.id)}
			<a
				href="/netmail/{m.id}"
				class="group flex items-center gap-3 px-4 py-2.5 transition hover:bg-slate-800/60 {i > 0
					? 'border-t border-slate-800/60'
					: ''} {m.unread ? 'border-l-2 border-l-fuchsia-400' : 'border-l-2 border-l-transparent'}"
			>
				<div
					class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-gradient-to-br text-xs font-bold text-white {avatarGradient(
						m.from_name
					)}"
				>
					{initials(m.from_name)}
				</div>
				<div class="min-w-0 flex-1">
					<div
						class="truncate text-sm {m.unread
							? 'font-semibold text-slate-100'
							: 'font-medium text-slate-400'} transition group-hover:text-white"
					>
						{m.subject}
					</div>
					<div class="truncate text-xs text-slate-500">{m.from_name}</div>
				</div>
				<span class="shrink-0 text-xs text-slate-500">{relativeTime(m.posted_at)}</span>
			</a>
		{/each}
	</div>
{/if}
