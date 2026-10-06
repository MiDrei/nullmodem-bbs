<script lang="ts">
	import { t } from '$lib/i18n.svelte';
	// The security levels' names, shown wherever a level is chosen in
	// the admin ("20 – Regular user") and in the Telnet sysop menu.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { levels } from '$lib/admin/levels.svelte';
	import { putSecurityLevels, ApiError, type SecurityLevel } from '$lib/api';

	let rows = $state<SecurityLevel[]>([]);
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
		await levels.load(true);
		rows = levels.list.map((l) => ({ ...l }));
	});

	function add() {
		const used = new Set(rows.map((r) => r.level));
		let next = 20;
		while (used.has(next) && next < 255) next += 10;
		rows = [...rows, { level: Math.min(next, 254), name: '' }];
	}

	async function save(list: SecurityLevel[]) {
		if (!auth.token) return;
		saving = true;
		try {
			const r = await putSecurityLevels(auth.token, list);
			levels.set(r.levels, r.custom);
			rows = r.levels.map((l) => ({ ...l }));
			toast.push(t('admin.levels.saved'), 'success');
		} catch (err) {
			await failed(err, t('admin.levels.could_not_save'));
		} finally {
			saving = false;
		}
	}

	async function reset() {
		if (!confirm(t('admin.levels.reset_confirm'))) return;
		await save([]);
	}

	const sorted = $derived([...rows].sort((a, b) => a.level - b.level));
</script>

<div class="mb-6">
	<h1 class="page-title">{t('admin.common.security_levels')}</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">{t('admin.levels.subtitle')}</p>
</div>

<section class="card max-w-2xl">
	{#if !levels.custom}
		<p class="mb-4 text-sm text-muted">{t('admin.levels.defaults_note')}</p>
	{/if}
	<div class="flex flex-col gap-2">
		{#each rows as r, i (i)}
			<div class="flex items-center gap-2">
				<input class="field field-sm w-24 font-mono" type="number" min="0" max="255" bind:value={r.level} aria-label={t('admin.levels.level')} />
				<input class="field field-sm flex-1" maxlength="40" placeholder={t('admin.levels.name_placeholder')} bind:value={r.name} aria-label={t('admin.levels.name')} />
				<button class="btn-secondary btn-sm" onclick={() => (rows = rows.filter((_, j) => j !== i))}>{t('web.common.remove')}</button>
			</div>
		{/each}
	</div>
	<div class="mt-4 flex flex-wrap justify-between gap-2">
		<div class="flex gap-2">
			<button class="btn-secondary btn-sm" onclick={add}>{t('admin.levels.add')}</button>
			{#if levels.custom}
				<button class="btn-secondary btn-sm" onclick={reset} disabled={saving}>{t('admin.levels.reset')}</button>
			{/if}
		</div>
		<button class="btn-primary btn-sm" onclick={() => save(sorted)} disabled={saving || rows.some((r) => !r.name.trim())}>
			{saving ? t('web.common.saving') : t('web.common.save')}
		</button>
	</div>
</section>
