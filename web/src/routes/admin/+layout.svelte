<script lang="ts">
	// The sysop side wears the same design D chrome as the portal: the
	// board's name (tagged ADMIN), icon links with the less-used tools
	// folded into a System menu, the operator and "Log out" on the right.
	import { onMount, onDestroy } from 'svelte';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { site } from '$lib/site.svelte';
	import { adminTheme } from '$lib/theme.svelte';
	import { servicesState, SERVICE_INFO } from '$lib/services.svelte';
	import Icon from '$lib/Icon.svelte';
	import Toaster from '$lib/Toaster.svelte';

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

	const mainLinks: { href: string; label: string; icon: IconName }[] = [
		{ href: '/admin/dashboard', label: 'Dashboard', icon: 'dashboard' },
		{ href: '/admin/message-areas', label: 'Message Areas', icon: 'areas' },
		{ href: '/admin/file-areas', label: 'File Areas', icon: 'files' },
		{ href: '/admin/doors', label: 'Doors', icon: 'doors' },
		{ href: '/admin/designer', label: 'Designer', icon: 'designer' },
		{ href: '/admin/screens', label: 'Screens', icon: 'screens' }
	];

	// null marks a divider; an entry without an icon is indented under
	// the one above it.
	const systemLinks: ({ href: string; label: string; icon?: IconName } | null)[] = [
		{ href: '/admin/users', label: 'Users', icon: 'users' },
		{ href: '/admin/sl-matrix', label: 'SL Matrix', icon: 'shield' },
		{ href: '/admin/services', label: 'Services', icon: 'dashboard' },
		{ href: '/admin/logs', label: 'Logs', icon: 'logs' },
		{ href: '/admin/binkp', label: 'BinkP', icon: 'binkp' },
		{ href: '/admin/binkp/uplinks', label: 'Uplinks (Nodes/Points)' },
		{ href: '/admin/areafix', label: 'Areafix / Filefix', icon: 'areafix' },
		{ href: '/admin/pending-areas', label: 'Pending Areas', icon: 'pending' },
		{ href: '/admin/netmail', label: 'Undeliverable Netmail', icon: 'undeliverable' },
		{ href: '/admin/archive', label: 'Packet Analyzer', icon: 'archive' },
		null,
		{ href: '/admin/settings', label: 'Settings', icon: 'system' }
	];

	function active(href: string): boolean {
		return page.url.pathname === href || page.url.pathname.startsWith(href + '/');
	}

	let systemActive = $derived(systemLinks.some((l) => l && active(l.href)));

	let systemMenuOpen = $state(false);

	function logout() {
		auth.clear();
		goto('/admin/login');
	}

	function closeSystemMenu() {
		systemMenuOpen = false;
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
	<title>{site.info.name} Admin</title>
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
			<nav class="flex flex-wrap items-center gap-x-6 gap-y-2 text-[13px] text-muted">
				{#each mainLinks as l (l.href)}
					<a
						href={l.href}
						class="flex items-center gap-1.5 transition-colors hover:text-accent {active(l.href)
							? 'text-accent'
							: ''}"
						aria-current={active(l.href) ? 'page' : undefined}
					>
						<Icon name={l.icon} />{l.label}
					</a>
				{/each}
				<div class="relative" use:clickOutside={closeSystemMenu}>
					<button
						type="button"
						class="flex items-center gap-1.5 transition-colors hover:text-accent {systemActive ||
						systemMenuOpen
							? 'text-accent'
							: ''}"
						aria-expanded={systemMenuOpen}
						onclick={() => (systemMenuOpen = !systemMenuOpen)}
					>
						<Icon name="system" />System<Icon name="chevron" size={12} />
					</button>
					{#if systemMenuOpen}
						<div
							class="absolute top-full left-1/2 z-20 mt-3 w-56 -translate-x-1/2 rounded-xl border border-line-strong bg-surface p-1.5 shadow-2xl shadow-black/50 md:right-0 md:left-auto md:translate-x-0"
						>
							{#each systemLinks as l, i (l?.href ?? `divider-${i}`)}
								{#if l === null}
									<div class="my-1.5 border-t border-line"></div>
								{:else}
									<a
										href={l.href}
										onclick={closeSystemMenu}
										class="flex items-center gap-2.5 rounded-lg py-2 text-[13px] transition-colors hover:bg-slate-800 hover:text-accent {l.icon
											? 'px-2.5'
											: 'pr-2.5 pl-[2.1rem] text-[12.5px]'} {active(l.href) &&
										!(l.href === '/admin/binkp' && active('/admin/binkp/uplinks'))
											? 'text-accent'
											: l.icon
												? 'text-ink-soft'
												: 'text-muted'}"
									>
										{#if l.icon}<Icon name={l.icon} />{/if}{l.label}
									</a>
								{/if}
							{/each}
						</div>
					{/if}
				</div>
			</nav>

			<div class="flex items-center gap-2.5 text-[13px] text-faint">
				<button
					type="button"
					class="flex items-center transition-colors hover:text-accent"
					onclick={() => adminTheme.toggle()}
					title={adminTheme.mode === 'light' ? 'Switch to dark mode' : 'Switch to light mode'}
					aria-label={adminTheme.mode === 'light' ? 'Switch to dark mode' : 'Switch to light mode'}
				>
					<Icon name={adminTheme.mode === 'light' ? 'moon' : 'sun'} />
				</button>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<a href="/message-areas" class="transition-colors hover:text-accent">Portal</a>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<span>{auth.username}</span>
				<span class="text-line-strong" aria-hidden="true">·</span>
				<button class="transition-colors hover:text-accent" onclick={logout}>Log out</button>
			</div>
		{/if}
	</header>

	{#if auth.username && servicesState.needingRestart.length > 0}
		<div class="border-b border-amber-500/30 bg-amber-950 px-6 py-2.5 text-[13px] md:px-10">
			<div class="mx-auto flex max-w-6xl flex-wrap items-center gap-x-4 gap-y-2">
				<span class="text-amber-300">Saved changes wait for a restart:</span>
				{#each servicesState.needingRestart as svc (svc.name)}
					<span class="flex items-center gap-2">
						<span class="text-ink" title={svc.restart_needed.join('; ')}>
							<strong>{SERVICE_INFO[svc.name].title}</strong>
							<span class="text-muted">({svc.restart_needed.join(', ')})</span>
						</span>
						<button
							class="btn-primary btn-xs"
							disabled={restarting}
							onclick={() => restartFromBanner(svc.name)}
							title={svc.name === 'bbs' ? 'Restarts as soon as no caller is online' : ''}
						>
							{svc.name === 'bbs' ? 'Restart when idle' : 'Restart'}
						</button>
					</span>
				{/each}
				<a href="/admin/services" class="ml-auto text-xs text-muted hover:text-accent">Services →</a>
			</div>
		</div>
	{/if}

	<main class="mx-auto w-full max-w-6xl flex-1 px-6 py-7 md:px-10">
		{@render children()}
	</main>

	<footer
		class="flex justify-between gap-4 border-t border-line px-6 py-4 font-mono text-[10.5px] text-dim md:px-10"
	>
		<span>NullModem BBS{site.info.version ? ` v${site.info.version}` : ''} · sysop</span>
		{#if site.telnetAddress}
			<span>telnet · {site.telnetAddress}</span>
		{/if}
	</footer>
</div>
<Toaster />
