<script lang="ts">
	// The reader's settings on this device: notifications, reading
	// offline, logging out.
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
			toast.push(err instanceof Error && !(err instanceof ApiError) ? err.message : errorText(err, 'Could not change the notifications.'), 'error');
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
			toast.push('Test sent -- it should show up in a moment.', 'success');
		} catch (err) {
			toast.push(errorText(err, 'Could not send the test.'), 'error');
		} finally {
			busy = false;
		}
	}

	function since(ms: number): string {
		if (!ms) return 'never';
		const min = Math.round((Date.now() - ms) / 60000);
		if (min < 1) return 'just now';
		if (min < 60) return `${min} min ago`;
		return new Date(ms).toLocaleString([], { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
	}
</script>

<div class="fixed inset-0 z-20 flex flex-col bg-black" style="padding-top: env(safe-area-inset-top)">
	<header class="r-bar">
		<span class="r-title">Settings</span>
		<button class="r-btn text-base font-semibold" onclick={onClose}>Done</button>
	</header>
	<div class="flex-1 overflow-y-auto pb-8">
		<div class="r-section">Notifications</div>
		{#if unavailable}
			<p class="px-4 py-2 text-sm text-muted">{unavailable}</p>
		{:else if loaded}
			<label class="r-row">
				<span class="flex-1 text-ink-strong">On this device</span>
				<input type="checkbox" class="check" checked={enabled} disabled={busy} onchange={(e) => apply(e.currentTarget.checked)} />
			</label>
			<label class="r-row">
				<span class="flex-1 {enabled ? 'text-ink' : 'text-faint'}">New netmail</span>
				<input type="checkbox" class="check" bind:checked={netmail} disabled={busy || !enabled} onchange={() => apply(true)} />
			</label>
			<label class="r-row">
				<span class="min-w-0 flex-1 {enabled ? 'text-ink' : 'text-faint'}">
					Echomail to me
					<span class="block text-xs text-faint">addressed to your name, in the areas you can read</span>
				</span>
				<input type="checkbox" class="check" bind:checked={echomail} disabled={busy || !enabled} onchange={() => apply(true)} />
			</label>
			{#if enabled}
				<button class="r-row text-accent" disabled={busy} onclick={test}>Send a test notification</button>
			{/if}
		{/if}

		<div class="r-section">Offline</div>
		<p class="px-4 py-2 text-sm text-muted">
			Unread mail is fetched ahead whenever the reader is open, to read it without a network. Replies written
			offline are sent once you're back online.
		</p>
		<div class="r-row">
			<span class="min-w-0 flex-1">
				<span class="block text-ink">Last fetched {since(offline.syncedAt)}</span>
				{#if offline.syncedAt && offline.fetched}<span class="block text-xs text-faint">{offline.fetched} unread message(s) kept</span>{/if}
				{#if offline.outbox}<span class="block text-xs text-amber-400">{offline.outbox} waiting to be sent</span>{/if}
			</span>
			<button class="r-btn text-base" disabled={offline.syncing || !offline.online} onclick={() => syncAhead(true)}>
				{offline.syncing ? 'Fetching…' : 'Fetch now'}
			</button>
		</div>

		<div class="r-section">Account</div>
		<button class="r-row text-red-400" onclick={onLogout}>Log out</button>
	</div>
</div>
