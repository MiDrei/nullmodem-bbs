<script lang="ts">
	import { t, tn, i18n } from '$lib/i18n.svelte';
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getDashboard, ApiError, type Dashboard, type UplinkStatus } from '$lib/api';

	const REFRESH_MS = 5000;

	let dashboard = $state<Dashboard | null>(null);
	let loadError = $state<string | null>(null);
	let now = $state(Date.now());

	let refreshTimer: ReturnType<typeof setInterval> | undefined;
	let clockTimer: ReturnType<typeof setInterval> | undefined;

	async function load() {
		if (!auth.token) return;
		try {
			dashboard = await getDashboard(auth.token);
			loadError = null;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : t('admin.dashboard.could_not_load_the_dashboard');
		}
	}

	function connectedFor(connectedAt: string): string {
		const seconds = Math.max(0, Math.floor((now - new Date(connectedAt).getTime()) / 1000));
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = seconds % 60;
		if (h > 0) return `${h}h ${m}m`;
		if (m > 0) return `${m}m ${s}s`;
		return `${s}s`;
	}

	// "3 min ago" for an RFC3339 time; "" stays "".
	function ago(iso: string): string {
		if (!iso || iso.startsWith('0001')) return '';
		const sec = Math.max(0, (now - new Date(iso).getTime()) / 1000);
		if (sec < 90) return t('web.time.just_now');
		if (sec < 3600) return t('web.time.minutes', { N: Math.round(sec / 60) });
		if (sec < 86400 * 1.5) return t('web.time.hours', { N: Math.round(sec / 3600) });
		return t('web.time.days_long', { N: Math.round(sec / 86400) });
	}

	function bytes(n: number): string {
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		let i = 0;
		while (n >= 1024 && i < units.length - 1) {
			n /= 1024;
			i++;
		}
		return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
	}

	const hours = (iso: string) => (iso && !iso.startsWith('0001') ? (now - new Date(iso).getTime()) / 3_600_000 : Infinity);

	// An uplink's state: failing (its last session failed), quiet (no
	// session got through in two days though it's polled regularly),
	// paused, ok.
	function uplinkState(u: UplinkStatus): 'error' | 'quiet' | 'paused' | 'ok' {
		if (u.last_error && (!u.last_ok || u.last_error > u.last_ok)) return 'error';
		if (u.hold && u.poll_disabled) return 'paused';
		// A crash-only uplink is only called when there's mail: silence is normal.
		if (hours(u.last_ok) > 48 && !u.poll_disabled) return u.downlink ? 'paused' : 'quiet';
		return 'ok';
	}
	const dot = { error: 'bg-red-500', quiet: 'bg-amber-400', paused: 'bg-slate-500', ok: 'bg-emerald-500' };
	const stateText = $derived({
		error: t('admin.dashboard.state_error'),
		quiet: t('admin.dashboard.state_quiet'),
		paused: t('admin.dashboard.state_paused'),
		ok: t('admin.dashboard.state_ok')
	});

	const nothingToDo = $derived(
		!!dashboard &&
			dashboard.pending_message_area_count === 0 &&
			dashboard.pending_file_area_count === 0 &&
			dashboard.unresolved_netmail_count === 0 &&
			dashboard.pending_user_count === 0 &&
			dashboard.locked_out_count === 0 &&
			dashboard.paging.length === 0 &&
			dashboard.problems.length === 0
	);

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
		refreshTimer = setInterval(load, REFRESH_MS);
		clockTimer = setInterval(() => (now = Date.now()), 1000);
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
		if (clockTimer) clearInterval(clockTimer);
	});
