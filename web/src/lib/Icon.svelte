<script lang="ts">
	// The line icons of design D: 24px grid, 1.8 stroke, round caps,
	// drawn in currentColor so a link's colour carries through.
	type Name =
		| 'areas'
		| 'netmail'
		| 'files'
		| 'profile'
		| 'qwk'
		| 'logout'
		| 'dashboard'
		| 'designer'
		| 'screens'
		| 'system'
		| 'chevron'
		| 'users'
		| 'shield'
		| 'logs'
		| 'binkp'
		| 'areafix'
		| 'pending'
		| 'undeliverable'
		| 'archive'
		| 'doors'
		| 'sun'
		| 'moon';
	let { name, size = 15 }: { name: Name; size?: number } = $props();
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
	class="shrink-0"
>
	{#if name === 'areas'}
		<path
			d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"
		/>
	{:else if name === 'netmail'}
		<rect x="3" y="5" width="18" height="14" rx="2" />
		<path d="m3 7 9 6 9-6" />
	{:else if name === 'files'}
		<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" />
	{:else if name === 'profile'}
		<circle cx="12" cy="8" r="4" />
		<path d="M4 20c0-4 4-6 8-6s8 2 8 6" />
	{:else if name === 'qwk'}
		<path d="M21 8v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8" />
		<path d="M1 3h22v5H1z" />
		<path d="M10 12h4" />
	{:else if name === 'logout'}
		<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
		<path d="m16 17 5-5-5-5" />
		<path d="M21 12H9" />
	{:else if name === 'dashboard'}
		<rect x="3" y="3" width="8" height="8" rx="1" />
		<rect x="13" y="3" width="8" height="8" rx="1" />
		<rect x="3" y="13" width="8" height="8" rx="1" />
		<rect x="13" y="13" width="8" height="8" rx="1" />
	{:else if name === 'designer'}
		<path d="M4 16l4.5-9 4.5 9M6 12h5" />
		<path d="M14 5l6 6-6 6M20 11h-8" />
	{:else if name === 'screens'}
		<rect x="3" y="4" width="18" height="13" rx="1" />
		<path d="M8 20h8M12 17v3" />
	{:else if name === 'system'}
		<circle cx="12" cy="12" r="3" />
		<path d="M19.4 13a7.9 7.9 0 0 0 0-2l2-1.5-2-3.4-2.4 1a7.9 7.9 0 0 0-1.7-1L15 3h-4l-.3 2.6a7.9 7.9 0 0 0-1.7 1l-2.4-1-2 3.4L6.6 11a7.9 7.9 0 0 0 0 2l-2 1.5 2 3.4 2.4-1a7.9 7.9 0 0 0 1.7 1L11 21h4l.3-2.6a7.9 7.9 0 0 0 1.7-1l2.4 1 2-3.4-2-1.5z" />
	{:else if name === 'chevron'}
		<path d="M6 9l6 6 6-6" />
	{:else if name === 'users'}
		<circle cx="8" cy="8" r="3.2" />
		<path d="M2.5 19c0-3 2.5-5.4 5.5-5.4s5.5 2.4 5.5 5.4" />
		<circle cx="17" cy="8.5" r="2.6" />
		<path d="M14.8 13.8c2.6.3 4.7 2.5 4.7 5.2" />
	{:else if name === 'shield'}
		<path d="M12 3l7 3v5c0 4.5-3 7.8-7 9-4-1.2-7-4.5-7-9V6l7-3z" />
		<path d="M9 12l2 2 4-4" />
	{:else if name === 'logs'}
		<path d="M5 3h9l5 5v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z" />
		<path d="M14 3v5h5" />
		<path d="M8 13h8" />
		<path d="M8 17h8" />
	{:else if name === 'binkp'}
		<path d="M4 4l16 8-16 8V4z" />
		<path d="M4 12h6" />
	{:else if name === 'areafix'}
		<rect x="4" y="4" width="16" height="16" rx="2" />
		<path d="M9 9h6M9 12.5h6M9 16h3" />
	{:else if name === 'pending'}
		<circle cx="12" cy="12" r="9" />
		<path d="M12 7v5l3 3" />
	{:else if name === 'undeliverable'}
		<path d="M3 6h18v13a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6z" />
		<path d="M3 6l9 7 9-7" />
		<path d="M15 3.5l4 4" />
		<path d="M19 3.5l-4 4" />
	{:else if name === 'archive'}
		<rect x="3" y="4" width="18" height="5" rx="1" />
		<path d="M4 9v9a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1V9" />
		<path d="M10 13h4" />
	{:else if name === 'sun'}
		<circle cx="12" cy="12" r="4" />
		<path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
	{:else if name === 'moon'}
		<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
	{:else if name === 'doors'}
		<rect x="2" y="7" width="20" height="11" rx="4" />
		<path d="M7 10.5v4M5 12.5h4" />
		<circle cx="15.5" cy="11.5" r="1" />
		<circle cx="17.5" cy="13.5" r="1" />
	{/if}
</svg>
