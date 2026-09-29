<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toast } from '$lib/toast.svelte';
	import { ApiError, type ServiceStatus } from '$lib/api';
	import { servicesState, serviceInfo } from '$lib/services.svelte';

	let busy = $state<string | null>(null);

	onMount(() => {
		if (!auth.token) {
			goto('/admin/login');
			return;
		}
		return servicesState.start();
	});

	function since(iso?: string): string {
		if (!iso) return '—';
		const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
		if (secs < 60) return `${secs}s`;
		const mins = Math.floor(secs / 60);
		if (mins < 60) return `${mins}m`;
		const hours = Math.floor(mins / 60);
		if (hours < 48) return `${hours}h ${mins % 60}m`;
		return `${Math.floor(hours / 24)}d ${hours % 24}h`;
	}

	function statusOf(s: ServiceStatus): { label: string; tone: string } {
		if (s.restart_pending) {
			return s.restart_mode === 'idle' && s.running
				? { label: 'Restart when idle', tone: 'amber' }
				: { label: 'Restarting…', tone: 'amber' };
		}
		if (s.running) return { label: 'Running', tone: 'green' };
		if (!s.started_at) return { label: 'Never started', tone: 'grey' };
		return { label: 'Not responding', tone: 'red' };
	}

	async function restart(s: ServiceStatus, mode: 'now' | 'idle') {
		if (s.name === 'bbs' && mode === 'now' && s.online > 0) {
			if (!confirm(`Restart the BBS now? ${s.online} caller(s) online will be disconnected.`)) return;
		}
		busy = s.name;
		try {
			await servicesState.restart(s.name, mode);
			toast.push(
				mode === 'idle' ? 'The BBS restarts as soon as nobody is online.' : `Restarting ${serviceInfo(s.name).title}…`,
				'success'
			);
		} catch (err) {
			if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
				auth.clear();
				await goto('/admin/login');
				return;
			}
			toast.push(err instanceof ApiError ? err.message : 'Could not request the restart.', 'error');
		} finally {
			busy = null;
		}
	}
</script>

<div class="mb-6">
	<h1 class="page-title">Services</h1>
	<p class="page-subtitle max-w-2xl leading-relaxed">
		The three daemons behind the BBS, and the background programs of doors that need one. Some settings are only read when a daemon starts -- after
		saving those, the daemon is marked here and a banner offers the restart. A restarted daemon is
		back within a few seconds.
	</p>
</div>

{#if !servicesState.loaded}
	<p class="text-sm text-muted">Loading…</p>
{:else}
	<div class="flex flex-col gap-3">
		{#each servicesState.list as s (s.name)}
			{@const st = statusOf(s)}
			<div class="card flex flex-wrap items-start justify-between gap-4 p-5">
				<div class="min-w-0 flex-1">
					<div class="flex items-center gap-2.5">
						<span
							class="inline-block h-2.5 w-2.5 rounded-full {st.tone === 'green'
								? 'bg-emerald-500'
								: st.tone === 'amber'
									? 'bg-amber-500'
									: st.tone === 'red'
										? 'bg-red-500'
										: 'bg-slate-600'}"
							aria-hidden="true"
						></span>
						<span class="text-[15px] font-semibold text-ink-strong">{serviceInfo(s.name).title}</span>
						<span class="text-[13px] text-muted">{st.label}</span>
					</div>
					<p class="mt-1 text-[13px] text-muted">{serviceInfo(s.name).description}</p>
					<div class="mt-2 font-mono text-[11px] text-faint">
						{s.version ? (s.name.startsWith('door:') ? s.version : `v${s.version}`) : '—'} · up {since(s.started_at)} · pid {s.pid || '—'}{s.name ===
						'bbs'
							? ` · ${s.online} online`
							: ''}
					</div>
					{#if s.restart_needed.length > 0 && !s.restart_pending}
						<div class="mt-3 flex flex-wrap gap-1.5">
							{#each s.restart_needed as reason (reason)}
								<span
									class="rounded-md border border-amber-500/40 px-2 py-0.5 text-xs text-amber-400"
									>Needs restart: {reason}</span
								>
							{/each}
						</div>
					{/if}
				</div>
				<div class="flex shrink-0 flex-wrap justify-end gap-2">
					{#if s.name === 'bbs'}
						<button
							class="btn-secondary btn-sm"
							disabled={busy !== null || s.restart_pending}
							onclick={() => restart(s, 'idle')}
							title="Restart as soon as no caller is online"
						>
							Restart when idle
						</button>
					{/if}
					<button
						class="{s.restart_needed.length > 0 ? 'btn-primary' : 'btn-secondary'} btn-sm"
						disabled={busy !== null || (s.restart_pending && s.restart_mode !== 'idle')}
						onclick={() => restart(s, 'now')}
					>
						Restart now
					</button>
				</div>
			</div>
		{/each}
	</div>
	<p class="mt-5 text-xs leading-relaxed text-faint">
		A daemon restarts by ending itself; Docker (restart policy "unless-stopped") starts it again. A
		door's background program is restarted by the BBS daemon, which runs it. The
		mailer finishes a poll round and inbound sessions in progress first. Without Docker, a daemon
		stopped this way has to be started again by hand.
	</p>
{/if}
