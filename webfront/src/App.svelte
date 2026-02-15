<script lang="ts">
  import { onMount } from 'svelte'
  import { authStore } from './lib/authStore'
  import LoginForm from './components/LoginForm.svelte'
  import Dashboard from './components/Dashboard.svelte'

  let initializing = $state(true)
  let authenticated = $state(false)

  onMount(() => {
    const hasToken = authStore.init()
    authenticated = hasToken
    initializing = false
  })

  // Subscribe to auth state changes
  $effect(() => {
    const unsubscribe = authStore.subscribe((state) => {
      authenticated = state.isAuthenticated
    })
    return unsubscribe
  })
</script>

{#if initializing}
  <div class="loading-screen">
    <p>Loading...</p>
  </div>
{:else if !authenticated}
  <LoginForm />
{:else}
  <Dashboard />
{/if}

<style>
  .loading-screen {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: #1a1a2e;
    color: #e0e0e0;
  }
</style>
