<script lang="ts">
	// The statistics: the public part on the front page, all of it
	// (full) in the admin.
	import type { StatsReport, StatsDay } from '$lib/api';
	import Bars from './Bars.svelte';
	import RankList from './RankList.svelte';

	let { r, full = false }: { r: StatsReport; full?: boolean } = $props();

	const day = (d: string) => new Date(d + 'T12:00:00').toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
	const dayTitles = (ds: StatsDay[], what: string) => ds.map((d) => `${day(d.date)}: ${d.count} ${what}`);
	const avg = $derived(r.calls_per_day.length ? r.calls / r.calls_per_day.length : 0);
	const traffic = $derived((r.networks ?? []).filter((n) => n.in + n.out > 0));
</script>

<div class="flex flex-col gap-4">
	<div class="grid gap-4 md:grid-cols-3">
		<div class="card md:col-span-2">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-x-4">
				<h2 class="card-label">Calls per day</h2>
				<span class="text-xs text-muted">
					{r.calls.toLocaleString()} calls by {r.callers} caller{r.callers === 1 ? '' : 's'} · ⌀ {avg.toFixed(1)} a day
				</span>
			</div>
			<Bars
				values={r.calls_per_day.map((d) => d.count)}
				titles={dayTitles(r.calls_per_day, 'calls')}
				first={r.calls_per_day.length ? day(r.calls_per_day[0].date) : ''}
				last="today"
			/>
		</div>
		<RankList title="Most frequent callers" items={r.top_callers} detail={full} />
	</div>

	{#if full}
		<div class="grid gap-4 md:grid-cols-3">
			<div class="card md:col-span-2">
				<div class="mb-3 flex items-baseline justify-between">
					<h2 class="card-label">Messages per day (all areas)</h2>
				</div>
				<Bars
					values={(r.posts_per_day ?? []).map((d) => d.count)}
					titles={dayTitles(r.posts_per_day ?? [], 'messages')}
					first={r.posts_per_day?.length ? day(r.posts_per_day[0].date) : ''}
					last="today"
				/>
			</div>
			<div class="card">
				<h2 class="card-label mb-3">Calls by hour</h2>
				<Bars
					values={r.by_hour ?? []}
					titles={(r.by_hour ?? []).map((n, h) => `${String(h).padStart(2, '0')}:00–${String(h).padStart(2, '0')}:59: ${n} calls`)}
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
		<RankList title="Busiest areas" items={r.top_areas} unit={[' msg', ' msgs']} />
		<RankList title="Most active writers" items={r.top_posters} unit={[' post', ' posts']} empty="No posts written here yet." />
		{#if full || r.top_doors?.length}
			<RankList title="Most played doors" items={r.top_doors} unit="×" empty="No doors played yet." />
		{/if}
	</div>

	{#if traffic.length}
		<div class="card">
			<div class="mb-3 flex flex-wrap items-baseline justify-between gap-x-4">
				<h2 class="card-label">Echomail per network · last 12 weeks</h2>
				<span class="flex items-center gap-3 text-xs text-muted">
					<span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-accent"></span>received</span>
					<span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-accent/40"></span>written here</span>
				</span>
			</div>
			<div class="grid gap-x-6 gap-y-5 sm:grid-cols-2 lg:grid-cols-3">
				{#each traffic as n (n.network)}
					<div>
						<div class="mb-1.5 flex items-baseline justify-between text-sm">
							<span class="text-ink-strong">{n.network}</span>
							<span class="font-mono text-xs text-muted">{n.in.toLocaleString()} in · {n.out.toLocaleString()} out</span>
						</div>
						<Bars
							values={n.weeks.map((w) => w.in)}
							b={n.weeks.map((w) => w.out)}
							titles={n.weeks.map((w) => `week of ${day(w.week)}: ${w.in} received, ${w.out} written here`)}
							height={44}
						/>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<div class="grid gap-4 md:grid-cols-3">
		{#if full || r.top_files?.length}
			<RankList title="Most downloaded files" items={r.top_files} unit="×" empty="No downloads yet." />
		{/if}
		{#if full}
			<RankList
				title="BinkP sessions per system"
				items={(r.uplinks ?? []).map((u) => ({ ...u, detail: Number(u.detail) ? `${u.detail} failed` : '' }))}
				empty="No sessions."
			/>
			<RankList title="New accounts per month" items={r.new_users ?? null} empty="No new accounts." />
		{/if}
	</div>
</div>
