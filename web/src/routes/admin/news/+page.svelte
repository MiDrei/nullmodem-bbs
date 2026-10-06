<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// The sysop's news: written in English and German, shown to callers
	// at login (the ones they haven't seen), in the main menu's N, on
	// the front page and in the portal.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { listNews, createNews, updateNews, deleteNews, ApiError, type NewsItem, type NewsInput } from '$lib/api';

	const empty = (): NewsInput => ({ title_en: '', text_en: '', title_de: '', text_de: '', expires_at: '' });

	let news = $state<NewsItem[]>([]);
	let form = $state<NewsInput>(empty());
	let editing = $state<number | null>(null);
	let saving = $state(false);

	async function failed(err: unknown, fallback: string) {
		if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
			auth.clear();
			await goto('/admin/login');
			return;
		}
		toast.push(err instanceof ApiError ? err.message : fallback, 'error');
	}

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			news = await listNews(auth.token);
		} catch (err) {
			await failed(err, t('admin.news.could_not_load'));
		}
	});

	const complete = $derived(
		(form.title_en.trim() !== '' || form.title_de.trim() !== '') && (form.text_en.trim() !== '' || form.text_de.trim() !== '')
	);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!auth.token) return;
		saving = true;
		try {
			news = editing === null ? await createNews(auth.token, form) : await updateNews(auth.token, editing, form);
			toast.push(editing === null ? t('admin.news.published') : t('admin.news.saved'), 'success');
			form = empty();
			editing = null;
		} catch (err) {
			await failed(err, t('admin.news.could_not_save'));
		} finally {
			saving = false;
		}
	}

	function edit(n: NewsItem) {
		editing = n.id;
		form = { title_en: n.title_en, text_en: n.text_en, title_de: n.title_de, text_de: n.text_de, expires_at: n.expires_at };
		window.scrollTo({ top: 0, behavior: 'smooth' });
	}

	function cancel() {
		editing = null;
		form = empty();
	}

	async function remove(n: NewsItem) {
		if (!auth.token || !confirm(t('admin.news.delete_confirm', { TITLE: n.title_de || n.title_en }))) return;
		try {
			news = await deleteNews(auth.token, n.id);
			if (editing === n.id) cancel();
		} catch (err) {
			await failed(err, t('admin.common.could_not_delete_it'));
		}
	}
</script>

<div class="mb-6">
	<h1 class="page-title">{t('web.news.title')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">{t('admin.news.subtitle')}</p>
</div>

<form class="card mb-6 flex flex-col gap-4" onsubmit={save}>
	<h2 class="card-label">{editing === null ? t('admin.news.new') : t('admin.news.edit')}</h2>
	<div class="grid gap-4 lg:grid-cols-2">
		<div class="flex flex-col gap-2">
			<span class="text-xs font-semibold tracking-wide text-muted uppercase">{t('admin.news.german')}</span>
			<input class="field" placeholder={t('admin.news.title_field')} bind:value={form.title_de} maxlength="64" />
			<textarea class="field min-h-32" placeholder={t('admin.news.text')} bind:value={form.text_de}></textarea>
		</div>
		<div class="flex flex-col gap-2">
			<span class="text-xs font-semibold tracking-wide text-muted uppercase">{t('admin.news.english')}</span>
			<input class="field" placeholder={t('admin.news.title_field')} bind:value={form.title_en} maxlength="64" />
			<textarea class="field min-h-32" placeholder={t('admin.news.text')} bind:value={form.text_en}></textarea>
		</div>
	</div>
	<p class="text-xs text-faint">{t('admin.news.languages_hint')}</p>
	<div class="flex flex-wrap items-end justify-between gap-3">
		<label class="flex flex-col gap-1 text-sm text-muted">
			{t('admin.news.expires')}
			<input class="field field-sm" type="date" bind:value={form.expires_at} />
		</label>
		<div class="flex gap-2">
			{#if editing !== null}
				<button type="button" class="btn-secondary btn-sm" onclick={cancel}>{t('web.common.cancel')}</button>
			{/if}
			<button type="submit" class="btn-primary btn-sm" disabled={saving || !complete}>
				{editing === null ? t('admin.news.publish') : t('web.common.save')}
			</button>
		</div>
	</div>
</form>

{#if news.length === 0}
	<p class="text-sm text-muted">{t('admin.news.none')}</p>
{/if}
<div class="flex flex-col gap-3">
	{#each news as n (n.id)}
		<section class="card" class:opacity-60={n.expired}>
			<div class="mb-2 flex flex-wrap items-baseline gap-3">
				<span class="font-mono text-xs text-faint">{n.created_at.slice(0, 10)}</span>
				<h3 class="min-w-0 flex-1 font-semibold text-ink-strong">{n.title_de || n.title_en}</h3>
				{#if n.expired}
					<span class="text-xs text-faint">{t('admin.news.expired')}</span>
				{:else if n.expires_at}
					<span class="text-xs text-faint">{t('admin.news.until', { DATE: n.expires_at })}</span>
				{/if}
				<button class="btn-secondary btn-xs" onclick={() => edit(n)}>{t('web.common.edit')}</button>
				<button class="btn-secondary btn-xs" onclick={() => remove(n)}>{t('web.common.delete')}</button>
			</div>
			<p class="text-sm whitespace-pre-line text-ink-soft">{n.text_de || n.text_en}</p>
			{#if n.title_en && n.title_de}
				<p class="mt-2 text-xs text-faint">EN: {n.title_en}</p>
			{:else}
				<p class="mt-2 text-xs text-amber-400">{n.title_de ? t('admin.news.no_english') : t('admin.news.no_german')}</p>
			{/if}
		</section>
	{/each}
</div>
