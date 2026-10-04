// Shared bits of the mobile reader (routes/reader): it uses the BBS
// portal's session (bbsAuth) and API, but none of the portal's pages.
import { goto } from '$app/navigation';
import { bbsAuth } from '$lib/bbs-auth.svelte';
import { ApiError } from '$lib/api';
import { i18n } from '$lib/i18n.svelte';

/** The session token, or null after sending the reader to its login. */
export async function readerToken(): Promise<string | null> {
	if (!bbsAuth.token) {
		await goto('/reader/login', { replaceState: true });
		return null;
	}
	return bbsAuth.token;
}

/** Handles an expired session (back to login); true if it was one. */
export async function readerAuthFailed(err: unknown): Promise<boolean> {
	if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
		bbsAuth.clear();
		await goto('/reader/login', { replaceState: true });
		return true;
	}
	return false;
}

export function errorText(err: unknown, fallback: string): string {
	return err instanceof ApiError ? err.message : fallback;
}

/** A short date: time for today, else day and month. */
export function shortDate(iso: string): string {
	const d = new Date(iso);
	if (isNaN(d.getTime())) return '';
	const tz = bbsAuth.timezone;
	const now = new Date();
	const sameDay = d.toLocaleDateString('en-CA', { timeZone: tz }) === now.toLocaleDateString('en-CA', { timeZone: tz });
	return sameDay
		? d.toLocaleTimeString(i18n.locale, { hour: '2-digit', minute: '2-digit', timeZone: tz })
		: d.toLocaleDateString(i18n.locale, { day: 'numeric', month: 'short', timeZone: tz });
}