</script>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !dashboard}
	<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
{:else}
	<div class="mb-6">
		<h1 class="page-title">{dashboard.bbs_name}</h1>
		<p class="font-mono text-sm text-slate-500">{dashboard.version}</p>
	</div>

	{#if dashboard.pending_message_area_count > 0 || dashboard.pending_file_area_count > 0 || dashboard.unresolved_netmail_count > 0 || dashboard.pending_user_count > 0 || dashboard.locked_out_count > 0 || dashboard.paging.length > 0 || dashboard.problems.length > 0}
		<section class="mb-8 rounded border border-amber-800/60 bg-amber-950/20 p-4">
			<h2 class="mb-3 text-sm font-semibold tracking-wide text-amber-400 uppercase">
				{t('admin.dashboard.needs_attention')}
			</h2>
			<div class="flex flex-col gap-2 text-sm">
				{#each dashboard.problems as p (p.key)}
					{#if p.key.startsWith('door-update:')}
						<a href="/admin/doors" class="text-cyan-500 hover:text-cyan-300">
							<span class="font-semibold">⬆ {p.title}</span>{#if p.detail}<span class="opacity-70"> -- {p.detail}</span>{/if} →
						</a>
					{:else}
					<div class="text-red-300">
						<span class="font-semibold">⚠ {p.title}</span>{#if p.detail}<span class="text-red-300/70"> -- {p.detail}</span>{/if}
						<span class="text-xs text-faint"> {t('admin.dashboard.since_tolocalestring', { TOLOCALESTRING: new Date(p.since).toLocaleString(i18n.locale) })}</span>
					</div>
					{/if}
				{/each}
				{#each dashboard.paging as room (room)}
					<a href="/admin/chat?room={room}" class="font-semibold text-fuchsia-300 hover:text-fuchsia-200">
						{t('admin.dashboard.v_is_paging_you_answer', { V: room.replace(/^page-/, '') })}
					</a>
				{/each}
				{#if dashboard.pending_user_count > 0}
					<a href="/admin/users" class="text-amber-300 hover:text-amber-200">
						{tn('admin.dashboard.pending_users', dashboard.pending_user_count)}
					</a>
				{/if}
				{#if dashboard.locked_out_count > 0}
					<a href="/admin/security" class="text-amber-300 hover:text-amber-200">
						{tn('admin.dashboard.locked_out', dashboard.locked_out_count)}
					</a>
				{/if}
				{#if dashboard.pending_message_area_count > 0}
					<a href="/admin/pending-areas" class="text-amber-300 hover:text-amber-200">
						{tn('admin.dashboard.pending_msg_areas', dashboard.pending_message_area_count)}
					</a>
				{/if}
				{#if dashboard.pending_file_area_count > 0}
					<a href="/admin/pending-areas" class="text-amber-300 hover:text-amber-200">
						{tn('admin.dashboard.pending_file_areas', dashboard.pending_file_area_count)}
					</a>
				{/if}
				{#if dashboard.unresolved_netmail_count > 0}
					<a href="/admin/netmail" class="text-amber-300 hover:text-amber-200">
						{tn('admin.dashboard.undeliverable', dashboard.unresolved_netmail_count)}
					</a>
				{/if}
			</div>
		</section>
	{:else if nothingToDo}
		<p class="mb-8 text-sm text-emerald-500">✓ {t('admin.dashboard.nothing_to_do')}</p>
	{/if}

	<div class="mb-8 grid grid-cols-2 gap-4 sm:grid-cols-4">
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-cyan-400">{dashboard.nodes.length}</div>
			<div class="card-label">{t('admin.dashboard.nodes_online')}</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.user_count}</div>
			<div class="card-label">{t('admin.common.users')}</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.message_area_count}</div>
			<div class="card-label">{t('common.message_areas')}</div>
		</div>
		<div class="rounded-xl border border-line p-4">
			<div class="text-2xl font-semibold text-slate-100">{dashboard.file_area_count}</div>
			<div class="card-label">{t('common.file_areas')}</div>
		</div>
	</div>

	<section class="mb-8 rounded-xl border border-line p-4">
		<div class="mb-4 flex items-center justify-between">
			<h2 class="card-label">{t('admin.dashboard.binkp')}</h2>
			<a href="/admin/binkp/uplinks" class="text-xs text-cyan-500 hover:text-cyan-300"
				>{t('admin.dashboard.configure')}</a
			>
		</div>
		{#if dashboard.binkp.own_ftn_addresses.length === 0}
			<p class="text-sm text-slate-500">{t('admin.dashboard.no_ftn_address_configured_yet')}</p>
		{:else}
			<p class="mb-4 text-sm text-slate-400">
				{t('admin.dashboard.this_system')} <span class="font-mono text-slate-200"
					>{dashboard.binkp.own_ftn_addresses.join(', ')}</span
				>
			</p>
		{/if}
		<div class="grid grid-cols-2 gap-4 sm:grid-cols-5">
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.uplink_count}</div>
				<div class="card-label">{t('admin.dashboard.uplinks')}</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">
					{dashboard.binkp.crash_only_uplink_count}
				</div>
				<div class="card-label">{t('admin.common.crash_only')}</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.hold_uplink_count}</div>
				<div class="card-label">{t('admin.common.hold')}</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div class="text-lg font-semibold text-ink-strong">{dashboard.binkp.pending_outbound}</div>
				<div class="card-label">{t('admin.dashboard.pending_netmail')}</div>
			</div>
			<div class="rounded-xl border border-line p-3">
				<div
					class="text-xl font-semibold {dashboard.binkp.pending_crash > 0
						? 'text-amber-400'
						: 'text-slate-100'}"
				>
					{dashboard.binkp.pending_crash}
				</div>
				<div class="card-label">{t('admin.dashboard.pending_crash')}</div>
			</div>
		</div>
	</section>

	{#if dashboard.uplinks.length}
		<section class="mb-8 rounded-xl border border-line p-4">
			<div class="mb-4 flex items-center justify-between">
				<h2 class="card-label">{t('admin.dashboard.mailer_per_uplink')}</h2>
				<a href="/admin/logs" class="text-xs text-cyan-500 hover:text-cyan-300">{t('admin.dashboard.to_the_log')}</a>
			</div>
			<div class="overflow-x-auto">
				<table class="w-full text-left text-sm">
					<thead class="card-label">
						<tr class="border-b border-line">
							<th class="py-2 pr-4">{t('admin.dashboard.uplink')}</th>
							<th class="py-2 pr-4">{t('admin.dashboard.last_ok')}</th>
							<th class="py-2 pr-4">{t('admin.dashboard.last_error')}</th>
							<th class="py-2 text-right">{t('admin.dashboard.sessions_24h')}</th>
						</tr>
					</thead>
					<tbody>
						{#each dashboard.uplinks as u (u.address + u.host)}
							{@const st = uplinkState(u)}
							<tr class="border-b border-line align-top">
								<td class="py-2 pr-4">
									<div class="flex items-center gap-2">
										<span class="inline-block h-2 w-2 shrink-0 rounded-full {dot[st]}" title={stateText[st]}></span>
										<span class="font-mono text-ink-strong">{u.address}</span>
										<span class="text-xs text-faint">{u.network}</span>
									</div>
									{#if u.hold || u.poll_disabled}
										<div class="mt-0.5 pl-4 text-[11px] text-faint">
											{[u.hold ? t('admin.common.hold') : '', u.poll_disabled ? t('admin.common.crash_only') : ''].filter(Boolean).join(' · ')}
										</div>
									{/if}
								</td>
								<td class="py-2 pr-4 text-ink-soft">{ago(u.last_ok) || '—'}</td>
								<td class="py-2 pr-4">
									{#if u.last_error}
										<span class={st === 'error' ? 'text-red-400' : 'text-faint'}>{ago(u.last_error)}</span>
										{#if st === 'error' && u.error}
											<div class="max-w-md truncate text-xs text-red-500/80" title={u.error}>{u.error}</div>
										{/if}
									{:else}
										<span class="text-faint">—</span>
									{/if}
								</td>
								<td class="py-2 text-right font-mono">
									{u.sessions_24h}{#if u.errors_24h}<span class="text-red-400"> · {u.errors_24h} ✗</span>{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if dashboard.system}
	{@const sys = dashboard.system}
	<section class="mb-8 rounded-xl border border-line p-4">
		<h2 class="mb-4 card-label">{t('admin.dashboard.system')}</h2>
		<div class="grid gap-4 sm:grid-cols-2 {sys.email ? 'lg:grid-cols-3 xl:grid-cols-5' : 'lg:grid-cols-4'}">
			<a href="/admin/backups" class="rounded-xl border border-line p-3 hover:border-cyan-700">
				<div class="card-label mb-1">{t('admin.dashboard.backup')}</div>
				{#if !sys.backup_enabled}
					<div class="text-sm text-amber-400">{t('admin.dashboard.backup_off')}</div>
				{:else if sys.last_backup}
					<div class="text-sm {hours(sys.last_backup.time) > 36 ? 'text-amber-400' : 'text-ink-strong'}">{ago(sys.last_backup.time)}</div>
					<div class="text-xs text-faint">
						{bytes(sys.last_backup.size)}
						{#if sys.backup_check && sys.backup_check.name === sys.last_backup.name}
							· {#if sys.backup_check.ok}<span class="text-emerald-500">✓ {t('admin.dashboard.backup_checked')}</span>{:else}<span class="text-red-400" title={sys.backup_check.error}>✗ {t('admin.dashboard.backup_check_failed')}</span>{/if}
						{/if}
					</div>
				{:else}
					<div class="text-sm text-amber-400">{t('admin.dashboard.backup_none')}</div>
				{/if}
			</a>
			<a href="/admin/backups" class="rounded-xl border border-line p-3 hover:border-cyan-700">
				<div class="card-label mb-1">{t('admin.dashboard.offsite')}</div>
				{#if !sys.offsite_enabled}
					<div class="text-sm text-faint">{t('admin.dashboard.offsite_off')}</div>
				{:else if sys.offsite.last_error && sys.offsite.last_try > sys.offsite.last_ok}
					<div class="text-sm text-red-400">{t('admin.dashboard.offsite_failed', { WHEN: ago(sys.offsite.last_try) })}</div>
					<div class="truncate text-xs text-red-500/80" title={sys.offsite.last_error}>{sys.offsite.last_error}</div>
				{:else if sys.offsite.last_ok && !sys.offsite.last_ok.startsWith('0001')}
					<div class="text-sm {hours(sys.offsite.last_ok) > 36 ? 'text-amber-400' : 'text-ink-strong'}">{ago(sys.offsite.last_ok)}</div>
					<div class="text-xs text-faint">{tn('admin.dashboard.offsite_copies', sys.offsite.remote)}</div>
				{:else}
					<div class="text-sm text-amber-400">{t('admin.dashboard.offsite_never')}</div>
				{/if}
			</a>
			<div class="rounded-xl border border-line p-3">
				<div class="card-label mb-1">{t('admin.dashboard.space')}</div>
				<div class="text-sm text-ink-strong">{t('admin.dashboard.db_size', { SIZE: bytes(sys.db_bytes) })}</div>
				<div class="text-xs {sys.free_bytes < 2 * 1024 ** 3 ? 'text-amber-400' : 'text-faint'}">{t('admin.dashboard.free_space', { SIZE: bytes(sys.free_bytes) })}</div>
			</div>
			<a href="/admin/services" class="rounded-xl border border-line p-3 hover:border-cyan-700">
				<div class="card-label mb-1">{t('admin.dashboard.services')}</div>
				{#each sys.services as sv (sv.name)}
					<div class="flex items-center gap-2 text-sm">
						<span class="inline-block h-2 w-2 rounded-full {sv.running ? 'bg-emerald-500' : 'bg-red-500'}"></span>
						<span class="text-ink-strong">{sv.name}</span>
						<span class="ml-auto text-xs text-faint">{sv.running ? ago(sv.started_at) : t('admin.dashboard.service_down')}</span>
					</div>
				{/each}
			</a>
			{#if sys.email}
				{@const em = sys.email}
				<a href="/admin/email" class="rounded-xl border border-line p-3 hover:border-cyan-700">
					<div class="card-label mb-1">{t('admin.dashboard.email')}</div>
					{#if em.server}
						<div class="flex items-center gap-2 text-sm" title={em.server.error}>
							<span class="inline-block h-2 w-2 rounded-full {em.server.listening ? 'bg-emerald-500' : 'bg-red-500'}"></span>
							<span class="whitespace-nowrap text-ink-strong">{t('admin.dashboard.mail_server')}</span>
							{#if !em.server.listening}
								<span class="ml-auto text-xs whitespace-nowrap text-faint">{t('admin.dashboard.service_down')}</span>
							{/if}
						</div>
						{#if em.server.listening}
							<div class="pl-4 text-xs text-faint">{t('admin.dashboard.mail_counts', { TAKEN: em.server.taken, REFUSED: em.server.refused })}</div>
						{/if}
					{/if}
					{#if em.webhook}
						<div class="flex items-center gap-2 text-sm">
							<span class="inline-block h-2 w-2 rounded-full bg-emerald-500"></span>
							<span class="text-ink-strong">{t('admin.dashboard.webhook')}</span>
						</div>
					{/if}
					{#if em.mailbox}
						<div class="flex items-center gap-2 text-sm" title={em.fetch_error}>
							<span class="inline-block h-2 w-2 rounded-full {em.fetch_error ? 'bg-red-500' : 'bg-emerald-500'}"></span>
							<span class="whitespace-nowrap text-ink-strong">{t('admin.dashboard.mailbox')}</span>
							<span class="ml-auto text-xs whitespace-nowrap text-faint">{ago(em.last_fetch) || t('admin.email.never')}</span>
						</div>
					{/if}
					<div class="mt-1 text-xs text-faint">
						{t('admin.dashboard.mail_last', { IN: ago(em.last_in) || t('admin.email.never'), OUT: ago(em.last_out) || t('admin.email.never') })}
					</div>
					{#if em.waiting || em.failed}
						<div class="text-xs {em.failed ? 'text-red-400' : 'text-amber-400'}">{t('admin.email.queue', { WAITING: em.waiting, FAILED: em.failed })}</div>
					{/if}
					{#if em.send_error}
						<div class="truncate text-xs text-red-500/80" title={em.send_error}>{em.send_error}</div>
					{/if}
				</a>
			{/if}
		</div>
		{#if sys.warnings.length}
			<div class="mt-4">
				<div class="card-label mb-2">{t('admin.dashboard.recent_warnings')}</div>
				<ul class="flex flex-col gap-1 text-xs">
					{#each sys.warnings as w, i (i)}
						<li class="flex gap-3">
							<span class="shrink-0 text-faint">{ago(w.at)}</span>
							<span class="shrink-0 {w.level === 'error' ? 'text-red-400' : 'text-amber-400'}">{w.source}</span>
							<span class="min-w-0 truncate text-ink-soft" title={w.message}>{w.message}</span>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</section>
	{/if}

	<section class="rounded-xl border border-line p-4">
		<h2 class="mb-4 card-label">{t('common.who_s_online')}</h2>
		{#if dashboard.nodes.length === 0}
			<p class="text-sm text-slate-500">{t('admin.dashboard.no_active_sessions')}</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full text-left text-sm">
					<thead class="card-label">
						<tr class="border-b border-slate-800">
							<th class="py-2 pr-4">{t('common.node')}</th>
							<th class="py-2 pr-4">{t('common.handle')}</th>
							<th class="py-2 pr-4">{t('common.terminal')}</th>
							<th class="py-2 pr-4">{t('admin.dashboard.remote')}</th>
							<th class="py-2">{t('common.connected')}</th>
						</tr>
					</thead>
					<tbody>
						{#each dashboard.nodes as node (node.node)}
							<tr class="border-b border-line">
								<td class="py-2 pr-4 font-mono text-yellow-400">{node.node}</td>
								<td class="py-2 pr-4">{node.username}</td>
								<td class="py-2 pr-4 text-slate-400">{node.term_type}</td>
								<td class="py-2 pr-4 font-mono text-slate-400">{node.remote_ip}</td>
								<td class="py-2 text-slate-400">{connectedFor(node.connected_at)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{/if}
