<script lang="ts">
	// A reply, full screen over the message: To, Subject and the text,
	// sent as-is. The original is quoted below for reference only.
	let {
		to = $bindable(),
		subject = $bindable(),
		body = $bindable(),
		quote = '',
		toLocked = false,
		busy = false,
		onSend,
		onCancel
	}: {
		to: string;
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
		<span class="r-title text-center">Reply</span>
		<button class="r-btn text-base font-semibold" disabled={busy || !subject || !body.trim()} onclick={onSend}>
			{busy ? 'Sending…' : 'Send'}
		</button>
	</header>
	<div class="flex flex-1 flex-col gap-2 overflow-y-auto p-3">
		<input class="field py-2.5 text-base" bind:value={to} placeholder="To" readonly={toLocked} />
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
