// Reading without a network (the service worker, src/service-worker.ts,
// keeps what was fetched): this fetches ahead what's unread, and keeps
// messages written offline in an outbox until they can be sent.
import {
	getFirstUnreadMessagePosition,
	listBBSMessageAreas,
	listBBSMessages,
	listBBSNetmail,
	postBBSMessage,
	sendBBSNetmail
} from '$lib/api';
import { bbsAuth } from '$lib/bbs-auth.svelte';
import { toast } from '$lib/toast.svelte';

/** The message list's page size (MessageListPane), so what's fetched ahead is what it asks for. */
export const LIST_PAGE = 40;
/** At most this many messages fetched ahead, so a big backlog doesn't fill the phone. */
const AHEAD_MAX = 300;
/** How often opening the area list fetches ahead again. */
const SYNC_EVERY = 3 * 60 * 1000;
const OUTBOX_KEY = 'nullmodem.reader.outbox';
const SYNCED_KEY = 'nullmodem.reader.syncedAt';

export const offline = $state({
	online: typeof navigator === 'undefined' ? true : navigator.onLine,
	syncing: false,
	/** When unread mail was last fetched ahead (ms), 0 if never. */
	syncedAt: 0,
	/** How many messages that fetched. */
	fetched: 0,
	outbox: 0
});

/** Registers the reader's service worker and keeps the online state. */
export function startOffline() {
	if (typeof window === 'undefined') return;
	if ('serviceWorker' in navigator) {
		navigator.serviceWorker
			.register('/service-worker.js', { scope: '/reader', type: import.meta.env.DEV ? 'module' : 'classic' })
			.catch(() => {
				// No offline reading then; everything else works.
			});
	}
	try {
		offline.syncedAt = Number(localStorage.getItem(SYNCED_KEY)) || 0;
	} catch {
		// Never synced then.
	}
	offline.outbox = readOutbox().length;
	const update = () => {
		offline.online = navigator.onLine;
		if (offline.online) flushOutbox();
	};
	window.addEventListener('online', update);
	window.addEventListener('offline', update);
	flushOutbox();
}

/** Logging out: what the service worker kept belongs to that login. */
export function forgetOffline() {
	navigator.serviceWorker?.controller?.postMessage({ type: 'logout' });
	try {
		localStorage.removeItem(SYNCED_KEY);
	} catch {
		// Nothing kept.
	}
	offline.syncedAt = 0;
}

/** Fetches ahead the unread netmail and, area by area, the page of
 * messages the reader opens with and the unread ones on it -- without
 * marking them read. */
export async function syncAhead(force = false): Promise<void> {
	const token = bbsAuth.token;
	if (!token || offline.syncing || !navigator.onLine || !navigator.serviceWorker?.controller) return;
	// The area list reloads after every message read: not each time.
	if (!force && Date.now() - offline.syncedAt < SYNC_EVERY) return;
	offline.syncing = true;
	let fetched = 0;
	try {
		const ids: string[] = [];
		const [areas, netmail] = await Promise.all([listBBSMessageAreas(token), listBBSNetmail(token)]);
		for (const m of netmail) if (m.unread) ids.push(`/api/bbs/netmail/${m.id}?peek=1`);
		for (const a of areas) {
			if (a.new <= 0 || ids.length >= AHEAD_MAX) continue;
			const pos = await getFirstUnreadMessagePosition(token, a.id);
			const page = await listBBSMessages(token, a.id, LIST_PAGE, Math.max(0, pos.position - 3));
			for (const m of page.messages) if (m.unread) ids.push(`/api/bbs/messages/${m.id}?peek=1`);
		}
		const todo = ids.slice(0, AHEAD_MAX);
		const kept = await caches.open('reader-api');
		let i = 0;
		const worker = async () => {
			while (i < todo.length) {
				const url = todo[i++];
				if (await kept.match(url.replace('?peek=1', ''))) {
					fetched++;
					continue;
				}
				const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
				if (res.ok) fetched++;
			}
		};
		await Promise.all([worker(), worker(), worker(), worker()]);
		offline.syncedAt = Date.now();
		offline.fetched = fetched;
		try {
			localStorage.setItem(SYNCED_KEY, String(offline.syncedAt));
		} catch {
			// Not remembered; harmless.
		}
	} catch {
		// Offline or failing midway: next time.
	} finally {
		offline.syncing = false;
	}
}

type Outgoing =
	| { kind: 'echo'; areaId: number; to: string; subject: string; body: string }
	| { kind: 'netmail'; to: string; toName: string; subject: string; body: string };

function readOutbox(): Outgoing[] {
	try {
		return JSON.parse(localStorage.getItem(OUTBOX_KEY) || '[]') as Outgoing[];
	} catch {
		return [];
	}
}

function writeOutbox(list: Outgoing[]) {
	try {
		localStorage.setItem(OUTBOX_KEY, JSON.stringify(list));
	} catch {
		// Can't keep it; the caller said it was kept, though -- rare enough.
	}
	offline.outbox = list.length;
}

/** fetch() failing outright: no network, not an answer from the BBS. */
function isNetworkError(err: unknown): boolean {
	return err instanceof TypeError || (typeof navigator !== 'undefined' && !navigator.onLine);
}

function deliver(token: string, m: Outgoing): Promise<unknown> {
	return m.kind === 'echo'
		? postBBSMessage(token, m.areaId, m.to, m.subject, m.body)
		: sendBBSNetmail(token, m.to, m.subject, m.body, m.toName);
}

/** Sends m, or keeps it in the outbox without a network; 'queued' then. */
export async function sendOrQueue(token: string, m: Outgoing): Promise<'sent' | 'queued'> {
	try {
		await deliver(token, m);
		return 'sent';
	} catch (err) {
		if (!isNetworkError(err)) throw err;
		writeOutbox([...readOutbox(), m]);
		return 'queued';
	}
}

let flushing = false;

/** Sends what's in the outbox, in order; stops at the first that can't go yet. */
export async function flushOutbox() {
	const token = bbsAuth.token;
	if (flushing || !token || !navigator.onLine) return;
	flushing = true;
	try {
		let list = readOutbox();
		let sent = 0;
		while (list.length) {
			try {
				await deliver(token, list[0]);
				sent++;
			} catch (err) {
				if (isNetworkError(err)) break;
				// Refused by the BBS (an area gone, no permission): it would never go.
				toast.push(`A message written offline couldn't be sent: "${list[0].subject}"`, 'error');
			}
			list = list.slice(1);
			writeOutbox(list);
		}
		if (sent) toast.push(sent === 1 ? 'Message written offline sent.' : `${sent} messages written offline sent.`, 'success');
	} finally {
		flushing = false;
	}
}
