<script lang="ts">
	// The rooms callers may enter (/rooms, /join in the teleconference)
	// and the Discord bot that bridges them to channels.
	import { onMount, onDestroy } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listChatRoomSettings,
		saveChatRoom,
		deleteChatRoom,
		getDiscord,
		saveDiscord,
		ApiError,
		type ChatRoomSettings,
		type DiscordState
	} from '$lib/api';

	let { onchange }: { onchange?: (rooms: ChatRoomSettings[]) => void } = $props();

	let rooms = $state<ChatRoomSettings[]>([]);
	let editing = $state<ChatRoomSettings | null>(null);
	let isNew = $state(false);
	let dc = $state<DiscordState | null>(null);
	let tokenInput = $state('');
	let saving = $state(false);
	let timer: ReturnType<typeof setInterval> | undefined;

	const fail = (err: unknown, fallback: string) => toast.push(err instanceof ApiError ? err.message : fallback, 'error');

	async function load() {
		if (!auth.token) return;
		try {
			[rooms, dc] = await Promise.all([listChatRoomSettings(auth.token), getDiscord(auth.token)]);
			onchange?.(rooms);
		} catch (err) {
			fail(err, 'Could not load the rooms.');
		}
	}

	onMount(() => {
		load();
		// While connecting, the status and channels change by themselves.
		timer = setInterval(async () => {
			if (auth.token && dc?.enabled) dc = await getDiscord(auth.token).catch(() => dc);
		}, 5000);
	});
	onDestroy(() => clearInterval(timer));

	function edit(r?: ChatRoomSettings) {
		isNew = !r;
		editing = r ? { ...r } : { name: '', title: '', topic: '', min_sl: 0, sort_order: (rooms.length + 1) * 10, discord_channel: '' };
	}

	async function saveRoom(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token || !editing) return;
		try {
			await saveChatRoom(auth.token, { ...editing, name: editing.name.trim().toLowerCase() });
			editing = null;
			await load();
			toast.push('Room saved.', 'success');
		} catch (err) {
			fail(err, 'Could not save the room.');
		}
	}

	async function removeRoom(r: ChatRoomSettings) {
		if (!auth.token || !confirm(`Remove the room "${r.title}"? Callers can't /join it any more.`)) return;
		try {
			await deleteChatRoom(auth.token, r.name);
			await load();
		} catch (err) {
			fail(err, 'Could not remove the room.');
		}
	}

	async function saveBot(enabled: boolean, extra: { token?: string; clear_token?: boolean } = {}) {
		if (!auth.token || !dc) return;
		saving = true;
		try {
			dc = await saveDiscord(auth.token, { enabled, quiet: dc.quiet, ...extra });
			tokenInput = '';
			toast.push(enabled ? 'Saved -- the bot connects in a moment.' : 'Saved.', 'success');
			setTimeout(load, 3000);
		} catch (err) {
			fail(err, 'Could not save.');
		} finally {
			saving = false;
		}
	}

	const channelName = (id: string) => {
		const c = dc?.channels.find((x) => x.id === id);
		return c ? `#${c.name} (${c.guild})` : id ? `channel ${id}` : '';
	};
</script>

