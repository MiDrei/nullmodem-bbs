<script lang="ts">
	// Who was on which board of the network lately: the InterBBS Last
	// Callers list the boards post to their data echo (see
	// internal/lastcallers) -- a tab of the portal's Community page.
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { listLastCallers, ApiError, type LastCaller } from '$lib/api';

	let callers = $state<LastCaller[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	onMount(async () => {
		if (!bbsAuth.token) return;
		try {
			callers = await listLastCallers(bbsAuth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				bbsAuth.clear();
				await goto('/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : t('web.callers.load_failed');
		} finally {
			loaded = true;
		}
	});
</script>




{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else if callers.length === 0}
	<p class="text-sm text-muted">{t('web.callers.none')}</p>
{:else}
	<div class="overflow-x-auto">
		<table class="w-full min-w-[40rem] text-left text-[13px]">
			<thead class="card-label">
				<tr>
					<th class="pr-3 pb-2 font-normal">{t('web.callers.caller')}</th>
					<th class="pr-3 pb-2 font-normal">{t('web.callers.bbs')}</th>
					<th class="pr-3 pb-2 font-normal">{t('web.callers.when')}</th>
					<th class="pr-3 pb-2 font-normal">{t('web.callers.from')}</th>
					<th class="pb-2 font-normal">{t('web.callers.address')}</th>
				</tr>
			</thead>
			<tbody>
				{#each callers as c, i (i)}
					<tr class="border-t border-line">
						<td class="py-2 pr-3 font-medium text-ink-strong">{c.alias}</td>
						<td class="py-2 pr-3 text-ink">{c.bbs}</td>
						<td class="py-2 pr-3 font-mono text-xs whitespace-nowrap text-muted">{c.date} {c.time}</td>
						<td class="py-2 pr-3 text-muted">{c.location}</td>
						<td class="py-2 font-mono text-xs text-faint">{c.address}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<p class="mt-4 text-xs text-faint">{t('web.callers.clock')}</p>
{/if}
