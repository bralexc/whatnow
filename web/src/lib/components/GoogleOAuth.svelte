<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Integration, type IntegrationUpdate } from '#lib/api.ts';
	import { formFrom } from '#lib/integrations.ts';
	import { toasts } from '#lib/toasts.svelte.ts';

	let { integration = $bindable(), form = $bindable() }: { integration: Integration; form: IntegrationUpdate } = $props();
	let busy = $state(false);
	let redirectURI = $state('');
	onMount(() => { redirectURI = `${window.location.origin}/api/v1/integrations/gcal/oauth/callback`; });

	async function connect() {
		busy = true;
		try {
			integration = await api.saveIntegration(integration.id, form);
			form = formFrom(integration);
			const { url } = await api.startOAuth(integration.id, redirectURI);
			window.location.assign(url);
		} catch (err) {
			toasts.error(err);
		} finally {
			busy = false;
		}
	}

	async function disconnect() {
		busy = true;
		try {
			integration = await api.disconnectOAuth(integration.id);
		} catch (err) {
			toasts.error(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="oauth">
	<p class="muted">In Google Cloud, add this exact URL to your OAuth client's authorized redirect URIs:</p>
	<code>{redirectURI}</code>
	<p class="muted">Google sign-in grants read-only access to events. Your credentials stay in this machine's keychain.</p>
	<p role="status">{integration.secrets.refresh_token ? 'Google account connected' : 'Google account not connected'}</p>
	<div class="actions">
		<button type="button" class="btn" disabled={busy || !redirectURI} onclick={connect}>
			{busy ? 'Working…' : integration.secrets.refresh_token ? 'Reconnect with Google' : 'Save and connect with Google'}
		</button>
		{#if integration.secrets.refresh_token}
			<button type="button" class="btn" disabled={busy} onclick={disconnect}>Disconnect</button>
		{/if}
	</div>
	<p class="muted">Disconnect removes saved authorization from whatnow. You can also revoke access in your Google account's third-party connections.</p>
</div>

<style>
	.oauth { display: flex; flex-direction: column; gap: 8px; }
	p { margin: 0; font-size: 13px; }
	code { overflow-wrap: anywhere; font-size: 13px; }
	.actions { display: flex; flex-wrap: wrap; gap: 10px; }
</style>
