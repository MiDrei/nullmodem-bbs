<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		getConfig,
		requestAreafixChanges,
		requestAreafixList,
		getAreafixListReply,
		listAreafixSubscriptions,
		ApiError,
		type BBSConfig,
		type AreafixKind,
		type AreafixListReply
	} from '$lib/api';

	type Entry = {
		tag: string;
		description: string;
		checked: boolean;
		/** Whether this entry came from the hub's own %LIST reply, vs. one we already have recorded or typed in by hand -- purely informational, shown as a hint. */
		fromReply: boolean;
	};

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let loaded = $state(false);

	let uplinkIndex = $state(0);
	let kind = $state<AreafixKind>('echo');

	let existingSubscriptions = $state<string[]>([]);
	let listReply = $state<AreafixListReply | null>(null);
	let manualTags = $state<string[]>([]);
	let checkedOverrides = $state<Record<string, boolean>>({});
	let manualTagInput = $state('');
	let showRawReply = $state(false);

	let requestingList = $state(false);
	let refreshingReply = $state(false);
	let applying = $state(false);

	let uplink = $derived(config?.binkp_uplinks[uplinkIndex] ?? null);

	// Merges everything we know about into one checkbox list: areas
	// already on our own record (existingSubscriptions -- start
	// checked), areas the hub's own %LIST reply mentioned (checked if
	// the reply itself marked them subscribed), and any tag typed in
	// by hand -- deduplicated case-insensitively, in that order.
	let entries = $derived.by((): Entry[] => {
		const byTag = new Map<string, Entry>();
		for (const tag of existingSubscriptions) {
			byTag.set(tag.toUpperCase(), { tag, description: '', checked: true, fromReply: false });
		}
		for (const area of listReply?.areas ?? []) {
			const key = area.Tag.toUpperCase();
			const existing = byTag.get(key);
			byTag.set(key, {
				tag: area.Tag,
				description: area.Description,
				checked: existing ? existing.checked : area.Subscribed,
				fromReply: true
			});
		}
		for (const tag of manualTags) {
			const key = tag.toUpperCase();
			if (!byTag.has(key)) {
				byTag.set(key, { tag, description: '', checked: true, fromReply: false });
			}
		}
		for (const [key, entry] of byTag) {
			if (key in checkedOverrides) entry.checked = checkedOverrides[key];
		}
		return [...byTag.values()].sort((a, b) => a.tag.localeCompare(b.tag));
	});

	function toggleEntry(tag: string) {
		const key = tag.toUpperCase();
		const current = entries.find((e) => e.tag.toUpperCase() === key)?.checked ?? false;
		checkedOverrides = { ...checkedOverrides, [key]: !current };
	}

	async function handleAuthError(err: unknown): Promise<boolean> {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/login');
			return true;
		}
		return false;
	}

	async function loadState() {
		if (!auth.token || !uplink || !uplink.host.trim()) return;
		checkedOverrides = {};
		manualTags = [];
		try {
			const [subs, reply] = await Promise.all([
				listAreafixSubscriptions(auth.token, uplink.host, kind),
				getAreafixListReply(auth.token, uplink.address)
			]);
			existingSubscriptions = subs.area_tags;
			listReply = reply;
		} catch (err) {
			if (await handleAuthError(err)) return;
			// Non-fatal: the checkbox list just starts empty/manual-only.
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
			loadError = null;
		} catch (err) {
			if (await handleAuthError(err)) return;
			loadError = err instanceof ApiError ? err.message : 'Could not load configuration.';
		} finally {
			loaded = true;
		}
		await loadState();
	});

	function addManualTag() {
		const tag = manualTagInput.trim().toUpperCase();
		if (!tag) return;
		if (!manualTags.some((t) => t.toUpperCase() === tag)) {
			manualTags = [...manualTags, tag];
		}
		manualTagInput = '';
	}

	async function requestList() {
		if (!auth.token || !uplink) return;
		if (!uplink.host.trim() || !uplink.address.trim()) {
			toast.push('This uplink needs a host:port and address configured first.', 'error');
			return;
		}
		requestingList = true;
		try {
			await requestAreafixList(auth.token, uplink, kind);
			toast.push(
				'Area list requested -- the reply arrives as netmail, typically within a few minutes. Use "Refresh reply" once it does.',
				'success'
			);
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Requesting the list failed.', 'error');
		} finally {
			requestingList = false;
		}
	}

	async function refreshReply() {
		if (!auth.token || !uplink) return;
		refreshingReply = true;
		try {
			listReply = await getAreafixListReply(auth.token, uplink.address);
			if (!listReply.found) {
				toast.push('No reply from this uplink yet.', 'success');
			}
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Could not fetch the reply.', 'error');
		} finally {
			refreshingReply = false;
		}
	}

	async function applyChanges() {
		if (!auth.token || !uplink) return;
		if (!uplink.host.trim() || !uplink.address.trim()) {
			toast.push('This uplink needs a host:port and address configured first.', 'error');
			return;
		}
		const existing = new Set(existingSubscriptions.map((t) => t.toUpperCase()));
		const changes = entries
			.filter((e) => e.checked !== existing.has(e.tag.toUpperCase()))
			.map((e) => ({ area_tag: e.tag, subscribe: e.checked }));
		if (changes.length === 0) {
			toast.push('Nothing changed.', 'success');
			return;
		}
		applying = true;
		try {
			await requestAreafixChanges(auth.token, uplink, changes, kind);
			toast.push(
				`${changes.length} area change(s) queued -- sent within a few minutes via the mailer's crash-trigger.`,
				'success'
			);
			await loadState();
		} catch (err) {
			if (await handleAuthError(err)) return;
			toast.push(err instanceof ApiError ? err.message : 'Applying changes failed.', 'error');
		} finally {
			applying = false;
		}
	}
