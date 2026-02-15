<script lang="ts">
  import type { Config } from '../types'

  interface Props {
    config: Config
    onUpdate: (updates: Partial<Config>) => void
  }

  let { config, onUpdate }: Props = $props()

  function handleSave(serverIp: string, interval: number) {
    if (serverIp !== config.server_ip || interval !== config.update_interval) {
      onUpdate({
        server_ip: serverIp,
        update_interval: interval,
      })
    }
  }
</script>

<div class="panel">
  <h3>General Settings</h3>

  <div class="form-group">
    <label for="server-ip">Server IP</label>
    <input
      id="server-ip"
      type="text"
      value={config.server_ip}
      placeholder="e.g., acstuff.club"
    />
  </div>

  <div class="form-group">
    <label for="update-interval">Update Interval (seconds)</label>
    <input
      id="update-interval"
      type="number"
      value={config.update_interval}
      min="1"
      max="3600"
    />
  </div>

  <button
    onclick={() => handleSave(config.server_ip, config.update_interval)}
    class="btn-secondary"
  >Save</button>
</div>

<style>
  .panel {
    background: #252535;
    padding: 1.5rem;
    border-radius: 8px;
    margin-bottom: 1.5rem;
  }

  h3 {
    color: #ffffff;
    margin-bottom: 1rem;
    font-size: 1.125rem;
  }

  .form-group {
    margin-bottom: 1rem;
  }

  label {
    display: block;
    margin-bottom: 0.5rem;
    color: #c0c0c0;
    font-size: 0.875rem;
  }

  input {
    width: 100%;
    padding: 0.625rem;
    background: #1a1a25;
    border: 1px solid #353545;
    border-radius: 4px;
    color: #e0e0e0;
    font-size: 0.875rem;
  }

  input:focus {
    outline: none;
    border-color: #4a9eff;
  }

  .btn-secondary {
    padding: 0.5rem 1rem;
    background: #353545;
    color: #e0e0e0;
    border: none;
    border-radius: 4px;
    cursor: pointer;
  }

  .btn-secondary:hover {
    background: #404050;
  }
</style>
