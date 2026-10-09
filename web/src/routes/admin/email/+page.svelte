<script lang="ts">
	import SLSelect from '$lib/admin/SLSelect.svelte';
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
		newEmailWebhookSecret,
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
			imapOn = !!e.imap.host;
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
			e.receive.server = fresh.receive.server;
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
			const out = $state.snapshot(e) as EmailSettings;
			out.receive.extra_domains = words(extraDomains);
			out.receive.dnsbl = words(blockLists);
			e = await saveEmailSettings(auth.token, out);
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
			await failed(err, t('admin.common.the_test_failed'));
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

	async function newSecret() {
		if (!auth.token || !e) return;
		if (e.receive.webhook_secret && !confirm(t('admin.email.new_secret_confirm'))) return;
		busy = 'secret';
		try {
			const fresh = await newEmailWebhookSecret(auth.token);
			e.receive.webhook_secret = fresh.receive.webhook_secret;
			toast.push(t('admin.email.secret_made'), 'success');
		} catch (err) {
			await failed(err, t('admin.email.could_not_save'));
		} finally {
			busy = null;
		}
	}

	// The lists as the sysop edits them: words separated by spaces or commas.
	let extraDomains = $state('');
	let blockLists = $state('');
	$effect(() => {
		if (e) {
			extraDomains = e.receive.extra_domains.join(' ');
			blockLists = e.receive.dnsbl.join(' ');
		}
	});
	const words = (s: string) => s.split(/[\s,;]+/).map((w) => w.trim()).filter(Boolean);
	let webhookURL = $derived(e?.receive.webhook_secret ? `${location.origin}/api/email/inbound?key=${e.receive.webhook_secret}` : '');
	// Fetching from the IMAP mailbox; turned off, its access is deleted.
	let imapOn = $state(false);
	async function toggleImap(ev: Event) {
		const box = ev.currentTarget as HTMLInputElement;
		if (box.checked || !e) {
			imapOn = box.checked;
			return;
		}
		const saved = !!(e.imap.host || e.imap.user || e.imap.has_password);
		if (saved && !confirm(t('admin.email.imap_off_confirm'))) {
			box.checked = true;
			return;
		}
		imapOn = false;
		e.imap = { host: '', port: 0, security: 'tls', user: '', password: '', has_password: false, folder: '' };
		e.delete_fetched = false;
		if (saved) await save();
	}
	let direct = $derived(!!e && (e.receive.smtp || e.receive.webhook));
	let mxPort = $derived(e?.receive.listen?.split(':').pop() || '2525');

	// The usual port of each kind of server and security.
	function defaultPort(kind: 'imap' | 'smtp', security: MailServer['security']): number {
		if (kind === 'imap') return security === 'tls' ? 993 : 143;
		return security === 'tls' ? 465 : security === 'starttls' ? 587 : 25;
	}

	let example = $derived(e?.example || (e?.domain ? `swissmaik@${e.domain}` : ''));
</script>