</script>

<h1 class="mb-2 text-xl font-semibold text-slate-100">Areafix / Filefix</h1>
<p class="mb-6 text-sm text-slate-400">
	Request echomail (Areafix) or file-echo (Filefix) area subscriptions from a configured uplink.
	Every request is queued as a Crash-priority netmail and sent within a few minutes via the
	mailer's usual crash-trigger, not instantly -- the reply (accepted, rejected, or a password
	error) arrives back as ordinary netmail. Parsing a hub's area list into checkboxes is
	best-effort: hub software formats it differently, so always check the raw reply text below if
	something looks off, and add a tag by hand if it doesn't show up.
</p>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !loaded}
	<p class="text-sm text-slate-400">Loading…</p>
{:else if !config || config.binkp_uplinks.length === 0}
	<p class="text-sm text-slate-500">
		No BinkP uplinks configured yet -- add one on the <a href="/binkp" class="text-cyan-400 underline"
			>BinkP page</a
		>
		first.
	</p>
{:else}
	<div class="mb-6 flex flex-wrap items-end gap-3">
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Uplink</span>
			<select
				class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-slate-100 focus:border-cyan-500 focus:outline-none"
				bind:value={uplinkIndex}
				onchange={loadState}
			>
				{#each config.binkp_uplinks as u, i (i)}
					<option value={i}>{u.address || u.host || `Uplink ${i + 1}`}</option>
				{/each}
			</select>
		</label>
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Kind</span>
			<select
				class="rounded border border-slate-700 bg-slate-900 px-2 py-1 text-sm text-slate-100 focus:border-cyan-500 focus:outline-none"
				bind:value={kind}
				onchange={loadState}
			>
				<option value="echo">Echo areas (Areafix)</option>
				<option value="file">File areas (Filefix)</option>
			</select>
		</label>
		<button
			type="button"
			class="rounded border border-cyan-700 px-3 py-1.5 text-sm text-cyan-400 hover:bg-cyan-950 disabled:opacity-50"
			disabled={requestingList}
			onclick={requestList}
		>
			{requestingList ? 'Requesting…' : 'Request area list'}
		</button>
		<button
			type="button"
			class="rounded border border-slate-700 px-3 py-1.5 text-sm hover:bg-slate-800 disabled:opacity-50"
			disabled={refreshingReply}
			onclick={refreshReply}
		>
			{refreshingReply ? 'Refreshing…' : 'Refresh reply'}
		</button>
	</div>

	{#if listReply?.found}
		<p class="mb-3 text-xs text-slate-500">
			Showing the reply received {new Date(listReply.posted_at ?? '').toLocaleString()}
			(subject: {listReply.subject}).
			<button type="button" class="text-cyan-400 underline" onclick={() => (showRawReply = !showRawReply)}>
				{showRawReply ? 'Hide' : 'Show'} raw text
			</button>
		</p>
		{#if showRawReply}
			<pre
				class="mb-4 max-h-64 overflow-auto rounded border border-slate-800 bg-slate-950 p-3 font-mono text-xs whitespace-pre-wrap text-slate-300">{listReply.raw_body}</pre>
		{/if}
	{:else}
		<p class="mb-3 text-xs text-slate-500">
			No area-list reply on file for this uplink's address yet -- click "Request area list" above,
			wait for it to arrive, then "Refresh reply". You can still add area tags by hand below in the
			meantime.
		</p>
	{/if}

	<div class="mb-4 flex items-end gap-2">
		<label class="flex flex-col gap-1 text-sm">
			<span class="text-slate-400">Add a tag by hand</span>
			<input
				type="text"
				class="rounded border border-slate-700 bg-slate-900 px-2 py-1 font-mono text-sm text-slate-100 focus:border-cyan-500 focus:outline-none"
				placeholder="AREA_TAG"
				bind:value={manualTagInput}
				onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addManualTag())}
			/>
		</label>
		<button
			type="button"
			class="rounded border border-slate-700 px-3 py-1.5 text-sm hover:bg-slate-800"
			onclick={addManualTag}
		>
			Add
		</button>
	</div>

	{#if entries.length === 0}
		<p class="mb-4 text-sm text-slate-500">No areas to show yet.</p>
	{:else}
		<div class="mb-4 max-h-96 overflow-auto rounded border border-slate-800">
			<table class="w-full text-left text-sm">
				<tbody>
					{#each entries as entry (entry.tag)}
						<tr class="border-b border-slate-800 last:border-0 hover:bg-slate-900">
							<td class="w-8 px-3 py-1.5">
								<input
									type="checkbox"
									checked={entry.checked}
									onchange={() => toggleEntry(entry.tag)}
								/>
							</td>
							<td class="px-1 py-1.5 font-mono text-slate-200">{entry.tag}</td>
							<td class="px-3 py-1.5 text-slate-400">{entry.description}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}

	<button
		type="button"
		class="rounded bg-cyan-600 px-4 py-2 font-medium text-white hover:bg-cyan-500 disabled:opacity-50"
		disabled={applying}
		onclick={applyChanges}
	>
		{applying ? 'Applying…' : 'Apply changes'}
	</button>
{/if}
