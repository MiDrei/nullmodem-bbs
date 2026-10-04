<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listUnresolvedNetmail,
		getUnresolvedNetmail,
		deleteUnresolvedNetmail,
		batchDeleteUnresolvedNetmail,
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
	let selected = $state<Set<number>>(new Set());
	let batchDeleting = $state(false);

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
			const ids = new Set(messages.map((m) => m.id));
			selected = new Set([...selected].filter((id) => ids.has(id)));
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('admin.netmail.could_not_load_unresolved_netmail');
		} finally {
			loaded = true;
		}
	}

	function toggleSelected(id: number) {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selected = next;
	}

	function toggleSelectAll() {
		selected = selected.size === messages.length ? new Set() : new Set(messages.map((m) => m.id));
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
			expandError = err instanceof ApiError ? err.message : t('admin.netmail.could_not_load_message');
		}
	}

	async function remove(m: UnresolvedNetmailSummary) {
		if (!auth.token) return;
		if (!confirm(t('admin.netmail.delete_this_undeliverable_message_subjec', { SUBJECT: m.subject })))
			return;
		deletingID = m.id;
		try {
			await deleteUnresolvedNetmail(auth.token, m.id);
			messages = messages.filter((x) => x.id !== m.id);
			if (expandedID === m.id) {
				expandedID = null;
				expanded = null;
			}
			toast.push(t('admin.netmail.message_deleted'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.netmail.could_not_delete_message'), 'error');
		} finally {
			deletingID = null;
		}
	}

	async function removeSelected() {
		if (!auth.token || selected.size === 0) return;
		if (!confirm(t('admin.netmail.delete_size_selected_undeliverable_messa', { SIZE: selected.size })))
			return;
		batchDeleting = true;
		try {
			const ids = [...selected];
			await batchDeleteUnresolvedNetmail(auth.token, ids);
			const removed = new Set(ids);
			messages = messages.filter((m) => !removed.has(m.id));
			if (expandedID !== null && removed.has(expandedID)) {
				expandedID = null;
				expanded = null;
			}
			selected = new Set();
			toast.push(t('admin.netmail.selected_messages_deleted'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.netmail.could_not_delete_selected_messages'), 'error');
		} finally {
			batchDeleting = false;
		}
	}
</script>

<h1 class="mb-2 page-title">{t('admin.netmail.undeliverable_netmail')}</h1>
<p class="mb-6 text-sm text-slate-400">
	{t('admin.netmail.netmail_addressed_to_a_name')}
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">{t('admin.common.loading')}</p>
{:else if messages.length === 0}
	<p class="text-sm text-slate-500">{t('admin.netmail.nothing_undeliverable_right_now')}</p>
{:else}
	<div class="mb-3 flex items-center gap-3">
		<label class="flex items-center gap-2 text-sm text-slate-400">
			<input
				type="checkbox" class="check"
				checked={selected.size > 0 && selected.size === messages.length}
				onchange={toggleSelectAll}
			/>
			{t('admin.netmail.select_all')}
		</label>
		<button
			class="btn-danger btn-sm"
			disabled={selected.size === 0 || batchDeleting}
			onclick={removeSelected}
		>
			{batchDeleting ? t('admin.netmail.deleting') : t('admin.netmail.delete_selected_size', { SIZE: selected.size })}
		</button>
	</div>

	<div class="flex flex-col gap-3">
		{#each messages as m (m.id)}
			<div class="rounded-xl border border-line p-4">
				<div class="flex items-start gap-3">
					<input
						type="checkbox"
						class="check mt-1"
						checked={selected.has(m.id)}
						onchange={() => toggleSelected(m.id)}
					/>
					<button
						type="button"
						class="flex min-w-0 flex-1 items-start justify-between gap-4 text-left"
						onclick={() => toggle(m)}
					>
						<div class="min-w-0">
							<div class="truncate text-sm font-medium text-slate-100">{m.subject}</div>
							<div class="mt-0.5 text-xs text-slate-500">
								{t('admin.netmail.from')} <span class="text-slate-300">{m.from_name}</span>
								{#if m.from_address}<span class="font-mono">({m.from_address})</span>{/if}
								{t('admin.netmail.to')} <span class="font-mono text-slate-300">{m.to_name}</span>
								{t('admin.netmail.tolocalestring', { TOLOCALESTRING: new Date(m.posted_at).toLocaleString(i18n.locale) })}
							</div>
						</div>
						<span class="shrink-0 text-xs text-slate-500">{expandedID === m.id ? '▲' : '▼'}</span>
					</button>
				</div>

				{#if expandedID === m.id}
					<div class="mt-3 border-t border-slate-800 pt-3">
						{#if expandError}
							<p class="text-sm text-red-400">{expandError}</p>
						{:else if !expanded}
							<p class="text-sm text-slate-400">{t('admin.common.loading')}</p>
						{:else}
							<div class="inline-block font-mono text-sm leading-tight whitespace-pre text-slate-200">
								{@html expanded.body_html}
							</div>
						{/if}
						<div class="mt-3">
							<button
								class="btn-danger btn-sm"
								disabled={deletingID === m.id}
								onclick={() => remove(m)}
							>
								{deletingID === m.id ? t('admin.netmail.deleting') : t('admin.common.delete')}
							</button>
						</div>
					</div>
				{/if}
			</div>
		{/each}
	</div>
{/if}
