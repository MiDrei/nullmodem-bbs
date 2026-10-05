<script lang="ts">
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listArchive,
		downloadArchiveEntry,
		previewArchiveEntry,
		inspectArchiveEntry,
		deleteArchiveEntry,
		retossArchiveEntries,
		ApiError,
		type ArchiveEntry,
		type ArchiveInspection
	} from '$lib/api';

	const PAGE_SIZE = 50;

	let entries = $state<ArchiveEntry[]>([]);
	let total = $state(0);
	let offset = $state(0);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let selected = $state<Set<number>>(new Set());
	let expandedID = $state<number | null>(null);
	let inspection = $state<ArchiveInspection | null>(null);
	let inspectError = $state<string | null>(null);
	let showRaw = $state(false);
	let previewText = $state<string | null>(null);
	let previewError = $state<string | null>(null);
	let busyID = $state<number | null>(null);
	let retossing = $state(false);

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
			const res = await listArchive(auth.token, PAGE_SIZE, offset);
			entries = res.entries;
			total = res.total;
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('admin.archive.could_not_load_the_archive');
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

	function toggleSelected(id: number) {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selected = next;
	}

	async function toggleExpand(entry: ArchiveEntry) {
		if (expandedID === entry.id) {
			expandedID = null;
			return;
		}
		if (!auth.token) return;
		expandedID = entry.id;
		showRaw = false;
		inspection = null;
		inspectError = null;
		previewText = null;
		previewError = null;
		try {
			inspection = await inspectArchiveEntry(auth.token, entry.id);
		} catch (err) {
			if (await handleAuthError(err)) return;
			inspectError = err instanceof ApiError ? err.message : t('admin.archive.could_not_inspect_entry');
		}
	}

	async function loadRaw(entry: ArchiveEntry) {
		showRaw = true;
		if (previewText !== null || !auth.token) return;
		try {
			previewText = await previewArchiveEntry(auth.token, entry.id);
		} catch (err) {
			if (await handleAuthError(err)) return;
			previewError = err instanceof ApiError ? err.message : t('web.common.could_not_load_preview');
		}
	}

	async function download(entry: ArchiveEntry) {
		if (!auth.token) return;
		try {
			await downloadArchiveEntry(auth.token, entry.id, entry.filename);
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.archive.download_failed'), 'error');
		}
	}

	async function remove(entry: ArchiveEntry) {
		if (!auth.token) return;
		if (!confirm(t('admin.archive.delete_archived_filename_this_can', { FILENAME: entry.filename }))) return;
		busyID = entry.id;
		try {
			await deleteArchiveEntry(auth.token, entry.id);
			entries = entries.filter((e) => e.id !== entry.id);
			total -= 1;
			const next = new Set(selected);
			next.delete(entry.id);
			selected = next;
			if (expandedID === entry.id) expandedID = null;
			toast.push(t('admin.archive.entry_deleted'), 'success');
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.archive.could_not_delete_entry'), 'error');
		} finally {
			busyID = null;
		}
	}

	async function retossSelected() {
		if (!auth.token || selected.size === 0) return;
		retossing = true;
		try {
			const res = await retossArchiveEntries(auth.token, [...selected]);
			const skipped = res.skipped_files?.length
				? t('admin.archive.skipped_v', { V: res.skipped_files.join(', ') })
				: '';
			toast.push(
				t('admin.archive.retoss_done', { NETMAIL: res.received, ECHOMAIL: res.received_echo, FILES: res.received_files }) + skipped,
				'success'
			);
			selected = new Set();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('admin.archive.re_toss_failed'), 'error');
		} finally {
			retossing = false;
		}
	}

	function outcomeClass(outcome: string): string {
		if (outcome === 'error') return 'bg-red-950 text-red-400';
		if (outcome === 'skipped') return 'bg-amber-950 text-amber-400';
		return 'bg-emerald-950 text-emerald-400';
	}

	async function prevPage() {
		offset = Math.max(0, offset - PAGE_SIZE);
		await load();
	}
	async function nextPage() {
		offset += PAGE_SIZE;
		await load();
	}
