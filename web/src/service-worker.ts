/// <reference types="@sveltejs/kit" />
/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />

// The mobile reader's service worker (registered with scope /reader --
// not /reader/: SvelteKit shows the start page as /reader --
// see lib/reader/offline.svelte.ts; the portal and admin don't use it):
//
//  - The app itself is kept, so the reader opens without a network.
//  - API reads go to the network first and are kept; without a
//    network the kept answer is used. The reader fetches ahead what's
//    unread (?peek=1, not marking it read) to have it for later.
//  - A message read from what was kept is marked read once the
//    network is back (pending reads, replayed with the same login).
//  - Notifications (web push) are shown, and a tap opens the reader
//    at the message.
import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;

const SHELL = `reader-shell-${version}`;
const API = 'reader-api';
const META = 'reader-meta';
const PENDING_READS = '/__reader/pending-reads';
// How long a read waits for the network before taking what was kept.
const NETWORK_PATIENCE = 6000;

// The app's own files, plus the reader's icons and manifest.
const SHELL_FILES = [...build, ...files.filter((f) => f.startsWith('/reader/')), '/reader/'];

sw.addEventListener('install', (event) => {
	event.waitUntil(
		caches
			.open(SHELL)
			.then((c) => c.addAll(SHELL_FILES))
			.then(() => sw.skipWaiting())
	);
});

sw.addEventListener('activate', (event) => {
	event.waitUntil(
		(async () => {
			for (const key of await caches.keys()) {
				if (key.startsWith('reader-shell-') && key !== SHELL) await caches.delete(key);
			}
			await sw.clients.claim();
		})()
	);
});

/** The key a message is kept under: without ?peek, so what was
 * fetched ahead is found when it's read. */
function apiKey(url: URL): string {
	const u = new URL(url);
	u.searchParams.delete('peek');
	return u.pathname + u.search;
}

/** A single message or netmail: what reading it offline must mark read later. */
function isMessage(url: URL): boolean {
	return /^\/api\/bbs\/(messages|netmail)\/\d+$/.test(url.pathname);
}

sw.addEventListener('fetch', (event) => {
	const req = event.request;
	const url = new URL(req.url);
	if (url.origin !== sw.location.origin) return;

	if (req.mode === 'navigate' && url.pathname.startsWith('/reader')) {
		event.respondWith(navigate(req));
		return;
	}
	if (req.method !== 'GET') return;
	// Live things (push setup, chat) always go to the network.
	if (url.pathname.startsWith('/api/bbs/') && !url.pathname.startsWith('/api/bbs/push/') && !url.pathname.startsWith('/api/bbs/chat/')) {
		event.respondWith(apiGet(req, url));
		return;
	}
	if (build.includes(url.pathname) || files.includes(url.pathname)) {
		event.respondWith(caches.match(req).then((r) => r ?? fetch(req)));
	}
});

async function navigate(req: Request): Promise<Response> {
	try {
		const res = await fetch(req);
		if (res.ok) (await caches.open(SHELL)).put('/reader/', res.clone());
		return res;
	} catch {
		// Every reader page is the same app page.
		return (await caches.match('/reader/')) ?? Response.error();
	}
}

async function apiGet(req: Request, url: URL): Promise<Response> {
	const cache = await caches.open(API);
	const key = apiKey(url);
	const network = fetch(req).then(async (res) => {
		if (res.ok) {
			await cache.put(key, res.clone());
			// Online again: mark what was read meanwhile.
			replayPendingReads();
		}
		return res;
	});
	try {
		// A bad connection (a train) answers late or never: after a
		// while the kept answer is taken, if there is one.
		return await Promise.race([
			network,
			new Promise<Response>((resolve, reject) =>
				setTimeout(async () => {
					const kept = await cache.match(key);
					if (!kept) return network.then(resolve, reject);
					resolve(kept);
					// Read from what was kept: if the network never
					// answers, it's marked read later.
					if (isMessage(url) && !url.searchParams.has('peek')) {
						network.catch(() => addPendingRead(key, req.headers.get('Authorization') ?? ''));
					}
				}, NETWORK_PATIENCE)
			)
		]);
	} catch {
		const kept = (await cache.match(key)) ?? (await keptListPage(cache, url));
		if (!kept) {
			return new Response(JSON.stringify({ error: "You're offline, and this wasn't fetched ahead." }), {
				status: 503,
				headers: { 'Content-Type': 'application/json' }
			});
		}
		if (isMessage(url) && !url.searchParams.has('peek')) {
			await addPendingRead(key, req.headers.get('Authorization') ?? '');
		}
		return kept;
	}
}

/** An area's message list from another offset: reading moves the
 * first unread on, so the reader asks for a page that wasn't kept --
 * a page near it is better than nothing. */
async function keptListPage(cache: Cache, url: URL): Promise<Response | undefined> {
	if (!/^\/api\/bbs\/message-areas\/\d+\/messages$/.test(url.pathname)) return undefined;
	for (const req of await cache.keys()) {
		if (new URL(req.url).pathname === url.pathname) return cache.match(req);
	}
	return undefined;
}

type PendingRead = { url: string; auth: string };

async function pendingReads(): Promise<PendingRead[]> {
	const res = await (await caches.open(META)).match(PENDING_READS);
	return res ? ((await res.json()) as PendingRead[]) : [];
}

async function savePendingReads(list: PendingRead[]) {
	await (await caches.open(META)).put(PENDING_READS, new Response(JSON.stringify(list)));
}

async function addPendingRead(url: string, auth: string) {
	const list = await pendingReads();
	if (!list.some((p) => p.url === url)) list.push({ url, auth });
	await savePendingReads(list);
}

let replaying = false;

async function replayPendingReads() {
	if (replaying) return;
	replaying = true;
	try {
		const left: PendingRead[] = [];
		for (const p of await pendingReads()) {
			try {
				// Fetched again without ?peek: that marks it read.
				const res = await fetch(p.url, { headers: { Authorization: p.auth } });
				// A login that has expired meanwhile: give up on it.
				if (!res.ok && res.status !== 401 && res.status !== 403 && res.status !== 404) left.push(p);
			} catch {
				left.push(p);
			}
		}
		await savePendingReads(left);
	} finally {
		replaying = false;
	}
}

sw.addEventListener('message', (event) => {
	// Logged out: what was kept belongs to that login.
	if (event.data?.type === 'logout') {
		event.waitUntil(Promise.all([caches.delete(API), caches.delete(META)]));
	}
});

sw.addEventListener('push', (event) => {
	let n: { title?: string; body?: string; url?: string; tag?: string } = {};
	try {
		n = event.data?.json() ?? {};
	} catch {
		n = { body: event.data?.text() };
	}
	event.waitUntil(
		sw.registration.showNotification(n.title || 'New mail', {
			body: n.body ?? '',
			tag: n.tag,
			icon: '/reader/icon-192.png',
			badge: '/reader/icon-192.png',
			data: { url: n.url || '/reader/' }
		})
	);
});

sw.addEventListener('notificationclick', (event) => {
	event.notification.close();
	const url = (event.notification.data?.url as string) || '/reader/';
	event.waitUntil(
		(async () => {
			const open = await sw.clients.matchAll({ type: 'window', includeUncontrolled: true });
			for (const c of open) {
				if (new URL(c.url).pathname.startsWith('/reader')) {
					await (c as WindowClient).focus();
					return (c as WindowClient).navigate(url);
				}
			}
			return sw.clients.openWindow(url);
		})()
	);
});
