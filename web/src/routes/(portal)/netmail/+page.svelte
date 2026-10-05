<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	import { relativeTime } from '$lib/datetime';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bbsAuth } from '$lib/bbs-auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listBBSNetmail,
		listBBSNetmailSent,
		sendBBSNetmail,
		getBBSProfile,
		isFTNAddress,
		ApiError,
		type BBSNetmailSummary
	} from '$lib/api';

	let tab = $state<'inbox' | 'sent'>('inbox');
	let inbox = $state<BBSNetmailSummary[]>([]);
	let sent = $state<BBSNetmailSummary[]>([]);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let composing = $state(false);
	let composeTo = $state('');
	let composeToName = $state('');
	let composeSubject = $state('');
	let composeBody = $state('');
	let composeCrash = $state(false);
	let sending = $state(false);
	// The caller's own email address, when they may write email.
	let myEmail = $state('');

	// A local username needs no extra recipient name (it's unambiguous
	// on its own); an FTN address does, since a node has many possible
	// recipients -- mirrors the Telnet/SSH composer's "Recipient name"
	// prompt, which only appears for an FTN address too.
	let composeToIsFTN = $derived(isFTNAddress(composeTo.trim()));

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			bbsAuth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function load() {
		if (!bbsAuth.token) return;
		try {
			[inbox, sent] = await Promise.all([
				listBBSNetmail(bbsAuth.token),
				listBBSNetmailSent(bbsAuth.token)
			]);
			getBBSProfile(bbsAuth.token)
				.then((p) => (myEmail = p.email ?? ''))
				.catch(() => {});
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : t('web.netmail.load_failed');
		} finally {
			loaded = true;
		}
	}

	onMount(async () => {
		if (!bbsAuth.token) {
			await goto('/login');
			return;
		}
		await load();
	});

	async function send() {
		if (!bbsAuth.token) return;
		sending = true;
		try {
			await sendBBSNetmail(
				bbsAuth.token,
				composeTo,
				composeSubject,
				composeBody,
				composeToName,
				composeCrash
			);
			composing = false;
			composeTo = '';
			composeToName = '';
			composeSubject = '';
			composeBody = '';
			composeCrash = false;
			toast.push(t('common.netmail_sent'), 'success');
			await load();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : t('web.netmail.send_failed'), 'error');
		} finally {
			sending = false;
		}
	}
</script>

<div class="mb-5 flex items-center justify-between gap-4">
	<div>
		<h1 class="page-title">{t('common.netmail')}</h1>
		<p class="page-subtitle">{t('web.netmail.subtitle')}</p>
		{#if myEmail}
			<p class="mt-1 text-[12.5px] text-muted">
				{t('web.netmail.your_email')} <span class="font-mono text-slate-400">{myEmail}</span>
			</p>
		{/if}
	</div>
	{#if !composing}
		<button class="btn-primary" onclick={() => (composing = true)}>+ {t('web.common.new_netmail')}</button>
	{/if}
</div>

{#if composing}
	<form
		class="mb-7 flex flex-col gap-3.5"
		onsubmit={(e) => {
			e.preventDefault();
			send();
		}}
	>
		<label class="flex flex-col gap-2">
			<span class="card-label">{myEmail ? t('web.netmail.to_email') : t('web.netmail.to')}</span>
			<input class="field" bind:value={composeTo} />
		</label>
		{#if composeToIsFTN}
			<label class="flex flex-col gap-2">
				<span class="card-label">{t('web.netmail.recipient')}</span>
				<input class="field" placeholder={composeTo} bind:value={composeToName} />
			</label>
			<label class="flex items-center gap-2 text-[13px] text-muted">
				<input type="checkbox" class="accent-accent" bind:checked={composeCrash} />
				{t('web.netmail.crash')}
			</label>
		{/if}
		<label class="flex flex-col gap-2">
			<span class="card-label">{t('common.subject')}</span>
			<input class="field" bind:value={composeSubject} />
		</label>
		<label class="flex flex-col gap-2">
			<span class="card-label">{t('web.msg.message')}</span>
			<textarea
				class="body-panel h-64 resize-y outline-none focus:border-accent"
				bind:value={composeBody}
				placeholder={t('web.msg.body_placeholder')}
			></textarea>
		</label>
		<div class="flex justify-end gap-2.5">
			<button type="button" class="btn-secondary" onclick={() => (composing = false)}>{t('web.common.cancel')}</button>
			<button
				type="submit"
				class="btn-primary"
				disabled={sending || !composeTo || !composeSubject || !composeBody}
			>
				{sending ? t('web.netmail.sending') : t('web.netmail.send')}
			</button>
		</div>
	</form>
{/if}

<div class="mb-1 flex gap-6 border-b border-line" role="tablist">
	<button
		role="tab"
		aria-selected={tab === 'inbox'}
		class="tab {tab === 'inbox' ? 'tab-active' : ''}"
		onclick={() => (tab = 'inbox')}
	>
		{t('web.common.inbox')}{inbox.some((m) => m.unread) ? ` · ${t('common.count_new', { COUNT: inbox.filter((m) => m.unread).length })}` : ''}
	</button>
	<button
		role="tab"
		aria-selected={tab === 'sent'}
		class="tab {tab === 'sent' ? 'tab-active' : ''}"
		onclick={() => (tab = 'sent')}
	>
		{t('web.netmail.sent')}
	</button>
</div>

{#if loadError}
	<p class="mt-4 text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="mt-4 text-sm text-muted">{t('web.common.loading')}</p>
{:else if (tab === 'inbox' ? inbox : sent).length === 0}
	<p class="mt-4 text-sm text-muted">
		{tab === 'inbox' ? t('web.netmail.none') : t('web.netmail.none_sent')}
	</p>
{:else}
	<div class="flex flex-col">
		{#each tab === 'inbox' ? inbox : sent as m, i (m.id)}
			{@const unread = tab === 'inbox' && m.unread}
			<a href="/netmail/{m.id}" class="list-row group {unread ? 'list-row-unread' : ''}">
				<span class="list-num">{String(i + 1).padStart(2, '0')}</span>
				<div class="min-w-0 flex-1">
					<div
						class="truncate text-[13.5px] {unread
							? 'font-semibold text-white'
							: 'font-medium text-slate-100'} group-hover:text-accent"
					>
						{m.subject}
					</div>
					<div class="mt-0.5 truncate text-xs text-faint">
						{tab === 'inbox' ? m.from_name : t('web.netmail.to_name', { NAME: m.to_name })}
					</div>
				</div>
				{#if unread}
					<span class="badge-new">{t('common.new')}</span>
				{/if}
				<span class="list-meta w-16 shrink-0 text-right">{relativeTime(m.posted_at)}</span>
			</a>
		{/each}
	</div>
{/if}
