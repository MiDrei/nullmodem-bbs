// Small, deterministic "who is this" visual helper for compact list
// rows across the BBS portal (message/netmail senders, file
// uploaders) -- a colored initial circle instead of repeating the
// full name at every row, playful rather than the admin panel's flat
// plain-text lists.
const PALETTE = [
	'from-cyan-400 to-blue-500',
	'from-fuchsia-400 to-pink-500',
	'from-amber-400 to-orange-500',
	'from-emerald-400 to-teal-500',
	'from-violet-400 to-purple-500',
	'from-rose-400 to-red-500'
];

function hash(s: string): number {
	let h = 0;
	for (let i = 0; i < s.length; i++) {
		h = (h * 31 + s.charCodeAt(i)) | 0;
	}
	return Math.abs(h);
}

/** A "from-X to-Y" Tailwind gradient stop pair, stable for the same name. */
export function avatarGradient(name: string): string {
	if (!name) return PALETTE[0];
	return PALETTE[hash(name) % PALETTE.length];
}

/** One or two letters to show inside the avatar circle. */
export function initials(name: string): string {
	const trimmed = name.trim();
	if (!trimmed) return '?';
	return trimmed[0]!.toUpperCase();
}
