<script lang="ts">
	// A shared file: what it is, and the download -- no login. The page
	// a share link opens (its link preview comes from the server).
	import { t, tn, i18n } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';
	import AnsiArt from '$lib/AnsiArt.svelte';
	import { getPublicFile, getBBSInfo, type PublicFile, type BBSInfo } from '$lib/api';

	let f = $state<PublicFile | null>(null);
	let info = $state<BBSInfo | null>(null);
	let missing = $state(false);

	onMount(async () => {
		getBBSInfo().then((i) => (info = i)).catch(() => {});
		try {
			f = await getPublicFile(Number(page.params.id));
		} catch {
			missing = true;
		}
	});

	const date = (iso: string) => new Date(iso).toLocaleDateString(i18n.locale);
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{f ? `${f.filename} · ${info?.name ?? ''}` : (info?.name ?? 'NullModem BBS')}</title>
</svelte:head>

<div class="flex min-h-screen flex-col bg-ground text-ink">
	<header class="flex items-center justify-between border-b border-line px-6 py-5 md:px-10">
		<a href="/" class="text-[15px] font-bold tracking-tight text-ink-strong">{info?.name ?? ''}</a>
		<a href="/" class="text-[13px] text-muted hover:text-accent">{t('web.share.about')} →</a>
	</header>

	<main class="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-5 px-6 py-10 md:px-10">
		{#if missing}
			<p class="text-muted">{t('web.share.missing')}</p>
		{:else if f}
			<div>
				<h1 class="font-mono text-2xl font-semibold break-all text-ink-strong">{f.filename}</h1>
				<p class="mt-1 text-sm text-muted">
					{f.size} · {f.area}{f.network ? ` (${f.network})` : ''} · {date(f.uploaded_at)}{f.uploaded_by ? ` · ${t('web.share.from', { NAME: f.uploaded_by })}` : ''} · {tn('web.file.downloads', f.downloads)}
				</p>
			</div>

			{#if f.preformatted && f.grid}
				<div class="ansi-panel">
					<AnsiArt grid={f.grid} fit maxZoom={1.4} />
				</div>
			{:else if f.description}
				<div class="body-panel whitespace-pre-wrap">{f.description}</div>
			{/if}

			<div class="flex flex-wrap items-center gap-3">
				<a class="btn-primary px-6 py-3 text-base" href={f.download} download>{t('web.common.download')}</a>
				<span class="text-xs text-faint">{t('web.share.no_login')}</span>
			</div>

			<p class="mt-6 border-t border-line pt-5 text-sm text-muted">
				{t('web.share.its_from')} <a href="/" class="text-accent hover:underline">{info?.name ?? t('web.share.this_bbs')}</a> -- {t('web.share.pitch')}
			</p>
		{:else}
			<p class="text-muted">{t('web.common.loading')}</p>
		{/if}
	</main>
</div>
