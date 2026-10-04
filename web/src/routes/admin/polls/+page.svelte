<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// Polls the callers vote on (Telnet: V, portal: Community), and the
	// BBS list they keep -- to tidy up.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import {
		listPolls,
		createPoll,
		closePoll,
		deletePoll,
		adminListBBSList,
		adminDeleteBBSListEntry,
		ApiError,
		type Poll,
		type BBSListEntry
	} from '$lib/api';

	let polls = $state<Poll[]>([]);
	let bbs = $state<BBSListEntry[]>([]);
	let question = $state('');
	let options = $state(['', '', '']);
	let saving = $state(false);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	async function load() {
		if (!auth.token) return;
		try {
			[polls, bbs] = await Promise.all([listPolls(auth.token), adminListBBSList(auth.token)]);
		} catch (err) {
			await failed(err, t('admin.polls.could_not_load'));
		}
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		await load();
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token) return;
		saving = true;
		try {
			await createPoll(auth.token, question, options);
			question = '';
			options = ['', '', ''];
			toast.push(t('admin.polls.poll_started'), 'success');
			await load();
		} catch (err) {
			await failed(err, t('admin.polls.could_not_start_the_poll'));
		} finally {
			saving = false;
		}
	}

	async function toggle(p: Poll) {
		if (!auth.token) return;
		try {
			await closePoll(auth.token, p.id, !p.closed);
			await load();
		} catch (err) {
			await failed(err, t('admin.polls.could_not_change_it'));
		}
	}

	async function remove(p: Poll) {
		if (!auth.token || !confirm(t('admin.polls.delete_the_poll_question_and', { QUESTION: p.question }))) return;
		try {
			await deletePoll(auth.token, p.id);
			await load();
		} catch (err) {
			await failed(err, t('admin.polls.could_not_delete_it'));
		}
	}

	async function removeBBS(e: BBSListEntry) {
		if (!auth.token || !confirm(t('admin.polls.remove_name_from_the_bbs', { NAME: e.name }))) return;
		try {
			await adminDeleteBBSListEntry(auth.token, e.id);
			bbs = bbs.filter((x) => x.id !== e.id);
		} catch (err) {
			await failed(err, t('admin.polls.could_not_remove_it'));
		}
	}

	const pct = (p: Poll, v: number) => (p.total ? Math.round((v * 100) / p.total) : 0);
</script>

<div class="mb-6">
	<h1 class="page-title">{t('admin.polls.polls_bbs_list')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		{t('admin.polls.ask_your_callers_something_they')}
	</p>
</div>

<form class="card mb-4 flex flex-col gap-3" onsubmit={create}>
	<h2 class="card-label">{t('admin.polls.new_poll')}</h2>
	<input class="field" placeholder={t('admin.polls.question')} bind:value={question} required />
	<div class="grid gap-2 sm:grid-cols-2">
		{#each options as _, i (i)}
			<input class="field field-sm" placeholder="Answer {i + 1}" bind:value={options[i]} />
		{/each}
	</div>
	<div class="flex justify-between gap-2">
		<button type="button" class="btn-secondary btn-sm" disabled={options.length >= 10} onclick={() => (options = [...options, ''])}>{t('admin.polls.answer')}</button>
		<button type="submit" class="btn-primary btn-sm" disabled={saving || !question.trim() || options.filter((o) => o.trim()).length < 2}>{t('admin.polls.start_poll')}</button>
	</div>
</form>

<div class="mb-8 flex flex-col gap-3">
	{#each polls as p (p.id)}
		<section class="card">
			<div class="mb-2 flex flex-wrap items-baseline gap-3">
				<h3 class="min-w-0 flex-1 font-semibold text-ink-strong">{p.question}</h3>
				<span class="text-xs text-faint">{t('admin.polls.total_votes_v', { TOTAL: p.total, V: p.closed ? t('admin.polls.closed') : '' })}</span>
				<button class="btn-secondary btn-xs" onclick={() => toggle(p)}>{p.closed ? t('admin.polls.reopen') : t('admin.common.close')}</button>
				<button class="btn-secondary btn-xs" onclick={() => remove(p)}>{t('admin.common.delete')}</button>
			</div>
			{#each p.options as o (o.id)}
				<div class="relative mb-1 overflow-hidden rounded border border-line px-3 py-1.5 text-sm">
					<span class="absolute inset-y-0 left-0 bg-accent/15" style="width: {pct(p, o.votes)}%"></span>
					<span class="relative flex justify-between"><span class="text-ink">{o.text}</span><span class="text-muted">{pct(p, o.votes)}% · {o.votes}</span></span>
				</div>
			{/each}
		</section>
	{/each}
</div>

<section class="card">
	<h2 class="card-label mb-3">{t('admin.polls.bbs_list_length', { LENGTH: bbs.length })}</h2>
	{#if bbs.length === 0}
		<p class="text-sm text-muted">{t('admin.polls.empty_so_far')}</p>
	{/if}
	<table class="w-full text-left text-sm">
		<tbody class="divide-y divide-slate-800">
			{#each bbs as e (e.id)}
				<tr>
					<td class="py-2 text-ink-strong">{e.name}</td>
					<td class="py-2 font-mono text-xs text-accent">{e.address}</td>
					<td class="py-2 text-xs text-muted">{e.added_by}</td>
					<td class="py-2 text-right"><button class="btn-secondary btn-xs" onclick={() => removeBBS(e)}>{t('admin.common.remove')}</button></td>
				</tr>
			{/each}
		</tbody>
	</table>
</section>
