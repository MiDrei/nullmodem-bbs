<script lang="ts">
	// The statistics: the public part on the front page, all of it
	// (full) in the admin.
	import { t, i18n } from '$lib/i18n.svelte';
	import type { StatsReport, StatsDay } from '$lib/api';
	import Bars from './Bars.svelte';
	import RankList from './RankList.svelte';

	let { r, full = false }: { r: StatsReport; full?: boolean } = $props();

	const day = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString(i18n.locale, { day: 'numeric', month: 'short' });
	const dayTitles = (ds: StatsDay[], what: string) => ds.map((d) => `${day(d.date)}: ${t(what, { COUNT: d.count })}`);
	const avg = $derived(r.calls_per_day.length ? r.calls / r.calls_per_day.length : 0);
	const traffic = $derived((r.networks ?? []).filter((n) => n.in + n.out > 0));
</script>

<div class="flex flex-col gap-4">
	<div class="grid gap-4 md:grid-cols-3">
		<div class="card md:col-span-2">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-x-4">
				<h2 class="card-label">{t('web.stats.calls_per_day')}</h2>
				<span class="text-xs text-muted">
					{t('web.stats.calls_summary', { CALLS: r.calls.toLocaleString(i18n.locale), CALLERS: r.callers, AVG: avg.toLocaleString(i18n.locale, { maximumFractionDigits: 1, minimumFractionDigits: 1 }) })}
				</span>
			</div>
			<Bars
				values={r.calls_per_day.map((d) => d.count)}
				titles={dayTitles(r.calls_per_day, 'web.stats.n_calls')}
				first={r.calls_per_day.length ? day(r.calls_per_day[0].date) : ''}
				last={t('web.stats.today')}
			/>
		</div>
		<RankList title={t('web.stats.top_callers')} items={r.top_callers} detail={full} />
	</div>

	{#if full}
		<div class="grid gap-4 md:grid-cols-3">
			<div class="card md:col-span-2">
				<div class="mb-3 flex items-baseline justify-between">
					<h2 class="card-label">{t('web.stats.msgs_per_day')}</h2>
				</div>
				<Bars
					values={(r.posts_per_day ?? []).map((d) => d.count)}
					titles={dayTitles(r.posts_per_day ?? [], 'web.stats.n_messages')}
					first={r.posts_per_day?.length ? day(r.posts_per_day[0].date) : ''}
					last={t('web.stats.today')}
				/>
			</div>
			<div class="card">
				<h2 class="card-label mb-3">{t('web.stats.by_hour')}</h2>
				<Bars
					values={r.by_hour ?? []}
					titles={(r.by_hour ?? []).map((n, h) => `${String(h).padStart(2, '0')}:00–${String(h).padStart(2, '0')}:59: ${t('web.stats.n_calls', { COUNT: n })}`)}
					first="00"
					last="23"
					height={56}
				/>
				{#if r.via?.length}
					<p class="mt-3 text-xs text-muted">
						{r.via.map((v) => `${v.name} ${v.count}`).join(' · ')}
					</p>
				{/if}
			</div>
		</div>
	{/if}

	<div class="grid gap-4 md:grid-cols-3">
		<RankList title={t('web.stats.top_areas')} items={r.top_areas} unit={[t('web.stats.unit_msg'), t('web.stats.unit_msgs')]} />
		<RankList title={t('web.stats.top_posters')} items={r.top_posters} unit={[t('web.stats.unit_post'), t('web.stats.unit_posts')]} empty={t('web.stats.no_posts')} />
		{#if full || r.top_doors?.length}
			<RankList title={t('web.stats.top_doors')} items={r.top_doors} unit="×" empty={t('web.stats.no_doors')} />
		{/if}
	</div>

	{#if traffic.length}
		<div class="card">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-x-4">
				<h2 class="card-label">{t('web.stats.echomail')}</h2>
				<span class="flex items-center gap-3 text-xs text-muted">
					<span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-accent"></span>{t('web.stats.received')}</span>
					<span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-accent/40"></span>{t('web.stats.written_here')}</span>
				</span>
			</div>
			<div class="grid gap-x-6 gap-y-5 sm:grid-cols-2 lg:grid-cols-3">
				{#each traffic as n (n.network)}
					<div>
						<div class="mb-1.5 flex items-baseline justify-between text-sm">
							<span class="text-ink-strong">{n.network}</span>
							<span class="font-mono text-xs text-muted">{t('web.stats.in_out', { IN: n.in.toLocaleString(i18n.locale), OUT: n.out.toLocaleString(i18n.locale) })}</span>
						</div>
						<Bars
							values={n.weeks.map((w) => w.in)}
							b={n.weeks.map((w) => w.out)}
							titles={n.weeks.map((w) => t('web.stats.week', { WEEK: day(w.week), IN: w.in, OUT: w.out }))}
							height={44}
						/>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<div class="grid gap-4 md:grid-cols-3">
		{#if full || r.top_files?.length}
			<RankList title={t('web.stats.top_files')} items={r.top_files} unit="×" empty={t('web.stats.no_downloads')} />
		{/if}
		{#if full}
			<RankList
				title={t('web.stats.binkp')}
				items={(r.uplinks ?? []).map((u) => ({ ...u, detail: Number(u.detail) ? t('web.stats.failed', { COUNT: u.detail ?? '' }) : '' }))}
				empty={t('web.stats.no_sessions')}
			/>
			<RankList title={t('web.stats.new_users')} items={r.new_users ?? null} empty={t('web.stats.no_new_users')} />
		{/if}
	</div>
</div>
