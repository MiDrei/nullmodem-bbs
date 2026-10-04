<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// The netmail <-> email gateway: callers write to and get mail from
	// handle@domain, through a (catch-all) mailbox fetched over IMAP and
	// an SMTP server to send.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { formatDateTime, relativeTime } from '$lib/datetime';
	import {
		getEmailSettings,
		saveEmailSettings,
		testEmailSettings,
		fetchEmailNow,
		ApiError,
		type EmailSettings,
		type MailServer
	} from '$lib/api';

	let e = $state<EmailSettings | null>(null);
	let loadError = $state<string | null>(null);
	let busy = $state<string | null>(null);
	let testResult = $state<{ ok: boolean; error?: string } | null>(null);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	async function load() {
		if (!auth.token) return;
		try {
			e = await getEmailSettings(auth.token);
			loadError = null;
		} catch (err) {
			loadError = err instanceof ApiError ? err.message : t('admin.email.could_not_load');
		}
	}

	onMount(() => {
		load();
		// The status moves on its own (fetches every minute).
		const timer = setInterval(() => {
			if (!busy) refresh();
		}, 30000);
		return () => clearInterval(timer);
	});

	async function refresh() {
		if (!auth.token || !e) return;
		try {
			const fresh = await getEmailSettings(auth.token);
			e.status = fresh.status;
			e.waiting = fresh.waiting;
			e.failed = fresh.failed;
			e.recent = fresh.recent;
		} catch {
			// Shown again on the next try.
		}
	}

	async function save(ev?: Event) {
		ev?.preventDefault();
		if (!auth.token || !e) return;
		busy = 'save';
		try {
			e = await saveEmailSettings(auth.token, $state.snapshot(e) as EmailSettings);
			toast.push(t('admin.email.saved'), 'success');
		} catch (err) {
			await failed(err, t('admin.email.could_not_save'));
		} finally {
			busy = null;
		}
	}

	async function test() {
		if (!auth.token || !e) return;
		busy = 'test';
		testResult = null;
		try {
			testResult = await testEmailSettings(auth.token, $state.snapshot(e) as EmailSettings);
		} catch (err) {
			await failed(err, t('admin.email.the_test_failed'));
		} finally {
			busy = null;
		}
	}

	async function fetchNow() {
		if (!auth.token) return;
		busy = 'fetch';
		try {
			await fetchEmailNow(auth.token);
			toast.push(t('admin.email.fetching'), 'success');
			setTimeout(refresh, 4000);
		} catch (err) {
			await failed(err, t('admin.email.could_not_save'));
		} finally {
			busy = null;
		}
	}

	// The usual port of each kind of server and security.
	function defaultPort(kind: 'imap' | 'smtp', security: MailServer['security']): number {
		if (kind === 'imap') return security === 'tls' ? 993 : 143;
		return security === 'tls' ? 465 : security === 'starttls' ? 587 : 25;
	}

	let example = $derived(e?.example || (e?.domain ? `swissmaik@${e.domain}` : ''));
</script>