</script>

<h1 class="mb-2 page-title">{t('admin.common.packet_analyzer')}</h1>
<p class="mb-6 text-sm text-slate-400">
	{t('admin.archive.every_inbound_binkp_file_a')}
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
{:else}
	<div class="mb-4 flex items-center justify-between">
		<button
			class="btn-primary btn-sm"
			disabled={selected.size === 0 || retossing}
			onclick={retossSelected}
		>
			{retossing ? t('admin.archive.re_tossing') : t('admin.archive.re_toss_selected_size', { SIZE: selected.size })}
		</button>
		<div class="flex items-center gap-2 text-sm text-slate-400">
			<span>{t('admin.archive.v_v2_of_total', { V: total === 0 ? 0 : offset + 1, V2: Math.min(offset + PAGE_SIZE, total), TOTAL: total })}</span>
			<button
				class="btn-secondary btn-sm disabled:opacity-40"
				disabled={offset === 0}
				onclick={prevPage}
			>
				{t('admin.archive.prev')}
			</button>
			<button
				class="btn-secondary btn-sm disabled:opacity-40"
				disabled={offset + PAGE_SIZE >= total}
				onclick={nextPage}
			>
				{t('admin.archive.next')}
			</button>
		</div>
	</div>

	{#if entries.length === 0}
		<p class="text-sm text-slate-500">{t('admin.archive.nothing_captured_yet')}</p>
	{:else}
		<div class="flex flex-col gap-2">
			{#each entries as entry (entry.id)}
				<div class="rounded-xl border border-line p-3">
					<div class="flex items-start gap-3">
						<input
							type="checkbox"
							class="check mt-1"
							checked={selected.has(entry.id)}
							onchange={() => toggleSelected(entry.id)}
						/>
						<button
							type="button"
							class="min-w-0 flex-1 text-left"
							onclick={() => toggleExpand(entry)}
						>
							<div class="flex flex-wrap items-center gap-2">
								<span class="truncate font-mono text-sm text-slate-100">{entry.filename}</span>
								<span
									class="rounded px-1.5 py-0.5 text-xs font-semibold uppercase {outcomeClass(
										entry.outcome
									)}"
								>
									{entry.outcome}
								</span>
							</div>
							<div class="mt-0.5 text-xs text-slate-500">
								{entry.uplink_address}
								{#if entry.uplink_host}
									<span class="font-mono">({entry.uplink_host})</span>
								{/if}
								{t('admin.archive.size_human_tolocalestring', { SIZE_HUMAN: entry.size_human, TOLOCALESTRING: new Date(entry.received_at).toLocaleString(i18n.locale) })}
								{#if entry.detail}
									&middot; <span class="text-red-400">{entry.detail}</span>
								{/if}
							</div>
						</button>
						<div class="flex shrink-0 gap-2">
							<button
								class="btn-secondary btn-xs"
								onclick={() => download(entry)}
							>
								{t('web.common.download')}
							</button>
							<button
								class="btn-danger btn-xs"
								disabled={busyID === entry.id}
								onclick={() => remove(entry)}
							>
								{t('web.common.delete')}
							</button>
						</div>
					</div>

					{#if expandedID === entry.id}
						<div class="mt-3 border-t border-slate-800 pt-3">
							{#if inspectError}
								<p class="text-sm text-red-400">{inspectError}</p>
							{:else if inspection === null}
								<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
							{:else if inspection.kind === 'packet' || inspection.kind === 'bundle'}
								<div class="flex flex-col gap-3">
									{#if inspection.kind === 'bundle'}
										<p class="text-xs text-slate-500">
											{t('admin.archive.packet_bundle_containing_length_packet', { LENGTH: inspection.packets?.length ?? 0 })}
										</p>
									{/if}
									{#each inspection.packets ?? [] as p}
										<div class="rounded-xl border border-line/70 bg-slate-900/40 p-2">
											<div class="font-mono text-xs text-slate-400">
												{t('admin.archive.name_orig_addr_dest_addr', { NAME: p.name, ORIG_ADDR: p.orig_addr, DEST_ADDR: p.dest_addr, TOLOCALESTRING: new Date(
													p.created
												).toLocaleString(i18n.locale) })}
											</div>
											{#if p.messages.length === 0}
												<p class="mt-1 text-xs text-slate-500">{t('web.common.no_messages')}</p>
											{:else}
												<table class="mt-2 w-full text-xs">
													<thead class="text-slate-500">
														<tr class="text-left">
															<th class="py-1 pr-2 font-normal">{t('common.from')}</th>
															<th class="py-1 pr-2 font-normal">{t('web.common.to')}</th>
															<th class="py-1 pr-2 font-normal">{t('common.subject')}</th>
															<th class="py-1 pr-2 font-normal">{t('common.area')}</th>
															<th class="py-1 pr-2 font-normal">{t('admin.archive.written')}</th>
															<th class="py-1 font-normal">{t('common.size')}</th>
														</tr>
													</thead>
													<tbody>
														{#each p.messages as m}
															<tr class="border-t border-slate-800/60 text-slate-300">
																<td class="py-1 pr-2">{m.from_name}</td>
																<td class="py-1 pr-2">{m.to_name}</td>
																<td class="py-1 pr-2">{m.subject}</td>
																<td class="py-1 pr-2 font-mono">
																	{m.area_tag || (m.private ? 'netmail' : '')}
																</td>
																<td class="py-1 pr-2">{new Date(m.written).toLocaleString(i18n.locale)}</td>
																<td class="py-1">{m.body_size} B</td>
															</tr>
														{/each}
													</tbody>
												</table>
											{/if}
										</div>
									{/each}
								</div>
							{:else if inspection.kind === 'tic'}
								<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
									<dt class="text-slate-500">{t('common.area')}</dt>
									<dd class="font-mono text-slate-300">{inspection.tic?.area}</dd>
									<dt class="text-slate-500">{t('admin.common.file')}</dt>
									<dd class="font-mono text-slate-300">{inspection.tic?.file}</dd>
									<dt class="text-slate-500">{t('web.common.description')}</dt>
									<dd class="whitespace-pre-wrap text-slate-300">
										{inspection.tic?.description || t('admin.archive.none_this_is_the_missing')}
									</dd>
									<dt class="text-slate-500">{t('common.size')}</dt>
									<dd class="text-slate-300">{inspection.tic?.size_bytes} B</dd>
									{#if inspection.tic?.has_crc32}
										<dt class="text-slate-500">CRC-32</dt>
										<dd class="font-mono text-slate-300">{inspection.tic?.crc32}</dd>
									{/if}
									<dt class="text-slate-500">{t('admin.archive.origin')}</dt>
									<dd class="font-mono text-slate-300">{inspection.tic?.origin}</dd>
								</dl>
							{:else}
								<p class="text-sm text-slate-500">
									{inspection.error || "Not a recognized packet, bundle, or TIC descriptor."}
								</p>
							{/if}

							<div class="mt-3">
								{#if !showRaw}
									<button
										type="button"
										class="text-xs text-cyan-400 hover:underline"
										onclick={() => loadRaw(entry)}
									>
										{t('admin.archive.show_raw_bytes')}
									</button>
								{:else if previewError}
									<p class="text-sm text-red-400">{previewError}</p>
								{:else if previewText === null}
									<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
								{:else}
									<pre class="max-h-96 overflow-auto rounded bg-slate-950 p-3 font-mono text-xs break-all whitespace-pre-wrap text-slate-300">{previewText}</pre>
								{/if}
							</div>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
{/if}
