<script lang="ts">
	import { t, tn, i18n } from '$lib/i18n.svelte';
	// The FTN nodelists, as last imported from the networks' file
	// echoes (the mailer checks every 10 minutes for a new one).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { getNodelistStatus, syncNodelists, ApiError, type NodelistImport } from '$lib/api';

	let imports = $state<NodelistImport[]>([]);
	let loaded = $state(false);
	let syncing = $state(false);

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			imports = await getNodelistStatus(auth.token);
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : t('admin.nodelists.could_not_load_the_nodelists'), 'error');
		} finally {
			loaded = true;
		}
	});

	async function sync() {
		if (!auth.token) return;
		syncing = true;
		try {
			const res = await syncNodelists(auth.token);
			imports = res.imports;
			toast.push(tn('admin.nodelists.imported', res.imported), 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : t('admin.nodelists.the_import_failed'), 'error');
		} finally {
			syncing = false;
		}
	}
</script>

<div class="mb-6 flex items-start justify-between gap-4">
	<div>
		<h1 class="page-title">{t('admin.nodelists.nodelists')}</h1>
		<p class="page-subtitle max-w-2xl leading-relaxed">
			{t('admin.nodelists.each_network_s_directory_of')}
		</p>
	</div>
	<button class="btn-secondary btn-sm shrink-0" disabled={syncing} onclick={sync}>{syncing ? t('admin.nodelists.importing') : t('admin.nodelists.import_again')}</button>
</div>

{#if loaded && imports.length === 0}
	<p class="text-sm text-muted">{t('admin.nodelists.none_yet_a_nodelist_file')}</p>
{:else}
	<div class="overflow-hidden rounded-xl border border-line">
		<table class="w-full text-left text-sm">
			<thead class="card-label">
				<tr class="border-b border-line">
					<th class="p-3">{t('admin.common.network')}</th>
					<th class="p-3">{t('admin.nodelists.file')}</th>
					<th class="p-3">{t('admin.nodelists.systems')}</th>
					<th class="p-3">{t('admin.nodelists.imported')}</th>
				</tr>
			</thead>
			<tbody class="divide-y divide-slate-800">
				{#each imports as i (i.network)}
					<tr>
						<td class="p-3 text-ink-strong">{i.network}</td>
						<td class="p-3 font-mono text-xs text-muted">{i.filename}</td>
						<td class="p-3 text-ink">{i.entries}</td>
						<td class="p-3 text-xs text-muted">{new Date(i.imported_at).toLocaleString(i18n.locale)}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
