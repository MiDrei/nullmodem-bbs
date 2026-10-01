// The reader's notifications on this device: the browser's push
// subscription (through the reader's service worker) registered with
// the BBS, which sends to it when mail arrives (internal/push).
import { deletePushSubscription, getPushKey, savePushSubscription, type PushPrefs } from '$lib/api';

/** Why notifications can't be had here, or null if they can. */
export function pushUnavailable(): string | null {
	if (typeof window === 'undefined') return 'Not available.';
	const standalone =
		window.matchMedia('(display-mode: standalone)').matches ||
		(navigator as Navigator & { standalone?: boolean }).standalone === true;
	const ios = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.maxTouchPoints > 1 && /Mac/.test(navigator.userAgent));
	if (ios && !standalone) return 'On iPhone and iPad, notifications need the reader on the home screen: Share → Add to Home Screen, then open it from there.';
	if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
		return "This browser can't show notifications.";
	}
	return null;
}

async function registration(): Promise<ServiceWorkerRegistration> {
	const reg = await navigator.serviceWorker.getRegistration('/reader');
	if (!reg) throw new Error('The reader is still setting up; try again in a moment.');
	return reg;
}

/** This device's current push subscription, if any. */
export async function currentSubscription(): Promise<PushSubscription | null> {
	if (pushUnavailable()) return null;
	const reg = await navigator.serviceWorker.getRegistration('/reader');
	return (await reg?.pushManager.getSubscription()) ?? null;
}

function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
	const b64 = (base64url + '='.repeat((4 - (base64url.length % 4)) % 4)).replace(/-/g, '+').replace(/_/g, '/');
	const raw = atob(b64);
	const out = new Uint8Array(new ArrayBuffer(raw.length));
	for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
	return out;
}

/** Turns notifications on (asking for permission) or updates what they're for. */
export async function enablePush(token: string, prefs: PushPrefs): Promise<PushSubscription> {
	if ((await Notification.requestPermission()) !== 'granted') {
		throw new Error('Notifications are not allowed for the reader (see the settings).');
	}
	const reg = await registration();
	let sub = await reg.pushManager.getSubscription();
	if (!sub) {
		const { public_key } = await getPushKey(token);
		sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(public_key) });
	}
	await savePushSubscription(token, sub.toJSON(), prefs);
	return sub;
}

/** Turns notifications off on this device. */
export async function disablePush(token: string): Promise<void> {
	const sub = await currentSubscription();
	if (!sub) return;
	await deletePushSubscription(token, sub.endpoint).catch(() => {});
	await sub.unsubscribe();
}
