// Portal date/time display. Every portal page formats timestamps
// through here so they follow the caller's profile time zone -- the
// same zone Telnet/SSH shows them in -- falling back to the browser's
// own zone when none is set. (The admin UI keeps plain browser time.)
import { bbsAuth } from '$lib/bbs-auth.svelte';

function zoneOptions(): Intl.DateTimeFormatOptions {
	const timeZone = bbsAuth.timezone;
	if (!timeZone) return {};
	try {
		// Throws RangeError for a zone this browser doesn't know.
		new Intl.DateTimeFormat(undefined, { timeZone });
		return { timeZone };
	} catch {
		return {};
	}
}

/** Date and time with the zone's short name, e.g. for a message or file detail view. */
export function formatDateTime(iso: string): string {
	return new Date(iso).toLocaleString(undefined, {
		...zoneOptions(),
		dateStyle: 'medium',
		timeStyle: 'short'
	}) + ' ' + zoneName(iso);
}

/** Date only, e.g. for list rows. */
export function formatDate(iso: string): string {
	return new Date(iso).toLocaleDateString(undefined, zoneOptions());
}

function zoneName(iso: string): string {
	const part = new Intl.DateTimeFormat(undefined, { ...zoneOptions(), timeZoneName: 'short' })
		.formatToParts(new Date(iso))
		.find((p) => p.type === 'timeZoneName');
	return part?.value ?? '';
}

/** Every IANA zone this browser knows, for the profile picker. */
export function allTimezones(): string[] {
	try {
		return Intl.supportedValuesOf('timeZone');
	} catch {
		return ['UTC'];
	}
}

/** The browser's own zone, offered as a one-click choice on the profile page. */
export function browserTimezone(): string {
	return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
