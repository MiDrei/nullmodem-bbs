<script lang="ts">
	// A new message or a reply, full screen: To, Subject and the text,
	// sent as-is. A reply's original is quoted below for reference only.
	// For netmail to an FTN address, the recipient's name is asked too.
	import { isFTNAddress } from '$lib/api';

	let {
		heading = 'Reply',
		to = $bindable(),
		toName = $bindable(''),
		askToName = false,
		toPlaceholder = 'To',
		subject = $bindable(),
		body = $bindable(),
		quote = '',
		toLocked = false,
		busy = false,
		onSend,
		onCancel
	}: {
		heading?: string;
		to: string;
		toName?: string;
		/** Netmail: ask for the recipient's name when To is an FTN address. */
		askToName?: boolean;
		toPlaceholder?: string;
		subject: string;
		body: string;
		quote?: string;
		/** The recipient is fixed (a netmail reply goes back to its sender). */
		toLocked?: boolean;
		busy?: boolean;
		onSend: () => void;
		onCancel: () => void;
	} = $props();
</script>

<div class="fixed inset-0 z-20 flex flex-col bg-black" style="padding-top: env(safe-area-inset-top)">
	<header class="r-bar">
		<button class="r-btn text-base" onclick={onCancel}>Cancel</button>
		<span class="r-title text-center">{heading}</span>
		<button class="r-btn text-base font-semibold" disabled={busy || !to.trim() || !subject || !body.trim()} onclick={onSend}>
			{busy ? 'Sending…' : 'Send'}
		</button>
	</header>
	<div class="flex flex-1 flex-col gap-2 overflow-y-auto p-3">
		<input
			class="field py-2.5 text-base"
			bind:value={to}
			placeholder={toPlaceholder}
			readonly={toLocked}
			autocapitalize="off"
		/>
		{#if askToName && !toLocked && isFTNAddress(to.trim())}
			<input class="field py-2.5 text-base" bind:value={toName} placeholder="Name at that address" />
		{/if}
		<input class="field py-2.5 text-base" bind:value={subject} placeholder="Subject" />
		<textarea
			class="field min-h-[40vh] flex-1 py-2.5 font-mono text-[15px] leading-relaxed"
			bind:value={body}
			placeholder="Your reply"
		></textarea>
		{#if quote}
			<details class="text-sm text-muted">
				<summary class="py-2">Original message</summary>
				<div class="font-mono text-[13px] whitespace-pre-wrap text-faint">{quote}</div>
			</details>
		{/if}
	</div>
</div>
