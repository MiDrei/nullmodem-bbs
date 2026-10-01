<script lang="ts">
	// The BBS in the browser: a Telnet session through the web daemon,
	// for visitors without a Telnet client (BBS lists link here). No
	// portal login -- the BBS's own login screen is in the terminal.
	import Terminal from '$lib/terminal/Terminal.svelte';
	import { site } from '$lib/site.svelte';
</script>

<svelte:head>
	<title>{site.info.name ? `${site.info.name} -- Terminal` : 'Terminal'}</title>
</svelte:head>

<div class="min-h-dvh bg-black px-2 py-4 sm:px-6">
	<div class="mx-auto mb-3 flex max-w-[1100px] items-baseline justify-between gap-3 text-xs text-faint">
		<span class="font-mono">{site.info.name ?? 'BBS'} · web terminal</span>
		<a href="/login" class="text-muted hover:text-ink">Web portal →</a>
	</div>
	<Terminal />
	<p class="mx-auto mt-3 max-w-[1100px] text-center text-[11px] text-dim">
		A real Telnet client (SyncTERM, NetRunner, MuffinTerm) is nicer still{site.telnetAddress ? `: ${site.telnetAddress}` : ''}.
		File transfers (Zmodem) need one; in the browser, use the web portal for files.
	</p>
</div>
