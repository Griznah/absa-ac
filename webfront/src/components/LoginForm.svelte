<script lang="ts">
  import { authStore } from '../lib/authStore'
  import { api } from '../lib/apiClient'

  let token = $state('')
  let showToken = $state(false)
  let error = $state('')
  let loading = $state(false)

  async function handleSubmit() {
    if (!token.trim()) {
      error = 'Please enter a token'
      return
    }

    loading = true
    error = ''

    try {
      // Validate token via health endpoint
      await api.health()
      authStore.login(token.trim())
      // Redirect happens via authStore subscription in App
    } catch (err) {
      error = err instanceof Error ? err.message : 'Authentication failed'
    } finally {
      loading = false
    }
  }

  function toggleVisibility() {
    showToken = !showToken
  }
</script>

<div class="login-container">
  <div class="login-form">
    <h1>ABSA AC Bot</h1>
    <h2>Configuration Manager</h2>

    <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
      <div class="form-group">
        <label for="token">Bearer Token</label>
        <div class="input-group">
          <input
            id="token"
            type={showToken ? 'text' : 'password'}
            bind:value={token}
            placeholder="Enter your API bearer token"
            aria-invalid={!!error}
            aria-describedby={error ? 'token-error' : undefined}
            disabled={loading}
          />
          <button
            type="button"
            class="toggle-visibility"
            aria-label={showToken ? 'Hide token' : 'Show token'}
            onclick={toggleVisibility}
          >
            {showToken ? '👁️' : '👁️‍🗨️'}
          </button>
        </div>
        {#if error}
          <p id="token-error" class="error" role="alert">
            {error}
          </p>
        {/if}
      </div>

      <button type="submit" class="btn-primary" disabled={loading}>
        {loading ? 'Verifying...' : 'Login'}
      </button>
    </form>
  </div>
</div>

<style>
  .login-container {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
  }

  .login-form {
    background: #252535;
    padding: 2.5rem;
    border-radius: 12px;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
    width: 100%;
    max-width: 400px;
  }

  h1 {
    font-size: 1.75rem;
    color: #ffffff;
    margin-bottom: 0.25rem;
    text-align: center;
  }

  h2 {
    font-size: 1rem;
    color: #888899;
    margin-bottom: 2rem;
    text-align: center;
    font-weight: normal;
  }

  .form-group {
    margin-bottom: 1.5rem;
  }

  label {
    display: block;
    margin-bottom: 0.5rem;
    color: #c0c0c0;
    font-size: 0.875rem;
  }

  .input-group {
    display: flex;
    gap: 0.5rem;
  }

  input {
    flex: 1;
    padding: 0.75rem;
    background: #1a1a25;
    border: 1px solid #353545;
    border-radius: 6px;
    color: #e0e0e0;
    font-size: 0.875rem;
  }

  input:focus {
    outline: none;
    border-color: #4a9eff;
  }

  input:disabled {
    opacity: 0.6;
  }

  input[aria-invalid="true"] {
    border-color: #ff4a4a;
  }

  .toggle-visibility {
    padding: 0 0.75rem;
    background: #353545;
    border: 1px solid #454555;
    border-radius: 6px;
    cursor: pointer;
    font-size: 1.25rem;
  }

  .toggle-visibility:hover {
    background: #404050;
  }

  .error {
    margin-top: 0.5rem;
    color: #ff6b6b;
    font-size: 0.875rem;
  }

  .btn-primary {
    width: 100%;
    padding: 0.75rem;
    background: #4a9eff;
    color: white;
    border: none;
    border-radius: 6px;
    font-size: 1rem;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.2s;
  }

  .btn-primary:hover:not(:disabled) {
    background: #3a8eef;
  }

  .btn-primary:disabled {
    opacity: 0.7;
    cursor: not-allowed;
  }
</style>
