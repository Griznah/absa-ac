<script lang="ts">
  import type { Config } from '../types'
  import { authStore } from '../lib/authStore'
  import { configStore } from '../lib/configStore'
  import GeneralSettings from './GeneralSettings.svelte'
  import CategoryEditor from './CategoryEditor.svelte'
  import ServerList from './ServerList.svelte'

  let toast = $state<{ message: string; type: 'success' | 'error' } | null>(null)

  async function handleSave(updates: Partial<Config>) {
    await configStore.save(updates)
    if ($configStore.error) {
      showToast($configStore.error, 'error')
    } else {
      showToast('Configuration saved successfully', 'success')
    }
  }

  function handleLogout() {
    authStore.logout()
    window.location.href = '/'
  }

  function showToast(message: string, type: 'success' | 'error') {
    toast = { message, type }
    setTimeout(() => (toast = null), 3000)
  }

  // Load config on mount
  $effect(() => {
    configStore.load()
  })
</script>

<div class="dashboard">
  <header class="header">
    <h1>ABSA AC Bot Configuration</h1>
    <button onclick={handleLogout} class="btn-logout">Logout</button>
  </header>

  {#if $configStore.loading}
    <div class="loading">Loading configuration...</div>
  {:else if $configStore.error}
    <div class="error">
      {$configStore.error}
      <button onclick={() => configStore.load()}>Retry</button>
    </div>
  {:else if $configStore.config}
    <div class="content">
      <GeneralSettings
        config={$configStore.config}
        onUpdate={handleSave}
      />
      <CategoryEditor
        config={$configStore.config}
        onUpdate={handleSave}
      />
      <ServerList
        config={$configStore.config}
        onUpdate={handleSave}
      />
    </div>
  {/if}

  {#if toast}
    <div class="toast {toast.type}">
      {toast.message}
    </div>
  {/if}
</div>

<style>
  .dashboard {
    min-height: 100vh;
    background: #1a1a2e;
  }

  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1.5rem 2rem;
    background: #252535;
    border-bottom: 1px solid #353545;
  }

  .header h1 {
    color: #ffffff;
    font-size: 1.5rem;
  }

  .btn-logout {
    padding: 0.5rem 1.5rem;
    background: #ff4a4a;
    color: white;
    border: none;
    border-radius: 6px;
    cursor: pointer;
  }

  .btn-logout:hover {
    background: #ff3a3a;
  }

  .content {
    padding: 2rem;
  }

  .loading,
  .error {
    padding: 2rem;
    text-align: center;
    color: #e0e0e0;
  }

  .error {
    color: #ff6b6b;
  }

  .error button {
    margin-left: 1rem;
    padding: 0.5rem 1rem;
    background: #353545;
    color: #e0e0e0;
    border: none;
    border-radius: 4px;
    cursor: pointer;
  }

  .toast {
    position: fixed;
    bottom: 2rem;
    right: 2rem;
    padding: 1rem 1.5rem;
    border-radius: 8px;
    color: white;
    animation: slideIn 0.3s ease;
  }

  .toast.success {
    background: #4caf50;
  }

  .toast.error {
    background: #ff4a4a;
  }

  @keyframes slideIn {
    from {
      transform: translateY(100%);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }
</style>
