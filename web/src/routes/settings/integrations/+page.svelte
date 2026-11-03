<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Integration } from '#lib/api.ts';
	import IntegrationCard from '#lib/components/IntegrationCard.svelte';
	import { toasts } from '#lib/toasts.svelte.ts';

	let oauthMessage = $state('');
	let list = $state<Integration[] | null>(null);

	onMount(() => {
		const status = new URLSearchParams(window.location.search).get('google');
		if (status) {
			oauthMessage = status === 'connected' ? 'Google Calendar connected. Turn it on and choose Pull today to import events.'
				: status === 'denied' ? 'Google sign-in was cancelled or access was denied. You can try again.'
				: 'Google sign-in failed. Check your client credentials and redirect URI, then reconnect and allow calendar access.';
			window.history.replaceState(null, '', window.location.pathname);
		}
		api.integrations().then(
			(l) => (list = l),
			(err) => toasts.error(err)
		);
	});
</script>

{#if oauthMessage}<p role="status">{oauthMessage}</p>{/if}

{#if list}
	{#each list as integration, i (integration.id)}
		<IntegrationCard bind:integration={list[i]} />
	{/each}
{:else}
	<p class="muted">Loading…</p>
{/if}
