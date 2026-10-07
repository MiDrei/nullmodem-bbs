<script lang="ts">
	// The public front page: everything about the board on one page,
	// no login needed -- the welcome screen, every way to call (browser
	// terminal, Telnet/SSH, portal, reader app, QWK), the FTN details
	// for other sysops, and what's going on right now.
	import { t, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import DoorBulletins from '$lib/DoorBulletins.svelte';
	import NewsList from '$lib/NewsList.svelte';
	import { getPublicNews, type PublicNews, getPublicDoorBulletins, type DoorBulletinView, listPublicFiles, type PublicFile, getPublicFeeds, getPublicOverview, getPublicStats, getWelcomeScreen, type PublicOverview, type StatsReport, type WelcomeScreen } from '$lib/api';
	import StatsBoard from '$lib/stats/StatsBoard.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import Icon from '$lib/Icon.svelte';
	import LanguagePicker from '$lib/LanguagePicker.svelte';

	let o = $state<PublicOverview | null>(null);
	let welcome = $state<WelcomeScreen | null>(null);
	let report = $state<StatsReport | null>(null);
	let files = $state<PublicFile[]>([]);
	let scores = $state<DoorBulletinView[]>([]);
	let news = $state<PublicNews[]>([]);
	let feeds = $state<{ tag: string; name: string; network: string; url: string }[]>([]);
	let copied = $state('');
	const host = typeof location === 'undefined' ? '' : location.hostname;

	async function refresh() {
		try {
			o = await getPublicOverview();
		} catch {
			// Keep what's shown; the next refresh may work.
		}
	}

	onMount(() => {
		refresh();
		getWelcomeScreen()
			.then((w) => (welcome = w))
			.catch(() => {});
		getPublicStats()
			.then((r) => (report = r))
			.catch(() => {});
		getPublicFeeds()
			.then((f) => (feeds = f))
			.catch(() => {});
		listPublicFiles(6)
			.then((f) => (files = f))
			.catch(() => {});
		getPublicDoorBulletins()
			.then((b) => (scores = b))
			.catch(() => {});
		getPublicNews()
			.then((n) => (news = n))
			.catch(() => {});
		const timer = setInterval(refresh, 60_000);
		return () => clearInterval(timer);
	});

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = text;
			setTimeout(() => copied === text && (copied = ''), 1500);
		} catch {
			// No clipboard (plain http): the text is selectable anyway.
		}
	}

	function ago(iso: string): string {
		const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
		if (s < 90) return t('web.time.just_now');
		if (s < 3600) return t('web.time.minutes', { N: Math.round(s / 60) });
		if (s < 86400 * 1.5) return t('web.time.hours', { N: Math.round(s / 3600) });
		return t('web.time.days_long', { N: Math.round(s / 86400) });
	}

	const telnet = $derived(o?.telnet_port ? `${host}:${o.telnet_port}` : '');
	const ssh = $derived(o?.ssh_port ? `ssh -p ${o.ssh_port} bbs@${host}` : '');
	const stats = $derived(
		o
			? ([
					['users', 'web.home.stat_users'],
					['calls', 'web.home.stat_calls'],
					['messages', 'web.home.stat_messages'],
					['message_areas', 'web.home.stat_areas'],
					['files', 'web.home.stat_files'],
					['doors', 'web.home.stat_doors']
				] as const).filter(([k]) => (o!.stats[k] ?? 0) > 0)
			: []
	);
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{o?.name ?? 'NullModem BBS'}</title>
	<meta name="description" content={t('web.home.meta', { BBSNAME: o?.name ?? 'BBS' })} />
</svelte:head>

