<script lang="ts">
	// Netmail received, newest first.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { listBBSNetmail, type BBSNetmailSummary } from '$lib/api';
	import { readerToken, readerAuthFailed, errorText, shortDate } from '$lib/reader/session';

	let mails = $state<BBSNetmailSummary[]>([]);
	let error = $state<string | null>(null);
	let loaded = $state(false);

	onMount(async () => {
		const token = await readerToken();
		if (!token) return;
		try {
			mails = await listBBSNetmail(token);
		} catch (err) {
			if (await readerAuthFailed(err)) return;
			error = errorText(err, 'Could not load the netmail.');
		} finally {
			loaded = true;
		}
	});
</script>

<header class="r-bar">
	<button class="r-btn text-3xl leading-none" onclick={() => goto('/reader')} aria-label="Back">‹</button>
	<span class="r-title">Netmail</span>
</header>

{#if error}
	<p class="r-note text-red-400">{error}</p>
{:else if loaded}
	{#each mails as m (m.id)}
		<a class="r-row" href="/reader/netmail/{m.id}">
			<span class="h-2 w-2 shrink-0 rounded-full {m.unread ? 'bg-accent' : ''}"></span>
			<span class="min-w-0 flex-1">
				<span class="block truncate {m.unread ? 'font-semibold text-ink-strong' : 'text-ink-soft'}">{m.subject}</span>
				<span class="block truncate text-xs text-muted">{m.from_name}</span>
			</span>
			<span class="shrink-0 text-xs text-faint">{shortDate(m.posted_at)}</span>
		</a>
	{:else}
		<p class="r-note">No netmail.</p>
	{/each}
{/if}
