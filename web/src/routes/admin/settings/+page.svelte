<script lang="ts">
	import SLSelect from '$lib/admin/SLSelect.svelte';
	import { t } from '$lib/i18n.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { getConfig, putConfig, ApiError, type BBSConfig } from '$lib/api';

	let config = $state<BBSConfig | null>(null);
	let loadError = $state<string | null>(null);
	let saveError = $state<string | null>(null);
	let saveNote = $state<string | null>(null);
	let saving = $state(false);

	onMount(async () => {
		if (!auth.token) {
			await goto('/admin/login');
			return;
		}
		try {
			config = await getConfig(auth.token);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			loadError = err instanceof ApiError ? err.message : t('admin.common.could_not_load_configuration');
		}
	});

	async function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (!config || !auth.token) return;
		saveError = null;
		saveNote = null;
		saving = true;
		try {
			const res = await putConfig(auth.token, config);
			config = res.config;
			saveNote = res.note;
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			saveError = err instanceof ApiError ? err.message : t('admin.common.could_not_save_configuration');
		} finally {
			saving = false;
		}
	}
</script>

<h1 class="mb-6 page-title">{t('admin.settings.bbs_settings')}</h1>

{#if loadError}
	<p class="text-sm text-red-400">{loadError}</p>
{:else if !config}
	<p class="text-sm text-slate-400">{t('web.common.loading')}</p>
{:else}
	<form class="flex flex-col gap-6" onsubmit={handleSubmit}>
		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<h2 class="card-label">{t('admin.settings.general')}</h2>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.common.bbs_name')}</span>
				<input
					class="field"
					bind:value={config.name}
					required
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.settings.sysop_name')}</span>
				<input
					class="field"
					bind:value={config.sysop}
					required
				/>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('common.location')}</span>
				<input class="field" bind:value={config.location} placeholder={t('admin.settings.neunkirch_switzerland')} />
				<span class="text-xs text-slate-500">
					{t('admin.settings.sent_to_other_systems_in')}
				</span>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.settings.new_user_security_level_0')}</span>
				<SLSelect class="field" bind:value={config.new_user_sl} />
			</label>
			<label class="flex items-start gap-2 text-sm">
				<input type="checkbox" class="check mt-0.5" bind:checked={config.public_feeds} />
				<span>
					<span class="text-slate-400">{t('admin.settings.public_rss_feeds')}</span>
					<span class="block text-xs text-slate-500">
						{t('admin.settings.a_feed_of_the_newest')}
					</span>
				</span>
			</label>
			<label class="flex items-start gap-2 text-sm">
				<input type="checkbox" class="check mt-0.5" bind:checked={config.monthly_recap} />
				<span>
					<span class="text-slate-400">{t('admin.settings.monthly_recap')}</span>
					<span class="block text-xs text-slate-500">
						{t('admin.settings.on_the_1st_a_netmail')}
					</span>
				</span>
			</label>
		</section>

		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<h2 class="card-label">{t('admin.common.telnet')}</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={config.telnet_enabled} />
				<span class="text-slate-400">{t('admin.common.enabled')}</span>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.settings.listen_address')}</span>
				<input
					class="field font-mono"
					bind:value={config.telnet_addr}
					placeholder=":2323"
				/>
			</label>
		</section>

		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<h2 class="card-label">SSH</h2>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={config.ssh_enabled} />
				<span class="text-slate-400">{t('admin.common.enabled')}</span>
			</label>
			<label class="flex flex-col gap-1 text-sm">
				<span class="text-slate-400">{t('admin.settings.listen_address')}</span>
				<input
					class="field font-mono"
					bind:value={config.ssh_addr}
					placeholder=":2222"
				/>
			</label>
		</section>

		<section class="flex flex-col gap-4 rounded-xl border border-line p-4">
			<h2 class="card-label">{t('common.interbbs_last_callers')}</h2>
			<p class="text-xs leading-relaxed text-slate-500">
				{t('admin.settings.boards_of_a_network_post')}
			</p>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={config.last_callers.enabled} />
				<span class="text-slate-400">{t('admin.settings.take_part_post_a_record')}</span>
			</label>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="check" bind:checked={config.last_callers.show_at_login} />
				<span class="text-slate-400">{t('admin.settings.show_the_list_to_callers')}</span>
			</label>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">{t('admin.settings.data_echo')}</span>
					<input class="field font-mono" bind:value={config.last_callers.area} placeholder="FSX_DAT" />
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">{t('admin.settings.your_address_as_shown')}</span>
					<input class="field font-mono" bind:value={config.last_callers.address} placeholder="bbs.example.org:2323" />
				</label>
				<label class="flex flex-col gap-1 text-sm">
					<span class="text-slate-400">{t('admin.common.system')}</span>
					<input class="field" bind:value={config.last_callers.system} placeholder={t('admin.common.linux')} />
				</label>
			</div>
			<p class="text-xs leading-relaxed text-slate-500">
				{t('admin.settings.a_caller_s_place_in')}
			</p>
		</section>

		{#if saveError}
			<p class="text-sm text-red-400">{saveError}</p>
		{/if}
		{#if saveNote}
			<p class="text-sm text-amber-400">{saveNote}</p>
		{/if}

		<button
			type="submit"
			disabled={saving}
			class="btn-primary"
		>
			{saving ? t('web.common.saving') : t('admin.common.save_changes')}
		</button>
	</form>
{/if}