<div class="grid gap-4 lg:grid-cols-2">
	<section class="card">
		<div class="mb-3 flex items-center justify-between">
			<h2 class="card-label">Rooms</h2>
			{#if !editing}<button class="btn-primary btn-xs" onclick={() => edit()}>+ Add a room</button>{/if}
		</div>
		<p class="mb-3 text-xs leading-relaxed text-muted">
			In the teleconference, callers see the rooms with <span class="font-mono">/rooms</span> and change with
			<span class="font-mono">/join name</span>.
		</p>
		{#if editing}
			<form class="mb-4 grid gap-2.5 sm:grid-cols-2" onsubmit={saveRoom}>
				<label class="flex flex-col gap-1 text-xs text-muted">
					Name (for /join)
					<input class="field font-mono" bind:value={editing.name} disabled={!isNew} required maxlength="64" placeholder="tech" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					Title
					<input class="field" bind:value={editing.title} placeholder="Technik" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					Topic (shown on entering)
					<input class="field" bind:value={editing.topic} maxlength="200" />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					Lowest security level
					<input class="field" type="number" min="0" max="255" bind:value={editing.min_sl} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted">
					Order
					<input class="field" type="number" bind:value={editing.sort_order} />
				</label>
				<label class="flex flex-col gap-1 text-xs text-muted sm:col-span-2">
					Discord channel
					{#if dc?.channels.length}
						<select class="field" bind:value={editing.discord_channel}>
							<option value="">— not bridged —</option>
							{#each dc.channels as c (c.id)}
								<option value={c.id}>#{c.name} ({c.guild})</option>
							{/each}
						</select>
					{:else}
						<input class="field font-mono" bind:value={editing.discord_channel} placeholder="channel ID, or connect the bot first" />
					{/if}
				</label>
				<div class="flex justify-end gap-2 sm:col-span-2">
					<button type="button" class="btn-secondary btn-sm" onclick={() => (editing = null)}>Cancel</button>
					<button type="submit" class="btn-primary btn-sm">Save</button>
				</div>
			</form>
		{/if}
		<div class="flex flex-col divide-y divide-line">
			{#each rooms as r (r.name)}
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5 text-sm">
					<div class="min-w-0 flex-1">
						<div class="text-ink-strong">
							{r.title} <span class="font-mono text-xs text-faint">{r.name}</span>
							{#if r.min_sl > 0}<span class="text-xs text-faint"> · SL {r.min_sl}+</span>{/if}
						</div>
						{#if r.discord_channel}
							<div class="text-xs text-indigo-400">↔ Discord {channelName(r.discord_channel)}</div>
						{/if}
					</div>
					<button class="btn-secondary btn-xs" onclick={() => edit(r)}>Edit</button>
					{#if r.name !== 'main'}<button class="btn-secondary btn-xs" onclick={() => removeRoom(r)}>Remove</button>{/if}
				</div>
			{/each}
		</div>
	</section>

	<section class="card">
		<h2 class="card-label mb-3">Discord bridge</h2>
		{#if dc}
			<div class="mb-3 flex items-center gap-2 text-sm">
				<span class="inline-block h-2 w-2 rounded-full {dc.status.connected ? 'bg-emerald-400' : dc.enabled ? 'bg-amber-400' : 'bg-line-strong'}"></span>
				{#if dc.status.connected}
					<span>Connected as <span class="text-ink-strong">{dc.status.bot}</span>{#if dc.status.guilds?.length}{` · on ${dc.status.guilds.join(', ')}`}{/if}</span>
				{:else if dc.enabled}
					<span class="text-amber-300">{dc.status.error || 'Connecting…'}</span>
				{:else}
					<span class="text-muted">Off</span>
				{/if}
			</div>

			{#if dc.status.connected && !dc.status.guilds?.length}
				<p class="mb-3 rounded-lg border border-line p-3 text-xs leading-relaxed text-ink-soft">
					The bot isn't on a Discord server yet.
					<a href={dc.invite_url} target="_blank" rel="noopener" class="text-accent hover:underline">Add it to your server</a>
					— then pick a channel for each room on the left.
				</p>
			{:else if dc.invite_url}
				<p class="mb-3 text-xs text-muted">
					<a href={dc.invite_url} target="_blank" rel="noopener" class="text-accent hover:underline">Add the bot to another server</a>
				</p>
			{/if}

			<div class="flex flex-col gap-3">
				<label class="flex flex-col gap-1 text-xs text-muted">
					Bot token {dc.has_token ? '(saved — enter a new one to replace it)' : ''}
					<input class="field font-mono" type="password" autocomplete="off" bind:value={tokenInput} placeholder={dc.has_token ? '••••••••' : 'from the Developer Portal: Bot → Reset Token'} />
				</label>
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" bind:checked={dc.quiet} />
					Don't tell Discord when someone enters or leaves a room
				</label>
				<div class="flex flex-wrap justify-end gap-2">
					{#if dc.has_token && !dc.enabled}
						<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveBot(false, { clear_token: true })}>Forget the token</button>
					{/if}
					{#if dc.enabled}
						<button class="btn-secondary btn-sm" disabled={saving} onclick={() => saveBot(false, tokenInput ? { token: tokenInput } : {})}>Turn off</button>
					{/if}
					<button class="btn-primary btn-sm" disabled={saving || (!dc.has_token && !tokenInput.trim())} onclick={() => saveBot(true, tokenInput ? { token: tokenInput } : {})}>
						{dc.enabled ? 'Save' : 'Turn on'}
					</button>
				</div>
			</div>

			<details class="mt-4 text-xs leading-relaxed text-muted">
				<summary class="cursor-pointer text-ink-soft">How to set it up (about 10 minutes)</summary>
				<ol class="mt-2 ml-4 list-decimal space-y-1.5">
					<li>Your own Discord server, if you have none yet: in the Discord app, the <b>+</b> at the bottom of the server list → “Create My Own”.</li>
					<li>
						<a href="https://discord.com/developers/applications" target="_blank" rel="noopener" class="text-accent hover:underline">Developer Portal</a>
						→ <b>New Application</b> (the name is the bot's name, e.g. your BBS's).
					</li>
					<li><b>Bot</b> → turn on <b>Message Content Intent</b> → Save.</li>
					<li><b>Bot</b> → <b>Reset Token</b> → copy it into the field above → <b>Turn on</b>.</li>
					<li>Once it says “Connected”: <b>Add it to your server</b> (the link above) — it asks for the permissions it needs.</li>
					<li>Give each room its channel (Edit on the left). In Discord, what the callers say appears under their names.</li>
				</ol>
			</details>
		{/if}
	</section>
</div>
