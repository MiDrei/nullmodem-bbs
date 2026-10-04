<script lang="ts">
	import { t, ensureAdminTexts } from '$lib/i18n.svelte';
	// The sysop side wears the same design D chrome as the portal: the
	// board's name (tagged ADMIN), Dashboard and the grouped menus
	// (Areas, Screens, FTN, Users, System), the operator and "Log out"
	// on the right.
	import { onMount, onDestroy } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { site } from '$lib/site.svelte';
	import { adminTheme } from '$lib/theme.svelte';
	import { servicesState, serviceInfo } from '$lib/services.svelte';
	import LanguagePicker from '$lib/LanguagePicker.svelte';
	import Icon from '$lib/Icon.svelte';
	import Toaster from '$lib/Toaster.svelte';

	// Reached from the portal without a reload: fetch the admin's texts.
	ensureAdminTexts();

	let { children } = $props();

	onMount(() => {
		site.load();
		adminTheme.apply();
	});

	// Keep the daemons' status fresh while signed in, for the "restart
	// needed" banner below the header.
	$effect(() => {
		if (auth.username) return servicesState.start();
	});

	let restarting = $state(false);
	async function restartFromBanner(name: string) {
		restarting = true;
		try {
			await servicesState.restart(name, name === 'bbs' ? 'idle' : 'now');
		} catch {
			// The Services page shows the details.
		} finally {
			restarting = false;
		}
	}
	// The portal (reached via the "Portal" link) stays dark.
	onDestroy(() => {
		if (typeof document !== 'undefined') adminTheme.remove();
	});

	type IconName = import('svelte').ComponentProps<typeof Icon>['name'];

	// The admin menu: Dashboard, then groups that open as dropdowns --
	// what belongs together in one place (the FTN side apart from the
	// BBS's own content, users apart from running the system).
	type NavLink = { href: string; label: string; icon: IconName; exact?: boolean };
	type NavGroup = { label: string; icon: IconName; links: NavLink[] };

	// Derived: they follow a change of language.
	const dashboard: NavLink = $derived({ href: '/admin/dashboard', label: t('admin.nav.dashboard'), icon: 'dashboard' });

	const groups: NavGroup[] = $derived([
		{
			label: t('admin.nav.content'),
			icon: 'content',
			links: [
				{ href: '/admin/message-areas', label: t('admin.nav.message_areas'), icon: 'areas' },
				{ href: '/admin/file-areas', label: t('admin.nav.file_areas'), icon: 'files' },
				{ href: '/admin/pending-areas', label: t('admin.nav.pending_areas'), icon: 'pending' },
				{ href: '/admin/doors', label: t('admin.nav.doors'), icon: 'doors' },
				{ href: '/admin/menus', label: t('admin.nav.menus'), icon: 'menu' },
				{ href: '/admin/screens', label: t('admin.nav.screens'), icon: 'screens' },
				{ href: '/admin/languages', label: t('admin.nav.languages'), icon: 'language' },
				{ href: '/admin/designer', label: t('admin.nav.ansi_designer'), icon: 'designer' }
			]
		},
		{
			label: t('admin.nav.community'),
			icon: 'chat',
			links: [
				{ href: '/admin/chat', label: t('admin.nav.chat_one_liners'), icon: 'chat' },
				{ href: '/admin/polls', label: t('admin.nav.polls_bbs_list'), icon: 'poll' }
			]
		},
		{
			label: 'FTN',
			icon: 'binkp',
			links: [
				{ href: '/admin/binkp', label: t('admin.nav.networks_addresses'), icon: 'binkp', exact: true },
				{ href: '/admin/binkp/uplinks', label: t('admin.nav.uplinks_nodes_points'), icon: 'uplink' },
				{ href: '/admin/areafix', label: t('admin.nav.areafix_filefix'), icon: 'areafix' },
				{ href: '/admin/nodelists', label: t('admin.nav.nodelists'), icon: 'nodelist' },
				{ href: '/admin/netmail', label: t('admin.nav.undeliverable_netmail'), icon: 'undeliverable' },
				{ href: '/admin/archive', label: t('admin.nav.packet_analyzer'), icon: 'archive' }
			]
		},
		{
			label: t('admin.nav.users'),
			icon: 'users',
			links: [
				{ href: '/admin/users', label: t('admin.nav.users'), icon: 'users' },
				{ href: '/admin/security', label: t('admin.nav.security'), icon: 'lock' },
				{ href: '/admin/sl-matrix', label: t('admin.nav.sl_matrix'), icon: 'matrix' }
			]
		},
		{
			label: t('admin.nav.system'),
			icon: 'system',
			links: [
				{ href: '/admin/stats', label: t('admin.nav.statistics'), icon: 'chart' },
				{ href: '/admin/settings', label: t('admin.nav.settings'), icon: 'system' },
				{ href: '/admin/services', label: t('admin.nav.services_2'), icon: 'server' },
				{ href: '/admin/maintenance', label: t('admin.nav.maintenance'), icon: 'wrench' },
				{ href: '/admin/backups', label: t('admin.nav.backups'), icon: 'backup' },
				{ href: '/admin/logs', label: t('admin.nav.logs'), icon: 'logs' }
			]
		}
	]);

	function active(l: NavLink): boolean {
		const path = page.url.pathname;
		return path === l.href || (!l.exact && path.startsWith(l.href + '/'));
	}

	function groupActive(g: NavGroup): boolean {
		return g.links.some(active);
	}

	// The open dropdown's label, or null.
	let openGroup = $state<string | null>(null);

	function logout() {
		auth.clear();
		goto('/admin/login');
	}

	function closeMenus() {
		openGroup = null;
	}

	// clickOutside closes the System dropdown on any click that lands
	// outside it -- the layout persists across route changes, so
	// without this the menu would stay open after navigating.
	function clickOutside(node: HTMLElement, callback: () => void) {
		function handleClick(event: MouseEvent) {
			if (!node.contains(event.target as Node)) callback();
		}
		document.addEventListener('click', handleClick, true);
		return {
			destroy() {
				document.removeEventListener('click', handleClick, true);
			}
		};
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>{t('admin.nav.name_admin', { NAME: site.info.name })}</title>
</svelte:head>

<div class="flex min-h-screen flex-col bg-ground text-ink">
	<header
		class="flex flex-wrap items-center justify-between gap-x-8 gap-y-3 border-b border-line px-6 py-5 md:px-10"
	>
		<a
			href={auth.username ? '/admin/dashboard' : '/admin/login'}
			class="flex items-center gap-2.5 text-[15px] font-bold tracking-tight text-ink hover:text-ink"
		>
			{site.info.name}
			<span
				class="rounded-md border border-line-strong px-1.5 py-0.5 font-mono text-[9.5px] font-medium tracking-[0.1em] text-muted"
			>
				ADMIN
			</span>
		</a>

		{#if auth.username}
			<nav
				class="flex flex-wrap items-center gap-x-6 gap-y-2 text-[13px] text-muted"
				use:clickOutside={closeMenus}
			>
				<a
					href={dashboard.href}
					class="flex items-center gap-1.5 transition-colors hover:text-accent {active(dashboard)
						? 'text-accent'
						: ''}"
					aria-current={active(dashboard) ? 'page' : undefined}
				>
					<Icon name={dashboard.icon} />{dashboard.label}
				</a>
				{#each groups as g (g.label)}
					<div class="relative">
						<button
							type="button"
							class="flex items-center gap-1.5 transition-colors hover:text-accent {groupActive(g) ||
							openGroup === g.label
								? 'text-accent'
								: ''}"
							aria-expanded={openGroup === g.label}
							onclick={() => (openGroup = openGroup === g.label ? null : g.label)}
						>
							<Icon name={g.icon} />{g.label}<Icon name="chevron" size={12} />
						</button>
						{#if openGroup === g.label}
							<div
								class="absolute top-full left-0 z-20 mt-3 w-60 rounded-xl border border-line-strong bg-surface p-1.5 shadow-2xl shadow-black/50"
							>
								{#each g.links as l (l.href)}
									<a
										href={l.href}
										onclick={closeMenus}
										class="flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-[13px] transition-colors hover:bg-slate-800 hover:text-accent {active(
											l
										)
											? 'text-accent'
											: 'text-ink-soft'}"
										aria-current={active(l) ? 'page' : undefined}
									>
										<Icon name={l.icon} />{l.label}
									</a>
								{/each}
							</div>
						{/if}
					</div>
				{/each}
			</nav>

			<div class="flex items-center gap-2.5 text-[13px] text-faint">
				<button
					type="button"
					class="flex items-center transition-colors hover:text-accent"
					onclick={() => adminTheme.toggle()}
					title={adminTheme.mode === 'light' ? t('admin.nav.switch_to_dark_mode') : t('admin.nav.switch_to_light_mode')}
					aria-label={adminTheme.mode === 'light' ? t('admin.nav.switch_to_dark_mode') : t('admin.nav.switch_to_light_mode')}
				>
					<Icon name={adminTheme.mode === 'light' ? 'moon' : 'sun'} />
				</button>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<LanguagePicker compact />
				<span class="text-line-strong" aria-hidden="true">·</span>
				<a href="/message-areas" class="transition-colors hover:text-accent">{t('admin.nav.portal')}</a>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<span>{auth.username}</span>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<button class="transition-colors hover:text-accent" onclick={logout}>{t('admin.nav.log_out')}</button>
			</div>
		{/if}
	</header>

	{#if auth.username && servicesState.needingRestart.length > 0}
		<div class="border-b border-amber-500/30 bg-amber-950 px-6 py-2.5 text-[13px] md:px-10">
			<div class="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2">
				<span class="text-amber-300">{t('admin.nav.saved_changes_wait_for_a')}</span>
				{#each servicesState.needingRestart as svc (svc.name)}
					<span class="flex items-center gap-2">
						<span class="text-ink" title={svc.restart_needed.join('; ')}>
							<strong>{serviceInfo(svc.name).title}</strong>
							<span class="text-muted">({svc.restart_needed.join(', ')})</span>
						</span>
						<button
							class="btn-primary btn-xs"
							disabled={restarting}
							onclick={() => restartFromBanner(svc.name)}
							title={svc.name === 'bbs' ? t('admin.nav.restarts_as_soon_as_no') : ''}
						>
							{svc.name === 'bbs' ? t('admin.nav.restart_when_idle') : t('admin.common.restart')}
						</button>
					</span>
				{/each}
				<a href="/admin/services" class="ml-auto text-xs text-muted hover:text-accent">{t('admin.nav.services')}</a>
			</div>
		</div>
	{/if}

	<main class="mx-auto w-full max-w-6xl flex-1 px-6 py-7 md:px-10">
		{@render children()}
	</main>

	<footer
		class="flex justify-between gap-4 border-t border-line px-6 py-4 font-mono text-[10.5px] text-dim md:px-10"
	>
		<span>{t('admin.nav.nullmodem_bbs_v_sysop', { V: site.info.version ? ` v${site.info.version}` : '' })}</span>
		{#if site.telnetAddress}
			<span>{t('admin.nav.telnet_telnetaddress', { TELNETADDRESS: site.telnetAddress })}</span>
		{/if}
	</footer>
</div>
<Toaster />
