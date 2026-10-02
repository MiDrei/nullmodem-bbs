<script lang="ts">
	// The public front page: everything about the board on one page,
	// no login needed -- the welcome screen, every way to call (browser
	// terminal, Telnet/SSH, portal, reader app, QWK), the FTN details
	// for other sysops, and what's going on right now.
	import { onMount } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { getPublicOverview, getPublicStats, getWelcomeScreen, type PublicOverview, type StatsReport, type WelcomeScreen } from '$lib/api';
	import StatsBoard from '$lib/stats/StatsBoard.svelte';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import Icon from '$lib/Icon.svelte';

	let o = $state<PublicOverview | null>(null);
	let welcome = $state<WelcomeScreen | null>(null);
	let report = $state<StatsReport | null>(null);
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
		const t = setInterval(refresh, 60_000);
		return () => clearInterval(t);
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
		if (s < 90) return 'just now';
		if (s < 3600) return `${Math.round(s / 60)} min ago`;
		if (s < 86400 * 1.5) return `${Math.round(s / 3600)} h ago`;
		return `${Math.round(s / 86400)} days ago`;
	}

	const telnet = $derived(o?.telnet_port ? `${host}:${o.telnet_port}` : '');
	const ssh = $derived(o?.ssh_port ? `ssh -p ${o.ssh_port} bbs@${host}` : '');
	const stats = $derived(
		o
			? ([
					['users', 'callers'],
					['calls', 'calls'],
					['messages', 'messages'],
					['message_areas', 'message areas'],
					['files', 'files'],
					['doors', 'doors']
				] as const).filter(([k]) => (o!.stats[k] ?? 0) > 0)
			: []
	);
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{o?.name ?? 'NullModem BBS'}</title>
	<meta name="description" content="{o?.name ?? 'A BBS'} — call by Telnet, SSH or right in the browser." />
</svelte:head>

