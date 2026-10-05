<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// Statistics: calls, messages, doors, downloads and network traffic
	// over a chosen span -- the front page shows the last 30 days of
	// the public part.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getStats, sendRecap, ApiError, type StatsReport } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import StatsBoard from '$lib/stats/StatsBoard.svelte';

	let days = $state(30);
	let report = $state<StatsReport | null>(null);
	let loadError = $state<string | null>(null);

	async function load() {
		if (!auth.token) return;
		try {
			report = await getStats(auth.token, days);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : t('admin.stats.could_not_load_the_statistics');
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	let sending = $state(false);
	async function recapNow() {
		if (!auth.token) return;
		sending = true;
		try {
			const r = await sendRecap(auth.token);
			toast.push(t('admin.stats.recap_sent_to_sent_sysop', { SENT: r.sent }), 'success');
		} catch (err) {
			toast.push(err instanceof ApiError ? err.message : t('admin.stats.could_not_send_it'), 'error');
		} finally {
			sending = false;
		}
	}

	function pick(d: number) {
		days = d;
		load();
	}
</script>

<div class="mb-5 flex flex-wrap items-end justify-between gap-4">
	<div>
		<h1 class="page-title">{t('admin.stats.statistics')}</h1>
		<p class="page-subtitle">{t('admin.stats.calls_messages_doors_and_traffic')}</p>
	</div>
	<div class="flex gap-1">
		{#each [7, 30, 90, 365] as d (d)}
			<button class="pill {days === d ? 'pill-active' : ''}" onclick={() => pick(d)}>{d === 365 ? t('admin.stats.one_year') : t('admin.stats.d_days', { D: d })}</button>
		{/each}
	</div>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !report}
	<p class="text-sm text-muted">{t('admin.common.loading')}</p>
{:else}
	<StatsBoard r={report} full />
	<div class="mt-4 flex flex-wrap items-center justify-between gap-3">
		<p class="text-xs text-faint">{t('admin.stats.on_the_1st_a_recap')}</p>
		<button class="btn-secondary btn-sm" disabled={sending} onclick={recapNow}>{sending ? t('admin.stats.sending') : t('admin.stats.send_a_recap_now')}</button>
	</div>
	<p class="mt-2 text-xs text-faint">
		{t('admin.stats.calls_are_counted_since_this')}
	</p>
{/if}
