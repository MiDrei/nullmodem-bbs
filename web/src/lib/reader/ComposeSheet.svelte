<script lang="ts">
	// A new message or a reply, full screen: To, Subject and the text,
	// sent as-is. A reply's original is quoted below for reference only.
	// For netmail to an FTN address, the recipient's name is asked too.
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { isFTNAddress, lookupNodelist, type NodelistEntry } from '$lib/api';
	import { bbsAuth } from '$lib/bbs-auth.svelte';

	let area = $state<HTMLTextAreaElement | undefined>();
	// A reply starts below the quoted original.
	onMount(() => {
		if (area && body) {
			area.setSelectionRange(body.length, body.length);
			area.scrollTop = area.scrollHeight;
		}
	});

	let {
		heading = t('web.msg.reply'),
		to = $bindable(),
		toName = $bindable(''),
		askToName = false,
		toPlaceholder = t('web.msg.to'),
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

	// Who's behind an FTN address, by the nodelists -- their sysop is
	// the likely recipient.
	let node = $state<NodelistEntry | null>(null);
	let unknown = $state(false);
	let lookupTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const addr = to.trim();
		clearTimeout(lookupTimer);
		node = null;
		unknown = false;
		if (!askToName || toLocked || !isFTNAddress(addr) || !bbsAuth.token) return;
		const token = bbsAuth.token;
		lookupTimer = setTimeout(async () => {
			try {
				node = await lookupNodelist(token, addr);
				if (!toName.trim()) toName = node.sysop;
			} catch {
				unknown = true;
			}
		}, 400);
	});
</script>

<div class="fixed inset-0 z-20 flex flex-col bg-black" style="padding-top: env(safe-area-inset-top)">
	<header class="r-bar">
		<button class="r-btn text-base" onclick={onCancel}>{t('web.common.cancel')}</button>
		<span class="r-title text-center">{heading}</span>
		<button class="r-btn text-base font-semibold" disabled={busy || !to.trim() || !subject || !body.trim()} onclick={onSend}>
			{busy ? t('web.netmail.sending') : t('web.chat.send')}
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
			{#if node}
				<p class="px-1 text-sm text-emerald-400">→ {node.name}, {node.location} · {t('web.reader.sysop_of', { NAME: node.sysop })}</p>
			{:else if unknown}
				<p class="px-1 text-sm text-amber-400">{t('web.reader.not_in_nodelist')}</p>
			{/if}
			<input class="field py-2.5 text-base" bind:value={toName} placeholder={t('web.reader.name_at_address')} />
		{/if}
		<input class="field py-2.5 text-base" bind:value={subject} placeholder={t('web.msg.subject')} />
		<textarea
			bind:this={area}
			class="field min-h-[40vh] flex-1 py-2.5 font-mono text-base leading-relaxed"
			bind:value={body}
			placeholder={heading === t('web.msg.reply') ? t('web.reader.your_reply') : t('web.reader.your_message')}
		></textarea>
		{#if quote}
			<details class="text-sm text-muted">
				<summary class="py-2">{t('web.reader.original')}</summary>
				<div class="font-mono text-[13px] whitespace-pre-wrap text-faint">{quote}</div>
			</details>
		{/if}
	</div>
</div>
