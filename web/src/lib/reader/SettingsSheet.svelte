<script lang="ts">
	// The reader's settings on this device: notifications, reading
	// offline, logging out.
	import { t, tn, i18n } from '$lib/i18n.svelte';
	import LanguagePicker from '$lib/LanguagePicker.svelte';
	import { onMount } from 'svelte';
	import { getPushSubscription, testPush, ApiError } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import { readerToken, errorText } from '$lib/reader/session';
	import { offline, syncAhead } from '$lib/reader/offline.svelte';
	import { currentSubscription, disablePush, enablePush, pushUnavailable } from '$lib/reader/push';

	let { onClose, onLogout }: { onClose: () => void; onLogout: () => void } = $props();

	const unavailable = pushUnavailable();
	let enabled = $state(false);
	let netmail = $state(true);
	let echomail = $state(true);
	let busy = $state(false);
	let loaded = $state(false);

	onMount(async () => {
		const token = await readerToken();
		const sub = await currentSubscription().catch(() => null);
		if (token && sub) {
			try {
				const p = await getPushSubscription(token, sub.endpoint);
				enabled = true;
				netmail = p.netmail;
				echomail = p.echomail;
			} catch {
				// Subscribed in the browser but not on the BBS (e.g. as
				// someone else): off until turned on again.
				enabled = false;
			}
		}
		loaded = true;
	});

	async function apply(on: boolean) {
		const token = await readerToken();
		if (!token) return;
		busy = true;
		try {
			if (on) {
				await enablePush(token, { netmail, echomail });
			} else {
				await disablePush(token);
			}
			enabled = on;
		} catch (err) {
			toast.push(err instanceof Error && !(err instanceof ApiError) ? err.message : errorText(err, t('web.reader.push_failed')), 'error');
		} finally {
			busy = false;
		}
	}

	async function test() {
		const token = await readerToken();
		const sub = await currentSubscription();
		if (!token || !sub) return;
		busy = true;
		try {
			await testPush(token, sub.endpoint);
			toast.push(t('web.reader.test_sent'), 'success');
		} catch (err) {
			toast.push(errorText(err, t('web.reader.test_failed')), 'error');
		} finally {
			busy = false;
		}
	}

	function since(ms: number): string {
		if (!ms) return t('common.never');
		const min = Math.round((Date.now() - ms) / 60000);
		if (min < 1) return t('web.time.just_now');
		if (min < 60) return t('web.time.minutes', { N: min });
		return new Date(ms).toLocaleString(i18n.locale, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
	}
</script>

<div class="fixed inset-0 z-20 flex flex-col bg-black" style="padding-top: env(safe-area-inset-top)">
	<header class="r-bar">
		<span class="r-title">{t('web.common.settings')}</span>
		<button class="r-btn text-base font-semibold" onclick={onClose}>{t('web.reader.done')}</button>
	</header>
	<div class="flex-1 overflow-y-auto pb-8">
		<div class="r-section">{t('web.reader.notifications')}</div>
		{#if unavailable}
			<p class="px-4 py-2 text-sm text-muted">{unavailable}</p>
		{:else if loaded}
			<label class="r-row">
				<span class="flex-1 text-ink-strong">{t('web.reader.this_device')}</span>
				<input type="checkbox" class="check" checked={enabled} disabled={busy} onchange={(e) => apply(e.currentTarget.checked)} />
			</label>
			<label class="r-row">
				<span class="flex-1 {enabled ? 'text-ink' : 'text-faint'}">{t('web.common.new_netmail')}</span>
				<input type="checkbox" class="check" bind:checked={netmail} disabled={busy || !enabled} onchange={() => apply(true)} />
			</label>
			<label class="r-row">
				<span class="min-w-0 flex-1 {enabled ? 'text-ink' : 'text-faint'}">
					{t('web.reader.echomail_to_me')}
					<span class="block text-xs text-faint">{t('web.reader.echomail_hint')}</span>
				</span>
				<input type="checkbox" class="check" bind:checked={echomail} disabled={busy || !enabled} onchange={() => apply(true)} />
			</label>
			{#if enabled}
				<button class="r-row text-accent" disabled={busy} onclick={test}>{t('web.reader.send_test')}</button>
			{/if}
		{/if}

		<div class="r-section">{t('web.reader.offline')}</div>
		<p class="px-4 py-2 text-sm text-muted">
			{t('web.reader.offline_hint')}
		</p>
		<div class="r-row">
			<span class="min-w-0 flex-1">
				<span class="block text-ink">{t('web.reader.last_fetched', { WHEN: since(offline.syncedAt) })}</span>
				{#if offline.syncedAt && offline.fetched}<span class="block text-xs text-faint">{tn('web.reader.kept', offline.fetched)}</span>{/if}
				{#if offline.outbox}<span class="block text-xs text-amber-400">{t('web.reader.waiting', { COUNT: offline.outbox })}</span>{/if}
			</span>
			<button class="r-btn text-base" disabled={offline.syncing || !offline.online} onclick={() => syncAhead(true)}>
				{offline.syncing ? t('web.reader.fetching') : t('web.reader.fetch_now')}
			</button>
		</div>

		<div class="r-section">{t('common.language')}</div>
		<label class="r-row">
			<span class="flex-1 text-ink">{t('web.reader.language_hint')}</span>
			<LanguagePicker class="text-accent" />
		</label>

		<div class="r-section">{t('web.profile.account')}</div>
		<button class="r-row text-red-400" onclick={onLogout}>{t('web.common.log_out')}</button>
	</div>
</div>