{#snippet copyable(text: string)}
	<button
		class="group inline-flex max-w-full items-center gap-2 rounded-md border border-line px-2.5 py-1 font-mono text-[13px] text-accent transition hover:border-line-strong"
		title={t('web.common.copy')}
		onclick={() => copy(text)}
	>
		<span class="truncate">{text}</span>
		<span class="shrink-0 text-[10.5px] text-faint group-hover:text-muted">{copied === text ? t('web.home.copied') : t('web.home.copy_short')}</span>
	</button>
{/snippet}

<div class="flex min-h-screen flex-col bg-ground text-ink">
	<header class="flex flex-wrap items-center justify-between gap-x-8 gap-y-3 border-b border-line px-6 py-5 md:px-10">
		<span class="text-[15px] font-bold tracking-tight text-ink-strong">{o?.name ?? ''}</span>
		<nav class="flex items-center gap-x-5 text-[13px] text-muted">
			<a href="/terminal" class="flex items-center gap-1.5 transition-colors hover:text-accent"><Icon name="server" />{t('common.terminal')}</a>
			<a href="/reader" class="flex items-center gap-1.5 transition-colors hover:text-accent"><Icon name="qwk" />{t('web.home.reader')}</a>
			<LanguagePicker />
			{#if bbsAuth.token}
				<a href="/message-areas" class="btn-primary btn-sm">{t('web.home.open_portal')}</a>
			{:else}
				<a href="/login" class="btn-primary btn-sm">{t('web.home.login')}</a>
			{/if}
		</nav>
	</header>

	<main class="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-10 px-6 py-8 md:px-10">
		<!-- Who we are -->
		<section class="flex flex-col items-center gap-6">
			{#if welcome}
				<div class="ansi-panel w-[min(calc(100vw-2rem),64rem)] max-w-none">
					{#if welcome.preformatted && welcome.grid}
						<AnsiArt grid={welcome.grid} fit maxZoom={1.6} />
					{:else}
						<div class="inline-block font-mono text-sm leading-tight whitespace-pre">{@html welcome.html}</div>
					{/if}
				</div>
			{/if}
			{#if o}
				<div class="text-center">
					<h1 class="text-3xl font-semibold tracking-tight text-ink-strong">{o.name}</h1>
					<p class="mt-1.5 text-[13.5px] text-muted">
						{[o.sysop && t('web.bbslist.sysop_is', { NAME: o.sysop }), o.location, o.since_year && t('web.home.since', { YEAR: o.since_year })]
							.filter(Boolean)
							.join(' · ')}
					</p>
				</div>
				{#if stats.length}
					<div class="flex flex-wrap justify-center gap-x-8 gap-y-3">
						{#each stats as [k, label] (k)}
							<div class="text-center">
								<div class="font-mono text-xl text-ink-strong">{o.stats[k].toLocaleString(i18n.locale)}</div>
								<div class="card-label">{t(label)}</div>
							</div>
						{/each}
					</div>
				{/if}
			{/if}
		</section>

		<!-- Every way in -->
		<section>
			<h2 class="card-label mb-3">{t('web.home.how')}</h2>
			<div class="grid gap-4 md:grid-cols-2">
				<a href="/terminal" class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="server" />{t('web.home.browser')}</div>
					<p class="text-sm text-ink-soft">
						{t('web.home.browser_text')}
					</p>
					<span class="mt-auto text-sm text-accent">{t('web.home.open_terminal')} →</span>
				</a>

				<div class="card flex flex-col gap-2">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="binkp" />Telnet & SSH</div>
					<p class="text-sm text-ink-soft">
						{t('web.home.telnet_text')}
					</p>
					{#if telnet}
						<div class="flex flex-wrap items-center gap-2">
							{@render copyable(telnet)}
							<a href="telnet://{telnet}" class="text-xs text-muted hover:text-accent">telnet://</a>
						</div>
					{/if}
					{#if ssh}
						<div>{@render copyable(ssh)}</div>
						<p class="text-xs text-faint">{t('web.home.ssh_hint')}</p>
					{/if}
				</div>

				<a href={bbsAuth.token ? '/message-areas' : '/login'} class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="areas" />{t('web.terminal.portal')}</div>
					<p class="text-sm text-ink-soft">
						{t('web.home.portal_text')}
					</p>
					<span class="mt-auto text-sm text-accent">{bbsAuth.token ? t('web.home.open_portal') : t('web.home.login')} →</span>
				</a>

				<a href="/reader" class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="netmail" />{t('web.common.reader_app')}</div>
					<p class="text-sm text-ink-soft">
						{t('web.home.reader_text')}
					</p>
					<span class="mt-auto text-sm text-accent">{t('web.home.open_reader')} →</span>
				</a>

				<div class="card flex flex-col gap-2 md:col-span-2">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="qwk" />{t('web.home.qwk')}</div>
					<p class="text-sm text-ink-soft">
						{t('web.home.qwk_text1')}
						<a href="https://github.com/midrei/nullmodem-reader/releases" class="text-accent hover:underline" target="_blank" rel="noopener">NullModem Reader</a>
						{t('web.home.qwk_text2')}
						<span class="font-mono">O</span> / <span class="font-mono">U</span>.
					</p>
				</div>
			</div>
			<p class="mt-3 text-xs text-faint">
				{t('web.home.new_here')}
			</p>
		</section>

		{#if news.length}
			<section class="card">
				<h2 class="card-label mb-3">{t('web.news.title')}</h2>
				<NewsList list={news} />
			</section>
		{/if}

		<!-- What's going on -->
		{#if o}
			<section class="grid gap-4 md:grid-cols-2">
				<div class="card">
					<h2 class="card-label mb-3">{t('web.home.online')}</h2>
					{#if o.online.length === 0}
						<p class="text-sm text-muted">{t('web.home.nobody')}</p>
					{:else}
						<ul class="flex flex-col gap-1.5 text-sm">
							{#each o.online as n (n.node)}
								<li class="flex justify-between gap-3">
									<span><span class="font-mono text-faint">#{n.node}</span> <span class="text-ink-strong">{n.handle}</span></span>
									<span class="text-xs text-faint">{ago(n.since)}</span>
								</li>
							{/each}
						</ul>
					{/if}
					<h2 class="card-label mt-5 mb-3">{t('web.home.last_callers')}</h2>
					{#if o.callers.length === 0}
						<p class="text-sm text-muted">{t('web.home.be_first')}</p>
					{:else}
						<ul class="flex flex-col gap-1.5 text-sm">
							{#each o.callers as c (c.handle)}
								<li class="flex justify-between gap-3">
									<span class="min-w-0 truncate"><span class="text-ink-strong">{c.handle}</span>{#if c.place}<span class="text-faint"> · {c.place}</span>{/if}</span>
									<span class="shrink-0 text-xs text-faint">{ago(c.at)}</span>
								</li>
							{/each}
						</ul>
					{/if}
				</div>

				<div class="flex flex-col gap-4">
					<div class="card">
						<h2 class="card-label mb-3">{t('common.one_liners')}</h2>
						{#if o.oneliners.length === 0}
							<p class="text-sm text-muted">{t('web.home.wall_empty')}</p>
						{:else}
							<ul class="flex flex-col gap-2 text-sm">
								{#each o.oneliners as l, i (i)}
									<li class="min-w-0 break-words">
										<span class="text-accent">{l.handle}:</span> <span class="text-ink-soft">{l.text}</span>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
					{#if o.doors.length}
						<div class="card">
							<h2 class="card-label mb-3">{t('common.doors')}</h2>
							<div class="flex flex-wrap gap-1.5">
								{#each o.doors as d (d)}
									<span class="rounded-md border border-line px-2 py-0.5 text-xs text-ink-soft">{d}</span>
								{/each}
							</div>
						</div>
					{/if}
				</div>
			</section>

			{#if report}
				<section>
					<h2 class="card-label mb-3">{t('web.home.last_30')}</h2>
					<StatsBoard r={report} />
				</section>
			{/if}

			{#if scores.length}
				<section class="card">
					<h2 class="card-label mb-3">{t('web.home.scores')}</h2>
					<DoorBulletins list={scores} />
				</section>
			{/if}

			{#if files.length}
				<section class="card">
					<h2 class="card-label mb-3">{t('web.home.files')}</h2>
					<ul class="flex flex-col divide-y divide-line">
						{#each files as f (f.id)}
							<li>
								<a href={f.page} class="flex items-baseline gap-3 py-2 text-sm hover:text-accent">
									<span class="shrink-0 font-mono text-ink-strong">{f.filename}</span>
									<span class="min-w-0 flex-1 truncate text-ink-soft">{f.summary}</span>
									<span class="shrink-0 text-xs text-faint">{f.size}</span>
								</a>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if feeds.length}
				<section class="card">
					<h2 class="card-label mb-1">{t('web.home.rss')}</h2>
					<p class="mb-3 text-xs text-faint">{t('web.home.rss_text')}</p>
					<div class="flex flex-wrap gap-1.5">
						{#each feeds as f (f.tag)}
							<a href={f.url} class="rounded-md border border-line px-2 py-0.5 text-xs text-ink-soft transition hover:border-accent hover:text-accent" title={f.network || t('web.areas.local')}>
								{f.name}
							</a>
						{/each}
					</div>
				</section>
			{/if}

			<!-- For other sysops -->
			{#if o.networks.length || o.binkp_port}
				<section class="card">
					<h2 class="card-label mb-3">{t('web.home.sysops')}</h2>
					<div class="flex flex-col gap-3 text-sm">
						{#if o.binkp_port}
							<div class="flex flex-wrap items-center gap-2">
								<span class="text-muted">BinkP</span>
								{@render copyable(`${host}:${o.binkp_port}`)}
							</div>
						{/if}
						<div class="flex flex-wrap gap-x-8 gap-y-2">
							{#each o.networks as n (n.name)}
								<div>
									<span class="text-ink-strong">{n.name}</span>
									<span class="ml-1.5 font-mono text-accent">{n.addresses.join(', ')}</span>
								</div>
							{/each}
						</div>
						<p class="text-xs text-faint">{t('web.home.sysops_text')}</p>
					</div>
				</section>
			{/if}
		{/if}
	</main>

	<footer class="flex justify-between gap-4 border-t border-line px-6 py-4 font-mono text-[10.5px] text-dim md:px-10">
		<span title={o?.build ?? ''}>NullModem BBS{o?.version ? ` v${o.version}` : ''}{o?.build ? ` · ${o.build}` : ''}</span>
		{#if bbsAuth.isSysop}<a href="/admin" class="hover:text-ink">{t('web.common.admin')}</a>{/if}
	</footer>
</div>