{#snippet copyable(text: string)}
	<button
		class="group inline-flex max-w-full items-center gap-2 rounded-md border border-line px-2.5 py-1 font-mono text-[13px] text-accent transition hover:border-line-strong"
		title="Copy"
		onclick={() => copy(text)}
	>
		<span class="truncate">{text}</span>
		<span class="shrink-0 text-[10.5px] text-faint group-hover:text-muted">{copied === text ? 'copied ✓' : 'copy'}</span>
	</button>
{/snippet}

<div class="flex min-h-screen flex-col bg-ground text-ink">
	<header class="flex flex-wrap items-center justify-between gap-x-8 gap-y-3 border-b border-line px-6 py-5 md:px-10">
		<span class="text-[15px] font-bold tracking-tight text-ink-strong">{o?.name ?? ''}</span>
		<nav class="flex items-center gap-x-5 text-[13px] text-muted">
			<a href="/terminal" class="flex items-center gap-1.5 transition-colors hover:text-accent"><Icon name="server" />Terminal</a>
			<a href="/reader" class="flex items-center gap-1.5 transition-colors hover:text-accent"><Icon name="qwk" />Reader</a>
			{#if bbsAuth.token}
				<a href="/message-areas" class="btn-primary btn-sm">Open the portal</a>
			{:else}
				<a href="/login" class="btn-primary btn-sm">Log in</a>
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
						{[o.sysop && `Sysop ${o.sysop}`, o.location, o.since_year && `online since ${o.since_year}`]
							.filter(Boolean)
							.join(' · ')}
					</p>
				</div>
				{#if stats.length}
					<div class="flex flex-wrap justify-center gap-x-8 gap-y-3">
						{#each stats as [k, label] (k)}
							<div class="text-center">
								<div class="font-mono text-xl text-ink-strong">{o.stats[k].toLocaleString()}</div>
								<div class="card-label">{label}</div>
							</div>
						{/each}
					</div>
				{/if}
			{/if}
		</section>

		<!-- Every way in -->
		<section>
			<h2 class="card-label mb-3">How to call</h2>
			<div class="grid gap-4 md:grid-cols-2">
				<a href="/terminal" class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="server" />In the browser</div>
					<p class="text-sm text-ink-soft">
						The full BBS — ANSI, menus, doors, chat — in a terminal right here. Nothing to install.
					</p>
					<span class="mt-auto text-sm text-accent">Open the terminal →</span>
				</a>

				<div class="card flex flex-col gap-2">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="binkp" />Telnet & SSH</div>
					<p class="text-sm text-ink-soft">
						The classic way, with SyncTERM, NetRunner, MagiTerm or any terminal. File transfers by Zmodem.
					</p>
					{#if telnet}
						<div class="flex flex-wrap items-center gap-2">
							{@render copyable(telnet)}
							<a href="telnet://{telnet}" class="text-xs text-muted hover:text-accent">telnet://</a>
						</div>
					{/if}
					{#if ssh}
						<div>{@render copyable(ssh)}</div>
						<p class="text-xs text-faint">Any SSH user name and password will do — the BBS's own login follows.</p>
					{/if}
				</div>

				<a href={bbsAuth.token ? '/message-areas' : '/login'} class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="areas" />Web portal</div>
					<p class="text-sm text-ink-soft">
						Message and file areas, netmail, search, polls and QWK packets — with a mouse, in any browser.
					</p>
					<span class="mt-auto text-sm text-accent">{bbsAuth.token ? 'Open the portal →' : 'Log in →'}</span>
				</a>

				<a href="/reader" class="card flex flex-col gap-2 transition hover:border-line-strong">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="netmail" />Reader app</div>
					<p class="text-sm text-ink-soft">
						Echomail and netmail on your phone. Add it to the home screen: reads offline, sends when you're back
						online, and notifies you of new mail.
					</p>
					<span class="mt-auto text-sm text-accent">Open the reader →</span>
				</a>

				<div class="card flex flex-col gap-2 md:col-span-2">
					<div class="flex items-center gap-2 font-semibold text-ink-strong"><Icon name="qwk" />Offline with QWK</div>
					<p class="text-sm text-ink-soft">
						Fetch your mail as a QWK packet and read it offline — with the
						<a href="https://git.maik.ch/nullmodem/reader/releases" class="text-accent hover:underline" target="_blank" rel="noopener">NullModem Reader</a>
						(it calls the board itself) or any QWK reader such as MultiMail. Packets come from the portal (QWK Mail), or by Telnet with
						<span class="font-mono">O</span> / <span class="font-mono">U</span>.
					</p>
				</div>
			</div>
			<p class="mt-3 text-xs text-faint">
				New here? Sign up by Telnet, SSH or in the browser terminal — the sysop then activates your account.
			</p>
		</section>

		<!-- What's going on -->
		{#if o}
			<section class="grid gap-4 md:grid-cols-2">
				<div class="card">
					<h2 class="card-label mb-3">Online now</h2>
					{#if o.online.length === 0}
						<p class="text-sm text-muted">Nobody's on — the line is free.</p>
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
					<h2 class="card-label mt-5 mb-3">Last callers</h2>
					{#if o.callers.length === 0}
						<p class="text-sm text-muted">Be the first.</p>
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
						<h2 class="card-label mb-3">One-liners</h2>
						{#if o.oneliners.length === 0}
							<p class="text-sm text-muted">The wall is empty — leave the first line.</p>
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
							<h2 class="card-label mb-3">Doors</h2>
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
					<h2 class="card-label mb-3">The last 30 days</h2>
					<StatsBoard r={report} />
				</section>
			{/if}

			<!-- For other sysops -->
			{#if o.networks.length || o.binkp_port}
				<section class="card">
					<h2 class="card-label mb-3">For sysops: FidoNet-style networks</h2>
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
						<p class="text-xs text-faint">Want to be a point here or link up? Send the sysop a netmail.</p>
					</div>
				</section>
			{/if}
		{/if}
	</main>

	<footer class="flex justify-between gap-4 border-t border-line px-6 py-4 font-mono text-[10.5px] text-dim md:px-10">
		<span>NullModem BBS{o?.version ? ` v${o.version}` : ''}</span>
		{#if bbsAuth.isSysop}<a href="/admin" class="hover:text-ink">Admin</a>{/if}
	</footer>
</div>