{#snippet server(kind: 'imap' | 'smtp', s: MailServer)}
	<div class="rounded-lg border border-line p-3">
		<div class="mb-2.5 text-sm font-medium text-ink-strong">
			{kind === 'imap' ? t('admin.email.imap_title') : t('admin.email.smtp_title')}
		</div>
		<p class="mb-2.5 text-[11px] leading-relaxed text-faint">
			{kind === 'imap' ? t('admin.email.imap_hint') : t('admin.email.smtp_hint')}
		</p>
		<div class="grid gap-2.5 sm:grid-cols-2">
			<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
				{t('admin.email.host')}
				<input class="field field-sm font-mono" bind:value={s.host} placeholder={kind === 'imap' ? 'imap.example.com' : 'smtp.example.com'} />
			</label>
			<label class="flex flex-col gap-1 text-xs text-muted">
				{t('admin.email.security')}
				<select class="field field-sm" bind:value={s.security}>
					<option value="tls">{t('admin.email.security_tls')}</option>
					<option value="starttls">STARTTLS</option>
					<option value="none">{t('admin.email.security_none')}</option>
				</select>
			</label>
			<label class="flex flex-col gap-1 text-xs text-muted">
				{t('admin.email.port')}
				<input type="number" min="0" class="field field-sm" bind:value={s.port} placeholder={String(defaultPort(kind, s.security))} />
			</label>
			<label class="flex flex-col gap-1 text-xs text-muted">
				{t('admin.email.user')}
				<input class="field field-sm font-mono" bind:value={s.user} autocomplete="off" />
			</label>
			<label class="flex flex-col gap-1 text-xs text-muted">
				{t('admin.email.password')}
				<input
					type="password"
					class="field field-sm font-mono"
					bind:value={s.password}
					autocomplete="new-password"
					placeholder={s.has_password ? t('admin.email.password_kept') : ''}
				/>
			</label>
			{#if kind === 'imap'}
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.email.folder')}
					<input class="field field-sm font-mono" bind:value={s.folder} placeholder="INBOX" />
				</label>
			{/if}
		</div>
	</div>
{/snippet}

<div class="mb-6">
	<h1 class="page-title">{t('admin.nav.email_gateway')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">{t('admin.email.subtitle')}</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !e}
	<p class="text-sm text-muted">{t('admin.common.loading')}</p>
{:else}
	<div class="flex flex-col gap-4">
		{#if e.enabled}
			<section class="card">
				<h2 class="card-label mb-3">{t('admin.email.status')}</h2>
				<div class="grid gap-2 text-sm sm:grid-cols-2">
					<div>
						<span class="text-muted">{t('admin.email.last_fetch')}</span>
						<span class="text-ink">{e.status.last_fetch && !e.status.last_fetch.startsWith('0001') ? relativeTime(e.status.last_fetch) : t('admin.email.never')}</span>
					</div>
					<div>
						<span class="text-muted">{t('admin.email.last_send')}</span>
						<span class="text-ink">{e.status.last_send && !e.status.last_send.startsWith('0001') ? relativeTime(e.status.last_send) : t('admin.email.never')}</span>
					</div>
					<div class="text-muted">
						{t('admin.email.counts', { RECEIVED: e.status.received, SENT: e.status.sent, DROPPED: e.status.dropped })}
					</div>
					<div class={e.waiting || e.failed ? 'text-amber-300' : 'text-muted'}>
						{t('admin.email.queue', { WAITING: e.waiting, FAILED: e.failed })}
					</div>
				</div>
				{#if e.status.last_fetch_error}
					<p class="mt-2 text-xs text-amber-300">{t('admin.email.fetch_error', { ERROR: e.status.last_fetch_error })}</p>
				{/if}
				{#if e.status.last_send_error}
					<p class="mt-2 text-xs text-amber-300">{t('admin.email.send_error', { ERROR: e.status.last_send_error })}</p>
				{/if}
				<div class="mt-3 flex justify-end">
					<button type="button" class="btn-secondary btn-sm" disabled={busy !== null} onclick={fetchNow}>
						{t('admin.email.fetch_now')}
					</button>
				</div>
			</section>
		{/if}

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={e.enabled} />
				<span class="text-ink">{t('admin.email.turn_on')}</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.domain')}</span>
					<input class="field field-sm font-mono" bind:value={e.domain} placeholder="bbs.example.com" />
					{#if example}
						<span class="text-[11px] text-faint">{t('admin.email.domain_example', { ADDRESS: example })}</span>
					{/if}
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.min_sl')}</span>
					<input type="number" min="0" max="255" class="field field-sm" bind:value={e.min_sl} />
					<span class="text-[11px] text-faint">{t('admin.email.min_sl_hint')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.daily_limit')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={e.daily_limit} />
					<span class="text-[11px] text-faint">{t('admin.email.daily_limit_hint')}</span>
				</label>
			</div>
			<div class="grid gap-3 lg:grid-cols-2">
				{@render server('imap', e.imap)}
				{@render server('smtp', e.smtp)}
			</div>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={e.delete_fetched} />
				<span class="text-muted">{t('admin.email.delete_fetched')}</span>
			</label>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={e.deliver_spam} />
				<span class="text-muted">{t('admin.email.deliver_spam')}</span>
			</label>
			{#if testResult}
				<p class="text-xs {testResult.ok ? 'text-emerald-400' : 'text-red-400'}">
					{testResult.ok ? t('admin.email.test_ok') : t('admin.email.test_failed', { ERROR: testResult.error ?? '' })}
				</p>
			{/if}
			<div class="flex flex-wrap justify-end gap-2.5">
				<button type="button" class="btn-secondary btn-sm" disabled={busy !== null} onclick={test}>
					{busy === 'test' ? t('admin.email.testing') : t('admin.email.test')}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={busy !== null}>
					{busy === 'save' ? t('admin.common.saving') : t('admin.common.save')}
				</button>
			</div>
		</form>

		<section class="card">
			<h2 class="card-label mb-3">{t('admin.email.recent')}</h2>
			{#if e.recent.length === 0}
				<p class="text-sm text-muted">{t('admin.email.no_mail_yet')}</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full text-left text-sm">
						<tbody class="divide-y divide-slate-800">
							{#each e.recent as m (m.id)}
								<tr title={m.error || ''}>
									<td class="py-2 pr-2 whitespace-nowrap text-xs text-faint">{formatDateTime(m.at)}</td>
									<td class="py-2 pr-2 whitespace-nowrap text-ink">
										{m.incoming ? `${m.address} → ${m.user}` : `${m.user} → ${m.address}`}
									</td>
									<td class="py-2 pr-2 text-muted">{m.subject}</td>
									<td
										class="py-2 text-right text-xs whitespace-nowrap {m.status === 'failed'
											? 'text-red-400'
											: m.status === 'queued'
												? 'text-amber-300'
												: 'text-faint'}"
									>
										{m.status === 'received'
											? t('admin.email.st_received')
											: m.status === 'sent'
												? t('admin.email.st_sent')
												: m.status === 'failed'
													? t('admin.email.st_failed')
													: t('admin.email.st_queued')}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	</div>
{/if}