{#snippet server(kind: 'imap' | 'smtp', s: MailServer)}
	<div class="rounded-lg border border-line p-3">
		{#if kind === 'imap'}
			<label class="mb-2.5 flex items-center gap-2 text-sm font-medium text-ink-strong">
				<input type="checkbox" class="check" checked={imapOn} onchange={toggleImap} disabled={busy !== null} />
				{t('admin.email.imap_title')}
			</label>
		{:else}
			<div class="mb-2.5 text-sm font-medium text-ink-strong">{t('admin.email.smtp_title')}</div>
		{/if}
		<p class="mb-2.5 text-[11px] leading-relaxed text-faint">
			{kind === 'imap' ? (imapOn ? t('admin.email.imap_hint') : t('admin.email.imap_off')) : t('admin.email.smtp_hint')}
		</p>
		{#if kind === 'smtp' || imapOn}
			<div class="grid gap-2.5 sm:grid-cols-2">
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					{t('admin.email.host')}
					<input class="field field-sm font-mono" bind:value={s.host} placeholder={kind === 'imap' ? 'imap.example.com' : 'smtp.example.com'} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.common.security')}
					<select class="field field-sm" bind:value={s.security}>
						<option value="tls">{t('admin.email.security_tls')}</option>
						<option value="starttls">STARTTLS</option>
						<option value="none">{t('admin.email.security_none')}</option>
					</select>
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.common.port')}
					<input type="number" min="0" class="field field-sm" bind:value={s.port} placeholder={String(defaultPort(kind, s.security))} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('admin.common.user')}
					<input class="field field-sm font-mono" bind:value={s.user} autocomplete="off" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					{t('web.common.password')}
					<input
						type="password"
						class="field field-sm font-mono"
						bind:value={s.password}
						autocomplete="new-password"
						placeholder={s.has_password ? t('admin.common.saved') : ''}
					/>
				</label>
				{#if kind === 'imap'}
					<label class="flex flex-col gap-1 text-xs text-muted">
						{t('admin.email.folder')}
						<input class="field field-sm font-mono" bind:value={s.folder} placeholder="INBOX" />
					</label>
				{/if}
			</div>
		{/if}
	</div>
{/snippet}

<div class="mb-6">
	<h1 class="page-title">{t('common.email_gateway')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">{t('admin.email.subtitle')}</p>
</div>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !e}
	<p class="text-sm text-muted">{t('web.common.loading')}</p>
{:else}
	<div class="flex flex-col gap-4">
		{#if e.enabled}
			<section class="card">
				<h2 class="card-label mb-3">{t('admin.email.status')}</h2>
				<div class="grid gap-2 text-sm sm:grid-cols-2">
					{#if imapOn}
						<div>
							<span class="text-muted">{t('admin.email.last_fetch')}</span>
							<span class="text-ink">{e.status.last_fetch && !e.status.last_fetch.startsWith('0001') ? relativeTime(e.status.last_fetch) : t('admin.email.never')}</span>
						</div>
					{/if}
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
				{#if e.receive.smtp}
					<p class="mt-2 text-xs {e.receive.server.listening ? 'text-emerald-400' : 'text-amber-300'}">
						{#if e.receive.server.listening}
							{t('admin.email.server_listening', { ADDR: e.receive.server.addr, TAKEN: e.receive.server.taken, REFUSED: e.receive.server.refused })}
							{#if e.receive.server.last_from}
								· {t('admin.email.server_last', { FROM: e.receive.server.last_from, WHEN: relativeTime(e.receive.server.last_at) })}
							{/if}
						{:else}
							{t('admin.email.server_down', { ERROR: e.receive.server.error || '—' })}
						{/if}
					</p>
				{/if}
				{#if imapOn && e.status.last_fetch_error}
					<p class="mt-2 text-xs text-amber-300">{t('admin.email.fetch_error', { ERROR: e.status.last_fetch_error })}</p>
				{/if}
				{#if e.status.last_send_error}
					<p class="mt-2 text-xs text-amber-300">{t('admin.email.send_error', { ERROR: e.status.last_send_error })}</p>
				{/if}
				{#if imapOn}
					<div class="mt-3 flex justify-end">
						<button type="button" class="btn-secondary btn-sm" disabled={busy !== null} onclick={fetchNow}>
							{t('admin.email.fetch_now')}
						</button>
					</div>
				{/if}
			</section>
		{/if}

		<form class="card flex flex-col gap-4" onsubmit={save}>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={e.enabled} />
				<span class="text-ink">{t('admin.email.turn_on')}</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.common.domain')}</span>
					<input class="field field-sm font-mono" bind:value={e.domain} placeholder="bbs.example.com" />
					{#if example}
						<span class="text-[11px] text-faint">{t('admin.email.domain_example', { ADDRESS: example })}</span>
					{/if}
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.min_sl')}</span>
					<SLSelect bind:value={e.min_sl} />
					<span class="text-[11px] text-faint">{t('admin.email.min_sl_hint')}</span>
				</label>
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.daily_limit')}</span>
					<input type="number" min="1" class="field field-sm" bind:value={e.daily_limit} />
					<span class="text-[11px] text-faint">{t('admin.email.daily_limit_hint')}</span>
				</label>
			</div>
			<div class="grid gap-3 lg:grid-cols-2 lg:items-start">
				{@render server('imap', e.imap)}
				{@render server('smtp', e.smtp)}
			</div>
			{#if direct && imapOn}
				<p class="-mt-2 text-[11px] text-faint">{t('admin.email.imap_optional')}</p>
			{/if}

			<fieldset class="flex flex-col gap-3 rounded-lg border border-line p-3">
				<legend class="px-1 text-sm font-medium text-ink-strong">{t('admin.email.receive_title')}</legend>
				<label class="flex items-start gap-2 text-sm">
					<input type="checkbox" class="check mt-0.5" bind:checked={e.receive.smtp} />
					<span>
						<span class="text-ink">{t('admin.email.own_server')}</span>
						<span class="block text-xs text-faint">{t('admin.email.own_server_hint', { DOMAIN: e.domain || 'bbs.example.com', PORT: mxPort })}</span>
					</span>
				</label>
				{#if e.receive.smtp}
					<div class="grid gap-3 sm:grid-cols-3">
						<label class="flex flex-col gap-1">
							<span class="text-xs text-muted">{t('admin.email.server_hostname')}</span>
							<input class="field field-sm font-mono" bind:value={e.receive.hostname} placeholder={e.domain || 'mail.example.com'} />
						</label>
						<label class="flex flex-col gap-1">
							<span class="text-xs text-muted">{t('admin.email.server_listen')}</span>
							<input class="field field-sm font-mono" bind:value={e.receive.listen} placeholder=":2525" />
						</label>
						<label class="flex flex-col gap-1">
							<span class="text-xs text-muted">{t('admin.email.block_lists')}</span>
							<input class="field field-sm font-mono" bind:value={blockLists} placeholder="zen.spamhaus.org" />
						</label>
					</div>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={e.receive.greylist} />
						<span class="text-muted">{t('admin.email.greylist')}</span>
					</label>
					<label class="flex items-center gap-2 text-sm">
						<input type="checkbox" class="check" bind:checked={e.receive.spf} />
						<span class="text-muted">{t('admin.email.spf')}</span>
					</label>
				{/if}
				<label class="flex flex-col gap-1">
					<span class="text-xs text-muted">{t('admin.email.extra_domains')}</span>
					<input class="field field-sm font-mono" bind:value={extraDomains} placeholder="mail.example.org" />
					<span class="text-[11px] text-faint">{t('admin.email.extra_domains_hint')}</span>
				</label>
				<label class="flex items-start gap-2 text-sm">
					<input type="checkbox" class="check mt-0.5" bind:checked={e.receive.webhook} disabled={!e.receive.webhook_secret} />
					<span>
						<span class="text-ink">{t('admin.email.webhook')}</span>
						<span class="block text-xs text-faint">{t('admin.email.webhook_hint')}</span>
					</span>
				</label>
				<div class="flex flex-wrap items-center gap-2">
					{#if webhookURL}
						<code class="min-w-0 flex-1 truncate rounded bg-black/20 px-2 py-1 font-mono text-[11px] text-ink-soft" title={webhookURL}>{webhookURL}</code>
					{/if}
					<button type="button" class="btn-secondary btn-xs" disabled={busy !== null} onclick={newSecret}>
						{e.receive.webhook_secret ? t('admin.email.new_secret') : t('admin.email.make_secret')}
					</button>
				</div>
				{#if e.receive.webhook}
					<label class="flex flex-col gap-1">
						<span class="text-xs text-muted">{t('admin.email.signing_key')}</span>
						<div class="flex items-center gap-2">
							<input
								type="password"
								class="field field-sm min-w-0 flex-1 font-mono"
								bind:value={e.receive.signing_key}
								autocomplete="new-password"
								placeholder={e.receive.has_signing_key ? t('admin.common.saved') : ''}
							/>
							{#if e.receive.has_signing_key}
								<button
									type="button"
									class="btn-secondary btn-xs"
									onclick={() => {
										if (!e) return;
										e.receive.clear_signing_key = true;
										e.receive.has_signing_key = false;
										e.receive.signing_key = '';
									}}>{t('admin.email.signing_key_remove')}</button
								>
							{/if}
						</div>
						<span class="text-[11px] text-faint">{t('admin.email.signing_key_hint')}</span>
					</label>
				{/if}
			</fieldset>

			{#if imapOn}
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" class="check" bind:checked={e.delete_fetched} />
					<span class="text-muted">{t('admin.email.delete_fetched')}</span>
				</label>
			{/if}
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
					{busy === 'test' ? t('admin.common.testing') : t('admin.email.test')}
				</button>
				<button type="submit" class="btn-primary btn-sm" disabled={busy !== null}>
					{busy === 'save' ? t('web.common.saving') : t('web.common.save')}
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
											? t('web.common.received')
											: m.status === 'sent'
												? t('web.common.sent')
												: m.status === 'failed'
													? t('web.common.not_delivered')
													: t('admin.common.waiting')}
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
